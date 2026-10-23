package imap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"net/textproto"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/outbound"
)

func TestWatchIdleAndPolling(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "IMAP test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, _ := x509.ParseCertificate(der)
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	for _, test := range []struct {
		security     string
		idle, broken bool
		statusChange bool
	}{{"tls", true, false, false}, {"starttls", true, false, false}, {"tls", false, false, false}, {"tls", true, true, false}, {"tls", true, false, true}} {
		t.Run(fmt.Sprintf("%s-idle-%v-broken-%v-status-change-%v", test.security, test.idle, test.broken, test.statusChange), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			var workers sync.WaitGroup
			var connections atomic.Int32
			acceptDone := make(chan struct{})
			go func() {
				defer close(acceptDone)
				for {
					raw, err := listener.Accept()
					if err != nil {
						return
					}
					workers.Add(1)
					connections.Add(1)
					go func() {
						defer workers.Done()
						defer raw.Close()
						_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
						var conn net.Conn = raw
						if test.security == "tls" {
							conn = tls.Server(raw, serverTLS)
						}
						protocol := textproto.NewConn(conn)
						caps := "IMAP4rev1"
						if test.idle {
							caps += " IDLE"
						}
						if test.security == "starttls" {
							caps += " STARTTLS"
						}
						_ = protocol.PrintfLine("* OK [CAPABILITY %s] ready", caps)
						count := 1
						idleTag := ""
						for {
							line, err := protocol.ReadLine()
							if err != nil {
								return
							}
							fields := strings.Fields(line)
							if len(fields) < 1 {
								return
							}
							if fields[0] == "DONE" {
								_ = protocol.PrintfLine("%s OK IDLE ended", idleTag)
								continue
							}
							if len(fields) < 2 {
								return
							}
							tag, command := fields[0], strings.ToUpper(fields[1])
							switch command {
							case "STARTTLS":
								_ = protocol.PrintfLine("%s OK begin TLS", tag)
								conn = tls.Server(raw, serverTLS)
								protocol = textproto.NewConn(conn)
							case "CAPABILITY":
								_ = protocol.PrintfLine("* CAPABILITY %s", caps)
								_ = protocol.PrintfLine("%s OK caps", tag)
							case "LOGIN":
								if !strings.Contains(line, "imap-secret") {
									_ = protocol.PrintfLine("%s NO bad password", tag)
								} else {
									_ = protocol.PrintfLine("%s OK [CAPABILITY %s] logged in", tag, caps)
								}
							case "EXAMINE":
								_ = protocol.PrintfLine("* FLAGS (\\Seen)\r\n* 1 EXISTS\r\n* OK [UIDVALIDITY 7]\r\n%s OK [READ-ONLY] selected", tag)
							case "SELECT":
								t.Error("monitoring must use read-only EXAMINE")
								return
							case "STATUS":
								_ = protocol.PrintfLine("* STATUS INBOX (MESSAGES %d UNSEEN %d UIDNEXT %d UIDVALIDITY 7)", count, count, count+1)
								if test.statusChange && count == 1 {
									// The status snapshot is already stale before IDLE starts.
									count = 2
									_ = protocol.PrintfLine("* 2 EXISTS")
								}
								_ = protocol.PrintfLine("%s OK status", tag)
							case "IDLE":
								if test.broken {
									_ = protocol.PrintfLine("%s NO IDLE rejected", tag)
									continue
								}
								idleTag = tag
								_ = protocol.PrintfLine("+ idling")
								if count == 1 {
									count = 2
									_ = protocol.PrintfLine("* 2 EXISTS")
								}
							default:
								_ = protocol.PrintfLine("%s BAD unexpected command", tag)
							}
						}
					}()
				}
			}()
			defer func() { _ = listener.Close(); <-acceptDone; workers.Wait() }()
			port := uint16(listener.Addr().(*net.TCPAddr).Port)
			receiver := Receiver{RootCAs: roots, Policy: outbound.Policy{AllowedPorts: map[uint16]struct{}{port: {}}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			statuses := make(chan Status, 8)
			done := make(chan struct{})
			go func() {
				defer close(done)
				receiver.Watch(ctx, Configuration{Host: "127.0.0.1", Port: port, Security: test.security, Username: "imap-user", Password: []byte("imap-secret")}, 10*time.Second, func(status Status) { statuses <- status })
			}()
			for {
				select {
				case status := <-statuses:
					if status.Error != "" {
						t.Fatalf("monitor error: %s", status.Error)
					}
					if test.idle && !test.broken {
						if status.Mode != "idle" {
							t.Fatalf("expected IDLE: %+v", status)
						}
						if status.Messages < 2 {
							continue
						}
						if connections.Load() != 1 {
							t.Fatal("IDLE did not reuse its connection")
						}
					}
					if !test.idle || test.broken {
						if status.Mode != "polling" {
							continue
						}
					}
					cancel()
					select {
					case <-done:
						return
					case <-time.After(time.Second):
						t.Fatal("cancellation did not close IMAP")
						return
					}
				case <-ctx.Done():
					t.Fatal("no mailbox update received")
					return
				}
			}
		})
	}
}
