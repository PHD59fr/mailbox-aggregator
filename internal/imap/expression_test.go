package imap

import (
	"context"
	"errors"
	"strings"
	"testing"

	protocol "github.com/emersion/go-imap/v2"
)

func TestSearchExpressionIsBoundedAndRejectsCommands(t *testing.T) {
	for _, input := range []string{"OR a", "a AND", "NOT", "()", "(a", "a)", `from:"unterminated`, "unknown:value", "is:admin", "a\r\nUID FETCH 1 BODY[]", strings.Repeat("(", 10) + "a" + strings.Repeat(")", 10), strings.Repeat("a ", 33), strings.Repeat("a", 2049)} {
		if _, err := ParseSearchExpression(input); !errors.Is(err, ErrSearchExpression) {
			t.Errorf("accepted %q: %v", input, err)
		}
	}
	criteria, err := ParseSearchExpression(`from:"Alice Example" OR (subject:invoice AND NOT is:read)`)
	if err != nil || len(criteria.Or) != 1 || criteria.Or[0][0].Header[0].Value != "Alice Example" || len(criteria.Or[0][1].Not) != 1 {
		t.Fatalf("wrong expression: %#v %v", criteria, err)
	}
	quoted, err := ParseSearchExpression(`"OR"`)
	if err != nil || len(quoted.Text) != 1 || quoted.Text[0] != "OR" {
		t.Fatalf("quoted keyword: %#v %v", quoted, err)
	}
}

func TestBooleanAndStarredSearchRunsOnProviderWithoutBodies(t *testing.T) {
	r, cfg, user, audit := readerFixture(t)
	for _, item := range []struct {
		from, subject string
		flags         []protocol.Flag
	}{
		{"alice@example.test", "Other", nil},
		{"bob@example.test", "Invoice", []protocol.Flag{protocol.FlagFlagged}},
		{"bob@example.test", "Invoice", []protocol.Flag{protocol.FlagSeen, protocol.FlagFlagged}},
		{"bob@example.test", "Other", nil},
	} {
		raw := "From: " + item.from + "\r\nSubject: " + item.subject + "\r\n\r\nBody"
		if _, err := user.Append("INBOX", literal{strings.NewReader(raw)}, &protocol.AppendOptions{Flags: item.flags}); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Expression = `from:alice OR (subject:invoice AND NOT is:read)`
	page, err := r.List(context.Background(), cfg, 0, 0)
	if err != nil || len(page.Messages) != 2 || page.Messages[0].UID != 2 || page.Messages[1].UID != 1 {
		t.Fatalf("boolean results: %#v %v", page, err)
	}
	cfg.Flagged = true
	page, err = r.List(context.Background(), cfg, 0, 0)
	if err != nil || len(page.Messages) != 1 || page.Messages[0].UID != 2 {
		t.Fatalf("starred intersection: %#v %v", page, err)
	}
	if len(audit.parts) != 0 {
		t.Fatal("search fetched bodies")
	}
}

func TestReferencedSummariesRequireValidityAndApplySearchWithoutBodies(t *testing.T) {
	r, cfg, user, audit := readerFixture(t)
	for _, subject := range []string{"Invoice", "Other", "Invoice"} {
		if _, err := user.Append("INBOX", literal{strings.NewReader("Subject: " + subject + "\r\n\r\nPrivate body")}, &protocol.AppendOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := r.List(context.Background(), cfg, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg.UIDs = []uint32{1, 2}
	cfg.Subject = "Invoice"
	result, err := r.List(context.Background(), cfg, 0, page.UIDValidity)
	if err != nil || len(result.Messages) != 1 || result.Messages[0].UID != 1 {
		t.Fatalf("reference scope lost: %#v %v", result, err)
	}
	if len(audit.parts) != 0 {
		t.Fatal("reference summaries fetched bodies")
	}
	if _, err = r.List(context.Background(), cfg, 0, page.UIDValidity+1); !errors.Is(err, ErrMailboxChanged) {
		t.Fatalf("validity not enforced: %v", err)
	}
	if _, err = r.List(context.Background(), cfg, 0, 0); !errors.Is(err, ErrInvalidAction) {
		t.Fatalf("missing validity allowed: %v", err)
	}
}
