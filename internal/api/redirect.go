package api

import (
	"context"
	"errors"
	"time"

	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/mail"
	"github.com/example/mail-aggregator/internal/smtp"
	"github.com/gin-gonic/gin"
)

func (h *handlers) redirectMessage(c *gin.Context) {
	uid, validity, ok := messageReference(c)
	if !ok {
		return
	}
	var input struct {
		SMTPConfigID string         `json:"smtp_config_id" binding:"required,max=128"`
		To           []mail.Address `json:"to" binding:"required,min=1,max=100"`
		CC           []mail.Address `json:"cc" binding:"max=100"`
		BCC          []mail.Address `json:"bcc" binding:"max=100"`
		SentCopy     *sentCopyInput `json:"sent_copy"`
	}
	if c.ShouldBindJSON(&input) != nil || len(input.To)+len(input.CC)+len(input.BCC) > 100 || !validSentCopy(input.SentCopy) {
		respondError(c, 400, "INVALID_INPUT", "Invalid redirect recipients.")
		return
	}
	tools, ok := h.deps.Reader.(MailMessageTools)
	if !ok {
		respondError(c, 501, "UNAVAILABLE", "Redirecting is unavailable.")
		return
	}
	accounts, senders, err := h.deps.Sessions.Credentials(c.GetString(contextToken))
	defer clearCredentialCopies(accounts)
	defer clearCredentialCopies(senders)
	config, ok := senders[input.SMTPConfigID]
	if err != nil || !ok || config.SenderEmail == "" || (len(config.Secret) == 0 && len(config.OAuthToken) == 0) {
		respondError(c, 423, "SMTP_UNAVAILABLE", "Activate your sending identity before redirecting.")
		return
	}
	h.mailboxOperation(c, func(ctx context.Context, cfg imap.Configuration) {
		source, err := tools.Source(ctx, cfg, uid, validity)
		defer clear(source)
		if h.readError(c, err) {
			return
		}
		message, err := smtp.BuildRedirect(mail.Identity{Email: config.SenderEmail, Name: config.SenderName}, source, input.To, input.CC, input.BCC, time.Now())
		defer clear(message.Data)
		if err != nil {
			respondError(c, 400, "INVALID_INPUT", "Could not prepare this original message for redirecting.")
			return
		}
		if ctx.Err() != nil {
			h.readError(c, ctx.Err())
			return
		}
		err = h.deps.Sender.Send(ctx, smtp.Configuration{Host: config.Host, Port: config.Port, Security: config.Security, Username: config.Username, Password: config.Secret, OAuthToken: config.OAuthToken}, message)
		if errors.Is(err, smtp.ErrSubmissionUnknown) {
			respondError(c, 502, "SMTP_OUTCOME_UNKNOWN", "Delivery could not be confirmed. Check your provider before redirecting again.")
			return
		}
		if err != nil {
			respondError(c, 502, "SMTP_SEND_FAILED", "The SMTP provider could not redirect this message. It may reject the preserved original From address.")
			return
		}
		response := gin.H{"status": "sent", "message_id": message.MessageID}
		if input.SentCopy != nil {
			response["sent_copy"] = h.saveSentCopy(c, ctx, input.SentCopy, message, input.BCC, accounts)
		}
		c.JSON(200, response)
	})
}
