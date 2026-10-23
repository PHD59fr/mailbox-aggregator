package imap

import (
	"context"
	"errors"
	"strings"
	"testing"

	protocol "github.com/emersion/go-imap/v2"
)

func TestTrashMovesOnlySelectedUIDAndNeverExpungesUnrelatedMessages(t *testing.T) {
	for _, move := range []bool{true, false} {
		t.Run(map[bool]string{true: "MOVE", false: "UIDPLUS fallback"}[move], func(t *testing.T) {
			caps := protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapUIDPlus: {}}
			if move {
				caps[protocol.CapMove] = struct{}{}
			}
			receiver, cfg, user, audit := readerFixture(t, caps)
			audit.writes = true
			if err := user.Create("Trash", nil); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				flags := []protocol.Flag(nil)
				if i == 0 {
					flags = []protocol.Flag{protocol.FlagDeleted}
				}
				if _, err := user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{Flags: flags}); err != nil {
					t.Fatal(err)
				}
			}
			page, err := receiver.List(context.Background(), cfg, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err = receiver.Trash(context.Background(), cfg, 2, page.UIDValidity); err != nil {
				t.Fatal(err)
			}
			left, err := receiver.List(context.Background(), cfg, 0, 0)
			if err != nil || len(left.Messages) != 2 || left.Messages[0].UID != 3 || left.Messages[1].UID != 1 {
				t.Fatalf("unrelated message deleted: %#v %v", left, err)
			}
			cfg.Mailbox = "Trash"
			trashed, err := receiver.List(context.Background(), cfg, 0, 0)
			if err != nil || len(trashed.Messages) != 1 {
				t.Fatalf("message not in trash: %#v %v", trashed, err)
			}
			if err = receiver.Trash(context.Background(), cfg, trashed.Messages[0].UID, trashed.UIDValidity); !errors.Is(err, ErrAlreadyInTrash) {
				t.Fatalf("permanent delete was not rejected: %v", err)
			}
		})
	}
}

func TestTrashRefusesUnsafeFallbackAndNeverDeletesAfterFailedCopy(t *testing.T) {
	for _, test := range []struct {
		name                     string
		uidPlus, trash, failCopy bool
		want                     error
	}{
		{"no safe move support", false, true, false, ErrMoveUnsupported},
		{"no trash folder", true, false, false, ErrTrashUnavailable},
		{"copy rejected", true, true, true, ErrTrashUnconfirmed},
	} {
		t.Run(test.name, func(t *testing.T) {
			caps := protocol.CapSet{protocol.CapIMAP4rev1: {}}
			if test.uidPlus {
				caps[protocol.CapUIDPlus] = struct{}{}
			}
			receiver, cfg, user, audit := readerFixture(t, caps)
			audit.writes = true
			audit.failCopy = test.failCopy
			if test.trash {
				if err := user.Create("Trash", nil); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{}); err != nil {
				t.Fatal(err)
			}
			page, err := receiver.List(context.Background(), cfg, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err = receiver.Trash(context.Background(), cfg, 1, page.UIDValidity); !errors.Is(err, test.want) {
				t.Fatalf("wrong failure: %v", err)
			}
			if audit.deletedStores != 0 || audit.expunges != 0 {
				t.Fatal("failed/unsafe move deleted the original")
			}
			left, err := receiver.List(context.Background(), cfg, 0, 0)
			if err != nil || len(left.Messages) != 1 {
				t.Fatal("original disappeared")
			}
		})
	}
}

func TestManualTrashOverridesAutomaticAndProtectsOtherDeletedMessages(t *testing.T) {
	for _, move := range []bool{true, false} {
		t.Run(map[bool]string{true: "MOVE", false: "UIDPLUS"}[move], func(t *testing.T) {
			caps := protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapUIDPlus: {}}
			if move {
				caps[protocol.CapMove] = struct{}{}
			}
			receiver, cfg, user, audit := readerFixture(t, caps)
			audit.writes = true
			for _, name := range []string{"Trash", "Personal/Deleted été & work"} {
				if err := user.Create(name, nil); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 2; i++ {
				flags := []protocol.Flag(nil)
				if i == 0 {
					flags = []protocol.Flag{protocol.FlagDeleted}
				}
				if _, err := user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{Flags: flags}); err != nil {
					t.Fatal(err)
				}
			}
			page, err := receiver.List(context.Background(), cfg, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			cfg.TrashMailbox = "Personal/Deleted été & work"
			if err := receiver.Trash(context.Background(), cfg, 2, page.UIDValidity); err != nil {
				t.Fatal(err)
			}
			left, err := receiver.List(context.Background(), cfg, 0, 0)
			if err != nil || len(left.Messages) != 1 || left.Messages[0].UID != 1 {
				t.Fatalf("unrelated original removed: %#v %v", left, err)
			}
			cfg.Mailbox = "Trash"
			automatic, err := receiver.List(context.Background(), cfg, 0, 0)
			if err != nil || len(automatic.Messages) != 0 {
				t.Fatal("automatic Trash was used despite manual mapping")
			}
			cfg.Mailbox = cfg.TrashMailbox
			manual, err := receiver.List(context.Background(), cfg, 0, 0)
			if err != nil || len(manual.Messages) != 1 {
				t.Fatalf("message not in custom Trash: %#v %v", manual, err)
			}
			if err := receiver.Trash(context.Background(), cfg, manual.Messages[0].UID, manual.UIDValidity); !errors.Is(err, ErrAlreadyInTrash) {
				t.Fatalf("permanent deletion not rejected: %v", err)
			}
		})
	}
}

func TestMissingManualTrashNeverFallsBackOrDeletes(t *testing.T) {
	receiver, cfg, user, audit := readerFixture(t, protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapUIDPlus: {}})
	audit.writes = true
	if err := user.Create("Trash", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{}); err != nil {
		t.Fatal(err)
	}
	page, err := receiver.List(context.Background(), cfg, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg.TrashMailbox = "Missing custom folder"
	if err := receiver.Trash(context.Background(), cfg, 1, page.UIDValidity); !errors.Is(err, ErrTrashUnavailable) {
		t.Fatalf("wrong failure: %v", err)
	}
	if audit.deletedStores != 0 || audit.expunges != 0 {
		t.Fatal("missing destination deleted the original")
	}
	left, err := receiver.List(context.Background(), cfg, 0, 0)
	if err != nil || len(left.Messages) != 1 {
		t.Fatal("original disappeared")
	}
	// A folder manually assigned to another role must not be reused as Trash.
	cfg.TrashMailbox = ""
	cfg.TrashExcluded = []string{"Trash"}
	if err := receiver.Trash(context.Background(), cfg, 1, page.UIDValidity); !errors.Is(err, ErrTrashUnavailable) {
		t.Fatalf("reused a different role's folder: %v", err)
	}
}
