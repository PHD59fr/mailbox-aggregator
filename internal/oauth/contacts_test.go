package oauth

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestContactsConsentAndReadOnlyPagination(t *testing.T) {
	s, owner := fixture(t)
	if _, err := s.BeginWithContacts(owner, "google", "alice@example.test", 0, true); err == nil {
		t.Fatal("contacts consent enabled without administrator configuration")
	}
	s.config.GoogleContacts = true
	authorization, err := s.BeginWithContacts(owner, "google", "alice@example.test", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authorization)
	if !strings.Contains(u.Query().Get("scope"), ContactsScope) {
		t.Fatal("missing read-only consent")
	}
	s.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Host != "people.googleapis.com" || r.URL.Path != "/v1/people/me/connections" || r.URL.Query().Get("pageToken") != "next page" || r.URL.Query().Get("personFields") != "names,emailAddresses" || r.Header.Get("Authorization") != "Bearer access" {
			t.Fatal("unexpected contacts request", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"connections":[{"names":[{"displayName":"Alice"}],"emailAddresses":[{"value":"alice@example.test"},{"value":"invalid"}]}],"nextPageToken":"another-page"}`))}, nil
	})
	page, err := s.Contacts(context.Background(), owner, "access", "next page", 0)
	if err != nil || len(page.Contacts) != 1 || page.NextPage != "another-page" {
		t.Fatal(page, err)
	}
	s.sessions.Lock(owner)
	if _, err := s.Contacts(context.Background(), owner, "access", "next page", 0); err == nil {
		t.Fatal("read after explicit lock")
	}
}
