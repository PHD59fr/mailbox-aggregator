package smtp

import (
	"bufio"
	"bytes"
	"errors"
	stdmail "net/mail"
	"strings"
	"time"

	"github.com/example/mail-aggregator/internal/mail"
)

// BuildRedirect preserves original From/body, identifies the actual sender with
// Resent headers and binds the SMTP envelope to that authorized sender.
func BuildRedirect(identity mail.Identity, original []byte, to, cc, bcc []mail.Address, now time.Time) (BuiltMessage, error) {
	from, err := parseAddress(mail.Address{Name: identity.Name, Email: identity.Email})
	if err != nil {
		return BuiltMessage{}, err
	}
	targets, err := formatAddresses(to)
	if err != nil || len(targets) == 0 {
		return BuiltMessage{}, errors.New("invalid redirect recipients")
	}
	copies, err := formatAddresses(cc)
	if err != nil {
		return BuiltMessage{}, err
	}
	hidden, err := formatAddresses(bcc)
	if err != nil {
		return BuiltMessage{}, err
	}
	if len(hidden) > 0 {
		var probe bytes.Buffer
		if err := writeMessageHeader(&probe, "Resent-Bcc", strings.Join(hidden, ", ")); err != nil {
			return BuiltMessage{}, err
		}
	}
	if _, err = stdmail.ReadMessage(bufio.NewReader(bytes.NewReader(original))); err != nil {
		return BuiltMessage{}, errors.New("invalid original message")
	}
	separator := []byte("\r\n\r\n")
	split := bytes.Index(original, separator)
	if split < 0 {
		separator = []byte("\n\n")
		split = bytes.Index(original, separator)
	}
	if split < 0 {
		return BuiltMessage{}, errors.New("invalid original headers")
	}
	if bytes.IndexByte(original[:split], 0) >= 0 {
		return BuiltMessage{}, errors.New("invalid original headers")
	}
	id, err := newMessageID(identity.Email)
	if err != nil {
		return BuiltMessage{}, err
	}
	var output bytes.Buffer
	for _, field := range []struct{ name, value string }{
		{"Resent-Date", now.UTC().Format(time.RFC1123Z)}, {"Resent-Message-ID", id}, {"Resent-From", from}, {"Resent-To", strings.Join(targets, ", ")},
	} {
		if err := writeMessageHeader(&output, field.name, field.value); err != nil {
			return BuiltMessage{}, err
		}
	}
	if len(copies) > 0 {
		if err := writeMessageHeader(&output, "Resent-Cc", strings.Join(copies, ", ")); err != nil {
			return BuiltMessage{}, err
		}
	}
	// Do not disclose original Bcc or previous Resent-Bcc headers on a redirect.
	drop := false
	for _, line := range strings.Split(strings.ReplaceAll(string(original[:split]), "\r\n", "\n"), "\n") {
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			key, _, _ := strings.Cut(line, ":")
			drop = strings.EqualFold(key, "Bcc") || strings.EqualFold(key, "Resent-Bcc")
		}
		if !drop {
			output.WriteString(line + "\r\n")
		}
	}
	output.WriteString("\r\n")
	output.Write(original[split+len(separator):])
	return BuiltMessage{Data: output.Bytes(), EnvelopeFrom: identity.Email, Recipients: envelopeRecipients(to, cc, bcc), MessageID: id, Redirected: true}, nil
}
