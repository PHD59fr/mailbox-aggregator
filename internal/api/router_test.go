package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/example/mail-aggregator/internal/auth"
	"github.com/example/mail-aggregator/internal/config"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/outbound"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/example/mail-aggregator/internal/user"
)

type availableAuth struct{ auth.UnavailableProvider }

func (availableAuth) Available() bool { return true }
func (availableAuth) RegistrationStart(context.Context, string, json.RawMessage) (string, json.RawMessage, error) {
	return "", nil, nil
}

func testEnvelope(t *testing.T) []byte {
	t.Helper()
	nonce := make([]byte, 12)
	ciphertext := make([]byte, 17)
	_, _ = rand.Read(nonce)
	_, _ = rand.Read(ciphertext)
	data, _ := json.Marshal(map[string]any{"version": 1, "algorithm": "AES-256-GCM", "key_version": 1, "nonce": base64.RawStdEncoding.EncodeToString(nonce), "ciphertext": base64.RawStdEncoding.EncodeToString(ciphertext)})
	return data
}

type apiFixture struct {
	router   http.Handler
	store    *filestore.Store
	users    *user.Service
	sessions *session.Manager
	root     string
}

func newAPIFixture(t *testing.T, senders ...MessageSender) apiFixture {
	return newAPIFixtureWithWatcher(t, nil, senders...)
}

func newAPIFixtureWithWatcher(t *testing.T, receiver MailWatcher, senders ...MessageSender) apiFixture {
	return newAPIFixtureWithReader(t, receiver, nil, senders...)
}

func newAPIFixtureWithReader(t *testing.T, receiver MailWatcher, reader MailReader, senders ...MessageSender) apiFixture {
	return newAPIFixtureWithTester(t, receiver, reader, nil, senders...)
}
func newAPIFixtureWithTester(t *testing.T, receiver MailWatcher, reader MailReader, tester ConnectionTester, senders ...MessageSender) apiFixture {
	t.Helper()
	root := t.TempDir()
	store, err := filestore.New(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	index, err := filestore.OpenUserIndex(store.IndexDirectory())
	if err != nil {
		t.Fatal(err)
	}
	sessions, _ := session.NewManager(30*time.Minute, 12*time.Hour)
	users := user.NewService(store, index, sessions, nil)
	cfg := config.Config{MaxRequestBytes: 2 << 20, MaxVaultFileBytes: 1 << 20, RequestTimeout: time.Minute, ReauthLifetime: 5 * time.Minute}
	var sender MessageSender
	if len(senders) > 0 {
		sender = senders[0]
	}
	router, err := NewRouter(Dependencies{Config: cfg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Store: store, Index: index, Users: users, Sessions: sessions, Auth: availableAuth{}, Outbound: outbound.Policy{AllowedPorts: map[uint16]struct{}{465: {}, 587: {}, 993: {}}}, Sender: sender, Receiver: receiver, Reader: reader, Tester: tester})
	if err != nil {
		t.Fatal(err)
	}
	return apiFixture{router: router, store: store, users: users, sessions: sessions, root: root}
}
func (f apiFixture) addUser(t *testing.T, login string) (uuid.UUID, string, session.View) {
	t.Helper()
	envelope := testEnvelope(t)
	id, err := f.users.Register(login, user.RegistrationFiles{AuthJSON: []byte("{}"), Accounts: envelope})
	if err != nil {
		t.Fatal(err)
	}
	token, view, err := f.sessions.Create(id)
	if err != nil {
		t.Fatal(err)
	}
	return id, token, view
}
func perform(f apiFixture, method, path, token, csrf string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, req)
	return recorder
}

func TestUserCannotReadOrModifyAnotherVault(t *testing.T) {
	f := newAPIFixture(t)
	_, tokenA, viewA := f.addUser(t, "a@example.test")
	idB, _, _ := f.addUser(t, "b@example.test")
	response := perform(f, http.MethodGet, "/api/v1/vault/files/accounts?user_id="+idB.String(), tokenA, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("read own file failed: %d %s", response.Code, response.Body.String())
	}
	dataB, _, err := f.store.ReadUserFile(idB, "accounts.enc")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(response.Body.Bytes(), dataB) {
		t.Fatal("user A received user B's vault")
	}
	response = perform(f, http.MethodPut, "/api/v1/vault/files/accounts?user_id="+idB.String(), tokenA, viewA.CSRFToken, testEnvelope(t))
	if response.Code != http.StatusPreconditionRequired {
		t.Fatalf("unexpected response: %d", response.Code)
	}
}

func TestDeleteAlwaysTargetsAuthenticatedUser(t *testing.T) {
	f := newAPIFixture(t)
	idA, tokenA, viewA := f.addUser(t, "a@example.test")
	idB, _, _ := f.addUser(t, "b@example.test")
	response := perform(f, http.MethodDelete, "/api/v1/me?user_id="+idB.String(), tokenA, viewA.CSRFToken, []byte(`{"confirmation":"DELETE"}`))
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete failed: %d %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(f.root, "users", idA.String())); !os.IsNotExist(err) {
		t.Fatal("authenticated user's directory remains")
	}
	if _, err := os.Stat(filepath.Join(f.root, "users", idB.String())); err != nil {
		t.Fatalf("other user's directory removed: %v", err)
	}
	if response = perform(f, http.MethodGet, "/api/v1/me", tokenA, "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("deleted session accepted: %d", response.Code)
	}
}

func TestCSRFAndLockedState(t *testing.T) {
	f := newAPIFixture(t)
	_, token, view := f.addUser(t, "a@example.test")
	response := perform(f, http.MethodDelete, "/api/v1/session/credentials", token, "", nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF accepted: %d", response.Code)
	}
	response = perform(f, http.MethodDelete, "/api/v1/session/credentials", token, view.CSRFToken, nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("lock failed: %d", response.Code)
	}
}

func TestAccountConfigurationUsesEncryptedVaultOnly(t *testing.T) {
	f := newAPIFixture(t)
	id, token, view := f.addUser(t, "imap@example.test")
	original, _, err := f.store.ReadUserFile(id, "accounts.enc")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/accounts", "/api/v1/smtp"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			target := path
			if method == http.MethodDelete {
				target += "/unused"
			}
			response := perform(f, method, target, token, view.CSRFToken, []byte(`{"name":"Personal","imap_host":"imap.example.test"}`))
			if response.Code != http.StatusNotFound {
				t.Fatalf("plaintext route %s %s returned %d", method, target, response.Code)
			}
		}
	}
	response := perform(f, http.MethodGet, "/api/v1/vault/files/accounts", token, "", nil)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), original) {
		t.Fatalf("encrypted accounts were changed or unreadable: %d", response.Code)
	}
	revision := response.Header().Get("X-Resource-Revision")
	if revision == "" || revision != response.Header().Get("ETag") || response.Header().Get("Cache-Control") != "no-store, no-transform" {
		t.Fatal("missing transport-independent revision or cache protection")
	}
	// Model a compression proxy weakening the transport ETag. The browser must
	// still send the exact persisted-file revision, not the transformed ETag.
	response.Header().Set("ETag", "W/"+revision)

	envelope := testEnvelope(t)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/vault/files/accounts", bytes.NewReader(envelope))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	req.Header.Set("X-CSRF-Token", view.CSRFToken)
	req.Header.Set("If-Match", response.Header().Get("X-Resource-Revision"))
	updated := httptest.NewRecorder()
	f.router.ServeHTTP(updated, req)
	if updated.Code != http.StatusNoContent {
		t.Fatalf("encrypted save failed: %d %s", updated.Code, updated.Body.String())
	}
	if updated.Header().Get("X-Resource-Revision") == "" || updated.Header().Get("X-Resource-Revision") != updated.Header().Get("ETag") {
		t.Fatal("save did not return its new resource revision")
	}
	stale := httptest.NewRequest(http.MethodPut, "/api/v1/vault/files/accounts", bytes.NewReader(testEnvelope(t)))
	stale.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	stale.Header.Set("X-CSRF-Token", view.CSRFToken)
	stale.Header.Set("If-Match", revision)
	conflict := httptest.NewRecorder()
	f.router.ServeHTTP(conflict, stale)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("stale resource revision accepted: %d", conflict.Code)
	}
	response = perform(f, http.MethodGet, "/api/v1/vault/files/accounts", token, "", nil)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), envelope) {
		t.Fatal("saved encrypted accounts did not round trip")
	}
}
