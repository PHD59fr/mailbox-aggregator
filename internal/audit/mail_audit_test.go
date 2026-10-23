package audit

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"
	"github.com/example/mail-aggregator/internal/api"
	"github.com/example/mail-aggregator/internal/config"
	"github.com/example/mail-aggregator/internal/filestore"
	"github.com/example/mail-aggregator/internal/imap"
	"github.com/example/mail-aggregator/internal/mail"
	"github.com/example/mail-aggregator/internal/operations"
	"github.com/example/mail-aggregator/internal/outbound"
	"github.com/example/mail-aggregator/internal/session"
	"github.com/example/mail-aggregator/internal/smtp"
	"github.com/example/mail-aggregator/internal/user"
	"github.com/google/uuid"
)

type auditAvailability struct{}

func (auditAvailability) Available() bool { return true }

type auditReady struct{ URL, Token, CSRF string }

// This subprocess uses an explicitly authenticated synthetic fixture. OPAQUE
// itself has a separate JS/Go integration test; no fake login helper is used.
func TestMailAuditProcess(t *testing.T) {
	if os.Getenv("MAIL_AUDIT_CHILD") != "1" {
		return
	}
	root := os.Getenv("MAIL_AUDIT_ROOT")
	store, err := filestore.New(filepath.Join(root, "data"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	index, err := filestore.OpenUserIndex(store.IndexDirectory())
	if err != nil {
		t.Fatal(err)
	}
	sessions, _ := session.NewManager(30*time.Minute, time.Hour)
	ops := operations.NewRegistry()
	defer ops.Close()
	users := user.NewService(store, index, sessions, ops)
	entry, ok := index.Lookup("audit@example.test")
	if !ok {
		envelope := []byte(`{"version":1,"algorithm":"AES-256-GCM","key_version":1,"nonce":"AAAAAAAAAAAAAAAA","ciphertext":"AAAAAAAAAAAAAAAAAAAAAA"}`)
		id, err := users.Register("audit@example.test", user.RegistrationFiles{AuthJSON: []byte("{}"), VaultJSON: []byte("{}"), Accounts: envelope, SMTP: envelope, Preferences: envelope, Identities: envelope, ConfigEncrypted: envelope})
		if err != nil {
			t.Fatal(err)
		}
		entry.UserID = id
	}
	token, view, err := sessions.Create(entry.UserID)
	if err != nil {
		t.Fatal(err)
	}
	ports := map[uint16]struct{}{}
	for _, value := range strings.Split(os.Getenv("MAIL_AUDIT_PORTS"), ",") {
		n, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		ports[uint16(n)] = struct{}{}
	}
	roots := x509.NewCertPool()
	certificate, err := os.ReadFile(filepath.Join(root, "ca.pem"))
	if err != nil || !roots.AppendCertsFromPEM(certificate) {
		t.Fatal("missing test trust root")
	}
	policy := outbound.Policy{AllowedPorts: ports, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, Timeout: time.Second}
	logfile, err := os.OpenFile(filepath.Join(root, "logs", "http.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer logfile.Close()
	cfg := config.Config{MaxAccounts: 20, MaxRequestBytes: 2 << 20, MaxVaultFileBytes: 1 << 20, RequestTimeout: 10 * time.Second}
	logger := slog.New(slog.NewJSONHandler(logfile, nil))
	receiver := imap.Receiver{Policy: policy, RootCAs: roots, Logger: logger}
	router, err := api.NewRouter(api.Dependencies{Config: cfg, Logger: logger, Store: store, Index: index, Users: users, Sessions: sessions, Auth: auditAvailability{}, Operations: ops, Outbound: policy, Reader: receiver, Receiver: receiver, Sender: smtp.Sender{Policy: policy, RootCAs: roots, OperationTimeout: 10 * time.Second, Logger: logger}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router)
	defer server.Close()
	if err := json.NewEncoder(os.Stdout).Encode(auditReady{URL: server.URL, Token: token, CSRF: view.CSRFToken}); err != nil {
		t.Fatal(err)
	}
	select {}
}

type smtpCapture struct {
	received chan []byte
	reject   atomic.Bool
	block    atomic.Bool
	entered  chan struct{}
	release  chan struct{}
}
type captureSession struct {
	capture       *smtpCapture
	authenticated bool
}

func (s *captureSession) AuthMechanisms() []string { return []string{"PLAIN"} }
func (s *captureSession) Auth(mechanism string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(identity, username, password string) error {
		if username != "audit" || password != "synthetic-secret" {
			return gosmtp.ErrAuthFailed
		}
		s.authenticated = true
		return nil
	}), nil
}
func (s *captureSession) Reset()        {}
func (s *captureSession) Logout() error { return nil }
func (s *captureSession) Mail(string, *gosmtp.MailOptions) error {
	if !s.authenticated {
		return gosmtp.ErrAuthRequired
	}
	return nil
}
func (s *captureSession) Rcpt(string, *gosmtp.RcptOptions) error { return nil }
func (s *captureSession) Data(r io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(r, 20<<20))
	if err != nil {
		return err
	}
	if s.capture.block.Load() {
		s.capture.entered <- struct{}{}
		<-s.capture.release
		return &gosmtp.SMTPError{Code: 451, Message: "Interrupted test operation"}
	}
	if s.capture.reject.Load() {
		return &gosmtp.SMTPError{Code: 451, Message: "Synthetic provider unavailable"}
	}
	s.capture.received <- data
	return nil
}

type auditLiteral struct{ *bytes.Reader }

func (r auditLiteral) Size() int64 { return int64(r.Len()) }

func auditProviders(t *testing.T, root string) (uint16, uint16, *imapmemserver.User, *smtpCapture) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ca.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	imapListener, err := tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	memory := imapmemserver.New()
	account := imapmemserver.NewUser("audit", "synthetic-secret")
	if err := account.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	if err := account.Create("Sent", nil); err != nil {
		t.Fatal(err)
	}
	memory.AddUser(account)
	imapServer := imapserver.New(&imapserver.Options{NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return memory.NewSession(), nil, nil
	}})
	imapDone := make(chan struct{})
	go func() { defer close(imapDone); _ = imapServer.Serve(imapListener) }()
	t.Cleanup(func() { _ = imapServer.Close(); _ = imapListener.Close(); <-imapDone })
	capture := &smtpCapture{received: make(chan []byte, 8), entered: make(chan struct{}, 1), release: make(chan struct{})}
	smtpServer := gosmtp.NewServer(gosmtp.BackendFunc(func(*gosmtp.Conn) (gosmtp.Session, error) { return &captureSession{capture: capture}, nil }))
	smtpServer.Domain = "audit.test"
	smtpServer.ErrorLog = log.New(io.Discard, "", 0)
	smtpServer.ReadTimeout = 10 * time.Second
	smtpServer.WriteTimeout = 10 * time.Second
	smtpListener, err := tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	smtpDone := make(chan struct{})
	go func() { defer close(smtpDone); _ = smtpServer.Serve(smtpListener) }()
	t.Cleanup(func() { close(capture.release); _ = smtpServer.Close(); _ = smtpListener.Close(); <-smtpDone })
	return uint16(imapListener.Addr().(*net.TCPAddr).Port), uint16(smtpListener.Addr().(*net.TCPAddr).Port), account, capture
}

func startAuditProcess(t *testing.T, root string, ports string) (auditReady, func()) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMailAuditProcess$")
	cmd.Env = append(os.Environ(), "MAIL_AUDIT_CHILD=1", "MAIL_AUDIT_ROOT="+root, "MAIL_AUDIT_PORTS="+ports, "TMPDIR="+filepath.Join(root, "tmp"))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var stopped atomic.Bool
	stop := func() {
		if stopped.CompareAndSwap(false, true) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}
	t.Cleanup(stop)
	type startup struct {
		ready auditReady
		err   error
	}
	ready := make(chan startup, 1)
	go func() {
		var value auditReady
		err := json.NewDecoder(stdout).Decode(&value)
		ready <- startup{value, err}
	}()
	select {
	case result := <-ready:
		if result.err != nil {
			stop()
			t.Fatalf("audit process failed to start: %v %s", result.err, stderr.String())
		}
		return result.ready, stop
	case <-time.After(15 * time.Second):
		stop()
		t.Fatal("audit process startup timed out")
	}
	return auditReady{}, stop
}

func auditRequest(ctx context.Context, ready auditReady, method, path string, body any, want int) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, ready.URL+"/api/v1"+path, reader)
	if err != nil {
		return nil, err
	}
	req.AddCookie(&http.Cookie{Name: "mail_session", Value: ready.Token})
	req.Header.Set("X-CSRF-Token", ready.CSRF)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		return nil, fmt.Errorf("%s %s: wanted %d, got %d", method, path, want, response.StatusCode)
	}
	return io.ReadAll(response.Body)
}

func TestMA001AuthenticatedTLSMailAndCrashNonPersistence(t *testing.T) {
	if testing.Short() {
		t.Skip("process and TLS integration test")
	}
	root := t.TempDir()
	for _, name := range []string{"logs", "tmp"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	imapPort, smtpPort, provider, capture := auditProviders(t, root)
	ports := fmt.Sprintf("%d,%d", imapPort, smtpPort)
	ready, kill := startAuditProcess(t, root, ports)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	request := func(method, path string, body any, status int) []byte {
		t.Helper()
		data, err := auditRequest(ctx, ready, method, path, body, status)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	unique := uuid.NewString()
	markers := []string{"audit-subject-" + unique, "audit-body-" + unique, "audit-attachment-" + unique, "audit-header-" + unique}
	check := func() {
		t.Helper()
		if err := CheckNoMarkers([]string{root}, markers); err != nil {
			t.Fatal(err)
		}
	}
	credential := func(port uint16) map[string]any {
		return map[string]any{"host": "127.0.0.1", "port": port, "security": "tls", "username": "audit", "password": "synthetic-secret"}
	}
	activate := func() {
		sending := credential(smtpPort)
		sending["sender_email"] = "audit@example.test"
		request("POST", "/session/credentials", map[string]any{"mode": "merge", "lock_revision": 0, "lifetime_seconds": 300, "accounts": map[string]any{"imap": credential(imapPort)}, "smtp": map[string]any{"smtp": sending}}, 200)
	}
	activate()
	original, err := smtp.BuildMessage(mail.Identity{Email: "writer@example.test"}, mail.Draft{To: []mail.Address{{Email: "audit@example.test"}}, Subject: markers[0], Text: markers[1], Attachments: []mail.Attachment{{Filename: "audit.txt", ContentType: "text/plain", Data: []byte(markers[2])}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	original.Data = append([]byte("X-Audit-Marker: "+markers[3]+"\r\n"), original.Data...)
	appended, err := provider.Append("INBOX", auditLiteral{bytes.NewReader(original.Data)}, &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ref := fmt.Sprintf("/mailboxes/imap/messages/%d", appended.UID)
	query := fmt.Sprintf("?uid_validity=%d", appended.UIDValidity)
	var message imap.Message
	if err := json.Unmarshal(request("GET", ref+query, nil, 200), &message); err != nil {
		t.Fatal(err)
	}
	if message.Subject != markers[0] || !strings.Contains(message.Text, markers[1]) || len(message.Attachments) != 1 {
		t.Fatal("real IMAP read did not return the marker message")
	}
	attachment := request("GET", ref+"/attachments/"+message.Attachments[0].Part+query, nil, 200)
	if !bytes.Equal(attachment, []byte(markers[2])) {
		t.Fatal("attachment marker did not traverse IMAP and HTTP")
	}
	source := request("GET", ref+"/source"+query, nil, 200)
	if !bytes.Contains(source, []byte(markers[3])) {
		t.Fatal("header marker missing from source")
	}
	check()
	draft := map[string]any{"smtp_config_id": "smtp", "to": []map[string]string{{"email": "recipient@example.test"}}, "subject": markers[0], "text": markers[1], "attachments": []map[string]string{{"filename": "audit.txt", "content_type": "text/plain", "data": base64.StdEncoding.EncodeToString([]byte(markers[2]))}}, "sent_copy": map[string]string{"account_id": "imap", "folder": "Sent"}}
	var sent map[string]any
	if err := json.Unmarshal(request("POST", "/messages/send", draft, 200), &sent); err != nil {
		t.Fatal(err)
	}
	if copy, ok := sent["sent_copy"].(map[string]any); !ok || copy["status"] != "saved" {
		t.Fatal("real IMAP Sent append was not confirmed")
	}
	select {
	case data := <-capture.received:
		for _, marker := range markers[:3] {
			if !bytes.Contains(data, []byte(marker)) && !bytes.Contains(data, []byte(base64.StdEncoding.EncodeToString([]byte(marker)))) {
				t.Fatal("SMTP submission did not carry the marker")
			}
		}
	case <-ctx.Done():
		t.Fatal("no SMTP submission")
	}
	check()
	request("POST", ref+"/redirect"+query, map[string]any{"smtp_config_id": "smtp", "to": []map[string]string{{"email": "other@example.test"}}}, 200)
	select {
	case data := <-capture.received:
		if !bytes.Contains(data, []byte(markers[3])) {
			t.Fatal("redirect did not preserve the header marker")
		}
	case <-ctx.Done():
		t.Fatal("redirect not submitted")
	}
	check()
	unauthorized := ready
	unauthorized.Token = "invalid"
	if _, err := auditRequest(ctx, unauthorized, "POST", "/messages/send", draft, 200); err == nil {
		t.Fatal("401 was accepted as mail success")
	}
	capture.reject.Store(true)
	request("POST", "/messages/send", draft, 502)
	capture.reject.Store(false)
	check()
	capture.block.Store(true)
	completed := make(chan error, 1)
	go func() { _, err := auditRequest(ctx, ready, "POST", "/messages/send", draft, 200); completed <- err }()
	select {
	case <-capture.entered:
	case <-ctx.Done():
		t.Fatal("SMTP crash checkpoint not reached")
	}
	kill()
	select {
	case err := <-completed:
		if err == nil {
			t.Fatal("killed submission incorrectly confirmed")
		}
	case <-ctx.Done():
		t.Fatal("killed request did not end")
	}
	check()
	// A new process reopens the same persisted store; no old session survives.
	previous := ready
	ready, kill = startAuditProcess(t, root, ports)
	defer kill()
	if _, err := auditRequest(ctx, auditReady{URL: ready.URL, Token: previous.Token, CSRF: previous.CSRF}, "GET", "/session", nil, 401); err != nil {
		t.Fatal(err)
	}
	activate()
	request("GET", ref+query, nil, 200)
	check()
	// Positive witness in the exact supports used by this scenario.
	witness := filepath.Join(root, "tmp", "positive-control.bin")
	if err := os.WriteFile(witness, []byte("\x00"+markers[2]), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckNoMarkers([]string{root}, markers); err == nil {
		t.Fatal("the scenario's scanner missed its positive control")
	}
	if err := os.Remove(witness); err != nil {
		t.Fatal(err)
	}
	check()
}
