package api

import (
	"context"
	"time"

	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/mail"
	"github.com/example/mail-aggregator/internal/smtp"
	"github.com/gin-gonic/gin"
)

type MailDrafts interface {
	SaveDraft(context.Context, imap.Configuration, []byte, uint32, uint32) (imap.DraftReceipt, error)
	DeleteDraft(context.Context, imap.Configuration, uint32, uint32) error
}

func (h *handlers) saveRemoteDraft(c *gin.Context) {
	var input struct {
		Headers          map[string]string `json:"headers"`
		Sender           mail.Address      `json:"sender"`
		To               []mail.Address    `json:"to" binding:"max=100"`
		CC               []mail.Address    `json:"cc" binding:"max=100"`
		BCC              []mail.Address    `json:"bcc" binding:"max=100"`
		Subject          string            `json:"subject" binding:"max=998"`
		Text             string            `json:"text" binding:"max=1048576"`
		HTML             string            `json:"html" binding:"max=2097152"`
		InReplyTo        string            `json:"in_reply_to" binding:"max=1000"`
		References       []string          `json:"references" binding:"max=32,dive,max=1000"`
		Attachments      []sendAttachment  `json:"attachments" binding:"max=20,dive"`
		Folder           string            `json:"folder"`
		Excluded         []string          `json:"exclude"`
		PreviousUID      uint32            `json:"previous_uid"`
		PreviousValidity uint32            `json:"previous_validity"`
	}
	if c.ShouldBindJSON(&input) != nil || len(input.To)+len(input.CC)+len(input.BCC) > 100 || !validSentCopy(&sentCopyInput{AccountID: c.Param("account"), Folder: input.Folder, Excluded: input.Excluded}) || len(input.Sender.Name) > 128 || len(input.Sender.Email) > 254 {
		respondError(c, 400, "INVALID_INPUT", "Invalid draft or provider folder.")
		return
	}
	tools, ok := h.deps.Reader.(MailDrafts)
	if !ok {
		respondError(c, 501, "UNAVAILABLE", "Provider drafts are unavailable.")
		return
	}
	attachments := []mail.Attachment{}
	size := 0
	for _, item := range input.Attachments {
		size += len(item.Data)
		if size > maxSendAttachmentBytes {
			respondError(c, 413, "ATTACHMENTS_TOO_LARGE", "Attachments must total 10 MB or less.")
			return
		}
		attachments = append(attachments, mail.Attachment{ContentID: item.ContentID, Filename: item.Filename, ContentType: item.ContentType, Data: item.Data})
	}
	message, err := smtp.BuildDraft(mail.Identity{Name: input.Sender.Name, Email: input.Sender.Email}, mail.Draft{Headers: input.Headers, To: input.To, CC: input.CC, BCC: input.BCC, Subject: input.Subject, Text: input.Text, HTML: input.HTML, InReplyTo: input.InReplyTo, References: input.References, Attachments: attachments}, time.Now())
	if err != nil {
		respondError(c, 400, "INVALID_DRAFT", "Draft addresses or MIME content are invalid.")
		return
	}
	defer clear(message.Data)
	h.mailboxOperation(c, func(ctx context.Context, cfg imap.Configuration) {
		cfg.DraftsMailbox = input.Folder
		cfg.DraftsExcluded = input.Excluded
		receipt, err := tools.SaveDraft(ctx, cfg, message.Data, input.PreviousUID, input.PreviousValidity)
		if !h.readError(c, err) {
			c.JSON(200, receipt)
		}
	})
}
func (h *handlers) deleteRemoteDraft(c *gin.Context) {
	uid, validity, ok := messageReference(c)
	if !ok {
		return
	}
	if c.Query("folder_id") == "" {
		respondError(c, 400, "INVALID_INPUT", "Choose the exact draft folder.")
		return
	}
	tools, ok := h.deps.Reader.(MailDrafts)
	if !ok {
		respondError(c, 501, "UNAVAILABLE", "Provider drafts are unavailable.")
		return
	}
	h.mailboxOperation(c, func(ctx context.Context, cfg imap.Configuration) {
		if !h.readError(c, tools.DeleteDraft(ctx, cfg, uid, validity)) {
			c.Status(204)
		}
	})
}
