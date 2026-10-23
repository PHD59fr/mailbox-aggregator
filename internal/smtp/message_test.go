package smtp

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	stdmail "net/mail"
	"strings"
	"testing"
	"time"

	mailmodel "github.com/example/mail-aggregator/internal/mail"
)

func TestBuildMessageIncludesIdentityAndOmitsBcc(t *testing.T) {
	identity := mailmodel.Identity{ID: "work", Name: "Support", Email: "support@example.test", ReplyTo: "team@example.test", SMTPConfigID: "smtp-work"}
	draft := mailmodel.Draft{To: []mailmodel.Address{{Email: "one@example.test"}}, CC: []mailmodel.Address{{Email: "two@example.test"}}, BCC: []mailmodel.Address{{Email: "hidden@example.test"}}, Subject: "Hello ✓", Text: "plain", HTML: "<p>html</p>", Attachments: []mailmodel.Attachment{{Filename: "../report.txt", ContentType: "text/plain", Data: []byte("report")}}}
	built, err := BuildMessage(identity, draft, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	message := string(built.Data)
	if strings.Contains(strings.ToLower(message), "bcc:") || strings.Contains(message, "../report.txt") {
		t.Fatal("Bcc or unsafe filename leaked into headers")
	}
	if !strings.Contains(message, "support@example.test>") || !strings.Contains(message, "multipart/mixed") {
		t.Fatalf("required MIME headers missing:\n%s", message)
	}
	if len(built.Recipients) != 3 {
		t.Fatalf("got %d recipients", len(built.Recipients))
	}
}

func TestAttachmentMIMERoundTrip(t *testing.T) {
	content := []byte{0, 1, 2, 128, 255}
	built, err := BuildMessage(mailmodel.Identity{Email: "sender@example.test"}, mailmodel.Draft{
		To: []mailmodel.Address{{Email: "to@example.test"}}, Text: "Hello ✓",
		Attachments: []mailmodel.Attachment{{Filename: "rapport été.txt", ContentType: "text/plain", Data: content}},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	message, err := stdmail.ReadMessage(bytes.NewReader(built.Data))
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mixed := multipart.NewReader(message.Body, params["boundary"])
	body, err := mixed.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	_, params, err = mime.ParseMediaType(body.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	alternative := multipart.NewReader(body, params["boundary"])
	plain, err := alternative.NextRawPart()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(plain))
	if err != nil || string(decoded) != "Hello ✓" {
		t.Fatalf("plain text changed: %q %v", decoded, err)
	}
	if _, err = alternative.NextPart(); err != io.EOF {
		t.Fatal("plain-text attachments created an empty HTML alternative")
	}
	file, err := mixed.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	_, params, err = mime.ParseMediaType(file.Header.Get("Content-Disposition"))
	if err != nil || params["filename"] != "rapport été.txt" {
		t.Fatalf("filename changed: %v %v", params, err)
	}
	decoded, err = io.ReadAll(base64.NewDecoder(base64.StdEncoding, file))
	if err != nil || !bytes.Equal(decoded, content) {
		t.Fatalf("attachment changed: %v", err)
	}
}

func TestAttachmentRejectsContentTypeInjection(t *testing.T) {
	for _, contentType := range []string{"text/plain\r\nBcc: victim@example.test", "invalid"} {
		_, err := BuildMessage(mailmodel.Identity{Email: "sender@example.test"}, mailmodel.Draft{
			To: []mailmodel.Address{{Email: "to@example.test"}}, Attachments: []mailmodel.Attachment{{Filename: "test.txt", ContentType: contentType}},
		}, time.Now())
		if err == nil {
			t.Fatal("invalid MIME content type accepted")
		}
	}
}

func TestBuildMessageRejectsHeaderInjection(t *testing.T) {
	_, err := BuildMessage(mailmodel.Identity{Name: "Bad\r\nBcc: victim@example.test", Email: "sender@example.test"}, mailmodel.Draft{To: []mailmodel.Address{{Email: "to@example.test"}}}, time.Now())
	if err == nil {
		t.Fatal("header injection accepted")
	}
}

func TestLongHeadersAreFoldedWithoutChangingTheirMeaning(t *testing.T) {
	for _, subject := range []string{strings.Repeat("a", 998), strings.Repeat("é", 300), strings.Repeat("long words ", 90)} {
		var recipients []mailmodel.Address
		for i := 0; i < 100; i++ {
			recipients = append(recipients, mailmodel.Address{Name: "Quoted, Display Name", Email: fmt.Sprintf("recipient-%d@example.test", i)})
		}
		draft := mailmodel.Draft{To: recipients, CC: recipients, BCC: recipients, Subject: subject, Text: "text", References: []string{strings.Repeat("<reference@example.test> ", 32)}}
		built, err := BuildMessage(mailmodel.Identity{Email: "sender@example.test"}, draft, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		variants := [][]byte{built.Data, SentCopy(built, recipients)}
		redirected, err := BuildRedirect(mailmodel.Identity{Email: "sender@example.test"}, built.Data, recipients, recipients, recipients, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		variants = append(variants, SentCopy(redirected, recipients))
		for _, data := range variants {
			header := bytes.SplitN(data, []byte("\r\n\r\n"), 2)[0]
			for _, line := range bytes.Split(header, []byte("\r\n")) {
				if len(line) > 998 {
					t.Fatalf("header line has %d bytes", len(line))
				}
			}
			parsed, err := stdmail.ReadMessage(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
			if err != nil || decoded != subject {
				t.Fatal("folding changed the subject")
			}
			addresses, err := parsed.Header.AddressList("To")
			if err != nil || len(addresses) != 100 || addresses[0].Name != "Quoted, Display Name" {
				t.Fatal("folding changed addresses")
			}
			if parsed.Header.Get("Resent-To") != "" {
				resent, err := parsed.Header.AddressList("Resent-To")
				if err != nil || len(resent) != 100 || resent[0].Name != "Quoted, Display Name" {
					t.Fatal("folding changed redirect recipients")
				}
			}
		}
	}
}
