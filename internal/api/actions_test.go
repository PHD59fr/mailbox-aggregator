package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/session"
)

func TestMailboxActionsCannotUseAnotherUsersCredentialsAndRequireCSRF(t *testing.T) {
	calls := 0
	check := func(cfg imap.Configuration) {
		calls++
		if cfg.Username != "owner" {
			t.Fatal("cross-user credentials")
		}
	}
	r := fakeReader{
		flag: func(_ context.Context, cfg imap.Configuration, uid, validity uint32, flag string, enabled bool) error {
			check(cfg)
			if uid != 8 || validity != 9 || flag != "flagged" || !enabled {
				t.Fatal("wrong flag reference")
			}
			return nil
		},
		transfer: func(_ context.Context, cfg imap.Configuration, uid, validity uint32, destination string, move bool) error {
			check(cfg)
			if uid != 8 || validity != 9 || destination != "Other" || !move {
				t.Fatal("wrong transfer reference")
			}
			return nil
		},
		manage: func(_ context.Context, cfg imap.Configuration, action, name, newName string) error {
			check(cfg)
			if action != "delete" || name != "Other" {
				t.Fatal("wrong folder reference")
			}
			return nil
		},
	}
	f := newAPIFixtureWithReader(t, nil, r)
	_, owner, view := f.addUser(t, "actions-owner@example.test")
	_, other, otherView := f.addUser(t, "actions-other@example.test")
	_, _ = f.sessions.SetReceivingCredentials(owner, map[string]session.Credential{"same-id": {Username: "owner", Secret: []byte("owner secret")}}, time.Minute, 0)
	for _, test := range []struct {
		path string
		body any
	}{
		{"/api/v1/mailboxes/same-id/messages/8/flag?uid_validity=9", map[string]any{"flag": "flagged", "enabled": true}},
		{"/api/v1/mailboxes/same-id/messages/8/transfer?uid_validity=9", map[string]any{"destination": "Other", "move": true}},
		{"/api/v1/mailboxes/same-id/folders", map[string]any{"action": "delete", "name": "Other", "confirmation": "Other"}},
	} {
		body, _ := json.Marshal(test.body)
		if got := perform(f, "POST", test.path, owner, "", body); got.Code != 403 {
			t.Fatalf("CSRF bypass %d", got.Code)
		}
		if got := perform(f, "POST", test.path, other, otherView.CSRFToken, body); got.Code != 423 {
			t.Fatalf("cross-user access %d %s", got.Code, got.Body.String())
		}
		if calls != 0 {
			t.Fatal("unauthorized provider access")
		}
		if got := perform(f, "POST", test.path, owner, view.CSRFToken, body); got.Code != 204 {
			t.Fatalf("owner failure %d %s", got.Code, got.Body.String())
		}
		calls = 0
	}
	bad := perform(f, "POST", "/api/v1/mailboxes/same-id/folders", owner, view.CSRFToken, []byte(`{"action":"delete","name":"Other","confirmation":"wrong"}`))
	if bad.Code != 400 || calls != 0 {
		t.Fatal("folder deletion without exact confirmation")
	}
	bad = perform(f, "POST", "/api/v1/mailboxes/same-id/messages/8/flag?uid_validity=9", owner, view.CSRFToken, []byte(`{"flag":"deleted","enabled":true}`))
	if bad.Code != 400 || calls != 0 {
		t.Fatal("arbitrary destructive flag accepted")
	}
}
