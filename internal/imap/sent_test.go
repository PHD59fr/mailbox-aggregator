package imap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSentCopyUsesMappedFolderSeenFlagAndOriginalMessageID(t *testing.T) {
	r, cfg, user, _ := readerFixture(t)
	for _, folder := range []string{"Sent", "Work/Envoyés"} {
		if err := user.Create(folder, nil); err != nil {
			t.Fatal(err)
		}
	}
	cfg.SentMailbox = "Work/Envoyés"
	raw := []byte("Message-ID: <unique-sent@example.test>\r\n" + rawReaderMessage)
	for range 2 {
		if err := r.SaveSent(context.Background(), cfg, raw, "<unique-sent@example.test>", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Mailbox = cfg.SentMailbox
	page, err := r.List(context.Background(), cfg, 0, 0)
	if err != nil || len(page.Messages) != 1 || !page.Messages[0].Seen {
		t.Fatalf("sent copy or deduplication failed: %#v %v", page, err)
	}
	source, err := r.Source(context.Background(), cfg, page.Messages[0].UID, page.UIDValidity)
	if err != nil || string(source) != string(raw) {
		t.Fatalf("archived MIME changed: %v", err)
	}
	for _, name := range []string{"Sent", "INBOX"} {
		cfg.Mailbox = name
		page, err = r.List(context.Background(), cfg, 0, 0)
		if err != nil || len(page.Messages) != 0 {
			t.Fatalf("wrote to another folder: %s %v", name, err)
		}
	}
}

func TestSentCopyDetectsAutomaticFolderAndNeverCreatesOrFallsBackFromMissingMapping(t *testing.T) {
	r, cfg, user, _ := readerFixture(t)
	if err := user.Create("Sent", nil); err != nil {
		t.Fatal(err)
	}
	raw := []byte("Message-ID: <automatic@example.test>\r\n" + rawReaderMessage)
	if err := r.SaveSent(context.Background(), cfg, raw, "<automatic@example.test>", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"Missing", "INBOX"} {
		cfg.SentMailbox = target
		if err := r.SaveSent(context.Background(), cfg, raw, "<other@example.test>", time.Now()); !errors.Is(err, ErrSentUnavailable) {
			t.Fatalf("unexpected fallback: %v", err)
		}
	}
	cfg.SentMailbox = ""
	cfg.SentExcluded = []string{"Sent"}
	if err := r.SaveSent(context.Background(), cfg, raw, "<other@example.test>", time.Now()); !errors.Is(err, ErrSentUnavailable) {
		t.Fatalf("excluded folder reused: %v", err)
	}
	folders, err := r.Folders(context.Background(), cfg)
	if err != nil || len(folders.Folders) != 2 {
		t.Fatalf("a folder was created: %#v %v", folders, err)
	}
	for _, folder := range folders.Folders {
		if strings.EqualFold(folder.Name, "INBOX") && folder.Messages != 0 {
			t.Fatal("copy was placed in INBOX")
		}
	}
}

func TestSentCopyDeduplicatesRedirectsByTheirResentMessageID(t *testing.T) {
	r, cfg, user, _ := readerFixture(t)
	if err := user.Create("Sent", nil); err != nil {
		t.Fatal(err)
	}
	raw := []byte("Resent-Message-ID: <redirected@example.test>\r\nMessage-ID: <original@example.test>\r\n" + rawReaderMessage)
	for range 2 {
		if err := r.SaveSent(context.Background(), cfg, raw, "<redirected@example.test>", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Mailbox = "Sent"
	page, err := r.List(context.Background(), cfg, 0, 0)
	if err != nil || len(page.Messages) != 1 {
		t.Fatalf("redirect copy duplicated: %#v %v", page, err)
	}
}
