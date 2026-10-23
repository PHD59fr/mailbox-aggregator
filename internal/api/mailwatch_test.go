package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/google/uuid"
)

type fakeMailWatcher struct{ closed chan struct{} }

func (w *fakeMailWatcher) Watch(ctx context.Context, _ imap.Configuration, _ time.Duration, emit func(imap.Status)) {
	emit(imap.Status{Messages: 3, Unseen: 2, UIDNext: 4, UIDValidity: 1, Mode: "idle"})
	<-ctx.Done()
	close(w.closed)
}

func TestMailWatchIsSessionBoundAndStopsOnLock(t *testing.T) {
	watcher := &fakeMailWatcher{closed: make(chan struct{})}
	f := newAPIFixtureWithWatcher(t, watcher)
	_, token, view := f.addUser(t, "receiver@example.test")
	_, other, otherView := f.addUser(t, "other@example.test")
	_, err := f.sessions.SetReceivingCredentials(token, map[string]session.Credential{"receiver": {Host: "imap.example.test", Port: 993, Security: "tls", Username: "test", Secret: []byte("test-secret")}}, time.Minute, 0)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(f.router)
	defer server.Close()
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/mailboxes/watch", strings.NewReader(`{"refresh_interval_seconds":30}`))
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	request.Header.Set("X-CSRF-Token", view.CSRFToken)
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("watch returned %d", response.StatusCode)
	}
	id := response.Header.Get("X-Watch-ID")
	if id == "" {
		t.Fatal("missing presence ID")
	}
	decoder := json.NewDecoder(response.Body)
	var event map[string]any
	if decoder.Decode(&event) != nil || event["type"] != "ready" {
		t.Fatal("missing ready event")
	}
	if decoder.Decode(&event) != nil || event["account_id"] != "receiver" || event["unseen"] != float64(2) {
		t.Fatalf("missing status: %v", event)
	}
	body := []byte(`{"watch_id":"` + id + `"}`)
	if result := perform(f, http.MethodPost, "/api/v1/mailboxes/presence", other, otherView.CSRFToken, body); result.Code != 404 {
		t.Fatal("another session refreshed this lease")
	}
	if result := perform(f, http.MethodPost, "/api/v1/mailboxes/presence", token, "", body); result.Code != 403 {
		t.Fatal("presence bypassed CSRF")
	}
	if result := perform(f, http.MethodPost, "/api/v1/mailboxes/presence", token, view.CSRFToken, body); result.Code != 204 {
		t.Fatalf("owner presence failed: %s", result.Body.String())
	}
	if result := perform(f, http.MethodDelete, "/api/v1/session/credentials", token, view.CSRFToken, nil); result.Code != 204 {
		t.Fatal("lock failed")
	}
	select {
	case <-watcher.closed:
	case <-time.After(time.Second):
		t.Fatal("locking did not close upstream watch")
	}
}

func TestWatchRegistryPresenceAndLimits(t *testing.T) {
	registry := newWatchRegistry(30)
	user := uuid.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id, ok := registry.startWatch("owner", user, 20, cancel)
	if !ok {
		t.Fatal("initial lease rejected")
	}
	if _, ok := registry.startWatch("owner", user, 11, func() {}); ok {
		t.Fatal("per-user connection bound exceeded")
	}
	registry.mu.Lock()
	registry.streams[id].seen = time.Now().Add(-46 * time.Second)
	registry.mu.Unlock()
	if registry.alive(id) {
		t.Fatal("missing browser presence kept the watch alive")
	}
	if registry.touch("other", id) {
		t.Fatal("another session could renew the lease")
	}
	if !registry.touch("owner", id) || !registry.alive(id) {
		t.Fatal("owner could not renew its lease")
	}
	registry.stopSession("other")
	if ctx.Err() != nil {
		t.Fatal("another session cancelled this watch")
	}
	registry.stopSession("owner")
	if ctx.Err() == nil {
		t.Fatal("session cancellation failed")
	}
	registry.remove(id)
}

func TestMailWatchRejectsLockedAndInvalidRequests(t *testing.T) {
	f := newAPIFixture(t)
	_, token, view := f.addUser(t, "locked@example.test")
	for _, test := range []struct {
		body   string
		status int
	}{{`{"refresh_interval_seconds":1}`, 400}, {`{"refresh_interval_seconds":301}`, 400}, {`{"refresh_interval_seconds":30}`, 423}} {
		if response := perform(f, http.MethodPost, "/api/v1/mailboxes/watch", token, view.CSRFToken, []byte(test.body)); response.Code != test.status {
			t.Fatalf("got %d, want %d", response.Code, test.status)
		}
	}
}

func TestFullMailboxWatchCannotConsumeInteractiveReserve(t *testing.T) {
	registry := newWatchRegistry(20)
	user := uuid.New()
	id, ok := registry.startWatch("owner", user, 20, func() {})
	if !ok {
		t.Fatal("allowed mailboxes cannot all be watched")
	}
	defer registry.remove(id)
	for i := 0; i < 4; i++ {
		if _, ok := registry.start("owner", user, 1, func() {}); !ok {
			t.Fatal("watch consumed interactive reserve")
		}
	}
	if _, ok := registry.start("other-tab", user, 1, func() {}); ok {
		t.Fatal("interactive per-user budget exceeded")
	}
	if _, ok := registry.startWatch("other-tab", user, 1, func() {}); ok {
		t.Fatal("watch budget exceeded by another tab")
	}
	if _, ok := registry.start("other-user", uuid.New(), 1, func() {}); !ok {
		t.Fatal("another user lost its reserve")
	}
}
