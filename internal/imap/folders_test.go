package imap

import (
	"context"
	"fmt"
	"strings"
	"testing"

	protocol "github.com/emersion/go-imap/v2"
)

func TestFoldersDiscoverAndReadSelectedMailbox(t *testing.T) {
	receiver, cfg, user, audit := readerFixture(t)
	name := "Sent/été & work"
	if err := user.Create(name, nil); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"INBOX", name} {
		if _, err := user.Append(folder, literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := receiver.Folders(context.Background(), cfg)
	if err != nil || len(page.Folders) != 2 || page.Truncated {
		t.Fatalf("folders: %#v %v", page, err)
	}
	for _, folder := range page.Folders {
		if !folder.Selectable || folder.Messages != 1 || folder.Unseen != 1 || folder.UIDValidity == 0 {
			t.Fatalf("wrong folder counts: %#v", folder)
		}
	}
	cfg.Mailbox = name
	list, err := receiver.List(context.Background(), cfg, 0, 0)
	if err != nil || len(list.Messages) != 1 {
		t.Fatalf("folder list: %#v %v", list, err)
	}
	uid := list.Messages[0].UID
	if _, err = receiver.Read(context.Background(), cfg, uid, list.UIDValidity); err != nil {
		t.Fatal(err)
	}
	if data, err := receiver.Source(context.Background(), cfg, uid, list.UIDValidity); err != nil || !strings.Contains(string(data), "team@example.test") {
		t.Fatalf("folder source: %v", err)
	}
	audit.writes = true
	if err = receiver.MarkSeen(context.Background(), cfg, uid, list.UIDValidity); err != nil {
		t.Fatal(err)
	}
	page, err = receiver.Folders(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range page.Folders {
		if folder.Name == "INBOX" && folder.Unseen != 1 {
			t.Fatal("marking in another folder changed INBOX")
		}
		if folder.Name == name && folder.Unseen != 0 {
			t.Fatal("selected folder was not marked read")
		}
	}
}

func TestFolderDiscoveryIsCapped(t *testing.T) {
	receiver, cfg, user, _ := readerFixture(t)
	for i := 0; i < MaxFolders*3; i++ {
		if err := user.Create(fmt.Sprintf("Folder%03d", i), nil); err != nil {
			t.Fatal(err)
		}
	}
	page, err := receiver.Folders(context.Background(), cfg)
	if err != nil || !page.Truncated || len(page.Folders) != MaxFolders {
		t.Fatalf("unbounded folders: %d %v %v", len(page.Folders), page.Truncated, err)
	}
}

func TestFolderRolesPreferServerMetadataOverNames(t *testing.T) {
	for _, test := range []struct {
		name      string
		attrs     []protocol.MailboxAttr
		delimiter rune
		role      string
		special   bool
	}{
		{"Custom provider name", []protocol.MailboxAttr{protocol.MailboxAttrSent}, '/', "sent", true},
		{"INBOX/Éléments envoyés", nil, '/', "sent", false},
		{"[Gmail]/Sent Mail", nil, '/', "sent", false},
		{"INBOX.Trash", nil, '.', "trash", false},
		{"Brouillons", nil, '/', "drafts", false},
		{"Papierkorb", nil, '/', "trash", false},
		{"Projects/Trash", nil, '/', "", false},
		{"Outbox", nil, '/', "outbox", false},
	} {
		role, special := folderRole(&protocol.ListData{Mailbox: test.name, Attrs: test.attrs, Delim: test.delimiter})
		if role != test.role || special != test.special {
			t.Fatalf("bad role for %s: %s %v", test.name, role, special)
		}
	}
}
