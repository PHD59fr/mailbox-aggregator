package api

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/session"
)

type fakeReader struct {
	flag       func(context.Context, imap.Configuration, uint32, uint32, string, bool) error
	transfer   func(context.Context, imap.Configuration, uint32, uint32, string, bool) error
	manage     func(context.Context, imap.Configuration, string, string, string) error
	sent       func(context.Context, imap.Configuration, []byte, string, time.Time) error
	trash      func(context.Context, imap.Configuration, uint32, uint32) error
	folders    func(context.Context, imap.Configuration) (imap.FolderPage, error)
	list       func(context.Context, imap.Configuration, uint32, uint32) (imap.MessagePage, error)
	read       func(context.Context, imap.Configuration, uint32, uint32) (imap.Message, error)
	attachment func(context.Context, imap.Configuration, uint32, uint32, string) (imap.Download, error)
	source     func(context.Context, imap.Configuration, uint32, uint32) ([]byte, error)
	seen       func(context.Context, imap.Configuration, uint32, uint32, bool) error
}

func (r fakeReader) SetFlag(ctx context.Context, cfg imap.Configuration, uid, validity uint32, flag string, enabled bool) error {
	if r.flag == nil {
		return errors.New("unavailable")
	}
	return r.flag(ctx, cfg, uid, validity, flag, enabled)
}
func (r fakeReader) Transfer(ctx context.Context, cfg imap.Configuration, uid, validity uint32, destination string, move bool) error {
	if r.transfer == nil {
		return errors.New("unavailable")
	}
	return r.transfer(ctx, cfg, uid, validity, destination, move)
}
func (r fakeReader) ManageFolder(ctx context.Context, cfg imap.Configuration, action, name, newName string) error {
	if r.manage == nil {
		return errors.New("unavailable")
	}
	return r.manage(ctx, cfg, action, name, newName)
}

func (r fakeReader) SaveSent(ctx context.Context, cfg imap.Configuration, data []byte, messageID string, date time.Time) error {
	if r.sent == nil {
		return imap.ErrSentUnavailable
	}
	return r.sent(ctx, cfg, data, messageID, date)
}

func (r fakeReader) Trash(ctx context.Context, cfg imap.Configuration, uid, validity uint32) error {
	if r.trash == nil {
		return errors.New("unavailable")
	}
	return r.trash(ctx, cfg, uid, validity)
}

func TestTrashIsSessionScopedCSRFProtectedAndUsesExactFolderReference(t *testing.T) {
	var upstream error
	calls := 0
	reader := fakeReader{trash: func(_ context.Context, cfg imap.Configuration, uid, validity uint32) error {
		calls++
		if cfg.Username != "own" || cfg.Mailbox != "Archive" || uid != 12 || validity != 7 {
			t.Error("incorrect trash scope")
		}
		return upstream
	}}
	f := newAPIFixtureWithReader(t, nil, reader)
	_, token, view := f.addUser(t, "trash@example.test")
	_, _ = f.sessions.SetReceivingCredentials(token, map[string]session.Credential{"own": {Username: "own", Secret: []byte("secret")}}, time.Minute, 0)
	path := "/api/v1/mailboxes/own/messages/12/trash?uid_validity=7" + folderIDQuery(t, token, "own", "Archive")
	if response := perform(f, "POST", path, token, "", nil); response.Code != 403 {
		t.Fatal("CSRF was not required")
	}
	if calls != 0 {
		t.Fatal("upstream called without CSRF")
	}
	for _, test := range []struct {
		err    error
		status int
	}{{nil, 204}, {imap.ErrTrashUnavailable, 422}, {imap.ErrMoveUnsupported, 422}, {imap.ErrTrashUnconfirmed, 502}, {imap.ErrAlreadyInTrash, 409}, {imap.ErrMailboxChanged, 409}} {
		upstream = test.err
		response := perform(f, "POST", path, token, view.CSRFToken, nil)
		if response.Code != test.status {
			t.Fatalf("trash status: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestManualTrashTargetIsValidatedAndScopedToThisOperation(t *testing.T) {
	calls := 0
	reader := fakeReader{trash: func(_ context.Context, cfg imap.Configuration, uid, validity uint32) error {
		calls++
		if cfg.TrashMailbox != "Personal/été & deleted" || len(cfg.TrashExcluded) != 1 || cfg.TrashExcluded[0] != "Sent" || cfg.Mailbox != "Archive" || cfg.Username != "own" || uid != 12 || validity != 7 {
			t.Fatal("incorrect manual Trash scope")
		}
		return nil
	}, list: func(_ context.Context, cfg imap.Configuration, _, _ uint32) (imap.MessagePage, error) {
		if cfg.TrashMailbox != "" {
			t.Fatal("manual Trash target leaked into a read operation")
		}
		return imap.MessagePage{}, nil
	}}
	f := newAPIFixtureWithReader(t, nil, reader)
	_, token, view := f.addUser(t, "manual-trash@example.test")
	_, _ = f.sessions.SetReceivingCredentials(token, map[string]session.Credential{"own": {Username: "own", Secret: []byte("secret")}}, time.Minute, 0)
	base := "/api/v1/mailboxes/own/messages/12/trash?uid_validity=7" + folderIDQuery(t, token, "own", "Archive") + "&trash_folder="
	for _, target := range []string{"inbox", "Bad\r\nfolder", "bad\x00folder", strings.Repeat("é", 513), string([]byte{0xff})} {
		if response := perform(f, "POST", base+url.QueryEscape(target), token, view.CSRFToken, nil); response.Code != 400 {
			t.Fatalf("unsafe target accepted: %d", response.Code)
		}
	}
	if calls != 0 {
		t.Fatal("unsafe target reached IMAP")
	}
	for _, query := range []string{"&trash_exclude=INBOX", "&trash_exclude=bad%0Afolder", strings.Repeat("&trash_exclude=Sent", 7)} {
		if response := perform(f, "POST", base+url.QueryEscape("Personal/été & deleted")+query, token, view.CSRFToken, nil); response.Code != 400 {
			t.Fatal("invalid exclusions accepted")
		}
	}
	body, _ := json.Marshal(map[string]any{"trash_folder": "Personal/été & deleted", "trash_exclude": []string{"Sent"}})
	if response := perform(f, "POST", "/api/v1/mailboxes/own/messages/12/trash?uid_validity=7"+folderIDQuery(t, token, "own", "Archive"), token, view.CSRFToken, body); response.Code != 204 {
		t.Fatalf("manual target failed: %d %s", response.Code, response.Body.String())
	}
	if calls != 1 {
		t.Fatal("wrong number of upstream calls")
	}
	if response := perform(f, "GET", "/api/v1/mailboxes/own/messages?trash_folder=Other", token, "", nil); response.Code != 200 {
		t.Fatal("subsequent read failed")
	}
}

func (r fakeReader) Folders(ctx context.Context, cfg imap.Configuration) (imap.FolderPage, error) {
	if r.folders == nil {
		return imap.FolderPage{}, errors.New("unavailable")
	}
	return r.folders(ctx, cfg)
}

func TestFoldersAndMessageOperationsUseSelectedFolder(t *testing.T) {
	check := func(cfg imap.Configuration) {
		if cfg.Mailbox != "Sent/été & work" || cfg.Username != "own-user" {
			t.Errorf("incorrect folder scope: %#v", cfg)
		}
	}
	reader := fakeReader{
		folders: func(_ context.Context, cfg imap.Configuration) (imap.FolderPage, error) {
			if cfg.Username != "own-user" {
				t.Error("wrong folder credentials")
			}
			return imap.FolderPage{Folders: []imap.Folder{{Name: "Sent/été & work", Selectable: true}}}, nil
		},
		list: func(_ context.Context, cfg imap.Configuration, _, _ uint32) (imap.MessagePage, error) {
			check(cfg)
			return imap.MessagePage{}, nil
		},
		read: func(_ context.Context, cfg imap.Configuration, _, _ uint32) (imap.Message, error) {
			check(cfg)
			return imap.Message{}, nil
		},
		source: func(_ context.Context, cfg imap.Configuration, _, _ uint32) ([]byte, error) {
			check(cfg)
			return []byte("original"), nil
		},
		seen: func(_ context.Context, cfg imap.Configuration, _, _ uint32, _ bool) error { check(cfg); return nil },
		attachment: func(_ context.Context, cfg imap.Configuration, _, _ uint32, _ string) (imap.Download, error) {
			check(cfg)
			return imap.Download{Filename: "file"}, nil
		},
	}
	f := newAPIFixtureWithReader(t, nil, reader)
	_, token, view := f.addUser(t, "folders@example.test")
	path := "/api/v1/mailboxes/own/folders"
	if response := perform(f, "GET", path, token, "", nil); response.Code != 423 {
		t.Fatal("folders available without credentials")
	}
	_, _ = f.sessions.SetReceivingCredentials(token, map[string]session.Credential{"own": {Username: "own-user", Secret: []byte("secret")}}, time.Minute, 0)
	if response := perform(f, "GET", path, token, "", nil); response.Code != 200 || !strings.Contains(response.Body.String(), "Sent/été") || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("folder discovery failed: %d %s", response.Code, response.Body.String())
	}
	query := "?uid_validity=7" + folderIDQuery(t, token, "own", "Sent/été & work")
	for _, suffix := range []string{"", "/12", "/12/document", "/12/source", "/12/attachments/2", "/12/seen", "/12/unseen"} {
		method := "GET"
		if strings.HasSuffix(suffix, "/seen") || strings.HasSuffix(suffix, "/unseen") {
			method = "POST"
		}
		response := perform(f, method, "/api/v1/mailboxes/own/messages"+suffix+query, token, view.CSRFToken, nil)
		if response.Code != 200 && response.Code != 204 {
			t.Fatalf("folder operation %s failed: %d %s", suffix, response.Code, response.Body.String())
		}
	}
	for _, folder := range []string{"bad%0D%0Aname", "bad%00name", "%FF", strings.Repeat("x", 1025)} {
		if response := perform(f, "GET", "/api/v1/mailboxes/own/messages?folder="+folder, token, "", nil); response.Code != 400 {
			t.Fatalf("unsafe folder accepted: %d", response.Code)
		}
	}
}

func (r fakeReader) Source(ctx context.Context, cfg imap.Configuration, uid, validity uint32) ([]byte, error) {
	if r.source == nil {
		return nil, errors.New("unavailable")
	}
	return r.source(ctx, cfg, uid, validity)
}
func (r fakeReader) MarkSeen(ctx context.Context, cfg imap.Configuration, uid, validity uint32) error {
	if r.seen == nil {
		return errors.New("unavailable")
	}
	return r.seen(ctx, cfg, uid, validity, true)
}
func (r fakeReader) MarkUnseen(ctx context.Context, cfg imap.Configuration, uid, validity uint32) error {
	if r.seen == nil {
		return errors.New("unavailable")
	}
	return r.seen(ctx, cfg, uid, validity, false)
}

func (r fakeReader) List(ctx context.Context, cfg imap.Configuration, before, validity uint32) (imap.MessagePage, error) {
	return r.list(ctx, cfg, before, validity)
}
func (r fakeReader) Read(ctx context.Context, cfg imap.Configuration, uid, validity uint32) (imap.Message, error) {
	return r.read(ctx, cfg, uid, validity)
}
func (r fakeReader) Attachment(ctx context.Context, cfg imap.Configuration, uid, validity uint32, part string) (imap.Download, error) {
	return r.attachment(ctx, cfg, uid, validity, part)
}

func TestReadingUsesOnlyOwnActivatedReceivingIdentity(t *testing.T) {
	reader := fakeReader{list: func(_ context.Context, cfg imap.Configuration, before, validity uint32) (imap.MessagePage, error) {
		if cfg.Username != "own-user" || string(cfg.Password) != "own-secret" || before != 20 || validity != 7 {
			t.Error("incorrect scoped credentials or page anchor")
		}
		return imap.MessagePage{Messages: []imap.MessageSummary{{UID: 19, Subject: "Hello"}}, UIDValidity: 7}, nil
	}}
	f := newAPIFixtureWithReader(t, nil, reader)
	_, token, _ := f.addUser(t, "reader@example.test")
	_, other, _ := f.addUser(t, "other-reader@example.test")
	path := "/api/v1/mailboxes/own/messages?before=20&uid_validity=7"
	if response := perform(f, "GET", path, token, "", nil); response.Code != 423 {
		t.Fatal("locked reading allowed")
	}
	_, err := f.sessions.SetReceivingCredentials(token, map[string]session.Credential{"own": {Username: "own-user", Secret: []byte("own-secret")}}, time.Minute, 0)
	if err != nil {
		t.Fatal(err)
	}
	if response := perform(f, "GET", path, "", "", nil); response.Code != 401 {
		t.Fatal("anonymous reading allowed")
	}
	if response := perform(f, "GET", path, other, "", nil); response.Code != 423 {
		t.Fatal("cross-session credentials exposed")
	}
	if response := perform(f, "GET", "/api/v1/mailboxes/not-own/messages", token, "", nil); response.Code != 423 {
		t.Fatal("unknown mailbox allowed")
	}
	response := perform(f, "GET", path, token, "", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "Hello") {
		t.Fatalf("list failed: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "own-secret") {
		t.Fatal("mail or credentials may be cached/leaked")
	}
	for _, path := range []string{"/api/v1/mailboxes/own/messages?before=20", "/api/v1/mailboxes/own/messages?before=-1", "/api/v1/mailboxes/own/messages/0?uid_validity=7", "/api/v1/mailboxes/own/messages/1", "/api/v1/mailboxes/own/messages/1/attachments/invalid?uid_validity=7"} {
		if response := perform(f, "GET", path, token, "", nil); response.Code != 400 {
			t.Fatalf("invalid reference allowed: %s %d", path, response.Code)
		}
	}
}

func TestSearchFiltersAreValidatedAndScoped(t *testing.T) {
	reader := fakeReader{list: func(_ context.Context, cfg imap.Configuration, _, _ uint32) (imap.MessagePage, error) {
		if cfg.Search != "café" || cfg.Seen != "unread" || cfg.From != "team@example.test" || cfg.Mailbox != "Archive" || cfg.To != "me@example.test" || cfg.Subject != "Invoice" || cfg.Since.Format("2006-01-02") != "2026-10-01" || cfg.Before.Format("2006-01-02") != "2026-11-01" {
			t.Errorf("wrong search scope: %#v", cfg)
		}
		return imap.MessagePage{Messages: []imap.MessageSummary{}}, nil
	}}
	f := newAPIFixtureWithReader(t, nil, reader)
	_, token, _ := f.addUser(t, "search@example.test")
	_, _ = f.sessions.SetReceivingCredentials(token, map[string]session.Credential{"own": {Secret: []byte("secret")}}, time.Minute, 0)
	response := perform(f, "GET", "/api/v1/mailboxes/own/messages?search=caf%C3%A9&seen=unread&from=team%40example.test&to=me%40example.test&subject=Invoice&since=2026-10-01&until=2026-10-31"+folderIDQuery(t, token, "own", "Archive"), token, "", nil)
	if response.Code != 200 {
		t.Fatalf("search failed: %d %s", response.Code, response.Body.String())
	}
	for _, query := range []string{"search=" + strings.Repeat("x", 513), "search=bad%0D%0Avalue", "from=" + strings.Repeat("x", 255), "from=bad%00value", "seen=unknown", "search=%FF", "to=" + strings.Repeat("x", 255), "subject=" + strings.Repeat("x", 513), "to=bad%0Avalue", "subject=%FF", "since=2026-02-30", "until=invalid", "since=2026-11-01&until=2026-10-01", "since=0000-01-01", "until=9999-12-31"} {
		if response := perform(f, "GET", "/api/v1/mailboxes/own/messages?"+query, token, "", nil); response.Code != 400 {
			t.Fatalf("invalid search accepted: %s %d", query, response.Code)
		}
	}
}

func TestReadingSafeErrorsAndAttachmentHeaders(t *testing.T) {
	var upstream error
	reader := fakeReader{read: func(context.Context, imap.Configuration, uint32, uint32) (imap.Message, error) {
		return imap.Message{Text: "Hello"}, upstream
	}, attachment: func(context.Context, imap.Configuration, uint32, uint32, string) (imap.Download, error) {
		return imap.Download{Filename: "../../été\r\nInjected.pdf", Data: []byte("file")}, nil
	}}
	f := newAPIFixtureWithReader(t, nil, reader)
	_, token, _ := f.addUser(t, "reader@example.test")
	_, _ = f.sessions.SetReceivingCredentials(token, map[string]session.Credential{"own": {Secret: []byte("secret")}}, time.Minute, 0)
	path := "/api/v1/mailboxes/own/messages/12?uid_validity=7"
	for _, test := range []struct {
		err    error
		status int
	}{{nil, 200}, {imap.ErrAuthentication, 422}, {imap.ErrMailboxChanged, 409}, {imap.ErrMessageMissing, 404}, {imap.ErrPartTooLarge, 413}, {errors.New("provider secret username password"), 502}} {
		upstream = test.err
		response := perform(f, "GET", path, token, "", nil)
		if response.Code != test.status || strings.Contains(response.Body.String(), "provider secret") {
			t.Fatalf("unsafe/wrong error: %d %s", response.Code, response.Body.String())
		}
	}
	response := perform(f, "GET", "/api/v1/mailboxes/own/messages/12/attachments/2?uid_validity=7", token, "", nil)
	_, params, err := mime.ParseMediaType(response.Header().Get("Content-Disposition"))
	if response.Code != 200 || err != nil || params["filename"] != "étéInjected.pdf" || response.Header().Get("Content-Type") != "application/octet-stream" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe attachment headers: %v", response.Header())
	}
}

func TestLockCancelsInFlightReading(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	reader := fakeReader{read: func(ctx context.Context, _ imap.Configuration, _, _ uint32) (imap.Message, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return imap.Message{}, ctx.Err()
	}}
	f := newAPIFixtureWithReader(t, nil, reader)
	_, token, view := f.addUser(t, "reader@example.test")
	_, _ = f.sessions.SetReceivingCredentials(token, map[string]session.Credential{"own": {Secret: []byte("secret")}}, time.Minute, 0)
	done := make(chan int, 1)
	go func() {
		done <- perform(f, "GET", "/api/v1/mailboxes/own/messages/12?uid_validity=7", token, "", nil).Code
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("read did not start")
	}
	if response := perform(f, "DELETE", "/api/v1/session/credentials", token, view.CSRFToken, nil); response.Code != 204 {
		t.Fatal("lock failed")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("lock did not cancel reading")
	}
	select {
	case code := <-done:
		if code != 423 {
			t.Fatalf("wrong locked response: %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("read handler did not stop")
	}
}

func TestDocumentSourceAndFlagRoutes(t *testing.T) {
	var flags []bool
	reader := fakeReader{read: func(context.Context, imap.Configuration, uint32, uint32) (imap.Message, error) {
		return imap.Message{HTML: `<p style="font-size:24px">Formatted</p><img src="https://tracker.test/image"><script>bad()</script>`, CSS: `p{color:#123456}`}, nil
	}, source: func(context.Context, imap.Configuration, uint32, uint32) ([]byte, error) {
		return []byte("X-Original: yes\r\n\r\nRaw message"), nil
	}, seen: func(_ context.Context, _ imap.Configuration, uid, validity uint32, seen bool) error {
		if uid != 12 || validity != 7 {
			t.Error("invalid flag reference")
		}
		flags = append(flags, seen)
		return nil
	}}
	f := newAPIFixtureWithReader(t, nil, reader)
	_, token, view := f.addUser(t, "tools@example.test")
	_, _ = f.sessions.SetReceivingCredentials(token, map[string]session.Credential{"own": {Secret: []byte("secret")}}, time.Minute, 0)
	base := "/api/v1/mailboxes/own/messages/12/"
	private := perform(f, "GET", base+"document?uid_validity=7", token, "", nil)
	if private.Code != 200 || strings.Contains(private.Body.String(), "tracker.test") || strings.Contains(private.Body.String(), "bad()") || !strings.Contains(private.Body.String(), "font-size:24px") && !strings.Contains(private.Body.String(), "font-size: 24px") {
		t.Fatalf("bad private document: %s", private.Body.String())
	}
	if !strings.Contains(private.Header().Get("Content-Security-Policy"), "style-src 'unsafe-inline'") || !strings.Contains(private.Header().Get("Content-Security-Policy"), "img-src 'none'") || private.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Fatal("incorrect isolated document security policy")
	}
	external := perform(f, "GET", base+"document?uid_validity=7&external=1", token, "", nil)
	if !strings.Contains(external.Body.String(), "tracker.test/image") || !strings.Contains(external.Header().Get("Content-Security-Policy"), "img-src https: http:") {
		t.Fatal("explicit external image request failed")
	}
	source := perform(f, "GET", base+"source?uid_validity=7", token, "", nil)
	if source.Code != 200 || source.Body.String() != "X-Original: yes\r\n\r\nRaw message" || source.Header().Get("Content-Type") != "application/octet-stream" || source.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("source not returned safely")
	}
	if response := perform(f, "POST", base+"seen?uid_validity=7", token, "", nil); response.Code != 403 {
		t.Fatal("flag mutation bypassed CSRF")
	}
	for _, route := range []string{"seen", "unseen"} {
		if response := perform(f, "POST", base+route+"?uid_validity=7", token, view.CSRFToken, nil); response.Code != 204 {
			t.Fatalf("flag update failed: %d", response.Code)
		}
	}
	if len(flags) != 2 || !flags[0] || flags[1] {
		t.Fatal("wrong read/unread flags")
	}
	app := perform(f, "GET", "/api/v1/session", token, "", nil)
	if strings.Contains(app.Header().Get("Content-Security-Policy"), "'unsafe-inline'") {
		t.Fatal("app CSP was relaxed")
	}
}
