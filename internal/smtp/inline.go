package smtp

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"

	mailmodel "github.com/example/mail-aggregator/internal/mail"
)

func writeMIMEAttachment(writer *multipart.Writer, attachment mailmodel.Attachment) error {
	if len(attachment.Data) > maxAttachmentBytes {
		return errors.New("attachment exceeds size limit")
	}
	filename := sanitizeFilename(attachment.Filename)
	kind, params, err := mime.ParseMediaType(attachment.ContentType)
	if err != nil || containsNewline(attachment.ContentType) || !strings.Contains(kind, "/") {
		return errors.New("invalid attachment content type")
	}
	params["name"] = filename
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", mime.FormatMediaType(kind, params))
	disposition := "attachment"
	if attachment.ContentID != "" {
		disposition = "inline"
		header.Set("Content-ID", "<"+attachment.ContentID+">")
	}
	header.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": filename}))
	header.Set("Content-Transfer-Encoding", "base64")
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	wrapped := &lineWrapWriter{writer: part}
	encoded := base64.NewEncoder(base64.StdEncoding, wrapped)
	if _, err = encoded.Write(attachment.Data); err != nil {
		return err
	}
	if err = encoded.Close(); err != nil {
		return err
	}
	return wrapped.Close()
}
func writeRelated(output io.Writer, draft mailmodel.Draft, boundary string) error {
	related := multipart.NewWriter(output)
	if err := related.SetBoundary(boundary); err != nil {
		return err
	}
	alternative := randomBoundary()
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", `multipart/alternative; boundary="`+alternative+`"`)
	part, err := related.CreatePart(header)
	if err != nil {
		return err
	}
	if err = writeAlternativeBoundary(part, draft.Text, draft.HTML, alternative); err != nil {
		return err
	}
	for _, attachment := range draft.Attachments {
		if attachment.ContentID != "" {
			if err = writeMIMEAttachment(related, attachment); err != nil {
				return err
			}
		}
	}
	return related.Close()
}
func writeWithInline(output *bytes.Buffer, draft mailmodel.Draft) error {
	ordinary := []mailmodel.Attachment{}
	for _, a := range draft.Attachments {
		if a.ContentID == "" {
			ordinary = append(ordinary, a)
		}
	}
	relatedBoundary := randomBoundary()
	relatedType := `multipart/related; type="multipart/alternative"; boundary="` + relatedBoundary + `"`
	if len(ordinary) == 0 {
		if err := writeMessageHeader(output, "Content-Type", relatedType); err != nil {
			return err
		}
		output.WriteString("\r\n")
		return writeRelated(output, draft, relatedBoundary)
	}
	mixed := multipart.NewWriter(output)
	if err := writeMessageHeader(output, "Content-Type", `multipart/mixed; boundary="`+mixed.Boundary()+`"`); err != nil {
		return err
	}
	output.WriteString("\r\n")
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", relatedType)
	part, err := mixed.CreatePart(header)
	if err != nil {
		return err
	}
	if err = writeRelated(part, draft, relatedBoundary); err != nil {
		return err
	}
	for _, attachment := range ordinary {
		if err = writeMIMEAttachment(mixed, attachment); err != nil {
			return err
		}
	}
	return mixed.Close()
}
