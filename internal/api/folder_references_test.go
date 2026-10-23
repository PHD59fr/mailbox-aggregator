package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/session"
)

func folderIDQuery(t *testing.T, token, account, name string) string {
	t.Helper()
	id, err := encodeFolderReference(token, account, name)
	if err != nil {
		t.Fatal(err)
	}
	return "&folder_id=" + id
}
func TestOpaqueFolderReferencesAreRandomizedAndBoundToSessionAndAccount(t *testing.T) {
	name := "Personal/été & medical"
	id, err := encodeFolderReference("session-a", "account-a", name)
	if err != nil {
		t.Fatal(err)
	}
	again, err := encodeFolderReference("session-a", "account-a", name)
	if err != nil || again == id || strings.Contains(id, "Personal") {
		t.Fatal("references are predictable or expose a name")
	}
	if decoded, err := decodeFolderReference("session-a", "account-a", id); err != nil || decoded != name {
		t.Fatal("folder reference did not resolve")
	}
	for _, input := range []struct{ token, account, id string }{{"session-b", "account-a", id}, {"session-a", "account-b", id}, {"session-a", "account-a", id[:len(id)-4] + "AAAA"}, {"session-a", "account-a", "Personal"}, {"session-a", "account-a", strings.Repeat("x", 2000)}} {
		if _, err := decodeFolderReference(input.token, input.account, input.id); err == nil {
			t.Fatal("foreign, forged or plaintext folder reference accepted")
		}
	}
	short, _ := encodeFolderReference("session-a", "account-a", "Secret")
	other, _ := encodeFolderReference("session-a", "account-a", "Another private folder")
	if len(short) != len(other) {
		t.Fatal("short folder names are not length padded")
	}
}

func TestFolderReferenceEndpointAndPrivateSearch(t *testing.T) {
	calls := 0
	reader := fakeReader{list: func(_ context.Context, cfg imap.Configuration, _, _ uint32) (imap.MessagePage, error) {
		calls++
		if cfg.Mailbox != "Private/medical" || cfg.Search != "a private search" || cfg.From != "sender@example.test" {
			t.Fatal("private read scope lost")
		}
		return imap.MessagePage{}, nil
	}}
	f := newAPIFixtureWithReader(t, nil, reader)
	_, token, view := f.addUser(t, "folder-private@example.test")
	_, _ = f.sessions.SetReceivingCredentials(token, map[string]session.Credential{"own": {Username: "own", Secret: []byte("secret")}}, time.Minute, 0)
	path := "/api/v1/mailboxes/own/folder-references"
	if result := perform(f, "POST", path, token, "", []byte(`{"name":"Private/medical"}`)); result.Code != 403 {
		t.Fatal("reference creation lacks CSRF protection")
	}
	result := perform(f, "POST", path, token, view.CSRFToken, []byte(`{"name":"Private/medical"}`))
	var ref struct {
		ID string `json:"id"`
	}
	if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &ref) != nil || ref.ID == "" || strings.Contains(result.Body.String(), "medical") {
		t.Fatalf("invalid reference response: %d", result.Code)
	}
	result = perform(f, "POST", "/api/v1/mailboxes/own/messages/search?folder_id="+ref.ID, token, view.CSRFToken, []byte(`{"search":"a private search","from":"sender@example.test"}`))
	if result.Code != 200 || calls != 1 {
		t.Fatalf("private search failed: %d %s", result.Code, result.Body.String())
	}
	for _, query := range []string{"folder=Private%2Fmedical", "folder_id=Private%2Fmedical", "folder_id=" + strings.Repeat("x", 1600)} {
		if result := perform(f, "GET", "/api/v1/mailboxes/own/messages?"+query, token, "", nil); result.Code != 400 {
			t.Fatal("nonopaque folder selection accepted")
		}
	}
	if calls != 1 {
		t.Fatal("invalid reference reached IMAP")
	}
}
