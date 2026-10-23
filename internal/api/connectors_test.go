package api

import (
	"testing"
)

func TestOptionalConnectorsAreDisabledBeforeAnyProviderRequest(t *testing.T) {
	f := newAPIFixture(t)
	_, token, view := f.addUser(t, "connectors@example.test")
	for _, path := range []string{"/api/v1/sieve", "/api/v1/contacts/google"} {
		response := perform(f, "POST", path, token, view.CSRFToken, []byte(`{}`))
		if response.Code != 404 {
			t.Fatalf("disabled route %s: %d %s", path, response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing no-store")
		}
	}
	response := perform(f, "POST", "/api/v1/contacts/external", token, view.CSRFToken, []byte(`{"kind":"carddav","lock_revision":0}`))
	if response.Code != 404 {
		t.Fatalf("disabled directory route: %d %s", response.Code, response.Body.String())
	}
}
