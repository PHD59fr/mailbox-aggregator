package smtp

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	messagetextproto "github.com/emersion/go-message/textproto"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	stdmail "net/mail"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	mailmodel "github.com/example/mail-aggregator/internal/mail"
)

const maxHeaderLineLen = 998

const maxAttachmentBytes = 25 << 20

type BuiltMessage struct {
	Data         []byte
	EnvelopeFrom string
	Recipients   []string
	MessageID    string
	Redirected   bool
}

// Bcc belongs only in the sender's private archive, never in the SMTP payload.
func SentCopy(message BuiltMessage, bcc []mailmodel.Address) []byte {
	addresses, err := formatAddresses(bcc)
	if err != nil || len(addresses) == 0 {
		return append([]byte(nil), message.Data...)
	}
	header := "Bcc"
	if message.Redirected {
		header = "Resent-Bcc"
	}
	var prefix bytes.Buffer
	if err := writeMessageHeader(&prefix, header, strings.Join(addresses, ", ")); err != nil {
		return append([]byte(nil), message.Data...)
	}
	return append(prefix.Bytes(), message.Data...)
}

func BuildMessage(identity mailmodel.Identity, draft mailmodel.Draft, now time.Time) (BuiltMessage, error) {
	return buildMessage(identity, draft, now, false)
}

// BuildDraft allows incomplete recipient lists, preserves private Bcc and never
// submits SMTP. The resulting MIME is appended only to the provider's Drafts.
func BuildDraft(identity mailmodel.Identity, draft mailmodel.Draft, now time.Time) (BuiltMessage, error) {
	message, err := buildMessage(identity, draft, now, true)
	if err != nil {
		return message, err
	}
	data := SentCopy(message, draft.BCC)
	clear(message.Data)
	message.Data = data
	return message, nil
}
func buildMessage(identity mailmodel.Identity, draft mailmodel.Draft, now time.Time, allowEmpty bool) (BuiltMessage, error) {
	draft.HTML = sanitizeHTML(draft.HTML)
	if containsNewline(identity.Name) || containsNewline(identity.Email) || containsNewline(identity.ReplyTo) || containsNewline(draft.Subject) || containsNewline(draft.InReplyTo) {
		return BuiltMessage{}, errors.New("header contains a newline")
	}
	from, err := parseAddress(mailmodel.Address{Name: identity.Name, Email: identity.Email})
	if err != nil {
		return BuiltMessage{}, err
	}
	to, err := formatAddresses(draft.To)
	if err != nil || len(to) == 0 && !allowEmpty {
		return BuiltMessage{}, errors.New("at least one valid recipient is required")
	}
	cc, err := formatAddresses(draft.CC)
	if err != nil {
		return BuiltMessage{}, err
	}
	if _, err = formatAddresses(draft.BCC); err != nil {
		return BuiltMessage{}, err
	}
	if bcc, _ := formatAddresses(draft.BCC); len(bcc) > 0 {
		var probe bytes.Buffer
		if err = writeMessageHeader(&probe, "Bcc", strings.Join(bcc, ", ")); err != nil {
			return BuiltMessage{}, err
		}
	}
	recipients := envelopeRecipients(draft.To, draft.CC, draft.BCC)
	messageID, err := newMessageID(identity.Email)
	if err != nil {
		return BuiltMessage{}, err
	}
	var output bytes.Buffer
	var headerErr error
	writeHeader := func(name, value string) {
		if headerErr == nil {
			headerErr = writeMessageHeader(&output, name, value)
		}
	}
	writeHeader("Date", now.UTC().Format(time.RFC1123Z))
	writeHeader("Message-ID", messageID)
	writeHeader("From", from)
	if len(to) > 0 {
		writeHeader("To", strings.Join(to, ", "))
	}
	if len(cc) > 0 {
		writeHeader("Cc", strings.Join(cc, ", "))
	}
	if identity.ReplyTo != "" {
		parsed, parseErr := stdmail.ParseAddress(identity.ReplyTo)
		if parseErr != nil {
			return BuiltMessage{}, errors.New("invalid Reply-To address")
		}
		writeHeader("Reply-To", parsed.String())
	}
	writeHeader("Subject", encodeSubject(draft.Subject))
	writeHeader("MIME-Version", "1.0")
	if draft.InReplyTo != "" {
		writeHeader("In-Reply-To", draft.InReplyTo)
	}
	if len(draft.References) > 0 {
		for _, reference := range draft.References {
			if containsNewline(reference) {
				return BuiltMessage{}, errors.New("invalid References header")
			}
		}
		writeHeader("References", strings.Join(draft.References, " "))
	}
	if len(draft.Attachments) == 0 {
		if err = writeAlternative(&output, draft.Text, draft.HTML); err != nil {
			return BuiltMessage{}, err
		}
	} else {
		mixed := multipart.NewWriter(&output)
		writeHeader("Content-Type", `multipart/mixed; boundary="`+mixed.Boundary()+`"`)
		output.WriteString("\r\n")
		bodyHeader := textproto.MIMEHeader{}
		bodyBoundary := randomBoundary()
		bodyHeader.Set("Content-Type", `multipart/alternative; boundary="`+bodyBoundary+`"`)
		part, createErr := mixed.CreatePart(bodyHeader)
		if createErr != nil {
			return BuiltMessage{}, createErr
		}
		if err = writeAlternativeBoundary(part, draft.Text, draft.HTML, bodyBoundary); err != nil {
			return BuiltMessage{}, err
		}
		for _, attachment := range draft.Attachments {
			if len(attachment.Data) > maxAttachmentBytes {
				return BuiltMessage{}, errors.New("attachment exceeds size limit")
			}
			filename := sanitizeFilename(attachment.Filename)
			header := textproto.MIMEHeader{}
			contentType := attachment.ContentType
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			mediaType, parameters, parseErr := mime.ParseMediaType(contentType)
			if parseErr != nil || containsNewline(contentType) || !strings.Contains(mediaType, "/") {
				return BuiltMessage{}, errors.New("invalid attachment content type")
			}
			parameters["name"] = filename
			header.Set("Content-Type", mime.FormatMediaType(mediaType, parameters))
			header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
			header.Set("Content-Transfer-Encoding", "base64")
			part, createErr = mixed.CreatePart(header)
			if createErr != nil {
				return BuiltMessage{}, createErr
			}
			wrapped := &lineWrapWriter{writer: part}
			encoded := base64.NewEncoder(base64.StdEncoding, wrapped)
			if _, err = encoded.Write(attachment.Data); err != nil {
				return BuiltMessage{}, err
			}
			if err = encoded.Close(); err != nil {
				return BuiltMessage{}, err
			}
			if err = wrapped.Close(); err != nil {
				return BuiltMessage{}, err
			}
		}
		if err = mixed.Close(); err != nil {
			return BuiltMessage{}, err
		}
	}
	if headerErr != nil {
		return BuiltMessage{}, headerErr
	}
	return BuiltMessage{Data: output.Bytes(), EnvelopeFrom: identity.Email, Recipients: recipients, MessageID: messageID}, nil
}

func writeAlternative(output *bytes.Buffer, text, html string) error {
	if html == "" {
		output.WriteString("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
		writer := quotedprintable.NewWriter(output)
		_, err := writer.Write([]byte(text))
		if err == nil {
			err = writer.Close()
		}
		return err
	}
	boundary := randomBoundary()
	_, _ = fmt.Fprintf(output, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundary)
	return writeAlternativeBoundary(output, text, html, boundary)
}
func writeAlternativeBoundary(output interface{ Write([]byte) (int, error) }, text, html, boundary string) error {
	writer := multipart.NewWriter(output)
	if err := writer.SetBoundary(boundary); err != nil {
		return err
	}
	if text != "" || html == "" {
		header := textproto.MIMEHeader{"Content-Type": {"text/plain; charset=utf-8"}, "Content-Transfer-Encoding": {"quoted-printable"}}
		part, err := writer.CreatePart(header)
		if err != nil {
			return err
		}
		qp := quotedprintable.NewWriter(part)
		if _, err = qp.Write([]byte(text)); err != nil {
			return err
		}
		if err = qp.Close(); err != nil {
			return err
		}
	}
	if html != "" {
		header := textproto.MIMEHeader{"Content-Type": {"text/html; charset=utf-8"}, "Content-Transfer-Encoding": {"quoted-printable"}}
		part, err := writer.CreatePart(header)
		if err != nil {
			return err
		}
		qp := quotedprintable.NewWriter(part)
		if _, err = qp.Write([]byte(html)); err != nil {
			return err
		}
		if err = qp.Close(); err != nil {
			return err
		}
	}
	return writer.Close()
}
func parseAddress(address mailmodel.Address) (string, error) {
	if containsNewline(address.Name) || containsNewline(address.Email) {
		return "", errors.New("invalid address")
	}
	parsed, err := stdmail.ParseAddress(address.Email)
	if err != nil || !strings.EqualFold(parsed.Address, address.Email) {
		return "", errors.New("invalid address")
	}
	return (&stdmail.Address{Name: address.Name, Address: parsed.Address}).String(), nil
}
func formatAddresses(values []mailmodel.Address) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		formatted, err := parseAddress(value)
		if err != nil {
			return nil, err
		}
		result = append(result, formatted)
	}
	return result, nil
}
func envelopeRecipients(groups ...[]mailmodel.Address) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, group := range groups {
		for _, address := range group {
			key := strings.ToLower(address.Email)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				result = append(result, address.Email)
			}
		}
	}
	return result
}
func newMessageID(email string) (string, error) {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	domain := "localhost"
	if at := strings.LastIndexByte(email, '@'); at >= 0 {
		domain = email[at+1:]
	}
	return fmt.Sprintf("<%s@%s>", base64.RawURLEncoding.EncodeToString(value), domain), nil
}
func containsNewline(value string) bool { return strings.ContainsAny(value, "\r\n\x00") }
func sanitizeFilename(value string) string {
	value = filepath.Base(strings.ReplaceAll(value, "\\", "/"))
	value = strings.Map(func(char rune) rune {
		if char < 32 || char == 127 || char == '"' {
			return '_'
		}
		return char
	}, value)
	if value == "" || value == "." {
		return "attachment"
	}
	if len(value) > 180 {
		value = value[:180]
	}
	return value
}
func randomBoundary() string {
	value := make([]byte, 18)
	_, _ = rand.Read(value)
	return "postbox-" + base64.RawURLEncoding.EncodeToString(value)
}

type lineWrapWriter struct {
	writer interface{ Write([]byte) (int, error) }
	column int
}

func (w *lineWrapWriter) Write(p []byte) (int, error) {
	consumed := 0
	for len(p) > 0 {
		room := 76 - w.column
		size := len(p)
		if size > room {
			size = room
		}
		if _, err := w.writer.Write(p[:size]); err != nil {
			return consumed, err
		}
		consumed += size
		w.column += size
		p = p[size:]
		if w.column == 76 {
			if _, err := w.writer.Write([]byte("\r\n")); err != nil {
				return consumed, err
			}
			w.column = 0
		}
	}
	return consumed, nil
}
func (w *lineWrapWriter) Close() error {
	if w.column > 0 {
		_, err := w.writer.Write([]byte("\r\n"))
		w.column = 0
		return err
	}
	return nil
}

// Long unstructured subjects are encoded into independent RFC 2047 words,
// allowing folding without inserting spaces into an original unbroken word.
func encodeSubject(value string) string {
	if len(value) <= 67 {
		return mime.QEncoding.Encode("utf-8", value)
	}
	var words []string
	for len(value) > 0 {
		n := min(42, len(value))
		for n < len(value) && !utf8.RuneStart(value[n]) {
			n--
		}
		words = append(words, "=?utf-8?b?"+base64.StdEncoding.EncodeToString([]byte(value[:n]))+"?=")
		value = value[n:]
	}
	return strings.Join(words, " ")
}

func writeMessageHeader(output *bytes.Buffer, name, value string) error {
	var header messagetextproto.Header
	header.Add(name, value)
	raw, err := header.Raw(name)
	if err != nil {
		return err
	}
	canonicalEnd := bytes.IndexByte(raw, ':')
	raw = append([]byte(name), raw[canonicalEnd:]...)
	// The library may split a token when no legal whitespace exists. Structured
	// tokens must be rejected rather than silently changing their meaning.
	unfolded := strings.ReplaceAll(strings.TrimSuffix(string(raw), "\r\n"), "\r\n", "")
	if unfolded != name+": "+value {
		return errors.New("header token is too long to fold safely")
	}
	for _, line := range bytes.Split(raw, []byte("\r\n")) {
		if len(line) > maxHeaderLineLen {
			return errors.New("header line exceeds 998 bytes")
		}
	}
	_, err = output.Write(raw)
	return err
}
