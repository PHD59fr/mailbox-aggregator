package imap

import (
	"context"
	"crypto/tls"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/example/mail-aggregator/internal/outbound"
)

type lostAppendSession struct {
	imapserver.Session
	connection *imapserver.Conn
	writes     *atomic.Int32
}

func (s *lostAppendSession) Append(folder string, reader protocol.LiteralReader, options *protocol.AppendOptions) (*protocol.AppendData, error) {
	s.writes.Add(1)
	result, err := s.Session.Append(folder, reader, options)
	if err == nil {
		s.connection.NetConn().Close()
	}
	return result, err
}
func TestUnacknowledgedCrossAccountAppendKeepsSourceAndDoesNotRetry(t *testing.T) {
	var writes atomic.Int32
	r, source, user, audit := readerFixture(t, protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapUIDPlus: {}})
	audit.writes = true
	r2, target, _, targetAudit := readerFixtureWithConnSession(t, func(c *imapserver.Conn, s imapserver.Session) imapserver.Session {
		return &lostAppendSession{Session: s, connection: c, writes: &writes}
	}, protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapUIDPlus: {}})
	targetAudit.writes = true
	conn, err := tls.Dial("tcp", outbound.HostPort(target.Host, target.Port), &tls.Config{RootCAs: r2.RootCAs, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	r.RootCAs.AddCert(conn.ConnectionState().PeerCertificates[0])
	conn.Close()
	r.Policy.AllowedPorts[target.Port] = struct{}{}
	date := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	_, err = user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{Time: date, Flags: []protocol.Flag{protocol.FlagFlagged, protocol.FlagSeen}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := r.List(context.Background(), source, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.TransferBetween(context.Background(), source, target, 1, page.UIDValidity, true); !errors.Is(err, ErrActionUnconfirmed) {
		t.Fatal("lost response was not ambiguous", err)
	}
	left, err := r.List(context.Background(), source, 0, 0)
	if err != nil || len(left.Messages) != 1 || audit.deletedStores != 0 || audit.expunges != 0 {
		t.Fatal("unconfirmed move touched source", err)
	}
	copied, err := r.List(context.Background(), target, 0, 0)
	if err != nil || len(copied.Messages) != 1 || !copied.Messages[0].Flagged || !copied.Messages[0].Seen {
		t.Fatal("copy metadata not preserved", err)
	}
	if writes.Load() != 1 {
		t.Fatal("unconfirmed APPEND was retried")
	}
}
func TestUnacknowledgedDraftReplacementKeepsPreviousVersion(t *testing.T) {
	var writes atomic.Int32
	r, cfg, user, audit := readerFixtureWithConnSession(t, func(c *imapserver.Conn, s imapserver.Session) imapserver.Session {
		return &lostAppendSession{Session: s, connection: c, writes: &writes}
	}, protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapUIDPlus: {}})
	audit.writes = true
	user.Create("Drafts", nil)
	user.Append("Drafts", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{Flags: []protocol.Flag{protocol.FlagDraft}})
	cfg.Mailbox = "Drafts"
	cfg.DraftsMailbox = "Drafts"
	page, err := r.List(context.Background(), cfg, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.SaveDraft(context.Background(), cfg, []byte(rawReaderMessage), 1, page.UIDValidity); !errors.Is(err, ErrActionUnconfirmed) {
		t.Fatal(err)
	}
	left, err := r.List(context.Background(), cfg, 0, 0)
	if err != nil || len(left.Messages) != 2 || audit.deletedStores != 0 || audit.expunges != 0 || writes.Load() != 1 {
		t.Fatal("unconfirmed replacement removed or retried a draft", err)
	}
	if !slices.ContainsFunc(left.Messages, func(m MessageSummary) bool { return m.UID == 1 && m.Draft }) {
		t.Fatal("previous draft disappeared")
	}
}
