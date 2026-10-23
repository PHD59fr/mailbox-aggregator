package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/session"
	"github.com/google/uuid"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T) (*Service, string) {
	t.Helper()
	sessions, err := session.NewManager(time.Hour, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sessions.Create(uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	s := New(Config{PublicURL: "https://mail.example.test", Google: ProviderConfig{ClientID: "google-client", ClientSecret: "private-client-secret"}, Microsoft: ProviderConfig{ClientID: "microsoft-client"}}, sessions, nil)
	t.Cleanup(s.Close)
	return s, owner
}
func TestAuthorizationUsesPKCEAndBindsResultToOriginalSession(t *testing.T) {
	s, owner := fixture(t)
	authorization, err := s.Begin(owner, "google", "user@example.test", 0)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(authorization)
	params := parsed.Query()
	state := params.Get("state")
	if parsed.Host != "accounts.google.com" || state == "" || params.Get("code_challenge_method") != "S256" || params.Get("access_type") != "offline" || strings.Contains(authorization, "private-client-secret") {
		t.Fatal("unsafe authorization URL")
	}
	s.client.Transport = roundTrip(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://oauth2.googleapis.com/token" || request.Method != "POST" {
			t.Fatal("unexpected token endpoint")
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(request.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(digest[:]) != params.Get("code_challenge") || request.Form.Get("redirect_uri") != s.Callback("google") || request.Form.Get("code") != "provider-code" {
			t.Fatal("PKCE or callback binding lost")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"access","refresh_token":"refresh","expires_in":3600,"token_type":"Bearer"}`)), Header: make(http.Header)}, nil
	})
	if err = s.Complete(context.Background(), "microsoft", state, "provider-code", false); err == nil {
		t.Fatal("provider mix-up accepted")
	}
	if err = s.Complete(context.Background(), "google", state, "provider-code", false); err != nil {
		t.Fatal(err)
	}
	other, _, _ := s.sessions.Create(uuid.New())
	if _, err = s.Take(other, state); err == nil {
		t.Fatal("another session could take tokens")
	}
	result, err := s.Take(owner, state)
	if err != nil || result.RefreshToken != "refresh" || result.Email != "user@example.test" {
		t.Fatalf("missing tokens: %#v %v", result, err)
	}
	if _, err = s.Take(owner, state); err == nil {
		t.Fatal("authorization result was reusable")
	}
}
func TestExpiredOrLockedOAuthFlowsNeverReturnTokens(t *testing.T) {
	s, owner := fixture(t)
	authorization, _ := s.Begin(owner, "google", "user@example.test", 0)
	parsed, _ := url.Parse(authorization)
	state := parsed.Query().Get("state")
	if _, err := s.sessions.Lock(owner); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(context.Background(), "google", state, "provider-code", false); err == nil {
		t.Fatal("a locked flow exchanged tokens")
	}
	if _, err := s.Take(owner, state); err == nil {
		t.Fatal("a locked flow returned tokens")
	}
}
func TestRefreshIsBoundedAndDoesNotExposeProviderErrorBody(t *testing.T) {
	s, owner := fixture(t)
	s.client.Transport = roundTrip(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "oauth2.googleapis.com" {
			t.Fatal("unexpected provider")
		}
		_ = request.ParseForm()
		if request.Form.Get("grant_type") != "refresh_token" {
			t.Fatal("not a refresh request")
		}
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error_description":"private-refresh-token"}`)), Header: make(http.Header)}, nil
	})
	if _, err := s.Refresh(context.Background(), owner, "google", "refresh", 0); err == nil || strings.Contains(err.Error(), "private-refresh-token") {
		t.Fatal("provider error was exposed")
	}
	if _, err := s.Refresh(context.Background(), owner, "untrusted", "refresh", 0); err == nil {
		t.Fatal("arbitrary provider accepted")
	}
}
