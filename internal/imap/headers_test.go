package imap

import (
	"context"
	"errors"
	"strings"
	"testing"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

// Preserve the provider's encoded words on the wire: the in-memory server
// otherwise decodes headers itself and hides missing client charset support.
type encodedEnvelopeSession struct {
	imapserver.Session
	envelope *protocol.Envelope
}

func (s *encodedEnvelopeSession) Fetch(w *imapserver.FetchWriter, set protocol.NumSet, opts *protocol.FetchOptions) error {
	if len(opts.BodySection) != 0 || len(opts.BinarySection) != 0 {
		return errors.New("listing must not fetch message bodies")
	}
	if !opts.Envelope {
		return s.Session.Fetch(w, set, opts)
	}
	response := w.CreateMessage(1)
	response.WriteUID(1)
	response.WriteEnvelope(s.envelope)
	response.WriteFlags(nil)
	return response.Close()
}

func TestListDecodesProviderEnvelopeCharsets(t *testing.T) {
	for _, test := range []struct {
		name, subject, want string
	}{
		{"windows-1252 quoted printable", "=?windows-1252?Q?Votre_colis_a_=E9t=E9_livr=E9_=96_10_=80?=", "Votre colis a été livré – 10 €"},
		{"windows-1252 base64", "=?WINDOWS-1252?B?Q2Fm6Q==?=", "Café"},
		{"iso-8859-15", "=?ISO-8859-15?Q?Facture_=E9t=E9_10_=A4?=", "Facture été 10 €"},
		{"utf-8", "=?UTF-8?Q?Un_=C3=A9t=C3=A9_tranquille?=", "Un été tranquille"},
		{"unicode", "Déjà décodé 📬", "Déjà décodé 📬"},
		{"unknown charset", "=?unknown-charset?Q?Original?=", "=?unknown-charset?Q?Original?="},
	} {
		t.Run(test.name, func(t *testing.T) {
			envelope := &protocol.Envelope{
				Subject: test.subject,
				From:    []protocol.Address{{Name: "=?windows-1252?Q?=C9quipe_La_Poste?=", Mailbox: "team", Host: "example.test"}},
				To:      []protocol.Address{{Name: "=?ISO-8859-15?Q?Andr=E9?=", Mailbox: "me", Host: "example.test"}},
			}
			receiver, cfg, user, audit := readerFixtureWithSession(t, func(session imapserver.Session) imapserver.Session {
				return &encodedEnvelopeSession{Session: session, envelope: envelope}
			})
			if _, err := user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{}); err != nil {
				t.Fatal(err)
			}
			for _, seen := range []string{"", "unread"} {
				cfg.Seen = seen
				page, err := receiver.List(context.Background(), cfg, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				if len(page.Messages) != 1 {
					t.Fatalf("expected one message, got %+v", page)
				}
				message := page.Messages[0]
				if message.Subject != test.want {
					t.Errorf("filter %q: subject = %q, want %q", seen, message.Subject, test.want)
				}
				if len(message.From) != 1 || message.From[0].Name != "Équipe La Poste" || len(message.To) != 1 || message.To[0].Name != "André" {
					t.Errorf("encoded address names not decoded: %+v", message)
				}
			}
			if len(audit.parts) != 0 {
				t.Fatal("listing downloaded message bodies")
			}
		})
	}
}
