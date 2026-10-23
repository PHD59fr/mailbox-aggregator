package imap

import (
	"bytes"
	"context"
	"errors"
	stdmail "net/mail"
	"time"

	protocol "github.com/emersion/go-imap/v2"
)

var ErrSentUnavailable = errors.New("no server Sent folder was found")
var ErrSentUnconfirmed = errors.New("saving the sent copy could not be confirmed")

// SaveSent uses the existing mapped/SPECIAL-USE folder. It never creates one,
// and never retries an APPEND whose result might already have been accepted.
func (r Receiver) SaveSent(ctx context.Context, cfg Configuration, data []byte, messageID string, date time.Time) (resultErr error) {
	defer r.operation(ctx, cfg, "save_sent").Result(&resultErr)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client, closeConn, err := r.connect(ctx, cfg, nil)
	if err != nil {
		return err
	}
	defer closeConn()
	folder, err := serverRoleFolder(client, closeConn, "sent", cfg.SentMailbox, cfg.SentExcluded, ErrSentUnavailable)
	if err != nil {
		return err
	}
	if _, err = client.Select(folder, &protocol.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return err
	}
	// Some providers archive SMTP submissions themselves. Do not append a copy
	// if this exact Message-ID is already visible in Sent.
	if messageID != "" {
		header := "Message-ID"
		message, parseErr := stdmail.ReadMessage(bytes.NewReader(data))
		if parseErr != nil {
			return ErrSentUnconfirmed
		}
		if message.Header.Get("Resent-Message-ID") == messageID {
			header = "Resent-Message-ID"
		}
		found, searchErr := client.UIDSearch(&protocol.SearchCriteria{Header: []protocol.SearchCriteriaHeaderField{{Key: header, Value: messageID}}}, nil).Wait()
		if searchErr != nil {
			return searchErr
		}
		uids, ok := found.All.(protocol.UIDSet)
		if !ok || uids.Dynamic() {
			return ErrSentUnconfirmed
		}
		if len(uids) > 0 {
			return nil
		}
	}
	command := client.Append(folder, int64(len(data)), &protocol.AppendOptions{Flags: []protocol.Flag{protocol.FlagSeen}, Time: date})
	if _, err = command.Write(data); err != nil {
		closeConn()
		return ErrSentUnconfirmed
	}
	if err = command.Close(); err != nil {
		closeConn()
		return ErrSentUnconfirmed
	}
	if _, err = command.Wait(); err != nil {
		return ErrSentUnconfirmed
	}
	return nil
}
