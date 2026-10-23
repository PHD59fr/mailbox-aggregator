package api

import (
	"encoding/json"
	"github.com/example/mail-aggregator/internal/session"
	"testing"
	"time"
)

func TestImportIsRevisionGuardedSessionScopedAndRevokesAllUserSessions(t *testing.T) {
	f := newAPIFixture(t)
	id, token, view := f.addUser(t, "import@example.test")
	_, otherToken, otherView := f.addUser(t, "other@example.test")
	second, _, _ := f.sessions.Create(id)
	_, _ = f.sessions.MergeCredentials(token, map[string]session.Credential{"imap": {Secret: []byte("secret")}}, nil, time.Minute, 0)
	files := map[string]any{}
	// f.addUser intentionally initializes only accounts; initialize the two other collections.
	for _, name := range []string{"accounts", "smtp", "preferences"} {
		if name != "accounts" {
			_, current, err := f.store.ReadUserFile(id, name+".enc")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.WriteUserFile(id, name+".enc", testEnvelope(t), current); err != nil {
				t.Fatal(err)
			}
		}
		_, revision, err := f.store.ReadUserFile(id, name+".enc")
		if err != nil {
			t.Fatal(err)
		}
		files[name] = map[string]any{"data": json.RawMessage(testEnvelope(t)), "revision": revision}
	}
	payload, _ := json.Marshal(map[string]any{"files": files})
	if r := perform(f, "POST", "/api/v1/vault/import", token, "", payload); r.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	if r := perform(f, "POST", "/api/v1/vault/import", otherToken, "", payload); r.Code != 403 {
		t.Fatal("other session bypassed CSRF")
	}
	if r := perform(f, "POST", "/api/v1/vault/import", otherToken, otherView.CSRFToken, payload); r.Code != 409 {
		t.Fatalf("another user accepted revisions from this vault: %d", r.Code)
	}
	stale := files["smtp"].(map[string]any)
	revision := stale["revision"]
	stale["revision"] = "stale"
	body, _ := json.Marshal(map[string]any{"files": files})
	if r := perform(f, "POST", "/api/v1/vault/import", token, view.CSRFToken, body); r.Code != 409 {
		t.Fatalf("stale import: %d", r.Code)
	}
	stale["revision"] = revision
	if r := perform(f, "POST", "/api/v1/vault/import", token, view.CSRFToken, payload); r.Code != 204 {
		t.Fatalf("import: %d %s", r.Code, r.Body.String())
	}
	for _, revoked := range []string{token, second} {
		if _, err := f.sessions.Peek(revoked); err == nil {
			t.Fatal("provider settings import kept an old session")
		}
	}
	if _, err := f.sessions.Peek(otherToken); err != nil {
		t.Fatal("import revoked another user's session")
	}
}
