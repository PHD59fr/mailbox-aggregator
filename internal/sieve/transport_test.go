package sieve

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/outbound"
)

func TestVerifiedTLSScriptsConflictAndLostResponse(t *testing.T) {
	certificateServer := httptest.NewTLSServer(nil)
	serverTLS := certificateServer.TLS.Clone()
	roots := x509.NewCertPool()
	roots.AddCert(certificateServer.Certificate())
	certificateServer.Close()
	for _, security := range []string{"tls", "starttls"} {
		t.Run(security, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			acceptDone := make(chan struct{})
			var workers sync.WaitGroup
			var mu sync.Mutex
			content := "keep;\n"
			writes := 0
			go func() {
				defer close(acceptDone)
				for {
					raw, err := listener.Accept()
					if err != nil {
						return
					}
					workers.Add(1)
					go func() {
						defer workers.Done()
						defer raw.Close()
						mu.Lock()
						defer mu.Unlock()
						raw.SetDeadline(time.Now().Add(5 * time.Second))
						var connection net.Conn = raw
						if security == "tls" {
							connection = tls.Server(raw, serverTLS)
						}
						reader := bufio.NewReader(connection)
						write := func(value string) { io.WriteString(connection, value) }
						greeting := func() { write("\"SASL\" \"PLAIN\"\r\n\"SIEVE\" \"fileinto imap4flags\"\r\nOK\r\n") }
						if security == "tls" {
							greeting()
						} else {
							write("\"STARTTLS\"\r\n\"SASL\" \"LOGIN\"\r\nOK\r\n")
						}
						for {
							line, err := reader.ReadString('\n')
							if err != nil {
								return
							}
							line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
							switch {
							case line == "STARTTLS":
								write("OK\r\n")
								connection = tls.Server(raw, serverTLS)
								reader = bufio.NewReader(connection)
								greeting()
							case strings.HasPrefix(line, "AUTHENTICATE "):
								expected := `AUTHENTICATE "PLAIN" "` + base64.StdEncoding.EncodeToString([]byte("\x00alice\x00secret")) + `"`
								if line != expected {
									write("NO\r\n")
									return
								}
								write("OK\r\n")
							case line == "LISTSCRIPTS":
								write("\"main\" ACTIVE\r\nOK\r\n")
							case line == `GETSCRIPT "main"`:
								write(fmt.Sprintf("{%d}\r\n%s\r\nOK\r\n", len(content), content))
							case strings.HasPrefix(line, "CHECKSCRIPT ") || strings.HasPrefix(line, "PUTSCRIPT "):
								match := regexp.MustCompile(`\{([0-9]+)\+\}$`).FindStringSubmatch(line)
								if match == nil {
									return
								}
								n, _ := strconv.Atoi(match[1])
								body := make([]byte, n)
								if _, err := io.ReadFull(reader, body); err != nil {
									return
								}
								reader.ReadString('\n')
								if strings.HasPrefix(line, "PUTSCRIPT ") {
									writes++
									content = string(body)
									if strings.Contains(content, "lost-response") {
										return
									}
								}
								write("OK\r\n")
							default:
								write("NO\r\n")
							}
						}
					}()
				}
			}()
			defer func() { listener.Close(); <-acceptDone; workers.Wait() }()
			port := uint16(listener.Addr().(*net.TCPAddr).Port)
			client := Client{RootCAs: roots, Policy: outbound.Policy{AllowedPorts: map[uint16]struct{}{port: {}}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}}
			request := Request{Config: Config{Host: "127.0.0.1", Port: port, Security: security, Username: "alice", Password: "secret"}, Action: "get", Name: "main"}
			initial, err := client.Do(context.Background(), request)
			if err != nil || initial.Script != "keep;\n" || len(initial.Extensions) != 2 {
				t.Fatal(initial, err)
			}
			request.Action = "put"
			request.Script = "discard;\n"
			request.ExpectedHash = "stale"
			request.ExpectedActive = "main"
			if _, err = client.Do(context.Background(), request); err != ErrConflict {
				t.Fatal("missed concurrent change", err)
			}
			request.ExpectedHash = initial.Hash
			changed, err := client.Do(context.Background(), request)
			if err != nil || changed.Hash != hash(request.Script) {
				t.Fatal(changed, err)
			}
			request.ExpectedHash = changed.Hash
			request.Script = "# lost-response\nkeep;\n"
			if _, err = client.Do(context.Background(), request); err == nil {
				t.Fatal("lost mutation response reported success")
			}
			mu.Lock()
			defer mu.Unlock()
			if writes != 2 {
				t.Fatalf("automatic retry or unexpected mutation: %d", writes)
			}
		})
	}
}
