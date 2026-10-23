package imap

import (
	"context"
	"errors"
	"strings"
	"testing"

	protocol "github.com/emersion/go-imap/v2"
)

func TestUserFlagsUseExactUIDAndValidity(t *testing.T) {
	r, cfg, user, audit := readerFixture(t)
	audit.writes = true
	for i := 0; i < 2; i++ {
		if _, err := user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := r.List(context.Background(), cfg, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"seen", "flagged", "answered"} {
		if err := r.SetFlag(context.Background(), cfg, 2, page.UIDValidity, flag, true); err != nil {
			t.Fatal(err)
		}
	}
	changed, err := r.List(context.Background(), cfg, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !changed.Messages[0].Flagged || !changed.Messages[0].Answered || !changed.Messages[0].Seen || changed.Messages[1].Seen || changed.Messages[1].Flagged {
		t.Fatal("flags applied to incorrect message")
	}
	if err := r.SetFlag(context.Background(), cfg, 2, page.UIDValidity+1, "flagged", false); !errors.Is(err, ErrMailboxChanged) {
		t.Fatalf("stale UIDVALIDITY: %v", err)
	}
	if err := r.SetFlag(context.Background(), cfg, 2, page.UIDValidity, "deleted", true); !errors.Is(err, ErrInvalidAction) {
		t.Fatal("Deleted accepted")
	}
	for _, flag := range []string{"seen", "flagged", "answered"} {
		if err := r.SetFlag(context.Background(), cfg, 2, page.UIDValidity, flag, false); err != nil {
			t.Fatal(err)
		}
	}
	changed, err = r.List(context.Background(), cfg, 0, 0)
	if err != nil || changed.Messages[0].Flagged || changed.Messages[0].Answered || changed.Messages[0].Seen {
		t.Fatal("flags not removed")
	}
}

func TestArbitraryFolderCopyAndMovePreserveUnrelatedDeletedMessages(t *testing.T) {
	for _, moveCap := range []bool{false, true} {
		caps := protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapUIDPlus: {}}
		if moveCap {
			caps[protocol.CapMove] = struct{}{}
		}
		r, cfg, user, audit := readerFixture(t, caps)
		audit.writes = true
		if err := r.ManageFolder(context.Background(), cfg, "create", "Projects/été & work", ""); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			flags := []protocol.Flag{}
			if i == 0 {
				flags = append(flags, protocol.FlagDeleted)
			}
			if _, err := user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{Flags: flags}); err != nil {
				t.Fatal(err)
			}
		}
		page, _ := r.List(context.Background(), cfg, 0, 0)
		if err := r.Transfer(context.Background(), cfg, 2, page.UIDValidity, "Projects/été & work", false); err != nil {
			t.Fatal(err)
		}
		if err := r.Transfer(context.Background(), cfg, 3, page.UIDValidity, "Projects/été & work", true); err != nil {
			t.Fatal(err)
		}
		left, _ := r.List(context.Background(), cfg, 0, 0)
		if len(left.Messages) != 2 || left.Messages[1].UID != 1 {
			t.Fatal("wrong source message removed")
		}
		cfg.Mailbox = "Projects/été & work"
		copied, _ := r.List(context.Background(), cfg, 0, 0)
		if len(copied.Messages) != 2 {
			t.Fatal("copies missing")
		}
		for _, action := range []string{"subscribe", "unsubscribe"} {
			if err := r.ManageFolder(context.Background(), cfg, action, cfg.Mailbox, ""); err != nil {
				t.Fatal(err)
			}
		}
		if err := r.ManageFolder(context.Background(), cfg, "rename", cfg.Mailbox, "Renamed été"); err != nil {
			t.Fatal(err)
		}
		if err := r.ManageFolder(context.Background(), cfg, "delete", "Renamed été", ""); err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{"delete", "rename"} {
			if err := r.ManageFolder(context.Background(), cfg, action, "inbox", "Other"); !errors.Is(err, ErrInvalidAction) {
				t.Fatal("INBOX could be destroyed")
			}
		}
	}
}

func TestTransferFailureNeverDeletesSource(t *testing.T) {
	for _, caps := range []protocol.CapSet{{protocol.CapIMAP4rev1: {}}, {protocol.CapIMAP4rev1: {}, protocol.CapUIDPlus: {}}} {
		r, cfg, user, audit := readerFixture(t, caps)
		audit.writes = true
		audit.failCopy = true
		_, _ = user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{})
		page, _ := r.List(context.Background(), cfg, 0, 0)
		if err := r.Transfer(context.Background(), cfg, 1, page.UIDValidity, "Missing", true); err == nil {
			t.Fatal("failed move accepted")
		}
		if audit.deletedStores != 0 || audit.expunges != 0 {
			t.Fatal("original deleted after failed copy")
		}
	}
}

func TestFlagUnsupportedDoesNotIssueAStore(t *testing.T) {
	r, cfg, user, audit := readerFixture(t)
	audit.writes = true
	audit.permanentFlags = []protocol.Flag{protocol.FlagSeen}
	_, _ = user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{})
	page, err := r.List(context.Background(), cfg, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetFlag(context.Background(), cfg, 1, page.UIDValidity, "flagged", true); !errors.Is(err, ErrFlagUnsupported) {
		t.Fatalf("unsupported flag accepted: %v", err)
	}
	page, err = r.List(context.Background(), cfg, 0, 0)
	if err != nil || page.Messages[0].Flagged {
		t.Fatal("unsupported flag was stored")
	}
}
