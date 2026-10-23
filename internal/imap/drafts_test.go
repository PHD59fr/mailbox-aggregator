package imap

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/example/mail-aggregator/internal/mail"
	"github.com/example/mail-aggregator/internal/smtp"
)

func TestRemoteDraftPreservesInlineImageIDBytesAndAllowedHeaders(t *testing.T) {
	r, cfg, user, audit := readerFixture(t, protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapUIDPlus: {}})
	audit.writes = true
	user.Create("Drafts", nil)
	var payload bytes.Buffer
	png.Encode(&payload, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	draft := mail.Draft{InReplyTo: "<parent@example.test>", References: []string{"<root@example.test>", "<parent@example.test>"}, HTML: `<p>Draft</p><img src="cid:photo@example.test" alt="Photo">`, Headers: map[string]string{"Organization": "Example"}, Attachments: []mail.Attachment{{Filename: "photo.png", ContentType: "image/png", ContentID: "photo@example.test", Data: payload.Bytes()}}}
	built, err := smtp.BuildDraft(mail.Identity{Email: "writer@example.test"}, draft, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := r.SaveDraft(context.Background(), cfg, built.Data, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Mailbox = saved.Folder
	opened, err := r.Read(context.Background(), cfg, saved.UID, saved.UIDValidity)
	if err != nil || opened.Truncated || !strings.Contains(opened.HTML, "cid:photo@example.test") || len(opened.Attachments) != 1 || opened.Attachments[0].ContentID != "photo@example.test" || opened.Headers["Organization"] != "Example" || opened.InReplyTo != "<parent@example.test>" || strings.Join(opened.References, " ") != "<root@example.test> <parent@example.test>" {
		t.Fatalf("draft metadata lost: %+v %v", opened, err)
	}
	attachment, err := r.Attachment(context.Background(), cfg, saved.UID, saved.UIDValidity, opened.Attachments[0].Part)
	if err != nil || !bytes.Equal(attachment.Data, payload.Bytes()) {
		t.Fatal("inline bytes lost", err)
	}
}

func TestDraftSaveReplaceOpenAndDeleteUseOnlyProviderAndPreserveAttachments(t *testing.T) {
	r, cfg, user, audit := readerFixture(t, protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapUIDPlus: {}})
	audit.writes = true
	if err := user.Create("Drafts", nil); err != nil {
		t.Fatal(err)
	}
	draft := mail.Draft{Subject: "Unfinished", BCC: []mail.Address{{Email: "private@example.test"}}, Text: "Draft body", Attachments: []mail.Attachment{{Filename: "private.txt", ContentType: "text/plain", Data: []byte("attachment payload")}}}
	message, err := smtp.BuildDraft(mail.Identity{Email: "writer@example.test"}, draft, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	first, err := r.SaveDraft(context.Background(), cfg, message.Data, 0, 0)
	if err != nil || first.UID == 0 || first.UIDValidity == 0 {
		t.Fatalf("initial draft: %+v %v", first, err)
	}
	cfg.Mailbox = first.Folder
	opened, err := r.Read(context.Background(), cfg, first.UID, first.UIDValidity)
	if err != nil || !opened.Draft || len(opened.BCC) != 1 || opened.BCC[0].Email != "private@example.test" || len(opened.Attachments) != 1 {
		t.Fatalf("incomplete restored draft: %+v %v", opened, err)
	}
	attachment, err := r.Attachment(context.Background(), cfg, first.UID, first.UIDValidity, opened.Attachments[0].Part)
	if err != nil || string(attachment.Data) != "attachment payload" {
		t.Fatal("draft attachment lost")
	}
	// An unrelated deleted message must survive every targeted expunge.
	if _, err := user.Append("Drafts", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{Flags: []protocol.Flag{protocol.FlagDeleted}}); err != nil {
		t.Fatal(err)
	}
	draft.Subject = "Updated"
	message, err = smtp.BuildDraft(mail.Identity{Email: "writer@example.test"}, draft, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cfg.DraftsMailbox = first.Folder
	second, err := r.SaveDraft(context.Background(), cfg, message.Data, first.UID, first.UIDValidity)
	if err != nil || second.UID == first.UID || second.PreviousRemoval != "confirmed" {
		t.Fatalf("draft replacement: %+v %v", second, err)
	}
	page, err := r.List(context.Background(), cfg, 0, 0)
	if err != nil || len(page.Messages) != 2 {
		t.Fatal("replacement removed unrelated message or left duplicate")
	}
	if err := r.DeleteDraft(context.Background(), cfg, 2, page.UIDValidity); !errors.Is(err, ErrNotDraft) {
		t.Fatal("non-draft was deletable")
	}
	if err := r.DeleteDraft(context.Background(), cfg, second.UID, second.UIDValidity+1); !errors.Is(err, ErrMailboxChanged) {
		t.Fatal("stale draft reference accepted")
	}
	if err := r.DeleteDraft(context.Background(), cfg, second.UID, second.UIDValidity); err != nil {
		t.Fatal(err)
	}
	page, err = r.List(context.Background(), cfg, 0, 0)
	if err != nil || len(page.Messages) != 1 || page.Messages[0].UID != 2 {
		t.Fatal("draft deletion expunged another message")
	}
}
func TestDraftReplacementRefusesUnsafeOrMissingMappingBeforeAppend(t *testing.T) {
	r, cfg, user, audit := readerFixture(t, protocol.CapSet{protocol.CapIMAP4rev1: {}})
	audit.writes = true
	_ = user.Create("Drafts", nil)
	_, _ = user.Append("Drafts", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{Flags: []protocol.Flag{protocol.FlagDraft}})
	cfg.Mailbox = "Drafts"
	page, _ := r.List(context.Background(), cfg, 0, 0)
	if _, err := r.SaveDraft(context.Background(), cfg, []byte(rawReaderMessage), 1, page.UIDValidity); !errors.Is(err, ErrMoveUnsupported) {
		t.Fatalf("unsafe replacement: %v", err)
	}
	cfg.DraftsMailbox = "Missing"
	if _, err := r.SaveDraft(context.Background(), cfg, []byte(rawReaderMessage), 0, 0); !errors.Is(err, ErrDraftsUnavailable) {
		t.Fatalf("missing mapping fell back: %v", err)
	}
	page, _ = r.List(context.Background(), cfg, 0, 0)
	if len(page.Messages) != 1 || audit.expunges != 0 || audit.deletedStores != 0 {
		t.Fatal("failed replacement changed provider data")
	}
}
