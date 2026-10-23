package imap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/example/mail-aggregator/internal/mailhtml"
	"github.com/example/mail-aggregator/internal/outbound"
)

type literal struct{ *strings.Reader }

func (r literal) Size() int64 { return int64(r.Len()) }

type fetchAudit struct {
	permanentFlags                 []protocol.Flag
	mu                             sync.Mutex
	parts                          []string
	writes                         bool
	failCopy                       bool
	moves, deletedStores, expunges int
}
type auditedSession struct {
	imapserver.Session
	t     *testing.T
	audit *fetchAudit
}

func (s *auditedSession) Select(name string, opts *protocol.SelectOptions) (*protocol.SelectData, error) {
	if !opts.ReadOnly && !s.audit.writes {
		s.t.Error("reader changed mailbox out of read-only mode")
	}
	data, err := s.Session.Select(name, opts)
	if err == nil && s.audit.writes {
		// The in-memory server advertises only flags already present, unlike
		// normal providers. Explicitly advertise supported persistent flags.
		for _, flag := range []protocol.Flag{protocol.FlagSeen, protocol.FlagFlagged, protocol.FlagAnswered} {
			if !slices.Contains(data.PermanentFlags, flag) {
				data.PermanentFlags = append(data.PermanentFlags, flag)
			}
		}
	}
	if err == nil && s.audit.permanentFlags != nil {
		data.PermanentFlags = s.audit.permanentFlags
	}
	return data, err
}
func (s *auditedSession) Move(w *imapserver.MoveWriter, set protocol.NumSet, dest string) error {
	s.audit.mu.Lock()
	s.audit.moves++
	s.audit.mu.Unlock()
	return s.Session.(imapserver.SessionMove).Move(w, set, dest)
}
func (s *auditedSession) Copy(set protocol.NumSet, dest string) (*protocol.CopyData, error) {
	if s.audit.failCopy {
		return nil, errors.New("test COPY rejected")
	}
	return s.Session.Copy(set, dest)
}
func (s *auditedSession) Store(w *imapserver.FetchWriter, set protocol.NumSet, flags *protocol.StoreFlags, options *protocol.StoreOptions) error {
	for _, flag := range flags.Flags {
		if flag == protocol.FlagDeleted {
			s.audit.mu.Lock()
			s.audit.deletedStores++
			s.audit.mu.Unlock()
		}
	}
	return s.Session.Store(w, set, flags, options)
}
func (s *auditedSession) Expunge(w *imapserver.ExpungeWriter, uids *protocol.UIDSet) error {
	if uids == nil {
		s.t.Error("an unrestricted EXPUNGE must never be issued")
	}
	s.audit.mu.Lock()
	s.audit.expunges++
	s.audit.mu.Unlock()
	return s.Session.Expunge(w, uids)
}
func (s *auditedSession) Fetch(w *imapserver.FetchWriter, set protocol.NumSet, opts *protocol.FetchOptions) error {
	for _, section := range opts.BodySection {
		if !section.Peek || section.Partial == nil {
			s.t.Error("body fetch must be bounded and PEEK")
		}
		s.audit.mu.Lock()
		if section.Specifier != protocol.PartSpecifierHeader {
			s.audit.parts = append(s.audit.parts, partID(section.Part))
		} else if section.Partial.Size > 32769 {
			s.t.Error("header fetch exceeded References budget")
		}
		s.audit.mu.Unlock()
	}
	if seq, ok := set.(protocol.SeqSet); ok {
		for _, window := range seq {
			if window.Stop-window.Start >= PageSize {
				s.t.Error("unbounded list window")
			}
		}
	}
	return s.Session.Fetch(w, set, opts)
}

const rawReaderMessage = "From: =?UTF-8?Q?=C3=89quipe?= <team@example.test>\r\nTo: Alex <alex@example.test>\r\nReferences: <root@example.test>\r\n <parent@example.test>\r\nSubject: =?UTF-8?Q?Un_=C3=A9t=C3=A9_tranquille?=\r\nDate: Thu, 22 Oct 2026 12:00:00 +0200\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=outer\r\n\r\n" +
	"--outer\r\nContent-Type: multipart/alternative; boundary=inner\r\n\r\n" +
	"--inner\r\nContent-Type: text/plain; charset=iso-8859-1\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nBonjour, bel =E9t=E9 !\r\n" +
	"--inner\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n<h1>Bonjour</h1><table><tr><td>Un =C3=A9t=C3=A9</td></tr></table><img src=3D\"https://tracker.test/pixel\"><script>alert(1)</script><a href=3D\"https://example.test\">Site</a>\r\n--inner--\r\n" +
	"--outer\r\nContent-Type: application/pdf; name=note.pdf\r\nContent-Disposition: attachment; filename=note.pdf\r\nContent-Transfer-Encoding: base64\r\n\r\naGVsbG8gZmlsZQ==\r\n--outer--\r\n"

func readerFixture(t *testing.T, caps ...protocol.CapSet) (Receiver, Configuration, *imapmemserver.User, *fetchAudit) {
	t.Helper()
	return readerFixtureWithSession(t, nil, caps...)
}

func readerFixtureWithSession(t *testing.T, wrap func(imapserver.Session) imapserver.Session, caps ...protocol.CapSet) (Receiver, Configuration, *imapmemserver.User, *fetchAudit) {
	return readerFixtureWithConnSession(t, func(_ *imapserver.Conn, s imapserver.Session) imapserver.Session {
		if wrap != nil {
			return wrap(s)
		}
		return s
	}, caps...)
}
func readerFixtureWithConnSession(t *testing.T, wrap func(*imapserver.Conn, imapserver.Session) imapserver.Session, caps ...protocol.CapSet) (Receiver, Configuration, *imapmemserver.User, *fetchAudit) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	user := imapmemserver.NewUser("reader", "test-password")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	memory := imapmemserver.New()
	memory.AddUser(user)
	audit := &fetchAudit{}
	options := &imapserver.Options{NewSession: func(connection *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		var session imapserver.Session = &auditedSession{Session: memory.NewSession(), t: t, audit: audit}
		if wrap != nil {
			session = wrap(connection, session)
		}
		return session, nil, nil
	}}
	if len(caps) > 0 {
		options.Caps = caps[0]
	}
	server := imapserver.New(options)
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(tls.NewListener(listener, tlsConfig)) }()
	t.Cleanup(func() { _ = server.Close(); _ = listener.Close(); <-done })
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	return Receiver{RootCAs: roots, Policy: outbound.Policy{AllowedPorts: map[uint16]struct{}{port: {}}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}}, Configuration{Host: "127.0.0.1", Port: port, Security: "tls", Username: "reader", Password: []byte("test-password")}, user, audit
}

func TestReaderPaginationAndMIME(t *testing.T) {
	receiver, cfg, user, audit := readerFixture(t)
	for i := 0; i < 120; i++ {
		if _, err := user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	page, err := receiver.List(ctx, cfg, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 50 || page.Messages[0].UID != 120 || page.NextBefore != 71 || page.Total != 120 {
		t.Fatalf("wrong first page: count=%d next=%d total=%d", len(page.Messages), page.NextBefore, page.Total)
	}
	if page.Messages[0].Subject != "Un été tranquille" || page.Messages[0].From[0].Name != "Équipe" {
		t.Fatalf("encoded headers not decoded: %+v", page.Messages[0])
	}
	audit.mu.Lock()
	fetched := len(audit.parts)
	audit.mu.Unlock()
	if fetched != 0 {
		t.Fatal("listing downloaded message bodies")
	}
	// New arrivals must not shift the next page or introduce duplicate messages.
	_, _ = user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{})
	second, err := receiver.List(ctx, cfg, page.NextBefore, page.UIDValidity)
	if err != nil || len(second.Messages) != 50 || second.Messages[0].UID != 70 || second.NextBefore != 21 {
		t.Fatalf("wrong second page: %v %+v", err, second)
	}
	third, err := receiver.List(ctx, cfg, second.NextBefore, page.UIDValidity)
	if err != nil || len(third.Messages) != 20 || third.Messages[19].UID != 1 || third.NextBefore != 0 {
		t.Fatalf("wrong final page: %v", err)
	}
	read, err := receiver.Read(ctx, cfg, 120, page.UIDValidity)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(read.References, " ") != "<root@example.test> <parent@example.test>" {
		t.Fatalf("lost References header: %v", read.References)
	}
	if !strings.Contains(read.Text, "bel été") || !strings.Contains(read.HTML, "<table>") || !strings.Contains(read.HTML, "Un été") {
		t.Fatalf("bad MIME: text=%q html=%q", read.Text, read.HTML)
	}
	for _, bad := range []string{"<script", "alert(1)"} {
		if strings.Contains(read.HTML, bad) {
			t.Fatalf("unsafe HTML: %s", bad)
		}
	}
	private, _ := mailhtml.Incoming(read.HTML, false)
	if strings.Contains(private, "<img") || strings.Contains(private, "tracker.test") {
		t.Fatal("private rendering retained remote content")
	}
	if len(read.Attachments) != 1 || read.Attachments[0].Filename != "note.pdf" {
		t.Fatalf("missing attachment metadata: %+v", read.Attachments)
	}
	audit.mu.Lock()
	fetchedParts := strings.Join(audit.parts, ",")
	audit.mu.Unlock()
	if fetchedParts != "1.1,1.2" {
		t.Fatalf("unexpected body fetches: %s", fetchedParts)
	}
	download, err := receiver.Attachment(ctx, cfg, 120, page.UIDValidity, read.Attachments[0].Part)
	if err != nil || string(download.Data) != "hello file" {
		t.Fatalf("attachment: %v %q", err, download.Data)
	}
	again, err := receiver.List(ctx, cfg, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if again.Messages[1].Seen {
		t.Fatal("opening a message changed its flags")
	}
	if _, err := receiver.Read(ctx, cfg, 120, page.UIDValidity+1); !errors.Is(err, ErrMailboxChanged) {
		t.Fatalf("UIDVALIDITY mismatch: %v", err)
	}
	if _, err := receiver.Read(ctx, cfg, 99999, page.UIDValidity); !errors.Is(err, ErrMessageMissing) {
		t.Fatalf("missing UID: %v", err)
	}
	if _, err := receiver.List(ctx, cfg, 99999, page.UIDValidity); !errors.Is(err, ErrMailboxChanged) {
		t.Fatalf("missing page anchor: %v", err)
	}
	if _, err := receiver.Attachment(ctx, cfg, 120, page.UIDValidity, "999"); !errors.Is(err, ErrMessageMissing) {
		t.Fatalf("missing part: %v", err)
	}
}

func TestReaderEmptyMailbox(t *testing.T) {
	receiver, cfg, _, _ := readerFixture(t)
	page, err := receiver.List(context.Background(), cfg, 0, 0)
	if err != nil || len(page.Messages) != 0 || page.Total != 0 || page.NextBefore != 0 {
		t.Fatal(fmt.Sprint(page, err))
	}
}

func TestMessageSourceAndExplicitReadFlags(t *testing.T) {
	receiver, cfg, user, audit := readerFixture(t)
	_, err := user.Append("INBOX", literal{strings.NewReader(rawReaderMessage)}, &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	page, err := receiver.List(context.Background(), cfg, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	source, err := receiver.Source(context.Background(), cfg, 1, page.UIDValidity)
	if err != nil || string(source) != rawReaderMessage {
		t.Fatalf("source changed: %v", err)
	}
	if err := receiver.Test(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	audit.writes = true
	if err := receiver.MarkSeen(context.Background(), cfg, 1, page.UIDValidity); err != nil {
		t.Fatal(err)
	}
	read, err := receiver.Read(context.Background(), cfg, 1, page.UIDValidity)
	if err != nil || !read.Seen {
		t.Fatalf("mark read failed: %v", err)
	}
	if err := receiver.MarkUnseen(context.Background(), cfg, 1, page.UIDValidity); err != nil {
		t.Fatal(err)
	}
	read, err = receiver.Read(context.Background(), cfg, 1, page.UIDValidity)
	if err != nil || read.Seen {
		t.Fatalf("mark unread failed: %v", err)
	}
	if err := receiver.MarkSeen(context.Background(), cfg, 1, page.UIDValidity+1); !errors.Is(err, ErrMailboxChanged) {
		t.Fatal("flag update ignored UIDVALIDITY")
	}
}

func TestPartLimitsAndOriginalAttachmentBytes(t *testing.T) {
	receiver, cfg, user, _ := readerFixture(t)
	raw := strings.Replace(rawReaderMessage, "Content-Type: application/pdf; name=note.pdf", "Content-Type: text/plain; charset=iso-8859-1; name=note.txt", 1)
	raw = strings.Replace(raw, "aGVsbG8gZmlsZQ==", "6Q==", 1) // one original Latin-1 byte, not UTF-8
	_, err := user.Append("INBOX", literal{strings.NewReader(raw)}, &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	page, err := receiver.List(context.Background(), cfg, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	client, closeConn, row, err := receiver.openMessage(context.Background(), cfg, 1, page.UIDValidity)
	if err != nil {
		t.Fatal(err)
	}
	defer closeConn()
	for _, part := range parts(row.BodyStructure) {
		if !part.attachment {
			continue
		}
		data, err := fetchPart(client, 1, part, 1)
		if err != nil || len(data) != 1 || data[0] != 0xe9 {
			t.Fatalf("download changed original bytes: %x %v", data, err)
		}
		part.body.Size = uint32(wireLimit(1, part.body.Encoding) + 1)
		if _, err := fetchPart(client, 1, part, 1); !errors.Is(err, ErrPartTooLarge) {
			t.Fatalf("oversized part not rejected: %v", err)
		}
	}
}

func TestReaderSkipsOversizedTextButKeepsSafeAlternative(t *testing.T) {
	receiver, cfg, user, audit := readerFixture(t)
	raw := "From: team@example.test\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=test\r\n\r\n--test\r\nContent-Type: text/plain\r\n\r\n" + strings.Repeat("x", MaxTextBytes+1) + "\r\n--test\r\nContent-Type: text/html\r\n\r\n<p>Small alternative</p>\r\n--test--\r\n"
	_, err := user.Append("INBOX", literal{strings.NewReader(raw)}, &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	page, err := receiver.List(context.Background(), cfg, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	read, err := receiver.Read(context.Background(), cfg, 1, page.UIDValidity)
	if err != nil || !read.Truncated || read.Text != "" || !strings.Contains(read.HTML, "Small alternative") {
		t.Fatalf("size limit/fallback failed: %v truncated=%v", err, read.Truncated)
	}
	audit.mu.Lock()
	defer audit.mu.Unlock()
	if strings.Join(audit.parts, ",") != "2" {
		t.Fatal("oversized text was downloaded")
	}
}
