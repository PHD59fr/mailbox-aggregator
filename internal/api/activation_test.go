package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/outbound"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/gin-gonic/gin"
)

type activationResolver struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *activationResolver) LookupNetIP(ctx context.Context, _, _ string) ([]netip.Addr, error) {
	r.once.Do(func() { close(r.entered) })
	select {
	case <-r.release:
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestPendingActivationCannotUndoLockOrLogout(t *testing.T) {
	for _, group := range []string{"accounts", "smtp", "both"} {
		for _, action := range []string{"lock", "logout"} {
			t.Run(group+"/"+action, func(t *testing.T) {
				f := newAPIFixture(t)
				id, token, view := f.addUser(t, "activation@example.test")
				otherToken, otherView, _ := f.sessions.Create(id)
				resolver := &activationResolver{entered: make(chan struct{}), release: make(chan struct{})}
				h := handlers{deps: Dependencies{Sessions: f.sessions, watches: newWatchRegistry(20), Outbound: outbound.Policy{Resolver: resolver, AllowedPorts: map[uint16]struct{}{993: {}}}}}
				router := gin.New()
				router.Use(authenticate(f.sessions, f.users.IsActive), requireCSRF(f.sessions))
				router.POST("/credentials", h.activateCredentials)
				router.DELETE("/credentials", h.lock)
				router.POST("/logout", h.logout)
				f.router = router
				credential := `{"own":{"host":"imap.example.test","port":993,"security":"tls","username":"own","password":"synthetic"}}`
				payload := map[string]any{"lock_revision": 0, "lifetime_seconds": 300}
				if group != "smtp" {
					payload["accounts"] = json.RawMessage(credential)
				}
				if group != "accounts" {
					payload["smtp"] = json.RawMessage(credential)
				}
				body, _ := json.Marshal(payload)
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { done <- perform(f, "POST", "/credentials", token, view.CSRFToken, body) }()
				select {
				case <-resolver.entered:
				case <-time.After(time.Second):
					t.Fatal("activation never reached DNS")
				}
				path, method := "/credentials", "DELETE"
				if action == "logout" {
					path, method = "/logout", "POST"
				}
				locked := perform(f, method, path, token, view.CSRFToken, nil)
				close(resolver.release)
				if locked.Code != 204 {
					t.Fatalf("%s: %d %s", action, locked.Code, locked.Body.String())
				}
				var result *httptest.ResponseRecorder
				select {
				case result = <-done:
				case <-time.After(time.Second):
					t.Fatal("activation did not finish")
				}
				if action == "logout" {
					if result.Code != 401 {
						t.Fatalf("activation after logout: %d %s", result.Code, result.Body.String())
					}
				} else {
					if result.Code != 409 || !strings.Contains(result.Body.String(), "LOCK_REVISION_CONFLICT") {
						t.Fatalf("stale activation: %d %s", result.Code, result.Body.String())
					}
					if locked.Header().Get("X-Lock-Revision") != "1" {
						t.Fatal("lock response did not identify its revision")
					}
					lockedView, _ := f.sessions.Peek(token)
					if lockedView.State != session.AuthenticatedLocked {
						t.Fatal("pending activation unlocked the session")
					}
					// An old tab reaching the server after the lock is rejected too.
					if r := perform(f, "POST", "/credentials", token, view.CSRFToken, body); r.Code != 409 {
						t.Fatalf("old tab activated after lock: %d", r.Code)
					}
					payload["lock_revision"] = 1
					fresh, _ := json.Marshal(payload)
					if r := perform(f, "POST", "/credentials", token, view.CSRFToken, fresh); r.Code != 200 {
						t.Fatalf("fresh activation: %d %s", r.Code, r.Body.String())
					}
				}
				if r := perform(f, "POST", "/credentials", otherToken, otherView.CSRFToken, body); r.Code != 200 {
					t.Fatalf("independent session was affected: %d %s", r.Code, r.Body.String())
				}
			})
		}
	}
}

func TestActivationRequiresExplicitValidLockRevision(t *testing.T) {
	f := newAPIFixture(t)
	_, token, view := f.addUser(t, "revision@example.test")
	for _, field := range []string{"", `"lock_revision":null,`, `"lock_revision":-1,`, `"lock_revision":"0",`} {
		body := []byte(fmt.Sprintf(`{%s"lifetime_seconds":300,"accounts":{}}`, field))
		if r := perform(f, "POST", "/api/v1/session/credentials", token, view.CSRFToken, body); r.Code != 400 {
			t.Fatalf("invalid revision accepted: %d %s", r.Code, r.Body.String())
		}
	}
}

func TestTargetedHTTPActivationsPreserveOtherReceivingAccounts(t *testing.T) {
	f := newAPIFixtureWithReader(t, nil, fakeReader{list: func(context.Context, imap.Configuration, uint32, uint32) (imap.MessagePage, error) {
		return imap.MessagePage{}, nil
	}})
	_, token, view := f.addUser(t, "merge@example.test")
	for _, id := range []string{"A", "B"} {
		body := []byte(fmt.Sprintf(`{"mode":"merge","lock_revision":0,"lifetime_seconds":60,"accounts":{"%s":{"host":"8.8.8.8","port":993,"security":"tls","username":"synthetic","password":"synthetic"}}}`, id))
		r := perform(f, "POST", "/api/v1/session/credentials", token, view.CSRFToken, body)
		if r.Code != 200 {
			t.Fatalf("activation failed: %d", r.Code)
		}
	}
	for _, id := range []string{"A", "B"} {
		r := perform(f, "GET", "/api/v1/mailboxes/"+id+"/messages", token, "", nil)
		if r.Code != 200 {
			t.Fatalf("activation removed %s: %d", id, r.Code)
		}
	}
}
