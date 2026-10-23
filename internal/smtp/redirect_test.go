package smtp

import (
	"github.com/example/mail-aggregator/internal/mail"
	"strings"
	"testing"
	"time"
)

func TestRedirectBindsEnvelopeAndPreservesOriginal(t *testing.T) {
	original := []byte("From: Original <original@example.test>\r\nTo: old@example.test\r\nBcc: secret@example.test\r\n another@example.test\r\nSubject: Original subject\r\nMessage-ID: <original@example.test>\r\n\r\nOriginal body\r\n")
	message, err := BuildRedirect(mail.Identity{Email: "authorized@example.test", Name: "Authorized"}, original, []mail.Address{{Email: "new@example.test"}}, nil, []mail.Address{{Email: "hidden@example.test"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if message.EnvelopeFrom != "authorized@example.test" || len(message.Recipients) != 2 {
		t.Fatal("redirect envelope not authorized")
	}
	data := string(message.Data)
	for _, expected := range []string{"Resent-From: \"Authorized\" <authorized@example.test>", "Resent-To: <new@example.test>", "From: Original <original@example.test>", "Message-ID: <original@example.test>", "Original body\r\n"} {
		if !strings.Contains(data, expected) {
			t.Fatalf("missing redirect field %q: %s", expected, data)
		}
	}
	for _, secret := range []string{"secret@example.test", "another@example.test", "hidden@example.test", "Bcc:"} {
		if strings.Contains(data, secret) {
			t.Fatalf("Bcc leak: %s", secret)
		}
	}
	archive := string(SentCopy(message, []mail.Address{{Email: "hidden@example.test"}}))
	if !strings.HasPrefix(archive, "Resent-Bcc: <hidden@example.test>\r\n") || strings.Contains(archive, "secret@example.test") || strings.Contains(string(message.Data), "hidden@example.test") {
		t.Fatal("private redirect archive changed or disclosed blind recipients")
	}
}
