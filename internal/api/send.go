package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/mail-aggregator/internal/identity"
	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/mail"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/example/mail-aggregator/internal/smtp"
	"github.com/gin-gonic/gin"
)

type sendInput struct {
	Headers      map[string]string `json:"headers"`
	SMTPConfigID string            `json:"smtp_config_id" binding:"required,max=128"`
	To           []mail.Address    `json:"to" binding:"required,min=1,max=100"`
	CC           []mail.Address    `json:"cc" binding:"max=100"`
	BCC          []mail.Address    `json:"bcc" binding:"max=100"`
	Subject      string            `json:"subject" binding:"max=998"`
	Text         string            `json:"text" binding:"max=1048576"`
	HTML         string            `json:"html" binding:"max=2097152"`
	Attachments  []sendAttachment  `json:"attachments" binding:"max=20,dive"`
	InReplyTo    string            `json:"in_reply_to" binding:"max=1000"`
	References   []string          `json:"references" binding:"max=32,dive,max=1000"`
	SentCopy     *sentCopyInput    `json:"sent_copy"`
}

type sentCopyInput struct {
	AccountID string   `json:"account_id" binding:"required,max=128"`
	Folder    string   `json:"folder" binding:"max=1024"`
	Excluded  []string `json:"exclude" binding:"max=6,dive,max=1024"`
}

type MailSentWriter interface {
	SaveSent(context.Context, imap.Configuration, []byte, string, time.Time) error
}

func validSentCopy(input *sentCopyInput) bool {
	if input == nil {
		return true
	}
	if input.AccountID == "" || len(input.AccountID) > 128 || len(input.Excluded) > 6 {
		return false
	}
	valid := func(value string) bool {
		return len(value) <= 1024 && utf8.ValidString(value) && !strings.ContainsAny(value, "\r\n\x00") && !strings.EqualFold(value, "INBOX")
	}
	if !valid(input.Folder) {
		return false
	}
	for _, value := range input.Excluded {
		if value == "" || !valid(value) {
			return false
		}
	}
	return true
}

const maxSendRequestBytes int64 = 20 << 20
const maxSendAttachmentBytes = 10 << 20

type sendAttachment struct {
	ContentID   string `json:"content_id" binding:"max=128"`
	Filename    string `json:"filename" binding:"required,max=255"`
	ContentType string `json:"content_type" binding:"max=127"`
	Data        []byte `json:"data"`
}

func (h *handlers) sendMessage(c *gin.Context) {
	var input sendInput
	if err := c.ShouldBindJSON(&input); err != nil || len(input.To)+len(input.CC)+len(input.BCC) > 100 || !validSentCopy(input.SentCopy) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			respondError(c, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "The message exceeds the request size limit.")
			return
		}
		respondError(c, http.StatusBadRequest, "INVALID_INPUT", "Invalid message or recipients.")
		return
	}
	attachments := make([]mail.Attachment, 0, len(input.Attachments))
	attachmentBytes := 0
	for _, attachment := range input.Attachments {
		attachmentBytes += len(attachment.Data)
		if attachmentBytes > maxSendAttachmentBytes {
			respondError(c, http.StatusRequestEntityTooLarge, "ATTACHMENTS_TOO_LARGE", "Attachments must total 10 MB or less.")
			return
		}
		attachments = append(attachments, mail.Attachment{ContentID: attachment.ContentID, Filename: attachment.Filename, ContentType: attachment.ContentType, Data: attachment.Data})
	}
	accounts, configs, err := h.deps.Sessions.Credentials(c.GetString(contextToken))
	defer clearCredentialCopies(accounts)
	defer clearCredentialCopies(configs)
	if err != nil {
		respondError(c, http.StatusLocked, "VAULT_LOCKED", "Activate your sending identity before sending.")
		return
	}
	config, ok := configs[input.SMTPConfigID]
	if !ok || (len(config.Secret) == 0 && len(config.OAuthToken) == 0) {
		respondError(c, http.StatusBadRequest, "SMTP_UNAVAILABLE", "The selected sending identity is unavailable or expired.")
		return
	}
	sender, err := identity.Authorize([]mail.Identity{{ID: input.SMTPConfigID, SMTPConfigID: input.SMTPConfigID, Name: config.SenderName, Email: config.SenderEmail, ReplyTo: config.ReplyTo}}, input.SMTPConfigID, map[string]struct{}{input.SMTPConfigID: {}})
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_IDENTITY", "The SMTP configuration needs a valid sender address.")
		return
	}
	message, err := smtp.BuildMessage(sender, mail.Draft{Headers: input.Headers, IdentityID: sender.ID, To: input.To, CC: input.CC, BCC: input.BCC, Subject: input.Subject, Text: input.Text, HTML: input.HTML, Attachments: attachments, InReplyTo: input.InReplyTo, References: input.References}, time.Now())
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_INPUT", "Invalid message headers or email addresses.")
		return
	}
	defer clear(message.Data)
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	if h.deps.Operations != nil {
		untrack := h.deps.Operations.Track(mustSession(c).UserID, cancel)
		defer untrack()
		if !h.deps.Users.IsActive(mustSession(c).UserID) {
			respondError(c, http.StatusUnauthorized, "SESSION_EXPIRED", "The account is no longer active.")
			return
		}
	}
	err = h.deps.Sender.Send(ctx, smtp.Configuration{Host: config.Host, Port: config.Port, Security: config.Security, Username: config.Username, Password: config.Secret, OAuthToken: config.OAuthToken}, message)
	if err != nil {
		if errors.Is(err, smtp.ErrSubmissionUnknown) {
			respondError(c, http.StatusBadGateway, "SMTP_OUTCOME_UNKNOWN", "Delivery could not be confirmed. Check your SMTP provider before sending again.")
		} else {
			respondError(c, http.StatusBadGateway, "SMTP_SEND_FAILED", "The SMTP server could not send the message. Check its configuration and credentials.")
		}
		return
	}
	response := gin.H{"status": "sent", "message_id": message.MessageID}
	if input.SentCopy != nil {
		response["sent_copy"] = h.saveSentCopy(c, ctx, input.SentCopy, message, input.BCC, accounts)
	}
	c.JSON(http.StatusOK, response)
}

// Once SMTP accepted a message, an IMAP failure must remain a sent success.
// Returning an SMTP error here could cause the user to send it twice.
func (h *handlers) saveSentCopy(c *gin.Context, ctx context.Context, input *sentCopyInput, message smtp.BuiltMessage, bcc []mail.Address, accounts map[string]session.Credential) gin.H {
	failed := func(message string) gin.H { return gin.H{"status": "failed", "message": message} }
	credential, ok := accounts[input.AccountID]
	writer, supported := h.deps.Reader.(MailSentWriter)
	if !ok || (len(credential.Secret) == 0 && len(credential.OAuthToken) == 0) || !supported {
		return failed("The message was sent, but its Sent copy could not be saved. Check the linked mailbox settings.")
	}
	view, err := h.deps.Sessions.Peek(c.GetString(contextToken))
	if err != nil || view.State != session.AuthenticatedUnlocked || !h.deps.Users.IsActive(mustSession(c).UserID) {
		return failed("The message was sent, but its Sent copy was interrupted by a session change.")
	}
	copyCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	id, available := h.deps.watches.start(c.GetString(contextToken), mustSession(c).UserID, 1, cancel)
	if !available {
		return failed("The message was sent, but the mailbox connection limit prevented saving its Sent copy.")
	}
	defer h.deps.watches.remove(id)
	view, err = h.deps.Sessions.Peek(c.GetString(contextToken))
	if err != nil || view.State != session.AuthenticatedUnlocked || copyCtx.Err() != nil || !h.deps.Users.IsActive(mustSession(c).UserID) {
		return failed("The message was sent, but its Sent copy was interrupted by a session change.")
	}
	data := smtp.SentCopy(message, bcc)
	defer clear(data)
	err = writer.SaveSent(copyCtx, imap.Configuration{Host: credential.Host, Port: credential.Port, Security: credential.Security, Username: credential.Username, Password: credential.Secret, OAuthToken: credential.OAuthToken, SASLMechanism: credential.SASLMechanism, SentMailbox: input.Folder, SentExcluded: input.Excluded}, data, message.MessageID, time.Now())
	if errors.Is(err, imap.ErrSentUnavailable) {
		return failed("The message was sent, but no Sent folder was found. Check the linked mailbox’s folder mapping.")
	}
	if err != nil {
		return failed("The message was sent, but saving its Sent copy could not be confirmed. Check Sent before trying to save another copy; do not resend the message.")
	}
	return gin.H{"status": "saved", "account_id": input.AccountID}
}

func clearCredentialCopies(records map[string]session.Credential) {
	for _, record := range records {
		clear(record.Secret)
		clear(record.OAuthToken)
	}
}
