package imap

import (
	"context"
	"errors"
	"slices"
	"time"

	protocol "github.com/emersion/go-imap/v2"
)

var ErrSourceRemovalUnconfirmed = errors.New("copy confirmed but source removal could not be confirmed")

// TransferBetween keeps at most one provider connection open at a time. Source
// bytes exist only in bounded RAM and are erased before returning. A move needs
// UIDPLUS on both ends and a verified destination UID before touching the source.
func (r Receiver) TransferBetween(ctx context.Context, source, target Configuration, uid, validity uint32, move bool) (resultErr error) {
	operation := "copy_between_accounts"
	if move {
		operation = "move_between_accounts"
	}
	defer r.operation(ctx, source, operation).Result(&resultErr)
	if !ValidFolderName(target.mailbox()) || uid == 0 || validity == 0 {
		return ErrInvalidAction
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client, closeSource, row, err := r.openMessageMode(ctx, source, uid, validity, true)
	if err != nil {
		return err
	}
	if move && !client.Caps().Has(protocol.CapUIDPlus) {
		closeSource()
		return ErrMoveUnsupported
	}
	if row.RFC822Size > MaxSourceBytes {
		closeSource()
		return ErrPartTooLarge
	}
	data, err := fetchSection(client, uid, &protocol.FetchItemBodySection{Peek: true, Partial: &protocol.SectionPartial{Size: MaxSourceBytes + 1}}, MaxSourceBytes)
	closeSource()
	if err != nil {
		return err
	}
	defer clear(data)
	destination, closeTarget, err := r.connect(ctx, target, nil)
	if err != nil {
		return err
	}
	defer closeTarget()
	if move && !destination.Caps().Has(protocol.CapUIDPlus) {
		return ErrMoveUnsupported
	}
	selected, err := destination.Select(target.mailbox(), &protocol.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return err
	}
	flags := []protocol.Flag{}
	for _, flag := range row.Flags {
		if flag == protocol.FlagDeleted || flag == protocol.Flag("\\Recent") {
			continue
		}
		if slices.Contains(selected.PermanentFlags, flag) || selected.PermanentFlags == nil && slices.Contains(selected.Flags, flag) {
			flags = append(flags, flag)
		}
	}
	command := destination.Append(target.mailbox(), int64(len(data)), &protocol.AppendOptions{Flags: flags, Time: row.InternalDate})
	if _, err = command.Write(data); err != nil {
		closeTarget()
		return ErrActionUnconfirmed
	}
	if err = command.Close(); err != nil {
		closeTarget()
		return ErrActionUnconfirmed
	}
	receipt, err := command.Wait()
	if err != nil {
		return ErrActionUnconfirmed
	}
	if !move {
		return nil
	}
	if receipt == nil || receipt.UID == 0 || receipt.UIDValidity == 0 || receipt.UIDValidity != selected.UIDValidity {
		return ErrActionUnconfirmed
	}
	confirmation, err := destination.Fetch(protocol.UIDSetNum(receipt.UID), &protocol.FetchOptions{UID: true, RFC822Size: true}).Collect()
	if err != nil || len(confirmation) != 1 || confirmation[0].UID != receipt.UID || confirmation[0].RFC822Size != int64(len(data)) {
		return ErrActionUnconfirmed
	}
	closeTarget()
	client, closeSource, _, err = r.openMessageMode(ctx, source, uid, validity, false)
	if err != nil {
		return ErrSourceRemovalUnconfirmed
	}
	defer closeSource()
	if !client.Caps().Has(protocol.CapUIDPlus) {
		return ErrSourceRemovalUnconfirmed
	}
	set := protocol.UIDSetNum(protocol.UID(uid))
	if err = client.Store(set, &protocol.StoreFlags{Op: protocol.StoreFlagsAdd, Silent: true, Flags: []protocol.Flag{protocol.FlagDeleted}}, nil).Close(); err != nil {
		return ErrSourceRemovalUnconfirmed
	}
	if err = client.UIDExpunge(set).Close(); err != nil {
		return ErrSourceRemovalUnconfirmed
	}
	return nil
}
