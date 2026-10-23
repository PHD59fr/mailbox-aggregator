package api

import (
	"bytes"
	"context"
	"errors"
	"github.com/example/mail-aggregator/internal/session"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTester struct {
	calls int
	err   error
}

func (t *fakeTester) Test(_ context.Context, protocol string, cfg session.Credential) error {
	t.calls++
	if protocol != "imap" || string(cfg.Secret) != "probe-secret" {
		return errors.New("invalid test input")
	}
	return t.err
}
func TestConnectionTestLimitIsSharedByIPAndProtocols(t *testing.T) {
	tester := &fakeTester{}
	f := newAPIFixtureWithTester(t, nil, nil, tester)
	_, token, view := f.addUser(t, "probe@example.test")
	body := []byte(`{"protocol":"imap","host":"1.1.1.1","port":993,"security":"tls","username":"probe","password":"probe-secret"}`)
	if response := perform(f, "POST", "/api/v1/connections/test", token, "", body); response.Code != 403 {
		t.Fatal("probe bypassed CSRF")
	}
	response := perform(f, "POST", "/api/v1/connections/test", token, view.CSRFToken, body)
	if response.Code != 200 || tester.calls != 1 || strings.Contains(response.Body.String(), "probe-secret") {
		t.Fatalf("probe failed: %d %s", response.Code, response.Body.String())
	}
	body = []byte(strings.ReplaceAll(string(body), `"imap"`, `"smtp"`))
	response = perform(f, "POST", "/api/v1/connections/test", token, view.CSRFToken, body)
	if response.Code != 429 || response.Header().Get("Retry-After") != "30" || tester.calls != 1 {
		t.Fatalf("probe not IP-limited: %d %s", response.Code, response.Body.String())
	}
}
func TestConnectionTestLimiterWindowAndConcurrency(t *testing.T) {
	limiter := newConnectionTestLimiter()
	now := time.Now()
	limiter.now = func() time.Time { return now }
	var allowed atomic.Int32
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if limiter.allow("one-ip") == 0 {
				allowed.Add(1)
			}
		}()
	}
	workers.Wait()
	if allowed.Load() != 1 {
		t.Fatal("concurrent probes bypassed the limit")
	}
	now = now.Add(29 * time.Second)
	if limiter.allow("one-ip") != time.Second {
		t.Fatal("incorrect cooldown")
	}
	if limiter.allow("other-ip") != 0 {
		t.Fatal("one IP blocked another")
	}
	now = now.Add(time.Second)
	if limiter.allow("one-ip") != 0 {
		t.Fatal("cooldown did not expire")
	}
}
func TestConnectionTestDoesNotTrustSpoofedIPOrLeakProviderErrors(t *testing.T) {
	tester := &fakeTester{err: errors.New("provider password=probe-secret")}
	f := newAPIFixtureWithTester(t, nil, nil, tester)
	_, token, view := f.addUser(t, "probe@example.test")
	body := []byte(`{"protocol":"imap","host":"1.1.1.1","port":993,"security":"tls","username":"probe","password":"probe-secret"}`)
	response := perform(f, "POST", "/api/v1/connections/test", token, view.CSRFToken, body)
	if response.Code != 502 || strings.Contains(response.Body.String(), "probe-secret") {
		t.Fatal("provider details leaked")
	}
	// New authenticated sessions on the same IP do not reset its cooldown.
	_, second, secondView := f.addUser(t, "second-probe@example.test")
	response = perform(f, "POST", "/api/v1/connections/test", second, secondView.CSRFToken, body)
	if response.Code != 429 || tester.calls != 1 {
		t.Fatal("new session bypassed IP limit")
	}
	request := httptest.NewRequest("POST", "/api/v1/connections/test", bytes.NewReader(body))
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	request.Header.Set("X-CSRF-Token", view.CSRFToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-For", "203.0.113.99")
	spoof := httptest.NewRecorder()
	f.router.ServeHTTP(spoof, request)
	if spoof.Code != 429 || tester.calls != 1 {
		t.Fatal("spoofed IP header bypassed probe limit")
	}
}
