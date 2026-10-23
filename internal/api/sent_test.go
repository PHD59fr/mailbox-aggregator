package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/example/mail-aggregator/internal/smtp"
)

func TestSentCopyRunsOnlyAfterSMTPAcceptedAndFailuresStaySent(t *testing.T) {
	for _, test := range []struct {
		name             string
		smtpErr, imapErr error
		copyState        string
		copies, status   int
	}{
		{"saved", nil, nil, "saved", 1, 200},
		{"missing folder", nil, imap.ErrSentUnavailable, "failed", 1, 200},
		{"ambiguous append", nil, imap.ErrSentUnconfirmed, "failed", 1, 200},
		{"provider failure", nil, errors.New("private upstream detail"), "failed", 1, 200},
		{"smtp rejected", errors.New("reject"), nil, "", 0, 502},
		{"smtp ambiguous", smtp.ErrSubmissionUnknown, nil, "", 0, 502},
	} {
		t.Run(test.name, func(t *testing.T) {
			sender := &recordingSender{err: test.smtpErr}
			copies := 0
			reader := fakeReader{sent: func(_ context.Context, cfg imap.Configuration, data []byte, id string, date time.Time) error {
				copies++
				if sender.calls != 1 || cfg.Username != "own" || string(cfg.Password) != "imap-secret" || cfg.SentMailbox != "Work/Sent" || len(cfg.SentExcluded) != 1 || cfg.SentExcluded[0] != "Old/Sent" {
					t.Error("wrong copy scope/order")
				}
				if !strings.Contains(string(data), "Bcc: <hidden@example.test>") || strings.Contains(string(sender.message.Data), "hidden@example.test") || !strings.Contains(string(data), id) || id != sender.message.MessageID || date.IsZero() {
					t.Error("private archive headers or message identity wrong")
				}
				return test.imapErr
			}}
			f := newAPIFixtureWithReader(t, nil, reader, sender)
			_, token, view := f.addUser(t, "sent-copy@example.test")
			perform(f, "POST", "/api/v1/session/credentials", token, view.CSRFToken, []byte(sendingActivation))
			_, _ = f.sessions.SetReceivingCredentials(token, map[string]session.Credential{"own": {Username: "own", Secret: []byte("imap-secret")}}, time.Minute, 0)
			body := strings.TrimSuffix(outgoingMessage, "}") + `,"sent_copy":{"account_id":"own","folder":"Work/Sent","exclude":["Old/Sent"]}}`
			response := perform(f, "POST", "/api/v1/messages/send", token, view.CSRFToken, []byte(body))
			if response.Code != test.status || sender.calls != 1 || copies != test.copies {
				t.Fatalf("wrong outcome: %d %s SMTP=%d copies=%d", response.Code, response.Body.String(), sender.calls, copies)
			}
			if test.copyState != "" {
				var result struct {
					Status string `json:"status"`
					Copy   struct {
						Status string `json:"status"`
					} `json:"sent_copy"`
				}
				_ = json.Unmarshal(response.Body.Bytes(), &result)
				if result.Status != "sent" || result.Copy.Status != test.copyState {
					t.Fatalf("SMTP success lost: %s", response.Body.String())
				}
			}
			if strings.Contains(response.Body.String(), "private upstream detail") {
				t.Fatal("upstream details leaked")
			}
		})
	}
}

func TestSentCopyCannotUseAnotherSessionsMailboxAndInvalidMappingNeverSends(t *testing.T) {
	copies := 0
	reader := fakeReader{sent: func(context.Context, imap.Configuration, []byte, string, time.Time) error { copies++; return nil }}
	sender := &recordingSender{}
	f := newAPIFixtureWithReader(t, nil, reader, sender)
	_, a, av := f.addUser(t, "a-copy@example.test")
	_, b, _ := f.addUser(t, "b-copy@example.test")
	_, _ = f.sessions.SetReceivingCredentials(b, map[string]session.Credential{"other": {Secret: []byte("secret")}}, time.Minute, 0)
	perform(f, "POST", "/api/v1/session/credentials", a, av.CSRFToken, []byte(sendingActivation))
	body := strings.TrimSuffix(outgoingMessage, "}") + `,"sent_copy":{"account_id":"other"}}`
	response := perform(f, "POST", "/api/v1/messages/send", a, av.CSRFToken, []byte(body))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"status":"failed"`) || copies != 0 {
		t.Fatalf("cross-session archive or lost send: %s", response.Body.String())
	}
	before := sender.calls
	for _, folder := range []string{"INBOX", "Sent\r\nInjected", "Sent\x00other"} {
		data, _ := json.Marshal(map[string]any{"smtp_config_id": "send-only", "to": []map[string]string{{"email": "to@example.test"}}, "sent_copy": map[string]any{"account_id": "other", "folder": folder}})
		if response := perform(f, "POST", "/api/v1/messages/send", a, av.CSRFToken, data); response.Code != 400 {
			t.Fatalf("unsafe mapping accepted: %s", response.Body.String())
		}
	}
	if sender.calls != before || copies != 0 {
		t.Fatal("invalid mapping reached a provider")
	}
}
