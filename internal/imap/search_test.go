package imap

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	protocol "github.com/emersion/go-imap/v2"
)

func TestSearchCombinesRecipientSubjectAndInclusiveReceivedDatesWithoutBodies(t *testing.T) {
	r, cfg, user, audit := readerFixture(t)
	for _, row := range []struct {
		date, from, to, cc, subject string
		seen                        bool
	}{
		{"2026-09-30", "team@example.test", "me@example.test", "", "Invoice October", false},
		{"2026-10-01", "team@example.test", "me@example.test", "", "Invoice October", false},
		{"2026-10-15", "team@example.test", "other@example.test", "me@example.test", "Invoice October", false},
		{"2026-10-31", "team@example.test", "me@example.test", "", "Invoice October", false},
		{"2026-11-01", "team@example.test", "me@example.test", "", "Invoice October", false},
		{"2026-10-15", "team@example.test", "me@example.test", "", "Other", false},
		{"2026-10-15", "team@example.test", "other@example.test", "", "Invoice October", false},
		{"2026-10-15", "team@example.test", "me@example.test", "", "Invoice October", true},
		{"2026-10-15", "other@example.test", "me@example.test", "", "Invoice October", false},
	} {
		date, _ := time.Parse("2006-01-02", row.date)
		flags := []protocol.Flag(nil)
		if row.seen {
			flags = []protocol.Flag{protocol.FlagSeen}
		}
		raw := fmt.Sprintf("From: %s\r\nTo: %s\r\nCc: %s\r\nSubject: %s\r\n\r\nBody", row.from, row.to, row.cc, row.subject)
		if _, err := user.Append("INBOX", literal{strings.NewReader(raw)}, &protocol.AppendOptions{Time: date, Flags: flags}); err != nil {
			t.Fatal(err)
		}
	}
	cfg.From = "team@example.test"
	cfg.To = "me@example.test"
	cfg.Subject = "Invoice"
	cfg.Seen = "unread"
	cfg.Since, _ = time.Parse("2006-01-02", "2026-10-01")
	cfg.Before, _ = time.Parse("2006-01-02", "2026-11-01")
	page, err := r.List(context.Background(), cfg, 0, 0)
	if err != nil || len(page.Messages) != 3 || page.Messages[0].UID != 4 || page.Messages[1].UID != 3 || page.Messages[2].UID != 2 {
		t.Fatalf("combined filters: %#v %v", page, err)
	}
	for _, test := range []struct {
		name      string
		configure func(*Configuration)
		count     int
	}{
		{"recipient", func(c *Configuration) { c.To = "me@example.test" }, 8},
		{"subject", func(c *Configuration) { c.Subject = "Invoice" }, 8},
		{"since", func(c *Configuration) { c.Since, _ = time.Parse("2006-01-02", "2026-10-01") }, 8},
		{"before", func(c *Configuration) { c.Before, _ = time.Parse("2006-01-02", "2026-10-01") }, 1},
		{"literal subject", func(c *Configuration) { c.Subject = `Invoice" OR ALL` }, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := cfg
			current.From = ""
			current.To = ""
			current.Subject = ""
			current.Seen = ""
			current.Since = time.Time{}
			current.Before = time.Time{}
			test.configure(&current)
			page, err := r.List(context.Background(), current, 0, 0)
			if err != nil || len(page.Messages) != test.count {
				t.Fatalf("%s: %#v %v", test.name, page, err)
			}
		})
	}
	if len(audit.parts) != 0 {
		t.Fatal("filters downloaded message bodies")
	}
}

func TestServerSearchFindsOldMessagesAndPaginatesWithoutBodies(t *testing.T) {
	receiver, cfg, user, audit := readerFixture(t)
	for i := 1; i <= 140; i++ {
		body, from := "needle café", "team@example.test"
		if i > 120 {
			body = "recent unrelated mail"
		}
		flags := []protocol.Flag(nil)
		if i%2 == 0 {
			flags = []protocol.Flag{protocol.FlagSeen}
			from = "other@example.test"
		}
		raw := fmt.Sprintf("From: %s\r\nTo: me@example.test\r\nSubject: Project %d\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s", from, i, body)
		if _, err := user.Append("INBOX", literal{strings.NewReader(raw)}, &protocol.AppendOptions{Flags: flags}); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Search = "needle"
	first, err := receiver.List(context.Background(), cfg, 0, 0)
	if err != nil || len(first.Messages) != 50 || first.Messages[0].UID != 120 || first.NextBefore != 71 {
		t.Fatalf("first search page: %#v %v", first, err)
	}
	second, err := receiver.List(context.Background(), cfg, first.NextBefore, first.UIDValidity)
	if err != nil || len(second.Messages) != 50 || second.Messages[0].UID != 70 || second.NextBefore != 21 {
		t.Fatalf("second search page: %#v %v", second, err)
	}
	third, err := receiver.List(context.Background(), cfg, second.NextBefore, second.UIDValidity)
	if err != nil || len(third.Messages) != 20 || third.NextBefore != 0 {
		t.Fatalf("last search page: %#v %v", third, err)
	}
	cfg.Search = "café"
	cfg.Seen = "unread"
	cfg.From = "team@example.test"
	filtered, err := receiver.List(context.Background(), cfg, 0, 0)
	if err != nil || len(filtered.Messages) != 50 {
		t.Fatalf("combined search: %d %v", len(filtered.Messages), err)
	}
	for _, row := range filtered.Messages {
		if row.Seen || row.UID%2 != 1 || row.UID > 120 {
			t.Fatalf("filter ignored: %#v", row)
		}
	}
	cfg.Search = ""
	cfg.Seen = "read"
	cfg.From = ""
	read, err := receiver.List(context.Background(), cfg, 0, 0)
	if err != nil || len(read.Messages) != 50 {
		t.Fatalf("read filter: %d %v", len(read.Messages), err)
	}
	for _, row := range read.Messages {
		if !row.Seen {
			t.Fatal("read filter returned unread message")
		}
	}
	cfg.Search = `needle" OR ALL`
	cfg.Seen = ""
	empty, err := receiver.List(context.Background(), cfg, 0, 0)
	if err != nil || len(empty.Messages) != 0 || empty.NextBefore != 0 {
		t.Fatalf("search syntax injected: %#v %v", empty, err)
	}
	if len(audit.parts) != 0 {
		t.Fatal("search downloaded message bodies")
	}
	if err = user.Create("Archive", nil); err != nil {
		t.Fatal(err)
	}
	cfg.Mailbox = "Archive"
	cfg.Search = "needle"
	empty, err = receiver.List(context.Background(), cfg, 0, 0)
	if err != nil || len(empty.Messages) != 0 {
		t.Fatal("search escaped selected folder")
	}
}

func TestSearchRejectsUnboundedOrDynamicUIDRangesBeforeExpanding(t *testing.T) {
	for _, set := range []protocol.NumSet{protocol.SeqSet{{Start: 1, Stop: 2}}, protocol.UIDSet{{Start: 1, Stop: 0}}, protocol.UIDSet{{Start: 0, Stop: 10}}, protocol.UIDSet{{Start: 1, Stop: 4294967295}}} {
		if _, err := boundedSearchUIDs(&protocol.SearchData{All: set}); err == nil {
			t.Fatalf("unsafe search result accepted: %v", set)
		}
	}
	uids, err := boundedSearchUIDs(&protocol.SearchData{All: protocol.UIDSet{{Start: 10, Stop: 12}, {Start: 20, Stop: 20}}})
	if err != nil || fmt.Sprint(uids) != "[20 12 11 10]" {
		t.Fatalf("invalid bounded results: %v %v", uids, err)
	}
}

func TestSearchContinuesPastAnEmptyNewestWindow(t *testing.T) {
	receiver, cfg, user, _ := readerFixture(t)
	for i := 0; i < searchWindow+10; i++ {
		body := "recent unrelated message"
		if i == 0 {
			body = "oldest-needle"
		}
		raw := "From: team@example.test\r\nSubject: Mail\r\n\r\n" + body
		if _, err := user.Append("INBOX", literal{strings.NewReader(raw)}, &protocol.AppendOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Search = "oldest-needle"
	page, err := receiver.List(context.Background(), cfg, 0, 0)
	if err != nil || len(page.Messages) != 1 || page.Messages[0].UID != 1 || page.NextBefore != 0 {
		t.Fatalf("older search window missed: %#v %v", page, err)
	}
}
