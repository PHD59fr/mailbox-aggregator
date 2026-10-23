package api

import (
	"context"

	"github.com/example/mail-aggregator/internal/imap"
	"github.com/gin-gonic/gin"
	"time"
)

type MailActions interface {
	SetFlag(context.Context, imap.Configuration, uint32, uint32, string, bool) error
	Transfer(context.Context, imap.Configuration, uint32, uint32, string, bool) error
	ManageFolder(context.Context, imap.Configuration, string, string, string) error
}
type CrossAccountTransfer interface {
	TransferBetween(context.Context, imap.Configuration, imap.Configuration, uint32, uint32, bool) error
}

func (h *handlers) messageFlag(c *gin.Context) {
	var input struct {
		Flag    string `json:"flag"`
		Enabled *bool  `json:"enabled"`
	}
	if c.ShouldBindJSON(&input) != nil || input.Enabled == nil || (input.Flag != "seen" && input.Flag != "flagged" && input.Flag != "answered") {
		respondError(c, 400, "INVALID_INPUT", "Choose a supported message flag.")
		return
	}
	uid, validity, ok := messageReference(c)
	if !ok {
		return
	}
	tools, ok := h.deps.Reader.(MailActions)
	if !ok {
		respondError(c, 501, "UNAVAILABLE", "Mailbox actions are unavailable.")
		return
	}
	h.mailboxOperation(c, func(ctx context.Context, cfg imap.Configuration) {
		if !h.readError(c, tools.SetFlag(ctx, cfg, uid, validity, input.Flag, *input.Enabled)) {
			c.Status(204)
		}
	})
}
func (h *handlers) messageTransfer(c *gin.Context) {
	var input struct {
		Destination string `json:"destination"`
		Move        bool   `json:"move"`
		Account     string `json:"destination_account"`
	}
	if c.ShouldBindJSON(&input) != nil || !imap.ValidFolderName(input.Destination) {
		respondError(c, 400, "INVALID_INPUT", "Choose a destination folder in this account.")
		return
	}
	uid, validity, ok := messageReference(c)
	if !ok {
		return
	}
	if input.Account != "" && input.Account != c.Param("account") {
		if len(input.Account) > 128 {
			respondError(c, 400, "INVALID_INPUT", "Invalid destination account.")
			return
		}
		tools, ok := h.deps.Reader.(CrossAccountTransfer)
		if !ok {
			respondError(c, 501, "UNAVAILABLE", "Cross-account transfer is unavailable.")
			return
		}
		accounts, smtp, err := h.deps.Sessions.Credentials(c.GetString(contextToken))
		defer clearCredentialCopies(accounts)
		defer clearCredentialCopies(smtp)
		target, exists := accounts[input.Account]
		if err != nil || !exists || len(target.Secret) == 0 {
			respondError(c, 423, "RECEIVER_UNAVAILABLE", "Unlock the destination account before transferring mail.")
			return
		}
		h.mailboxOperationBudget(c, 2, func(ctx context.Context, source imap.Configuration) {
			ctx, cancel := context.WithDeadline(ctx, target.ExpiresAt)
			defer cancel()
			if time.Now().After(target.ExpiresAt) {
				respondError(c, 423, "RECEIVER_UNAVAILABLE", "Destination credentials expired.")
				return
			}
			destination := imap.Configuration{Host: target.Host, Port: target.Port, Security: target.Security, Username: target.Username, Password: target.Secret, Mailbox: input.Destination}
			if !h.readError(c, tools.TransferBetween(ctx, source, destination, uid, validity, input.Move)) {
				c.Status(204)
			}
		})
		return
	}
	tools, ok := h.deps.Reader.(MailActions)
	if !ok {
		respondError(c, 501, "UNAVAILABLE", "Mailbox actions are unavailable.")
		return
	}
	h.mailboxOperation(c, func(ctx context.Context, cfg imap.Configuration) {
		if !h.readError(c, tools.Transfer(ctx, cfg, uid, validity, input.Destination, input.Move)) {
			c.Status(204)
		}
	})
}
func (h *handlers) manageFolder(c *gin.Context) {
	var input struct {
		Action       string `json:"action"`
		Name         string `json:"name"`
		NewName      string `json:"new_name"`
		Confirmation string `json:"confirmation"`
	}
	if c.ShouldBindJSON(&input) != nil || !imap.ValidFolderName(input.Name) {
		respondError(c, 400, "INVALID_INPUT", "Invalid folder action.")
		return
	}
	switch input.Action {
	case "create", "subscribe", "unsubscribe":
	case "rename":
		if !imap.ValidFolderName(input.NewName) {
			respondError(c, 400, "INVALID_INPUT", "Invalid folder name.")
			return
		}
	case "delete":
		if input.Confirmation != input.Name {
			respondError(c, 400, "CONFIRMATION_REQUIRED", "Type the exact folder name to confirm deletion of the folder and its messages.")
			return
		}
	default:
		respondError(c, 400, "INVALID_INPUT", "Invalid folder action.")
		return
	}
	tools, ok := h.deps.Reader.(MailActions)
	if !ok {
		respondError(c, 501, "UNAVAILABLE", "Mailbox actions are unavailable.")
		return
	}
	h.mailboxOperation(c, func(ctx context.Context, cfg imap.Configuration) {
		if !h.readError(c, tools.ManageFolder(ctx, cfg, input.Action, input.Name, input.NewName)) {
			c.Status(204)
		}
	})
}
