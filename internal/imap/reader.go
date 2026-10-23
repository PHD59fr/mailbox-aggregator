package imap

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/textproto"
	"sort"
	"strconv"
	"strings"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message"
	"github.com/emersion/go-message/charset"
	"github.com/example/mail-aggregator/internal/mail"
	"github.com/example/mail-aggregator/internal/mailhtml"
	"golang.org/x/text/encoding/charmap"
)

func init() {
	// Register windows-1252 (commonly used in legacy emails, e.g. La Poste)
	// Not included by default in github.com/emersion/go-message/charset
	charset.RegisterEncoding("windows-1252", charmap.Windows1252)
}

const PageSize = 50
const MaxTextBytes = 2 << 20
const MaxDownloadBytes = 10 << 20
const MaxSourceBytes = 20 << 20

var (
	ErrMailboxChanged = errors.New("mailbox changed; refresh the message list")
	ErrMessageMissing = errors.New("message no longer exists")
	ErrPartTooLarge   = errors.New("message part exceeds the size limit")
)

type MessageSummary struct {
	UID      uint32         `json:"uid"`
	Subject  string         `json:"subject"`
	From     []mail.Address `json:"from"`
	To       []mail.Address `json:"to"`
	CC       []mail.Address `json:"cc"`
	BCC      []mail.Address `json:"bcc,omitempty"`
	Date     time.Time      `json:"date"`
	Seen     bool           `json:"seen"`
	Flagged  bool           `json:"flagged"`
	Answered bool           `json:"answered"`
	Draft    bool           `json:"draft"`
	Size     int64          `json:"size"`
}
type MessagePage struct {
	Messages    []MessageSummary `json:"messages"`
	UIDValidity uint32           `json:"uid_validity"`
	NextBefore  uint32           `json:"next_before"`
	Total       uint32           `json:"total"`
}
type AttachmentInfo struct {
	ContentID   string `json:"content_id,omitempty"`
	Part        string `json:"part"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        uint32 `json:"size"`
}
type Message struct {
	Headers map[string]string `json:"headers,omitempty"`
	MessageSummary
	UIDValidity uint32           `json:"uid_validity"`
	Text        string           `json:"text"`
	HTML        string           `json:"html"`
	Attachments []AttachmentInfo `json:"attachments"`
	Truncated   bool             `json:"truncated"`
	CSS         string           `json:"css,omitempty"`
	ReplyTo     []mail.Address   `json:"reply_to"`
	MessageID   string           `json:"message_id,omitempty"`
	InReplyTo   string           `json:"in_reply_to,omitempty"`
	References  []string         `json:"references,omitempty"`
}
type Download struct {
	Filename, ContentType string
	Data                  []byte
}

// List fetches a fixed-size sequence window, never SEARCH ALL or message bodies.
// Page anchors are UIDs; an expunged anchor requires a refresh rather than
// silently reinterpreting a sequence number as a different message.
func (r Receiver) List(ctx context.Context, cfg Configuration, before, validity uint32) (result MessagePage, resultErr error) {
	defer r.operation(ctx, cfg, "list").Result(&resultErr)
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client, closeConn, err := r.connect(ctx, cfg, nil)
	if err != nil {
		return MessagePage{}, err
	}
	defer closeConn()
	selected, err := client.Select(cfg.mailbox(), &protocol.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return MessagePage{}, err
	}
	page := MessagePage{Messages: []MessageSummary{}, UIDValidity: selected.UIDValidity, Total: selected.NumMessages}
	if validity != 0 && validity != selected.UIDValidity {
		return page, ErrMailboxChanged
	}
	if len(cfg.UIDs) > 0 {
		if len(cfg.UIDs) > PageSize || validity == 0 {
			return page, ErrInvalidAction
		}
		var ids protocol.UIDSet
		for _, id := range cfg.UIDs {
			if id == 0 {
				return page, ErrInvalidAction
			}
			ids.AddNum(protocol.UID(id))
		}
		criteria, err := searchCriteria(cfg)
		if err != nil {
			return page, err
		}
		criteria.UID = []protocol.UIDSet{ids}
		matches, err := client.UIDSearch(criteria, nil).Wait()
		if err != nil {
			return page, err
		}
		found, err := boundedSearchUIDs(matches)
		if err != nil {
			return page, err
		}
		permitted := make(map[protocol.UID]bool, len(cfg.UIDs))
		for _, id := range cfg.UIDs {
			permitted[protocol.UID(id)] = true
		}
		for _, id := range found {
			if !permitted[id] {
				return page, ErrInvalidAction
			}
		}
		if len(found) == 0 {
			return page, nil
		}
		rows, err := client.Fetch(protocol.UIDSetNum(found...), &protocol.FetchOptions{UID: true, Envelope: true, Flags: true, InternalDate: true, RFC822Size: true}).Collect()
		if err != nil {
			return page, err
		}
		for _, row := range rows {
			if row.UID != 0 {
				page.Messages = append(page.Messages, summary(row))
			}
		}
		sort.Slice(page.Messages, func(i, j int) bool { return page.Messages[i].UID > page.Messages[j].UID })
		return page, nil
	}
	end := selected.NumMessages
	if before != 0 {
		if validity == 0 {
			return page, ErrMailboxChanged
		}
		anchor, err := client.Fetch(protocol.UIDSetNum(protocol.UID(before)), &protocol.FetchOptions{UID: true}).Collect()
		if err != nil {
			return page, err
		}
		if len(anchor) != 1 || uint32(anchor[0].UID) != before {
			return page, ErrMailboxChanged
		}
		end = anchor[0].SeqNum - 1
	}
	if cfg.Search != "" || cfg.Expression != "" || cfg.Seen != "" || cfg.From != "" || cfg.To != "" || cfg.Subject != "" || cfg.Flagged || !cfg.Since.IsZero() || !cfg.Before.IsZero() {
		return filteredPage(client, cfg, page, end)
	}
	if end == 0 {
		return page, nil
	}
	start := uint32(1)
	if end > PageSize {
		start = end - PageSize + 1
	}
	var window protocol.SeqSet
	window.AddRange(start, end)
	rows, err := client.Fetch(window, &protocol.FetchOptions{UID: true, Envelope: true, Flags: true, InternalDate: true, RFC822Size: true}).Collect()
	if err != nil {
		return page, err
	}
	for _, row := range rows {
		if row.UID == 0 || (before != 0 && uint32(row.UID) >= before) {
			continue
		}
		page.Messages = append(page.Messages, summary(row))
	}
	sort.Slice(page.Messages, func(i, j int) bool { return page.Messages[i].UID > page.Messages[j].UID })
	if start > 1 && len(page.Messages) > 0 {
		page.NextBefore = page.Messages[len(page.Messages)-1].UID
	}
	return page, nil
}

func summary(row *imapclient.FetchMessageBuffer) MessageSummary {
	m := MessageSummary{UID: uint32(row.UID), Date: row.InternalDate, Size: row.RFC822Size, From: []mail.Address{}, To: []mail.Address{}, CC: []mail.Address{}}
	if row.Envelope != nil {
		m.Subject = row.Envelope.Subject
		m.From = addresses(row.Envelope.From)
		m.To = addresses(row.Envelope.To)
		m.CC = addresses(row.Envelope.Cc)
		m.BCC = addresses(row.Envelope.Bcc)
		if !row.Envelope.Date.IsZero() {
			m.Date = row.Envelope.Date
		}
	}
	for _, flag := range row.Flags {
		if flag == protocol.FlagDraft {
			m.Draft = true
		}
		if flag == protocol.FlagFlagged {
			m.Flagged = true
		}
		if flag == protocol.FlagAnswered {
			m.Answered = true
		}
		if flag == protocol.FlagSeen {
			m.Seen = true
		}
	}
	return m
}
func addresses(input []protocol.Address) []mail.Address {
	output := []mail.Address{}
	for _, addr := range input {
		if email := addr.Addr(); email != "" {
			output = append(output, mail.Address{Name: addr.Name, Email: email})
		}
	}
	return output
}

type bodyPart struct {
	path       []int
	body       *protocol.BodyStructureSinglePart
	attachment bool
}

func parts(body protocol.BodyStructure) []bodyPart {
	output := []bodyPart{}
	if body == nil {
		return output
	}
	body.Walk(func(path []int, structure protocol.BodyStructure) bool {
		disposition := structure.Disposition()
		attachment := disposition != nil && strings.EqualFold(disposition.Value, "attachment")
		single, ok := structure.(*protocol.BodyStructureSinglePart)
		if !ok {
			return !attachment
		}
		output = append(output, bodyPart{path: append([]int(nil), path...), body: single, attachment: attachment || single.Filename() != "" || (single.MediaType() != "text/plain" && single.MediaType() != "text/html")})
		return false
	})
	return output
}
func partID(path []int) string {
	ids := make([]string, len(path))
	for i, n := range path {
		ids[i] = strconv.Itoa(n)
	}
	return strings.Join(ids, ".")
}

func (r Receiver) Read(ctx context.Context, cfg Configuration, uid, validity uint32) (result Message, resultErr error) {
	defer r.operation(ctx, cfg, "read").Result(&resultErr)
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client, closeConn, row, err := r.openMessage(ctx, cfg, uid, validity)
	if err != nil {
		return Message{}, err
	}
	defer closeConn()
	output := Message{MessageSummary: summary(row), UIDValidity: validity, Attachments: []AttachmentInfo{}}
	if row.Envelope != nil {
		output.ReplyTo = addresses(row.Envelope.ReplyTo)
		output.MessageID = row.Envelope.MessageID
		for _, id := range row.Envelope.InReplyTo {
			if output.InReplyTo != "" {
				output.InReplyTo += " "
			}
			output.InReplyTo += "<" + strings.Trim(id, "<>") + ">"
		}
	}
	{
		fieldsToRead := []string{"References"}
		if output.Draft {
			fieldsToRead = append(fieldsToRead, "Organization", "Importance", "X-Priority")
		}
		raw, err := fetchSection(client, uid, &protocol.FetchItemBodySection{Specifier: protocol.PartSpecifierHeader, HeaderFields: fieldsToRead, Peek: true, Partial: &protocol.SectionPartial{Size: mail.MaxReferencesBytes + 1}}, mail.MaxReferencesBytes)
		if err != nil {
			output.Truncated = true
		} else if fields, err := textproto.NewReader(bufio.NewReader(bytes.NewReader(raw))).ReadMIMEHeader(); err == nil {
			output.References = mail.ParseReferences(strings.Join(fields.Values("References"), " "))
			allowed := map[string]string{}
			for _, name := range []string{"Organization", "Importance", "X-Priority"} {
				if value := fields.Get(name); value != "" {
					if values, e := mail.CustomHeaders(map[string]string{name: value}); e == nil {
						for key, value := range values {
							allowed[key] = value
						}
					}
				}
			}
			output.Headers = allowed
		}
	}

	// Prefer the final alternatives, as MIME multipart/alternative orders them
	// from least to most faithful. Attachments are never downloaded implicitly.
	chosen := map[string]bodyPart{}
	for _, part := range parts(row.BodyStructure) {
		if part.attachment {
			name := part.body.Filename()
			if name == "" {
				name = "attachment-" + partID(part.path)
			}
			output.Attachments = append(output.Attachments, AttachmentInfo{ContentID: strings.Trim(part.body.ID, "<>"), Part: partID(part.path), Filename: name, ContentType: part.body.MediaType(), Size: part.body.Size})
		} else {
			chosen[part.body.MediaType()] = part
		}
	}
	for _, kind := range []string{"text/plain", "text/html"} {
		part, ok := chosen[kind]
		if !ok {
			continue
		}
		if part.body.Size > uint32(wireLimit(MaxTextBytes, part.body.Encoding)) {
			output.Truncated = true
			continue
		}
		data, err := fetchPart(client, uid, part, MaxTextBytes)
		if errors.Is(err, ErrPartTooLarge) {
			output.Truncated = true
			break
		}
		if err != nil {
			return Message{}, err
		}
		if kind == "text/html" {
			output.HTML, output.CSS = mailhtml.Incoming(string(data), true)
		} else {
			output.Text = strings.ToValidUTF8(string(data), "�")
		}
	}
	return output, nil
}

func (r Receiver) Attachment(ctx context.Context, cfg Configuration, uid, validity uint32, id string) (result Download, resultErr error) {
	defer r.operation(ctx, cfg, "attachment").Result(&resultErr)
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client, closeConn, row, err := r.openMessage(ctx, cfg, uid, validity)
	if err != nil {
		return Download{}, err
	}
	defer closeConn()
	for _, part := range parts(row.BodyStructure) {
		if !part.attachment || partID(part.path) != id {
			continue
		}
		data, err := fetchPart(client, uid, part, MaxDownloadBytes)
		name := part.body.Filename()
		if name == "" {
			name = "attachment-" + id
		}
		return Download{Filename: name, ContentType: part.body.MediaType(), Data: data}, err
	}
	return Download{}, ErrMessageMissing
}

func (r Receiver) openMessage(ctx context.Context, cfg Configuration, uid, validity uint32) (*imapclient.Client, func(), *imapclient.FetchMessageBuffer, error) {
	return r.openMessageMode(ctx, cfg, uid, validity, true)
}
func (r Receiver) openMessageMode(ctx context.Context, cfg Configuration, uid, validity uint32, readOnly bool) (*imapclient.Client, func(), *imapclient.FetchMessageBuffer, error) {
	client, closeConn, err := r.connect(ctx, cfg, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	selected, err := client.Select(cfg.mailbox(), &protocol.SelectOptions{ReadOnly: readOnly}).Wait()
	if err == nil && selected.UIDValidity != validity {
		err = ErrMailboxChanged
	}
	if err != nil {
		closeConn()
		return nil, nil, nil, err
	}
	rows, err := client.Fetch(protocol.UIDSetNum(protocol.UID(uid)), &protocol.FetchOptions{UID: true, Envelope: true, Flags: true, InternalDate: true, RFC822Size: true, BodyStructure: &protocol.FetchItemBodyStructure{Extended: true}}).Collect()
	if err == nil && (len(rows) != 1 || uint32(rows[0].UID) != uid) {
		err = ErrMessageMissing
	}
	if err != nil {
		closeConn()
		return nil, nil, nil, err
	}
	return client, closeConn, rows[0], nil
}

func wireLimit(decoded int, encoding string) int {
	switch strings.ToLower(encoding) {
	case "base64":
		return ((decoded+2)/3)*4 + (decoded/57+1)*2 + 4
	case "quoted-printable":
		return decoded*3 + 1024
	default:
		return decoded
	}
}

func fetchPart(client *imapclient.Client, uid uint32, part bodyPart, limit int) ([]byte, error) {
	// Ask for a bounded wire range AND limit the literal reader, including when
	// a provider ignores the requested partial range or lies about part size.
	wireMax := wireLimit(limit, part.body.Encoding)
	if part.body.Size > uint32(wireMax) {
		return nil, ErrPartTooLarge
	}
	section := &protocol.FetchItemBodySection{Part: part.path, Peek: true, Partial: &protocol.SectionPartial{Size: int64(wireMax) + 1}}
	raw, err := fetchSection(client, uid, section, wireMax)
	if err != nil {
		return nil, err
	}
	contentType := mime.FormatMediaType(part.body.MediaType(), part.body.Params)
	if part.attachment {
		contentType = "application/octet-stream"
	}
	var header message.Header
	header.Set("Content-Type", contentType)
	header.Set("Content-Transfer-Encoding", part.body.Encoding)
	entity, err := message.New(header, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode message part: %w", err)
	}
	decoded, err := io.ReadAll(io.LimitReader(entity.Body, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(decoded) > limit {
		return nil, ErrPartTooLarge
	}
	return decoded, nil
}

func fetchSection(client *imapclient.Client, uid uint32, section *protocol.FetchItemBodySection, wireMax int) ([]byte, error) {
	cmd := client.Fetch(protocol.UIDSetNum(protocol.UID(uid)), &protocol.FetchOptions{BodySection: []*protocol.FetchItemBodySection{section}})
	var raw []byte
	for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
		for item := msg.Next(); item != nil; item = msg.Next() {
			if body, ok := item.(imapclient.FetchItemDataBodySection); ok && body.Literal != nil && body.MatchCommand(section) {
				var err error
				raw, err = io.ReadAll(io.LimitReader(body.Literal, int64(wireMax)+1))
				if err != nil {
					_ = client.Close()
					return nil, err
				}
				if len(raw) > wireMax {
					_ = client.Close()
					return nil, ErrPartTooLarge
				}
			}
		}
	}
	if err := cmd.Close(); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, ErrMessageMissing
	}
	return raw, nil
}

func (r Receiver) Source(ctx context.Context, cfg Configuration, uid, validity uint32) (result []byte, resultErr error) {
	defer r.operation(ctx, cfg, "source").Result(&resultErr)
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client, closeConn, row, err := r.openMessage(ctx, cfg, uid, validity)
	if err != nil {
		return nil, err
	}
	defer closeConn()
	if row.RFC822Size > MaxSourceBytes {
		return nil, ErrPartTooLarge
	}
	return fetchSection(client, uid, &protocol.FetchItemBodySection{Peek: true, Partial: &protocol.SectionPartial{Size: MaxSourceBytes + 1}}, MaxSourceBytes)
}
func (r Receiver) MarkSeen(ctx context.Context, cfg Configuration, uid, validity uint32) error {
	return r.setSeen(ctx, cfg, uid, validity, true)
}
func (r Receiver) MarkUnseen(ctx context.Context, cfg Configuration, uid, validity uint32) error {
	return r.setSeen(ctx, cfg, uid, validity, false)
}
func (r Receiver) setSeen(ctx context.Context, cfg Configuration, uid, validity uint32, seen bool) (resultErr error) {
	operation := "mark_unseen"
	if seen {
		operation = "mark_seen"
	}
	defer r.operation(ctx, cfg, operation).Result(&resultErr)
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	client, closeConn, _, err := r.openMessageMode(ctx, cfg, uid, validity, false)
	if err != nil {
		return err
	}
	defer closeConn()
	op := protocol.StoreFlagsAdd
	if !seen {
		op = protocol.StoreFlagsDel
	}
	return client.Store(protocol.UIDSetNum(protocol.UID(uid)), &protocol.StoreFlags{Op: op, Silent: true, Flags: []protocol.Flag{protocol.FlagSeen}}, nil).Close()
}
