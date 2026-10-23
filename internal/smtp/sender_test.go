package smtp

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/netip"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/outbound"
)

func TestSenderTLSAndSTARTTLS(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "SMTP test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}

	for _, test := range []struct{ security, outcome string }{
		{"tls", "accepted"}, {"starttls", "accepted"}, {"tls", "login"}, {"starttls", "missing-starttls"}, {"tls", "deadline"}, {"tls", "quit-disconnect"}, {"tls", "unknown"}, {"tls", "auth-rejected"}, {"tls", "untrusted"}, {"tls", "probe"}, {"starttls", "probe"}, {"tls", "oauth-accepted"}, {"starttls", "oauth-probe"}, {"tls", "oauth-unsupported"},
	} {
		t.Run(test.security+"/"+test.outcome, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			port := uint16(listener.Addr().(*net.TCPAddr).Port)
			type result struct {
				body string
				err  error
			}
			completed := make(chan result, 1)
			go func() {
				var received result
				defer func() { completed <- received }()
				raw, acceptErr := listener.Accept()
				if acceptErr != nil {
					received.err = acceptErr
					return
				}
				defer raw.Close()
				_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
				connection := raw
				secured := test.security == "tls"
				if secured {
					connection = tls.Server(raw, serverTLS)
				}
				protocol := textproto.NewConn(connection)
				if err := protocol.PrintfLine("220 smtp.test ready"); err != nil {
					received.err = err
					return
				}
				for {
					line, readErr := protocol.ReadLine()
					if readErr != nil {
						received.err = readErr
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						if test.outcome == "deadline" {
							time.Sleep(300 * time.Millisecond)
						}
						if test.outcome == "missing-starttls" {
							_ = protocol.PrintfLine("250 smtp.test")
						} else if !secured {
							_ = protocol.PrintfLine("250-smtp.test\r\n250 STARTTLS")
						} else if test.outcome == "oauth-accepted" || test.outcome == "oauth-probe" {
							_ = protocol.PrintfLine("250-smtp.test\r\n250 AUTH XOAUTH2")
						} else if test.outcome == "login" {
							_ = protocol.PrintfLine("250-smtp.test\r\n250 AUTH LOGIN")
						} else {
							_ = protocol.PrintfLine("250-smtp.test\r\n250 AUTH PLAIN")
						}
					case line == "STARTTLS":
						_ = protocol.PrintfLine("220 begin TLS")
						connection = tls.Server(raw, serverTLS)
						protocol = textproto.NewConn(connection)
						secured = true
					case strings.HasPrefix(line, "AUTH XOAUTH2 "):
						expected := base64.StdEncoding.EncodeToString([]byte("user=smtp-user\x01auth=Bearer access-token\x01\x01"))
						if strings.TrimPrefix(line, "AUTH XOAUTH2 ") != expected {
							received.err = errors.New("invalid XOAUTH2 frame")
							return
						}
						_ = protocol.PrintfLine("235 authenticated")
					case strings.HasPrefix(line, "AUTH LOGIN "):
						username := strings.TrimPrefix(line, "AUTH LOGIN ")
						_ = protocol.PrintfLine("334 UGFzc3dvcmQ6")
						password, _ := protocol.ReadLine()
						if username != base64.StdEncoding.EncodeToString([]byte("smtp-user")) || password != base64.StdEncoding.EncodeToString([]byte("smtp-secret")) {
							received.err = errors.New("invalid LOGIN exchange")
							return
						}
						_ = protocol.PrintfLine("235 authenticated")
					case strings.HasPrefix(line, "AUTH PLAIN"):
						if test.outcome == "auth-rejected" {
							_ = protocol.PrintfLine("535 invalid credentials")
						} else {
							_ = protocol.PrintfLine("235 authenticated")
						}
					case strings.HasPrefix(line, "MAIL FROM:") || strings.HasPrefix(line, "RCPT TO:"):
						_ = protocol.PrintfLine("250 accepted")
					case line == "DATA":
						_ = protocol.PrintfLine("354 send message")
						body, dataErr := protocol.ReadDotBytes()
						received.body = string(body)
						if dataErr != nil {
							received.err = dataErr
							return
						}
						if test.outcome == "unknown" {
							return
						}
						_ = protocol.PrintfLine("250 queued")
					case line == "QUIT":
						if test.outcome != "quit-disconnect" {
							_ = protocol.PrintfLine("221 bye")
						}
						return
					default:
						received.err = fmt.Errorf("unexpected SMTP command: %s", strings.SplitN(line, " ", 2)[0])
						return
					}
				}
			}()
			pool := roots
			if test.outcome == "untrusted" {
				pool = x509.NewCertPool()
			}
			sender := Sender{RootCAs: pool, OperationTimeout: 3 * time.Second, Policy: outbound.Policy{AllowedPorts: map[uint16]struct{}{port: {}}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}}
			var logs bytes.Buffer
			sender.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			if test.outcome == "deadline" {
				sender.OperationTimeout = 80 * time.Millisecond
			}
			message := BuiltMessage{Data: []byte("From: from@example.test\r\nTo: to@example.test\r\n\r\nHello\r\n"), EnvelopeFrom: "from@example.test", Recipients: []string{"to@example.test"}}
			cfg := Configuration{Host: "127.0.0.1", Port: port, Security: test.security, Username: "smtp-user", Password: []byte("smtp-secret")}
			if strings.HasPrefix(test.outcome, "oauth-") {
				cfg.Password = nil
				cfg.OAuthToken = []byte("access-token")
			}
			if test.outcome == "probe" || test.outcome == "oauth-probe" {
				err = sender.Test(context.Background(), cfg)
			} else {
				err = sender.Send(context.Background(), cfg, message)
			}
			switch test.outcome {
			case "unknown":
				if !errors.Is(err, ErrSubmissionUnknown) {
					t.Fatalf("expected ambiguous submission, got %v", err)
				}
			case "oauth-unsupported", "auth-rejected", "untrusted", "missing-starttls", "deadline":
				if err == nil {
					t.Fatal("invalid credentials or certificate accepted")
				}
			default:
				if err != nil {
					t.Fatalf("accepted message reported as failed: %v", err)
				}
			}
			for _, secret := range []string{"access-token", "smtp-user", "smtp-secret", "from@example.test", "to@example.test", "Hello"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatalf("SMTP logs exposed message or credential data: %s", logs.String())
				}
			}
			if !strings.Contains(logs.String(), `"protocol":"smtp"`) || !strings.Contains(logs.String(), `"operation":"connect"`) {
				t.Fatalf("SMTP connection logs missing: %s", logs.String())
			}
			if test.outcome == "unknown" && !strings.Contains(logs.String(), `"status":"unknown"`) {
				t.Fatalf("ambiguous delivery was not logged: %s", logs.String())
			}
			if test.outcome == "auth-rejected" && (!strings.Contains(logs.String(), `"stage":"authenticate"`) || !strings.Contains(logs.String(), `"status":"failed"`)) {
				t.Fatalf("authentication failure was not logged: %s", logs.String())
			}
			select {
			case received := <-completed:
				if (test.outcome == "probe" || test.outcome == "oauth-probe") && (received.err != nil || received.body != "") {
					t.Fatal("connection probe submitted a message")
				}
				if test.outcome == "oauth-accepted" || test.outcome == "accepted" || test.outcome == "quit-disconnect" || test.outcome == "unknown" {
					if received.err != nil || !strings.Contains(received.body, "Hello") {
						t.Fatalf("SMTP body missing: %v", received.err)
					}
				}
			case <-time.After(5 * time.Second):
				t.Fatal("SMTP test server did not finish")
			}
		})
	}
}
