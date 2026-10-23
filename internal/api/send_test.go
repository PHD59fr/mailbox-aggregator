package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/session"
	"github.com/example/mail-aggregator/internal/smtp"
)

type recordingSender struct {
	calls              int
	message            smtp.BuiltMessage
	username, password string
	err                error
}

func (s *recordingSender) Send(_ context.Context, cfg smtp.Configuration, message smtp.BuiltMessage) error {
	s.calls++
	message.Data = append([]byte(nil), message.Data...)
	s.message = message
	s.username = cfg.Username
	s.password = string(cfg.Password)
	return s.err
}

const sendingActivation = `{"lock_revision":0,"lifetime_seconds":300,"smtp":{"send-only":{"host":"8.8.8.8","port":465,"security":"tls","username":"smtp-user","password":"smtp-secret","sender_name":"Support","sender_email":"support@example.test"}}}`
const outgoingMessage = `{"smtp_config_id":"send-only","to":[{"email":"to@example.test"}],"bcc":[{"email":"hidden@example.test"}],"subject":"Hello","text":"SMTP only","html":"<p>SMTP <strong>only</strong></p><script>alert(1)</script>","from":"attacker@example.test","attachments":[{"filename":"report.txt","content_type":"text/plain","data":"cmVwb3J0"}]}`

func TestSendWithSMTPOnlyAndNoIMAP(t *testing.T) {
	sender := &recordingSender{}
	f := newAPIFixture(t, sender)
	id, token, view := f.addUser(t, "smtp-only@example.test")
	dir := filepath.Join(f.root, "users", id.String())
	entries, _ := os.ReadDir(dir)
	before := make(map[string]string)
	for _, entry := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, entry.Name()))
		before[entry.Name()] = string(data)
	}
	response := perform(f, http.MethodPost, "/api/v1/session/credentials", token, view.CSRFToken, []byte(sendingActivation))
	if response.Code != http.StatusOK {
		t.Fatalf("SMTP-only activation failed: %d %s", response.Code, response.Body.String())
	}
	accounts, configs, err := f.sessions.Credentials(token)
	if err != nil || len(accounts) != 0 || len(configs) != 1 {
		t.Fatalf("unexpected activated configuration: %v", err)
	}
	clearCredentialCopies(configs)
	response = perform(f, http.MethodPost, "/api/v1/messages/send", token, view.CSRFToken, []byte(outgoingMessage))
	if response.Code != http.StatusOK || sender.calls != 1 {
		t.Fatalf("send failed: %d %s (calls %d)", response.Code, response.Body.String(), sender.calls)
	}
	if sender.message.EnvelopeFrom != "support@example.test" || strings.Contains(string(sender.message.Data), "attacker@example.test") {
		t.Fatal("sender was not bound to activated identity")
	}
	if len(sender.message.Recipients) != 2 || strings.Contains(string(sender.message.Data), "hidden@example.test") {
		t.Fatal("Bcc envelope recipient leaked into message headers")
	}
	if sender.username != "smtp-user" || sender.password != "smtp-secret" {
		t.Fatal("SMTP credentials were not used")
	}
	if !strings.Contains(string(sender.message.Data), "multipart/mixed") || !strings.Contains(string(sender.message.Data), "cmVwb3J0") {
		t.Fatal("attachment missing from MIME message")
	}
	if !strings.Contains(string(sender.message.Data), "<strong>only</strong>") || strings.Contains(string(sender.message.Data), "<script>") {
		t.Fatal("HTML formatting missing or unsafe HTML passed through")
	}
	after, _ := os.ReadDir(dir)
	if len(after) != len(entries) {
		t.Fatal("message created a persisted file")
	}
	for _, entry := range after {
		data, _ := os.ReadFile(filepath.Join(dir, entry.Name()))
		if string(data) != before[entry.Name()] {
			t.Fatalf("send modified persisted file %s", entry.Name())
		}
	}
}

func TestSendAttachmentsLimitsAndValidation(t *testing.T) {
	sender := &recordingSender{}
	f := newAPIFixture(t, sender)
	_, token, view := f.addUser(t, "attachments@example.test")
	perform(f, http.MethodPost, "/api/v1/session/credentials", token, view.CSRFToken, []byte(sendingActivation))
	for _, test := range []struct {
		name   string
		files  []sendAttachment
		status int
	}{
		{"larger than generic request limit", []sendAttachment{{Filename: "large.bin", Data: make([]byte, 3<<20)}}, http.StatusOK},
		{"aggregate too large", []sendAttachment{{Filename: "a.bin", Data: make([]byte, 5<<20)}, {Filename: "b.bin", Data: make([]byte, (5<<20)+1)}}, http.StatusRequestEntityTooLarge},
		{"missing filename", []sendAttachment{{Data: []byte("x")}}, http.StatusBadRequest},
		{"injected content type", []sendAttachment{{Filename: "a.txt", ContentType: "text/plain\r\nX-Injected: yes", Data: []byte("x")}}, http.StatusBadRequest},
		{"invalid content type", []sendAttachment{{Filename: "a.txt", ContentType: "invalid", Data: []byte("x")}}, http.StatusBadRequest},
		{"too many files", make([]sendAttachment, 21), http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"smtp_config_id": "send-only", "to": []map[string]string{{"email": "to@example.test"}}, "text": "with files", "attachments": test.files})
			if err != nil {
				t.Fatal(err)
			}
			calls := sender.calls
			response := perform(f, http.MethodPost, "/api/v1/messages/send", token, view.CSRFToken, body)
			if response.Code != test.status {
				t.Fatalf("got %d, want %d: %s", response.Code, test.status, response.Body.String())
			}
			if test.status != http.StatusOK && sender.calls != calls {
				t.Fatal("invalid attachments reached SMTP")
			}
		})
	}
	invalid := strings.Replace(outgoingMessage, base64.StdEncoding.EncodeToString([]byte("report")), "not base64!", 1)
	if response := perform(f, http.MethodPost, "/api/v1/messages/send", token, view.CSRFToken, []byte(invalid)); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid base64 returned %d", response.Code)
	}
	oversized := `{"smtp_config_id":"send-only","to":[{"email":"to@example.test"}],"text":"` + strings.Repeat("a", int(maxSendRequestBytes)) + `"}`
	if response := perform(f, http.MethodPost, "/api/v1/messages/send", token, view.CSRFToken, []byte(oversized)); response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized JSON returned %d: %s", response.Code, response.Body.String())
	}
}

func TestSMTPActivationPreservesReceivingIdentity(t *testing.T) {
	f := newAPIFixture(t, &recordingSender{})
	_, token, view := f.addUser(t, "both@example.test")
	_, err := f.sessions.SetCredentials(token, map[string]session.Credential{"receiver": {Host: "imap.example.test", Port: 993, Security: "tls", Username: "receiver", Secret: []byte("imap-secret")}}, nil, time.Minute, 0)
	if err != nil {
		t.Fatal(err)
	}
	response := perform(f, http.MethodPost, "/api/v1/session/credentials", token, view.CSRFToken, []byte(sendingActivation))
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	accounts, configs, err := f.sessions.Credentials(token)
	defer clearCredentialCopies(accounts)
	defer clearCredentialCopies(configs)
	if err != nil || len(accounts) != 1 || len(configs) != 1 || string(accounts["receiver"].Secret) != "imap-secret" {
		t.Fatalf("SMTP activation replaced receiving credentials: %v", err)
	}
}

func TestSendRequiresOwnActiveIdentityAndCSRF(t *testing.T) {
	sender := &recordingSender{}
	f := newAPIFixture(t, sender)
	_, tokenA, viewA := f.addUser(t, "a@example.test")
	_, tokenB, viewB := f.addUser(t, "b@example.test")
	activated := perform(f, http.MethodPost, "/api/v1/session/credentials", tokenA, viewA.CSRFToken, []byte(sendingActivation))
	if activated.Code != http.StatusOK {
		t.Fatal(activated.Body.String())
	}
	for _, test := range []struct {
		token, csrf string
		status      int
	}{
		{"", "", http.StatusUnauthorized},
		{tokenA, "", http.StatusForbidden},
		{tokenB, viewB.CSRFToken, http.StatusLocked},
	} {
		response := perform(f, http.MethodPost, "/api/v1/messages/send", test.token, test.csrf, []byte(outgoingMessage))
		if response.Code != test.status {
			t.Fatalf("got %d, want %d: %s", response.Code, test.status, response.Body.String())
		}
	}
	if _, err := f.sessions.Lock(tokenA); err != nil {
		t.Fatal(err)
	}
	response := perform(f, http.MethodPost, "/api/v1/messages/send", tokenA, viewA.CSRFToken, []byte(outgoingMessage))
	if response.Code != http.StatusLocked || sender.calls != 0 {
		t.Fatal("locked session sent a message")
	}
}

func TestSendRejectsInvalidMessageAndMissingSMTP(t *testing.T) {
	sender := &recordingSender{}
	f := newAPIFixture(t, sender)
	_, token, view := f.addUser(t, "sender@example.test")
	perform(f, http.MethodPost, "/api/v1/session/credentials", token, view.CSRFToken, []byte(sendingActivation))
	for _, body := range []string{
		strings.Replace(outgoingMessage, `"send-only"`, `"missing"`, 1),
		strings.Replace(outgoingMessage, `"Hello"`, `"Hello\r\nBcc: victim@example.test"`, 1),
		strings.Replace(outgoingMessage, `"to@example.test"`, `"invalid"`, 1),
		`{"smtp_config_id":"send-only","to":[]}`,
	} {
		response := perform(f, http.MethodPost, "/api/v1/messages/send", token, view.CSRFToken, []byte(body))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid send returned %d: %s", response.Code, response.Body.String())
		}
	}
	_, err := f.sessions.SetCredentials(token, map[string]session.Credential{"imap": {Secret: []byte("secret")}}, nil, time.Minute, 0)
	if err != nil {
		t.Fatal(err)
	}
	response := perform(f, http.MethodPost, "/api/v1/messages/send", token, view.CSRFToken, []byte(outgoingMessage))
	if response.Code != http.StatusBadRequest || sender.calls != 0 {
		t.Fatal("IMAP-only session could send")
	}
}

func TestSendReportsAmbiguousSubmissionWithoutRetry(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{smtp.ErrSubmissionUnknown, "SMTP_OUTCOME_UNKNOWN"},
		{errors.New("SMTP authentication failed"), "SMTP_SEND_FAILED"},
	} {
		t.Run(test.code, func(t *testing.T) {
			sender := &recordingSender{err: test.err}
			f := newAPIFixture(t, sender)
			_, token, view := f.addUser(t, "sender@example.test")
			perform(f, http.MethodPost, "/api/v1/session/credentials", token, view.CSRFToken, []byte(sendingActivation))
			response := perform(f, http.MethodPost, "/api/v1/messages/send", token, view.CSRFToken, []byte(outgoingMessage))
			if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), test.code) || sender.calls != 1 {
				t.Fatalf("unexpected result %d %s", response.Code, response.Body.String())
			}
		})
	}
}
