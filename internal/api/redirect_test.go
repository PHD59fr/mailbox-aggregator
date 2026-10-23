package api

import (
	"context"
	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/example/mail-aggregator/internal/smtp"
	"strings"
	"testing"
	"time"
)

func TestRedirectIsScopedToOwnMailboxAndAuthorizedSender(t *testing.T) {
	sources := 0
	reader := fakeReader{source: func(_ context.Context, cfg imap.Configuration, uid, validity uint32) ([]byte, error) {
		sources++
		if string(cfg.Password) != "imap-secret" || uid != 12 || validity != 7 {
			t.Error("wrong redirect source scope")
		}
		return []byte("From: Original <original@example.test>\r\nTo: owner@example.test\r\nSubject: Original\r\n\r\nOriginal content\r\n"), nil
	}}
	sender := &recordingSender{}
	f := newAPIFixtureWithReader(t, nil, reader, sender)
	_, token, view := f.addUser(t, "redirect@example.test")
	_, _ = f.sessions.SetReceivingCredentials(token, map[string]session.Credential{"own": {Secret: []byte("imap-secret")}}, time.Minute, 0)
	path := "/api/v1/mailboxes/own/messages/12/redirect?uid_validity=7"
	body := []byte(`{"smtp_config_id":"sender","to":[{"email":"next@example.test"}],"bcc":[{"email":"hidden@example.test"}],"from":"attacker@example.test"}`)
	if response := perform(f, "POST", path, token, "", body); response.Code != 403 {
		t.Fatal("redirect bypassed CSRF")
	}
	if response := perform(f, "POST", path, token, view.CSRFToken, body); response.Code != 423 {
		t.Fatal("redirect without activated sender allowed")
	}
	_, _ = f.sessions.SetSMTPCredentials(token, map[string]session.Credential{"sender": {Secret: []byte("smtp-secret"), SenderEmail: "authorized@example.test", SenderName: "Authorized"}}, time.Minute, 0)
	response := perform(f, "POST", path, token, view.CSRFToken, body)
	if response.Code != 200 || sender.calls != 1 || sources != 1 {
		t.Fatalf("redirect failed: %d %s", response.Code, response.Body.String())
	}
	if sender.message.EnvelopeFrom != "authorized@example.test" || !strings.Contains(string(sender.message.Data), "From: Original <original@example.test>") || strings.Contains(string(sender.message.Data), "hidden@example.test") || strings.Contains(string(sender.message.Data), "attacker@example.test") {
		t.Fatal("redirect identity/header isolation failed")
	}
	sender.err = smtp.ErrSubmissionUnknown
	response = perform(f, "POST", path, token, view.CSRFToken, body)
	if response.Code != 502 || !strings.Contains(response.Body.String(), "SMTP_OUTCOME_UNKNOWN") || sender.calls != 2 {
		t.Fatal("ambiguous redirect retried or hidden")
	}
}
