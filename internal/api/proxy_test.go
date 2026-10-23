package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/config"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/mailproxy"
	"github.com/example/mail-aggregator/internal/outbound"
)

func TestProxySettingsAreScopedRevisionProtectedAndSecretFree(t *testing.T) {
	f := newAPIFixture(t)
	idA, tokenA, viewA := f.addUser(t, "proxy-a@example.test")
	_, tokenB, viewB := f.addUser(t, "proxy-b@example.test")
	index, err := filestore.OpenUserIndex(f.store.IndexDirectory())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{IMAPProxyEnabled: true, SMTPProxyEnabled: true, MailProxyHostname: "mail.example.test", MaxRequestBytes: 2 << 20, RequestTimeout: time.Minute}
	policy := outbound.Policy{AllowedPorts: map[uint16]struct{}{993: {}, 465: {}}}
	proxy := mailproxy.New(cfg, f.store, index, f.users, policy, nil, nil)
	defer proxy.Close()
	router, err := NewRouter(Dependencies{Config: cfg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Store: f.store, Index: index, Users: f.users, Sessions: f.sessions, Auth: availableAuth{}, Outbound: policy, Proxy: proxy})
	if err != nil {
		t.Fatal(err)
	}
	f.router = router
	request := func(method, token, csrf, revision string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/proxy?user_id="+idA.String(), bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("If-Match", revision)
		if token != "" {
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		}
		response := httptest.NewRecorder()
		f.router.ServeHTTP(response, r)
		return response
	}
	if response := request("GET", "", "", "", nil); response.Code != 401 {
		t.Fatal("anonymous proxy settings access")
	}
	if response := request("GET", tokenA, "", "", nil); response.Code != 200 || response.Header().Get("ETag") != "*" || response.Header().Get("X-Resource-Revision") != "*" {
		t.Fatal("missing empty proxy status")
	}
	_, accountsRevision, err := f.store.ReadUserFile(idA, "accounts.enc")
	if err != nil {
		t.Fatal(err)
	}
	target := mailproxy.Target{ID: "a-mailbox", Host: "1.1.1.1", Port: 993, Security: "tls", Username: "provider-user", Password: "provider-secret"}
	body, _ := json.Marshal(map[string]any{"password": "separate-proxy-password-123", "imap": target, "accounts_revision": accountsRevision})
	if response := request("PUT", tokenA, "", "*", body); response.Code != 403 {
		t.Fatal("CSRF bypass")
	}
	if response := request("PUT", tokenA, viewA.CSRFToken, "", body); response.Code != 428 {
		t.Fatal("missing revision accepted")
	}
	if response := request("PUT", tokenA, viewA.CSRFToken, "*", body); response.Code != 204 {
		t.Fatalf("save failed: %d %s", response.Code, response.Body.String())
	}
	response := request("GET", tokenA, "", "", nil)
	revision := response.Header().Get("ETag")
	if response.Header().Get("X-Resource-Revision") != revision {
		t.Fatal("proxy settings lack an independent resource revision")
	}
	for _, secret := range []string{"separate-proxy-password-123", target.Password, target.Username, target.Host, "ciphertext", "salt"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatal("status disclosed proxy secret")
		}
	}
	if response.Header().Get("Cache-Control") != "no-store, no-transform" {
		t.Fatal("cacheable proxy response")
	}
	if response := request("GET", tokenB, "", "", nil); strings.Contains(response.Body.String(), target.ID) {
		t.Fatal("user B saw user A settings")
	}
	if response := request("DELETE", tokenB, viewB.CSRFToken, "*", nil); response.Code != 204 {
		t.Fatal("user B cannot disable own empty proxy")
	}
	status, _, err := proxy.Status(idA)
	if err != nil || status.IMAPID != target.ID {
		t.Fatal("user B disabled user A proxy")
	}
	if response := request("PUT", tokenA, viewA.CSRFToken, "*", body); response.Code != 409 {
		t.Fatal("stale revision accepted")
	}
	if response := request("DELETE", tokenA, viewA.CSRFToken, revision, nil); response.Code != 204 {
		t.Fatal("revocation failed")
	}
	status, _, err = proxy.Status(idA)
	if err != nil || status.IMAPID != "" {
		t.Fatal("proxy not disabled")
	}
}

func TestProxyIsUnavailableWithoutServerOptIn(t *testing.T) {
	f := newAPIFixture(t)
	_, token, view := f.addUser(t, "proxy@example.test")
	if response := perform(f, "GET", "/api/v1/proxy", token, "", nil); response.Code != 200 || strings.Contains(response.Body.String(), `"imap_available":true`) {
		t.Fatal("disabled server advertised gateway")
	}
	if response := perform(f, "PUT", "/api/v1/proxy", token, view.CSRFToken, []byte(`{}`)); response.Code != 409 {
		t.Fatal("proxy enabled without server opt-in")
	}
}
