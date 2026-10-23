package api

// Opt-in local browser fixture. Uses the real router/security headers with
// synthetic mail and a fake sender; never contacts a real mail provider.
import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/mail"
	"github.com/example/mail-aggregator/internal/session"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

type browserWatcher struct{}

func (browserWatcher) Watch(ctx context.Context, _ imap.Configuration, _ time.Duration, emit func(imap.Status)) {
	emit(imap.Status{Messages: 55984, Unseen: 1, UIDNext: 55985, UIDValidity: 7, Mode: "idle"})
	<-ctx.Done()
}

type browserTester struct{}

func (browserTester) Test(context.Context, string, session.Credential) error { return nil }
func TestBrowserMailTools(t *testing.T) {
	if os.Getenv("MAIL_TOOLS_BROWSER_FIXTURE") != "1" {
		t.Skip("opt-in local browser fixture")
	}
	var mu sync.Mutex
	seen := false
	trashed := map[uint32]bool{}
	summary := func(uid uint32) imap.MessageSummary {
		mu.Lock()
		defer mu.Unlock()
		return imap.MessageSummary{UID: uid, Subject: "Une boîte mail, enfin lisible", From: []mail.Address{{Name: "Camille Dupont", Email: "camille@example.test"}}, To: []mail.Address{{Email: "alex@example.test"}, {Email: "team@example.test"}}, CC: []mail.Address{{Email: "copy@example.test"}}, Date: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC).Add(-time.Duration(55984-uid) * time.Hour), Seen: uid != 55984 || seen, Size: 2000}
	}
	reader := fakeReader{
		sent: func(_ context.Context, _ imap.Configuration, data []byte, messageID string, _ time.Time) error {
			if len(data) == 0 || messageID == "" {
				return imap.ErrSentUnconfirmed
			}
			return nil
		},
		folders: func(context.Context, imap.Configuration) (imap.FolderPage, error) {
			return imap.FolderPage{Folders: []imap.Folder{
				{Name: "INBOX", Selectable: true, Messages: 55984, Unseen: 1, UIDNext: 55985, UIDValidity: 7},
				{Name: "[Gmail]/Sent Mail", Role: "sent", SpecialUse: true, Selectable: true, Messages: 120, UIDNext: 121, UIDValidity: 8},
				{Name: "Provider Trash", Role: "trash", SpecialUse: true, Selectable: true, UIDValidity: 7, UIDNext: 1},
				{Name: "Archives/2026", Delimiter: "/", Selectable: true, Messages: 50, UIDNext: 51, UIDValidity: 9},
				{Name: "Loyer", Delimiter: "/", Selectable: true, Messages: 10, UIDNext: 11, UIDValidity: 10},
				{Name: "Loyer/adhoc", Delimiter: "/", Selectable: true, Messages: 4, UIDNext: 5, UIDValidity: 11},
				{Name: "Loyer/nexim", Delimiter: "/", Selectable: true, Messages: 6, UIDNext: 7, UIDValidity: 12},
			}}, nil
		},
		list: func(_ context.Context, cfg imap.Configuration, before, validity uint32) (imap.MessagePage, error) {
			if cfg.Search == "no-match" {
				return imap.MessagePage{Messages: []imap.MessageSummary{}, UIDValidity: 7, Total: 55984}, nil
			}
			if before == 0 {
				before = 55985
			}
			page := imap.MessagePage{UIDValidity: 7, Total: 55984, NextBefore: before - 50}
			for uid := before - 1; uid > 0 && len(page.Messages) < 50; uid-- {
				mu.Lock()
				removed := trashed[uid]
				mu.Unlock()
				if !removed {
					page.Messages = append(page.Messages, summary(uid))
				}
			}
			if len(page.Messages) > 0 {
				page.NextBefore = page.Messages[len(page.Messages)-1].UID
			}
			return page, nil
		},
		read: func(_ context.Context, _ imap.Configuration, uid, validity uint32) (imap.Message, error) {
			return imap.Message{MessageSummary: summary(uid), UIDValidity: 7, Text: "Bonjour Alex,\nLe projet avance.\nCamille", HTML: `<h1 class="headline">Tout est à sa place.</h1><table width="100%"><tr><td style="padding:20px;text-align:center;background-color:#e5eaff"><p style="font-size:22px;color:#3153c8">Une mise en page fidèle.</p></td></tr></table><p>Bonjour Alex, voici les <strong>nouvelles du projet</strong>.</p><img src="https://tracker.test/pixel.png"><script>window.mailScriptRan=true</script>`, CSS: `.headline {font-size:30px;color:#3153c8}`, Attachments: []imap.AttachmentInfo{{Part: "2", Filename: "notes.pdf", ContentType: "application/pdf", Size: 120}}, ReplyTo: []mail.Address{{Email: "reply@example.test"}}, MessageID: "original@example.test"}, nil
		},
		attachment: func(context.Context, imap.Configuration, uint32, uint32, string) (imap.Download, error) {
			return imap.Download{Filename: "notes.pdf", Data: []byte("%PDF-preview-only")}, nil
		},
		source: func(context.Context, imap.Configuration, uint32, uint32) ([]byte, error) {
			return []byte("From: Camille <camille@example.test>\r\nTo: alex@example.test\r\nReceived: synthetic local preview\r\nSubject: Preview\r\nMessage-ID: <original@example.test>\r\n\r\nOriginal source <script>never executed</script>"), nil
		},
		seen: func(_ context.Context, _ imap.Configuration, _, _ uint32, value bool) error {
			mu.Lock()
			seen = value
			mu.Unlock()
			return nil
		},
		trash: func(_ context.Context, cfg imap.Configuration, uid, _ uint32) error {
			if cfg.TrashMailbox != "Archives/2026" {
				return imap.ErrTrashUnavailable
			}
			mu.Lock()
			trashed[uid] = true
			mu.Unlock()
			return nil
		},
	}
	f := newAPIFixtureWithTester(t, browserWatcher{}, reader, browserTester{}, &recordingSender{})
	uid, token, view := f.addUser(t, "browser-tools@example.test")
	otherID, otherToken, otherView := f.addUser(t, "browser-tools-other@example.test")
	server := httptest.NewServer(f.router)
	defer server.Close()
	path := "/tmp/emailer-browser-tools-fixture.json"
	data, _ := json.Marshal(map[string]string{"origin": server.URL, "userID": uid.String(), "token": token, "csrf": view.CSRFToken, "otherUserID": otherID.String(), "otherToken": otherToken, "otherCSRF": otherView.CSRFToken})
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	fmt.Println("Local synthetic mail fixture ready")
	timer := time.NewTimer(4 * time.Minute)
	defer timer.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-timer.C:
			t.Fatal("browser fixture timed out")
			return
		case <-tick.C:
			if _, err := os.Stat("/tmp/emailer-browser-tools-done"); err == nil {
				_ = os.Remove("/tmp/emailer-browser-tools-done")
				return
			}
		}
	}
}
