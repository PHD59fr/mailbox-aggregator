package imap

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	protocol "github.com/emersion/go-imap/v2"
)

var ErrInvalidAction = errors.New("invalid mailbox action")
var ErrFlagUnsupported = errors.New("flag is not permanent on this mailbox")
var ErrActionUnconfirmed = errors.New("remote mailbox action could not be confirmed; refresh before retrying")

func ValidFolderName(name string) bool {
	return name != "" && len(name) <= 1024 && utf8.ValidString(name) && !strings.ContainsAny(name, "\r\n\x00")
}

// SetFlag accepts only the three explicit user-facing system flags. Deleted is
// deliberately reserved for a confirmed MOVE/UIDPLUS transaction.
func (r Receiver) SetFlag(ctx context.Context, cfg Configuration, uid, validity uint32, name string, enabled bool) error {
	flag, ok := map[string]protocol.Flag{"seen": protocol.FlagSeen, "flagged": protocol.FlagFlagged, "answered": protocol.FlagAnswered}[name]
	if !ok {
		return ErrInvalidAction
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client, closeConn, _, err := r.openMessageMode(ctx, cfg, uid, validity, false)
	if err != nil {
		return err
	}
	defer closeConn()
	box := client.Mailbox()
	flags := box.PermanentFlags
	if flags == nil {
		flags = box.Flags
	}
	if !slices.Contains(flags, flag) {
		return ErrFlagUnsupported
	}
	op := protocol.StoreFlagsAdd
	if !enabled {
		op = protocol.StoreFlagsDel
	}
	if err := client.Store(protocol.UIDSetNum(protocol.UID(uid)), &protocol.StoreFlags{Op: op, Silent: true, Flags: []protocol.Flag{flag}}, nil).Close(); err != nil {
		return ErrActionUnconfirmed
	}
	return nil
}

// Transfer operates inside the selected provider account, never through a local
// spool. A rejected or ambiguous COPY can never trigger source deletion.
func (r Receiver) Transfer(ctx context.Context, cfg Configuration, uid, validity uint32, destination string, move bool) error {
	if !ValidFolderName(destination) || destination == cfg.mailbox() || strings.EqualFold(destination, "INBOX") && strings.EqualFold(cfg.mailbox(), "INBOX") {
		return ErrInvalidAction
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client, closeConn, _, err := r.openMessageMode(ctx, cfg, uid, validity, false)
	if err != nil {
		return err
	}
	defer closeConn()
	if move && !client.Caps().Has(protocol.CapMove) && !client.Caps().Has(protocol.CapUIDPlus) {
		return ErrMoveUnsupported
	}
	set := protocol.UIDSetNum(protocol.UID(uid))
	if move && client.Caps().Has(protocol.CapMove) {
		if _, err := client.Move(set, destination).Wait(); err != nil {
			return ErrActionUnconfirmed
		}
		return nil
	}
	if _, err := client.Copy(set, destination).Wait(); err != nil {
		return ErrActionUnconfirmed
	}
	if !move {
		return nil
	}
	if err := client.Store(set, &protocol.StoreFlags{Op: protocol.StoreFlagsAdd, Silent: true, Flags: []protocol.Flag{protocol.FlagDeleted}}, nil).Close(); err != nil {
		return ErrActionUnconfirmed
	}
	if err := client.UIDExpunge(set).Close(); err != nil {
		return ErrActionUnconfirmed
	}
	return nil
}

func (r Receiver) ManageFolder(ctx context.Context, cfg Configuration, action, name, newName string) error {
	if !ValidFolderName(name) {
		return ErrInvalidAction
	}
	switch action {
	case "create", "subscribe", "unsubscribe":
	case "rename":
		if !ValidFolderName(newName) || strings.EqualFold(newName, "INBOX") {
			return ErrInvalidAction
		}
		fallthrough
	case "delete":
		if strings.EqualFold(name, "INBOX") {
			return ErrInvalidAction
		}
	default:
		return ErrInvalidAction
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client, closeConn, err := r.connect(ctx, cfg, nil)
	if err != nil {
		return err
	}
	defer closeConn()
	switch action {
	case "create":
		err = client.Create(name, nil).Wait()
	case "rename":
		err = client.Rename(name, newName, nil).Wait()
	case "delete":
		err = client.Delete(name).Wait()
	case "subscribe":
		err = client.Subscribe(name).Wait()
	case "unsubscribe":
		err = client.Unsubscribe(name).Wait()
	}
	if err != nil {
		return ErrActionUnconfirmed
	}
	return nil
}
