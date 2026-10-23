package imap

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

var (
	ErrTrashUnavailable = errors.New("no server trash folder was found")
	ErrMoveUnsupported  = errors.New("the server cannot safely move this message")
	ErrAlreadyInTrash   = errors.New("this message is already in trash")
	ErrTrashUnconfirmed = errors.New("the move to trash could not be confirmed")
)

// Trash never performs an unrestricted EXPUNGE, creates folders or permanently
// deletes a message. MOVE is preferred; fallback requires targeted UID EXPUNGE.
func (r Receiver) Trash(ctx context.Context, cfg Configuration, uid, validity uint32) (resultErr error) {
	defer r.operation(ctx, cfg, "trash").Result(&resultErr)
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client, closeConn, _, err := r.openMessageMode(ctx, cfg, uid, validity, false)
	if err != nil {
		return err
	}
	defer closeConn()
	if !client.Caps().Has(protocol.CapMove) && !client.Caps().Has(protocol.CapUIDPlus) {
		return ErrMoveUnsupported
	}
	trash, err := serverTrash(client, closeConn, cfg.TrashMailbox, cfg.TrashExcluded)
	if err != nil {
		return err
	}
	if strings.EqualFold(trash, cfg.mailbox()) {
		return ErrAlreadyInTrash
	}
	set := protocol.UIDSetNum(protocol.UID(uid))
	if client.Caps().Has(protocol.CapMove) {
		if _, err = client.Move(set, trash).Wait(); err != nil {
			return ErrTrashUnconfirmed
		}
		return nil
	}
	// Do not use the library's MOVE fallback: it pipelines deletion before
	// checking COPY success and can issue unrestricted EXPUNGE without UIDPLUS.
	if _, err = client.Copy(set, trash).Wait(); err != nil {
		return ErrTrashUnconfirmed
	}
	if err = client.Store(set, &protocol.StoreFlags{Op: protocol.StoreFlagsAdd, Silent: true, Flags: []protocol.Flag{protocol.FlagDeleted}}, nil).Close(); err != nil {
		return ErrTrashUnconfirmed
	}
	if err = client.UIDExpunge(set).Close(); err != nil {
		return ErrTrashUnconfirmed
	}
	return nil
}

func serverTrash(client *imapclient.Client, closeConn func(), preferred string, excluded []string) (string, error) {
	return serverRoleFolder(client, closeConn, "trash", preferred, excluded, ErrTrashUnavailable)
}

func serverRoleFolder(client *imapclient.Client, closeConn func(), wanted, preferred string, excluded []string, unavailable error) (string, error) {
	command := client.List("", "*", folderListOptions(client))
	trash := ""
	for count := 0; ; count++ {
		data := command.Next()
		if data == nil {
			break
		}
		if count == MaxFolders {
			drained := make(chan struct{})
			go func() { _ = command.Close(); close(drained) }()
			closeConn()
			<-drained
			return "", unavailable
		}
		selectable := true
		for _, attr := range data.Attrs {
			if attr == protocol.MailboxAttrNoSelect || attr == protocol.MailboxAttrNonExistent {
				selectable = false
			}
		}
		role, special := folderRole(data)
		if preferred != "" {
			if selectable && data.Mailbox == preferred && !strings.EqualFold(preferred, "INBOX") {
				trash = data.Mailbox
			}
		} else if selectable && role == wanted && !slices.Contains(excluded, data.Mailbox) && (trash == "" || special) {
			trash = data.Mailbox
		}
	}
	if err := command.Close(); err != nil {
		return "", err
	}
	if trash == "" {
		return "", unavailable
	}
	return trash, nil
}
