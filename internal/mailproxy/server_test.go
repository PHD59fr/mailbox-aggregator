package mailproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/netip"
	stdsmtp "net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"
	"github.com/example/mail-aggregator/internal/config"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/operations"
	"github.com/example/mail-aggregator/internal/outbound"
	"github.com/example/mail-aggregator/internal/user"
	"github.com/google/uuid"
)

const testPassword = "dedicated-client-password-123456"

func proxyFixture(t *testing.T) (*Service, uuid.UUID, *tls.Config) {
	t.Helper()
	dir := t.TempDir()
	store, err := filestore.New(filepath.Join(dir, "data"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	index, err := filestore.OpenUserIndex(store.IndexDirectory())
	if err != nil {
		t.Fatal(err)
	}
	ops := operations.NewRegistry()
	users := user.NewService(store, index, nil, ops)
	id, err := users.Register("alice@example.test", user.RegistrationFiles{AuthJSON: []byte(`{}`), Accounts: []byte(`{}`), SMTP: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"proxy.example.test"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	cfg := config.Config{IMAPProxyEnabled: true, SMTPProxyEnabled: true, IMAPProxyAddress: "127.0.0.1:0", SMTPProxyAddress: "127.0.0.1:0", MailProxyHostname: "proxy.example.test", MailProxyCertFile: certPath, MailProxyKeyFile: keyPath}
	s := New(cfg, store, index, users, outbound.Policy{AllowedPorts: make(map[uint16]struct{}), AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}, ops, nil)
	s.RootCAs = roots
	t.Cleanup(func() { s.Close(); ops.Close() })
	return s, id, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "proxy.example.test", RootCAs: roots, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}

func imapProvider(t *testing.T, s *Service, tlsConfig *tls.Config, security string) Target {
	t.Helper()
	memory := imapmemserver.New()
	u := imapmemserver.NewUser("provider-user", "provider-imap-secret")
	if err := u.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	message := "From: sender@example.test\r\nTo: recipient@example.test\r\nSubject: Proxy reader\r\n\r\nHello through the proxy\r\n"
	if _, err := u.Append("INBOX", strings.NewReader(message), &imap.AppendOptions{}); err != nil {
		t.Fatal(err)
	}
	memory.AddUser(u)
	server := imapserver.New(&imapserver.Options{NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return memory.NewSession(), nil, nil
	}, TLSConfig: tlsConfig, Caps: imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIdle: {}}})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	s.Policy.AllowedPorts[port] = struct{}{}
	var served net.Listener = listener
	if security == "tls" {
		served = tls.NewListener(listener, tlsConfig)
	}
	go func() { _ = server.Serve(served) }()
	t.Cleanup(func() { _ = server.Close(); _ = served.Close() })
	return Target{ID: "inbox", Host: "127.0.0.1", Port: port, Security: security, Username: "provider-user", Password: "provider-imap-secret"}
}

func smtpProvider(t *testing.T, s *Service, tlsConfig *tls.Config, security string) (Target, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	s.Policy.AllowedPorts[port] = struct{}{}
	messages := make(chan string, 8)
	var mu sync.Mutex
	connections := make(map[net.Conn]struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections[raw] = struct{}{}
			mu.Unlock()
			go func() {
				defer raw.Close()
				defer func() { mu.Lock(); delete(connections, raw); mu.Unlock() }()
				_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
				var conn net.Conn = raw
				secure := security == "tls"
				if secure {
					conn = tls.Server(raw, tlsConfig)
				}
				p := textproto.NewConn(conn)
				_ = p.PrintfLine("220 test ESMTP")
				authed := false
				for {
					command, err := p.ReadLine()
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(command, "EHLO"):
						if secure {
							_ = p.PrintfLine("250-provider\r\n250 AUTH PLAIN")
						} else {
							_ = p.PrintfLine("250-provider\r\n250 STARTTLS")
						}
					case command == "STARTTLS":
						_ = p.PrintfLine("220 Begin TLS")
						conn = tls.Server(raw, tlsConfig)
						p = textproto.NewConn(conn)
						secure = true
					case strings.HasPrefix(command, "AUTH PLAIN "):
						data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, "AUTH PLAIN "))
						authed = secure && err == nil && string(data) == "\x00provider-user\x00provider-smtp-secret"
						if authed {
							_ = p.PrintfLine("235 authenticated")
						} else {
							_ = p.PrintfLine("535 rejected")
						}
					case command == "QUIT":
						_ = p.PrintfLine("221 Bye")
						return
					case !authed:
						_ = p.PrintfLine("530 Authenticate first")
					case strings.HasPrefix(command, "MAIL FROM:"), strings.HasPrefix(command, "RCPT TO:"), command == "RSET", command == "NOOP":
						_ = p.PrintfLine("250 Accepted")
					case command == "DATA":
						_ = p.PrintfLine("354 Data")
						data, err := p.ReadDotBytes()
						if err != nil {
							return
						}
						messages <- string(data)
						_ = p.PrintfLine("250 queued")
					default:
						_ = p.PrintfLine("500 Unknown")
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
		mu.Lock()
		for conn := range connections {
			_ = conn.Close()
		}
		mu.Unlock()
	})
	return Target{ID: "sender", Host: "127.0.0.1", Port: port, Security: security, Username: "provider-user", Password: "provider-smtp-secret", SenderEmail: "from@example.test"}, messages
}

func TestGatewaysReadAndSendWithExternalClients(t *testing.T) {
	for _, security := range []string{"tls", "starttls"} {
		t.Run(security, func(t *testing.T) {
			s, id, options := proxyFixture(t)
			imapTarget := imapProvider(t, s, options, security)
			smtpTarget, messages := smtpProvider(t, s, options, security)
			if _, err := s.Save(id, testPassword, Targets{IMAP: &imapTarget, SMTP: &smtpTarget}, "*"); err != nil {
				t.Fatal(err)
			}
			if err := s.Start(); err != nil {
				t.Fatal(err)
			}
			// Real IMAP client over TLS, then provider CAPABILITY, SELECT and FETCH.
			client, err := imapclient.DialTLS(s.listeners[0].Addr().String(), &imapclient.Options{TLSConfig: options})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			if err = client.Authenticate(sasl.NewPlainClient("", "alice@example.test", testPassword)); err != nil {
				t.Fatal(err)
			}
			caps, err := client.Capability().Wait()
			if err != nil || !caps.Has(imap.CapIdle) {
				t.Fatalf("upstream capabilities missing: %v", err)
			}
			selected, err := client.Select("INBOX", nil).Wait()
			if err != nil || selected.NumMessages != 1 {
				t.Fatalf("upstream inbox unavailable: %v", err)
			}
			fetched, err := client.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{Envelope: true, BodySection: []*imap.FetchItemBodySection{{Peek: true}}}).Collect()
			if err != nil || len(fetched) != 1 || fetched[0].Envelope.Subject != "Proxy reader" {
				t.Fatalf("message not relayed: %v", err)
			}
			// A separate SMTP login on the same IP must not be throttled after success.
			conn, err := tls.Dial("tcp", s.listeners[1].Addr().String(), options)
			if err != nil {
				t.Fatal(err)
			}
			smtpClient, err := stdsmtp.NewClient(conn, "proxy.example.test")
			if err != nil {
				t.Fatal(err)
			}
			defer smtpClient.Close()
			if err = smtpClient.Auth(stdsmtp.PlainAuth("", "alice@example.test", testPassword, "proxy.example.test")); err != nil {
				t.Fatal(err)
			}
			if err = smtpClient.Mail("from@example.test"); err != nil {
				t.Fatal(err)
			}
			if err = smtpClient.Rcpt("to@example.test"); err != nil {
				t.Fatal(err)
			}
			writer, err := smtpClient.Data()
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.WriteString(writer, "From: from@example.test\r\nTo: to@example.test\r\nSubject: Client proxy\r\n\r\nSynthetic message\r\n")
			if err != nil {
				t.Fatal(err)
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case body := <-messages:
				if !strings.Contains(body, "Synthetic message") {
					t.Fatal("body missing")
				}
			case <-time.After(time.Second):
				t.Fatal("message not received")
			}
			if err = smtpClient.Quit(); err != nil {
				t.Fatal(err)
			}
			if err = client.Logout().Wait(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProxyEncryptionIsolationRotationAndVaultChanges(t *testing.T) {
	s, id, _ := proxyFixture(t)
	target := Target{ID: "sender", Host: "smtp.example.test", Port: 465, Security: "tls", Username: "secret-user", Password: "provider-password", SenderEmail: "from@example.test"}
	revision, err := s.Save(id, testPassword, Targets{SMTP: &target}, "*")
	if err != nil {
		t.Fatal(err)
	}
	data, _, err := s.Store.ReadUserFile(id, "proxy.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{testPassword, target.Password, target.Username, target.Host} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("plaintext proxy secret persisted")
		}
	}
	info, err := os.Stat(filepath.Join(s.Store.Root(), "users", id.String(), "proxy.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("unsafe file permissions")
	}
	if _, _, _, err = s.unlock("alice@example.test", "wrong", "smtp"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if _, _, _, err = s.unlock("missing@example.test", testPassword, "smtp"); err == nil {
		t.Fatal("missing user accepted")
	}
	if _, _, _, err = s.unlock("alice@example.test", testPassword, "imap"); err == nil {
		t.Fatal("unconfigured protocol accepted")
	}
	other, err := s.Users.Register("bob@example.test", user.RegistrationFiles{Accounts: []byte(`{}`), SMTP: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.WriteUserFile(other, "proxy.json", data, "*"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = s.unlock("bob@example.test", testPassword, "smtp"); err == nil {
		t.Fatal("encrypted record was transferable between users")
	}
	if _, err = s.Save(id, "new-password-for-client-123", Targets{SMTP: &target}, "*"); err == nil {
		t.Fatal("stale revision overwrote proxy")
	}
	revision, err = s.Save(id, "new-password-for-client-123", Targets{SMTP: &target}, revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = s.unlock("alice@example.test", testPassword, "smtp"); err == nil {
		t.Fatal("old password survived rotation")
	}
	if _, _, _, err = s.unlock("alice@example.test", "new-password-for-client-123", "smtp"); err != nil {
		t.Fatal(err)
	}
	_, smtpRevision, err := s.Store.ReadUserFile(id, "smtp.enc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.WriteUserFile(id, "smtp.enc", []byte(`{"changed":true}`), smtpRevision); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = s.unlock("alice@example.test", "new-password-for-client-123", "smtp"); err == nil {
		t.Fatal("obsolete provider secret survived vault modification")
	}
	status, _, err := s.Status(id)
	if err != nil || !status.Stale {
		t.Fatal("outdated proxy not reported")
	}
	if _, err = s.Save(id, "", Targets{}, revision); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = s.unlock("alice@example.test", "new-password-for-client-123", "smtp"); err == nil {
		t.Fatal("disabled proxy authenticated")
	}
}

func TestSMTPRoutesOnlyTheAuthenticatedUsersAddresses(t *testing.T) {
	s, id, options := proxyFixture(t)
	one, messagesOne := smtpProvider(t, s, options, "tls")
	two, messagesTwo := smtpProvider(t, s, options, "starttls")
	one.ID, one.SenderEmail = "one", "alice@example.test"
	two.ID, two.SenderEmail = "two", "alias@example.test"
	if _, err := s.Save(id, testPassword, Targets{Senders: []Target{one, two}}, "*"); err != nil {
		t.Fatal(err)
	}
	bob, err := s.Users.Register("bob@example.test", user.RegistrationFiles{Accounts: []byte(`{}`), SMTP: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	bobTarget := two
	bobTarget.ID, bobTarget.SenderEmail = "bob", "bob@example.test"
	if _, err = s.Save(bob, testPassword, Targets{SMTP: &bobTarget}, "*"); err != nil {
		t.Fatal(err)
	}
	if err = s.Start(); err != nil {
		t.Fatal(err)
	}
	client, err := gosmtp.DialTLS(s.listeners[1].Addr().String(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err = client.Auth(sasl.NewLoginClient("alice@example.test", testPassword)); err != nil {
		t.Fatal(err)
	}
	for _, from := range []string{"outsider@example.test", bobTarget.SenderEmail} {
		if err = client.Mail(from, nil); err == nil {
			t.Fatalf("unauthorized envelope sender %s accepted", from)
		}
	}
	if err = client.SendMail(one.SenderEmail, []string{"to@example.test"}, strings.NewReader("From: bob@example.test\r\n\r\nForged author\r\n")); err == nil {
		t.Fatal("forged message author accepted")
	}
	if err = client.SendMail(one.SenderEmail, []string{"to@example.test"}, strings.NewReader("From: alice@example.test\r\nFrom: bob@example.test\r\n\r\nDuplicate author\r\n")); err == nil {
		t.Fatal("duplicate From header accepted")
	}
	select {
	case <-messagesOne:
		t.Fatal("forged author reached provider one")
	case <-messagesTwo:
		t.Fatal("forged author reached provider two")
	default:
	}
	for _, route := range []struct {
		from     string
		messages <-chan string
	}{{one.SenderEmail, messagesOne}, {two.SenderEmail, messagesTwo}} {
		body := "From: " + route.from + "\r\nTo: to@example.test\r\nSubject: Routed\r\n\r\nSynthetic routed message\r\n"
		if err = client.SendMail(route.from, []string{"to@example.test"}, strings.NewReader(body)); err != nil {
			t.Fatal(err)
		}
		select {
		case message := <-route.messages:
			if !strings.Contains(message, route.from) {
				t.Fatal("wrong route")
			}
		case <-time.After(time.Second):
			t.Fatal("message routed to wrong provider")
		}
	}
	bobClient, err := gosmtp.DialTLS(s.listeners[1].Addr().String(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer bobClient.Close()
	if err = bobClient.Auth(sasl.NewPlainClient("", "bob@example.test", testPassword)); err != nil {
		t.Fatal(err)
	}
	if err = bobClient.Mail(one.SenderEmail, nil); err == nil {
		t.Fatal("bob used alice's sender")
	}
	if err = bobClient.SendMail(bobTarget.SenderEmail, []string{"to@example.test"}, strings.NewReader("From: bob@example.test\r\n\r\nBob's synthetic message\r\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-messagesTwo:
	case <-time.After(time.Second):
		t.Fatal("bob's own sender unavailable")
	}
}

func TestSMTPRejectsAmbiguousAndUnownedAuthorHeaders(t *testing.T) {
	for _, headers := range []string{"From: other@example.test", "From: owner@example.test\r\nSender: other@example.test", "From: owner@example.test\r\nResent-From: other@example.test", "From: owner@example.test\r\nFrom: owner@example.test", "From: owner@example.test, other@example.test", "Subject: missing author"} {
		if validAuthor([]byte(headers+"\r\n\r\nBody"), "owner@example.test") {
			t.Fatal("unauthorized author headers accepted")
		}
	}
	if !validAuthor([]byte("From: Owner <owner@example.test>\r\n\r\nBody"), "owner@example.test") {
		t.Fatal("display-name author rejected")
	}
}

func TestSMTPRevocationInterruptsAnOpenSubmission(t *testing.T) {
	s, id, options := proxyFixture(t)
	target, messages := smtpProvider(t, s, options, "tls")
	revision, err := s.Save(id, testPassword, Targets{SMTP: &target}, "*")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(); err != nil {
		t.Fatal(err)
	}
	client, err := gosmtp.DialTLS(s.listeners[1].Addr().String(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err = client.Auth(sasl.NewPlainClient("", "alice@example.test", testPassword)); err != nil {
		t.Fatal(err)
	}
	if err = client.Mail(target.SenderEmail, nil); err != nil {
		t.Fatal(err)
	}
	if err = client.Rcpt("recipient@example.test", nil); err != nil {
		t.Fatal(err)
	}
	writer, err := client.Data()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(writer, "From: from@example.test\r\n\r\nUnfinished message")
	if _, err = s.Save(id, "", Targets{}, revision); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- writer.Close() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("revoked submission was accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("revocation left SMTP submission blocked")
	}
	select {
	case <-messages:
		t.Fatal("unfinished message reached upstream")
	default:
	}
}

func TestGatewaysCreateAndReuseTLSOnDirectStartup(t *testing.T) {
	s, _, _ := proxyFixture(t)
	dir := t.TempDir()
	s.Config.MailProxyCertFile = filepath.Join(dir, "proxy-certs/fullchain.pem")
	s.Config.MailProxyKeyFile = filepath.Join(dir, "proxy-certs/privkey.pem")
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	cert, err := os.ReadFile(s.Config.MailProxyCertFile)
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(s.Config.MailProxyKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cert) {
		t.Fatal("generated certificate is invalid")
	}
	client, err := tls.Dial("tcp", s.listeners[0].Addr().String(), &tls.Config{RootCAs: roots, ServerName: s.Config.MailProxyHostname, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	greeting, err := bufio.NewReader(client).ReadString('\n')
	_ = client.Close()
	if err != nil || !strings.HasPrefix(greeting, "* OK") {
		t.Fatal("gateway did not start with its generated certificate")
	}
	s.Close()
	again := New(s.Config, s.Store, s.Index, s.Users, s.Policy, s.Operations, nil)
	defer again.Close()
	if err = again.Start(); err != nil {
		t.Fatal(err)
	}
	certAfter, _ := os.ReadFile(s.Config.MailProxyCertFile)
	keyAfter, _ := os.ReadFile(s.Config.MailProxyKeyFile)
	if !bytes.Equal(cert, certAfter) || !bytes.Equal(key, keyAfter) {
		t.Fatal("restart changed persistent TLS identity")
	}
}

func TestProxyDisableAndAccountDeletionDisconnectClients(t *testing.T) {
	for _, action := range []string{"disable", "delete", "invalidate"} {
		t.Run(action, func(t *testing.T) {
			s, id, options := proxyFixture(t)
			target := imapProvider(t, s, options, "tls")
			revision, err := s.Save(id, testPassword, Targets{IMAP: &target}, "*")
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Start(); err != nil {
				t.Fatal(err)
			}
			client, err := imapclient.DialTLS(s.listeners[0].Addr().String(), &imapclient.Options{TLSConfig: options})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			if err = client.Login("alice@example.test", testPassword).Wait(); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "disable":
				_, err = s.Save(id, "", Targets{}, revision)
			case "delete":
				err = s.Users.DeleteOwn(id)
			case "invalidate":
				s.Invalidate(id)
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-client.Closed():
			case <-time.After(time.Second):
				t.Fatal("revocation left client connected")
			}
		})
	}
}

func TestProxyRejectsUnauthenticatedRelayAndSharesFailedIPCooldown(t *testing.T) {
	s, id, options := proxyFixture(t)
	target, messages := smtpProvider(t, s, options, "tls")
	if _, err := s.Save(id, testPassword, Targets{SMTP: &target}, "*"); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	conn, err := tls.Dial("tcp", s.listeners[1].Addr().String(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	p := textproto.NewConn(conn)
	if _, _, err = p.ReadResponse(220); err != nil {
		t.Fatal(err)
	}
	_ = p.PrintfLine("MAIL FROM:<forged@example.test>")
	if _, _, err = p.ReadResponse(502); err != nil {
		t.Fatal("unauthenticated relay accepted")
	}
	_ = p.PrintfLine("EHLO test")
	_, _, _ = p.ReadResponse(250)
	_ = p.PrintfLine("AUTH PLAIN %s", base64.StdEncoding.EncodeToString([]byte("\x00alice@example.test\x00wrong")))
	if _, _, err = p.ReadResponse(535); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	blockedAt := s.failures["127.0.0.1"]
	s.mu.Unlock()
	if blockedAt.IsZero() {
		t.Fatal("failure did not impose cooldown")
	}
	if _, _, _, err = s.authenticate(conn, "alice@example.test", testPassword, "imap"); err == nil {
		t.Fatal("cooldown not shared across protocols")
	}
	select {
	case <-messages:
		t.Fatal("unauthenticated message reached provider")
	default:
	}
}

func TestLoginArguments(t *testing.T) {
	for _, tc := range []struct {
		input, continuation, user, password string
		bad                                 bool
	}{
		{`user password`, "", "user", "password", false},
		{`"a b" "p\\\"word"`, "", "a b", `p\"word`, false},
		{"{4}", "user {8}\r\npassword\r\n", "user", "password", false},
		{`"user""password"`, "", "", "", true},
		{`user password extra`, "", "", "", true},
		{"{9000}", "", "", "", true},
	} {
		t.Run(fmt.Sprint(tc.input), func(t *testing.T) {
			var response bytes.Buffer
			u, p, err := loginArgs(tc.input, bufio.NewReader(strings.NewReader(tc.continuation)), &response)
			if (err != nil) != tc.bad || (!tc.bad && (u != tc.user || p != tc.password)) {
				t.Fatalf("unexpected LOGIN parse: err=%v", err)
			}
		})
	}
}

func TestProxyRequiresValidCertificateAndRespectsOutboundPolicy(t *testing.T) {
	s, _, _ := proxyFixture(t)
	s.Config.MailProxyHostname = "wrong.example.test"
	if s.Start() == nil {
		t.Fatal("hostname mismatch accepted")
	}
	s.Config.MailProxyHostname = "proxy.example.test"
	s.Config.MailProxyKeyFile = "/does-not-exist"
	if s.Start() == nil {
		t.Fatal("missing key accepted")
	}
	if _, _, err := s.connectSMTP(context.Background(), Target{Host: "127.0.0.1", Port: 465, Security: "tls"}); err == nil {
		t.Fatal("outbound port policy bypassed")
	}
}
