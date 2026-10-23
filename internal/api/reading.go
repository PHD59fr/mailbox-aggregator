package api

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/mailhtml"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/gin-gonic/gin"
)

type MailReader interface {
	List(context.Context, imap.Configuration, uint32, uint32) (imap.MessagePage, error)
	Read(context.Context, imap.Configuration, uint32, uint32) (imap.Message, error)
	Attachment(context.Context, imap.Configuration, uint32, uint32, string) (imap.Download, error)
}
type MailMessageTools interface {
	Source(context.Context, imap.Configuration, uint32, uint32) ([]byte, error)
	MarkSeen(context.Context, imap.Configuration, uint32, uint32) error
	MarkUnseen(context.Context, imap.Configuration, uint32, uint32) error
}

type MailFolderReader interface {
	Folders(context.Context, imap.Configuration) (imap.FolderPage, error)
}

type MailTrash interface {
	Trash(context.Context, imap.Configuration, uint32, uint32) error
}

func (h *handlers) trashMessage(c *gin.Context) {
	target := c.Query("trash_folder")
	excluded := c.QueryArray("trash_exclude")
	valid := func(name string) bool {
		return len(name) <= 1024 && utf8.ValidString(name) && !strings.ContainsAny(name, "\r\n\x00") && !strings.EqualFold(name, "INBOX")
	}
	if !valid(target) || len(excluded) > 6 {
		respondError(c, 400, "INVALID_INPUT", "Invalid Trash folder reference.")
		return
	}
	for _, name := range excluded {
		if name == "" || !valid(name) {
			respondError(c, 400, "INVALID_INPUT", "Invalid Trash folder exclusion.")
			return
		}
	}
	uid, validity, ok := messageReference(c)
	if !ok {
		return
	}
	tools, ok := h.deps.Reader.(MailTrash)
	if !ok {
		respondError(c, 501, "UNAVAILABLE", "Moving messages to Trash is unavailable.")
		return
	}
	h.mailboxOperation(c, func(ctx context.Context, cfg imap.Configuration) {
		cfg.TrashMailbox = target
		cfg.TrashExcluded = excluded
		if !h.readError(c, tools.Trash(ctx, cfg, uid, validity)) {
			c.Status(204)
		}
	})
}

func (h *handlers) listFolders(c *gin.Context) {
	reader, ok := h.deps.Reader.(MailFolderReader)
	if !ok {
		respondError(c, 501, "UNAVAILABLE", "Folder synchronization is unavailable.")
		return
	}
	h.mailboxOperation(c, func(ctx context.Context, cfg imap.Configuration) {
		folders, err := reader.Folders(ctx, cfg)
		if !h.readError(c, err) {
			c.JSON(200, folders)
		}
	})
}

func queryUint(c *gin.Context, name string, required bool) (uint32, bool) {
	value := c.Query(name)
	if value == "" && !required {
		return 0, true
	}
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil || n == 0 {
		respondError(c, 400, "INVALID_INPUT", "Invalid message reference.")
		return 0, false
	}
	return uint32(n), true
}
func messageReference(c *gin.Context) (uint32, uint32, bool) {
	uid, err := strconv.ParseUint(c.Param("uid"), 10, 32)
	if err != nil || uid == 0 {
		respondError(c, 400, "INVALID_INPUT", "Invalid message reference.")
		return 0, 0, false
	}
	validity, ok := queryUint(c, "uid_validity", true)
	return uint32(uid), validity, ok
}

// Every operation uses only this session's receiving credentials, shares the
// upstream connection budget and is cancelled by lock/logout/account deletion.
func (h *handlers) mailboxOperation(c *gin.Context, run func(context.Context, imap.Configuration)) {
	h.mailboxOperationBudget(c, 1, run)
}
func (h *handlers) mailboxOperationBudget(c *gin.Context, slots int, run func(context.Context, imap.Configuration)) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	folder := c.Query("folder")
	if len(folder) > 1024 || !utf8.ValidString(folder) || strings.ContainsAny(folder, "\r\n\x00") {
		respondError(c, 400, "INVALID_INPUT", "Invalid folder reference.")
		return
	}
	accounts, smtp, err := h.deps.Sessions.Credentials(c.GetString(contextToken))
	defer clearCredentialCopies(accounts)
	defer clearCredentialCopies(smtp)
	credential, ok := accounts[c.Param("account")]
	if err != nil || !ok || len(credential.Secret) == 0 {
		respondError(c, http.StatusLocked, "RECEIVER_UNAVAILABLE", "Unlock this receiving identity before reading mail.")
		return
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), credential.ExpiresAt)
	defer cancel()
	id, ok := h.deps.watches.start(c.GetString(contextToken), mustSession(c).UserID, slots, cancel)
	if !ok {
		respondError(c, 429, "WATCH_LIMIT", "Too many active mailbox connections. Try again shortly.")
		return
	}
	defer h.deps.watches.remove(id)
	if h.deps.Operations != nil {
		defer h.deps.Operations.Track(mustSession(c).UserID, cancel)()
	}
	view, err := h.deps.Sessions.Peek(c.GetString(contextToken))
	if err != nil || view.State != session.AuthenticatedUnlocked || !h.deps.Users.IsActive(mustSession(c).UserID) {
		respondError(c, 423, "VAULT_LOCKED", "The vault is locked or the session has ended.")
		return
	}
	run(ctx, imap.Configuration{Host: credential.Host, Mailbox: folder, Port: credential.Port, Security: credential.Security, Username: credential.Username, Password: credential.Secret})
}

func (h *handlers) readError(c *gin.Context, err error) bool {
	view, sessionErr := h.deps.Sessions.Peek(c.GetString(contextToken))
	if sessionErr != nil || view.State != session.AuthenticatedUnlocked {
		respondError(c, 423, "VAULT_LOCKED", "The vault is locked or the session has ended.")
		return true
	}
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, imap.ErrDraftsUnavailable):
		respondError(c, 422, "DRAFTS_UNAVAILABLE", "The provider Drafts folder is unavailable. Choose an existing Drafts folder in Settings.")
	case errors.Is(err, imap.ErrNotDraft):
		respondError(c, 409, "NOT_A_DRAFT", "This message is no longer marked as a draft. Refresh before continuing.")
	case errors.Is(err, imap.ErrSourceRemovalUnconfirmed):
		respondError(c, 502, "IMAP_SOURCE_REMOVAL_UNCONFIRMED", "The destination copy was confirmed, but source removal could not be confirmed. Check both folders before retrying.")
	case errors.Is(err, imap.ErrInvalidAction):
		respondError(c, 400, "INVALID_MAILBOX_ACTION", "This mailbox action is not permitted.")
	case errors.Is(err, imap.ErrFlagUnsupported):
		respondError(c, 422, "FLAG_UNSUPPORTED", "This mailbox does not support saving this flag.")
	case errors.Is(err, imap.ErrActionUnconfirmed):
		respondError(c, 502, "IMAP_ACTION_UNCONFIRMED", "The remote action could not be confirmed. Refresh the mailbox and check the destination before retrying.")
	case errors.Is(err, imap.ErrTrashUnavailable):
		respondError(c, 422, "TRASH_UNAVAILABLE", "The Trash folder could not be found. Check the folder mapping in this mailbox’s IMAP settings.")
	case errors.Is(err, imap.ErrMoveUnsupported):
		respondError(c, 422, "MOVE_UNSUPPORTED", "This server does not support moving this message safely. No message was deleted.")
	case errors.Is(err, imap.ErrAlreadyInTrash):
		respondError(c, 409, "ALREADY_IN_TRASH", "This message is already in Trash. Permanent deletion is not available here.")
	case errors.Is(err, imap.ErrTrashUnconfirmed):
		respondError(c, 502, "IMAP_TRASH_UNCONFIRMED", "Moving this message could not be confirmed. Refresh the mailbox before trying again.")
	case errors.Is(err, imap.ErrSearchTooBroad):
		respondError(c, 422, "SEARCH_TOO_BROAD", "This search covers too many messages. Refine it or select a smaller folder.")
	case errors.Is(err, imap.ErrAuthentication):
		respondError(c, 422, "IMAP_AUTH_FAILED", "IMAP sign-in failed. Update the password in Settings.")
	case errors.Is(err, imap.ErrMailboxChanged):
		respondError(c, 409, "MAILBOX_CHANGED", "This mailbox changed. Refresh the message list.")
	case errors.Is(err, imap.ErrMessageMissing):
		respondError(c, 404, "MESSAGE_NOT_FOUND", "This message or attachment no longer exists.")
	case errors.Is(err, imap.ErrPartTooLarge):
		respondError(c, 413, "PART_TOO_LARGE", "This content exceeds the safe download limit (10 MB attachment, 20 MB message source).")
	default:
		respondError(c, 502, "IMAP_READ_FAILED", "Could not read this mailbox. Check its connection and try again.")
	}
	return true
}

func (h *handlers) listMessages(c *gin.Context) {
	search, sender, seen := strings.TrimSpace(c.Query("search")), strings.TrimSpace(c.Query("from")), c.Query("seen")
	recipient, subject := strings.TrimSpace(c.Query("to")), strings.TrimSpace(c.Query("subject"))
	text := search + sender + recipient + subject
	if len(search) > 512 || len(sender) > 254 || len(recipient) > 254 || len(subject) > 512 || !utf8.ValidString(text) || strings.ContainsAny(text, "\r\n\x00") || (seen != "" && seen != "read" && seen != "unread") {
		respondError(c, 400, "INVALID_INPUT", "Invalid search or filter.")
		return
	}
	date := func(name string) (time.Time, bool) {
		value := c.Query(name)
		if value == "" {
			return time.Time{}, true
		}
		parsed, err := time.Parse("2006-01-02", value)
		return parsed, err == nil && len(value) == 10 && parsed.Year() > 0
	}
	since, sinceOK := date("since")
	until, untilOK := date("until")
	if !sinceOK || !untilOK || (!since.IsZero() && !until.IsZero() && until.Before(since)) || (!until.IsZero() && until.AddDate(0, 0, 1).Year() > 9999) {
		respondError(c, 400, "INVALID_INPUT", "Choose a valid received date range.")
		return
	}
	before, ok := queryUint(c, "before", false)
	if !ok {
		return
	}
	validity, ok := queryUint(c, "uid_validity", before != 0)
	if !ok {
		return
	}
	h.mailboxOperation(c, func(ctx context.Context, cfg imap.Configuration) {
		cfg.Search = search
		cfg.From = sender
		cfg.Seen = seen
		cfg.To = recipient
		cfg.Subject = subject
		cfg.Since = since
		if !until.IsZero() {
			cfg.Before = until.AddDate(0, 0, 1)
		}
		page, err := h.deps.Reader.List(ctx, cfg, before, validity)
		if !h.readError(c, err) {
			c.JSON(200, page)
		}
	})
}
func (h *handlers) readMessage(c *gin.Context) {
	uid, validity, ok := messageReference(c)
	if !ok {
		return
	}
	h.mailboxOperation(c, func(ctx context.Context, cfg imap.Configuration) {
		message, err := h.deps.Reader.Read(ctx, cfg, uid, validity)
		if !h.readError(c, err) {
			c.JSON(200, message)
		}
	})
}

func (h *handlers) messageDocument(c *gin.Context) {
	uid, validity, ok := messageReference(c)
	if !ok {
		return
	}
	external := c.Query("external") == "1"
	h.mailboxOperation(c, func(ctx context.Context, cfg imap.Configuration) {
		message, err := h.deps.Reader.Read(ctx, cfg, uid, validity)
		if h.readError(c, err) {
			return
		}
		images := "'none'"
		if external {
			images = "https: http:"
		}
		c.Header("Content-Security-Policy", "default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src "+images+"; connect-src 'none'; font-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'; sandbox allow-popups allow-popups-to-escape-sandbox")
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Data(200, "text/html; charset=utf-8", []byte(mailhtml.Document(message.HTML, message.CSS, external)))
	})
}
func (h *handlers) messageSource(c *gin.Context) {
	uid, validity, ok := messageReference(c)
	if !ok {
		return
	}
	tools, ok := h.deps.Reader.(MailMessageTools)
	if !ok {
		respondError(c, 501, "UNAVAILABLE", "Message source is unavailable.")
		return
	}
	h.mailboxOperation(c, func(ctx context.Context, cfg imap.Configuration) {
		data, err := tools.Source(ctx, cfg, uid, validity)
		defer clear(data)
		if h.readError(c, err) {
			return
		}
		c.Header("Content-Disposition", `attachment; filename="message.eml"`)
		c.Data(200, "application/octet-stream", data)
	})
}
func (h *handlers) markMessageSeen(c *gin.Context) {
	uid, validity, ok := messageReference(c)
	if !ok {
		return
	}
	tools, ok := h.deps.Reader.(MailMessageTools)
	if !ok {
		respondError(c, 501, "UNAVAILABLE", "Message flag changes are unavailable.")
		return
	}
	h.mailboxOperation(c, func(ctx context.Context, cfg imap.Configuration) {
		var err error
		if strings.HasSuffix(c.FullPath(), "/unseen") {
			err = tools.MarkUnseen(ctx, cfg, uid, validity)
		} else {
			err = tools.MarkSeen(ctx, cfg, uid, validity)
		}
		if !h.readError(c, err) {
			c.Status(204)
		}
	})
}
func (h *handlers) downloadAttachment(c *gin.Context) {
	uid, validity, ok := messageReference(c)
	if !ok {
		return
	}
	part := c.Param("part")
	if len(part) > 100 || strings.Trim(part, "0123456789.") != "" || part == "" {
		respondError(c, 400, "INVALID_INPUT", "Invalid attachment reference.")
		return
	}
	h.mailboxOperation(c, func(ctx context.Context, cfg imap.Configuration) {
		file, err := h.deps.Reader.Attachment(ctx, cfg, uid, validity, part)
		defer clear(file.Data)
		if h.readError(c, err) {
			return
		}
		name := filepath.Base(strings.ReplaceAll(file.Filename, "\\", "/"))
		name = strings.Map(func(r rune) rune {
			if r < 32 || r == 127 {
				return -1
			}
			return r
		}, name)
		if name == "" || name == "." {
			name = "attachment"
		}
		c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		c.Data(200, "application/octet-stream", file.Data)
	})
}
