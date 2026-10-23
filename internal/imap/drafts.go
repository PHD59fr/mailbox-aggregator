package imap

import (
	"context"
	"errors"
	"slices"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

var ErrDraftsUnavailable = errors.New("no provider Drafts folder was found")
var ErrNotDraft = errors.New("the referenced message is not a draft")

type DraftReceipt struct {
	Folder          string `json:"folder"`
	UID             uint32 `json:"uid"`
	UIDValidity     uint32 `json:"uid_validity"`
	PreviousRemoval string `json:"previous_removal,omitempty"`
}

func (r Receiver) SaveDraft(ctx context.Context, cfg Configuration, data []byte, previous, validity uint32) (DraftReceipt, error) {
	var receipt DraftReceipt
	if len(data) > MaxSourceBytes {
		return receipt, ErrPartTooLarge
	}
	if previous != 0 && validity == 0 {
		return receipt, ErrMailboxChanged
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client, closeConn, err := r.connect(ctx, cfg, nil)
	if err != nil {
		return receipt, err
	}
	defer closeConn()
	folder, err := serverRoleFolder(client, closeConn, "drafts", cfg.DraftsMailbox, cfg.DraftsExcluded, ErrDraftsUnavailable)
	if err != nil {
		return receipt, err
	}
	selected, err := client.Select(folder, &protocol.SelectOptions{ReadOnly: previous == 0}).Wait()
	if err != nil {
		return receipt, err
	}
	if previous != 0 {
		if !client.Caps().Has(protocol.CapUIDPlus) {
			return receipt, ErrMoveUnsupported
		}
		if selected.UIDValidity != validity {
			return receipt, ErrMailboxChanged
		}
		if err := requireDraft(client, previous); err != nil {
			return receipt, err
		}
	}
	command := client.Append(folder, int64(len(data)), &protocol.AppendOptions{Flags: []protocol.Flag{protocol.FlagDraft}, Time: time.Now()})
	if _, err := command.Write(data); err != nil {
		closeConn()
		return receipt, ErrActionUnconfirmed
	}
	if err := command.Close(); err != nil {
		closeConn()
		return receipt, ErrActionUnconfirmed
	}
	result, err := command.Wait()
	if err != nil {
		return receipt, ErrActionUnconfirmed
	}
	receipt.Folder = folder
	if result != nil {
		receipt.UID = uint32(result.UID)
		receipt.UIDValidity = result.UIDValidity
	}
	if previous == 0 {
		return receipt, nil
	}
	if receipt.UID == 0 || receipt.UIDValidity != selected.UIDValidity {
		return receipt, ErrActionUnconfirmed
	}
	if err := requireDraft(client, receipt.UID); err != nil {
		return receipt, ErrActionUnconfirmed
	}
	receipt.PreviousRemoval = "confirmed"
	if err := removeDraft(client, previous); err != nil {
		receipt.PreviousRemoval = "unconfirmed"
	}
	return receipt, nil
}
func requireDraft(client *imapclient.Client, uid uint32) error {
	rows, err := client.Fetch(protocol.UIDSetNum(protocol.UID(uid)), &protocol.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil {
		return err
	}
	if len(rows) != 1 || uint32(rows[0].UID) != uid {
		return ErrMessageMissing
	}
	if !slices.Contains(rows[0].Flags, protocol.FlagDraft) {
		return ErrNotDraft
	}
	return nil
}
func removeDraft(client *imapclient.Client, uid uint32) error {
	set := protocol.UIDSetNum(protocol.UID(uid))
	if err := client.Store(set, &protocol.StoreFlags{Op: protocol.StoreFlagsAdd, Silent: true, Flags: []protocol.Flag{protocol.FlagDeleted}}, nil).Close(); err != nil {
		return ErrActionUnconfirmed
	}
	if err := client.UIDExpunge(set).Close(); err != nil {
		return ErrActionUnconfirmed
	}
	return nil
}
func (r Receiver) DeleteDraft(ctx context.Context, cfg Configuration, uid, validity uint32) error {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client, closeConn, _, err := r.openMessageMode(ctx, cfg, uid, validity, false)
	if err != nil {
		return err
	}
	defer closeConn()
	if !client.Caps().Has(protocol.CapUIDPlus) {
		return ErrMoveUnsupported
	}
	if err := requireDraft(client, uid); err != nil {
		return err
	}
	return removeDraft(client, uid)
}
