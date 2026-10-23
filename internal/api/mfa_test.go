package api

import (
	"encoding/base32"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/auth"
	"github.com/example/mail-aggregator/internal/config"
	"github.com/example/mail-aggregator/internal/mfa"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/google/uuid"
)

type mfaAuth struct{ result func() auth.Result }

func (mfaAuth) Available() bool { return true }
func (mfaAuth) RegistrationStart(string, string) (string, string, uuid.UUID, error) {
	return "", "", uuid.Nil, errors.New("unused")
}
func (mfaAuth) RegistrationFinish(string, auth.RegistrationPayload) (auth.Result, error) {
	return auth.Result{}, errors.New("unused")
}
func (mfaAuth) LoginStart(string, string, string, uuid.UUID) (string, string, error) {
	return "flow", "proof", nil
}
func (p mfaAuth) LoginFinish(string, string) (auth.Result, error) { return p.result(), nil }

func TestOPAQUESuccessDoesNotIssueCookieOrVaultAccessUntilMFA(t *testing.T) {
	f := newAPIFixture(t)
	id, current, _ := f.addUser(t, "mfa@example.test")
	_, _ = f.sessions.SetReceivingCredentials(current, map[string]session.Credential{"imap": {Secret: []byte("secret")}}, time.Minute, 0)
	factor, err := mfa.NewService(f.store, f.sessions, []byte("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(factor.Close)
	encoded, _, err := factor.StartEnrollment(id, current)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(encoded)
	codes, err := factor.ConfirmEnrollment(id, current, mfa.TOTP(secret, time.Now().Unix()/30, 6))
	if err != nil {
		t.Fatal(err)
	}
	provider := mfaAuth{result: func() auth.Result {
		token, view, _ := f.sessions.Create(id)
		return auth.Result{Token: token, View: view, UserID: id}
	}}
	router, err := NewRouter(Dependencies{Config: config.Config{MaxRequestBytes: 2 << 20, RequestTimeout: time.Minute}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Store: f.store, Users: f.users, Sessions: f.sessions, Auth: provider, MFA: factor})
	if err != nil {
		t.Fatal(err)
	}
	f.router = router
	response := perform(f, "POST", "/api/v1/auth/login/finish", "", "", []byte(`{"flow_id":"flow","finish_login_request":"proof"}`))
	if response.Code != 200 || len(response.Result().Cookies()) != 0 || strings.Contains(response.Body.String(), "csrf_token") {
		t.Fatalf("issued a session before MFA: %d %s", response.Code, response.Body.String())
	}
	var challenge struct {
		Required bool   `json:"mfa_required"`
		Flow     string `json:"flow_id"`
	}
	if json.Unmarshal(response.Body.Bytes(), &challenge) != nil || !challenge.Required || challenge.Flow == "" {
		t.Fatal("missing MFA challenge")
	}
	for _, path := range []string{"/api/v1/me", "/api/v1/vault/metadata", "/api/v1/vault/files/accounts"} {
		if response := perform(f, "GET", path, "", "", nil); response.Code != 401 {
			t.Fatalf("anonymous pending login accessed %s: %d", path, response.Code)
		}
	}
	for _, code := range []string{"invalid", codes[0]} {
		input, _ := json.Marshal(map[string]string{"flow_id": challenge.Flow, "code": code})
		response = perform(f, "POST", "/api/v1/auth/mfa/verify", "", "", input)
		if code == "invalid" {
			if response.Code != 401 || len(response.Result().Cookies()) != 0 {
				t.Fatal("invalid factor granted access")
			}
			continue
		}
		if response.Code != 200 || len(response.Result().Cookies()) != 1 {
			t.Fatalf("valid code rejected: %d %s", response.Code, response.Body.String())
		}
		cookie := response.Result().Cookies()[0]
		if me := perform(f, "GET", "/api/v1/me", cookie.Value, "", nil); me.Code != 200 {
			t.Fatalf("verified session unavailable: %d", me.Code)
		}
	}
	input, _ := json.Marshal(map[string]string{"flow_id": challenge.Flow, "code": codes[0]})
	if response := perform(f, "POST", "/api/v1/auth/mfa/verify", "", "", input); response.Code != 401 {
		t.Fatal("MFA login flow was reused")
	}
}
