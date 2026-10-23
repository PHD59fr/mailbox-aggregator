package imap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http/httptest"
	"net/netip"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/outbound"
)

func TestQuotaUnitsUnsupportedAndBoundedProviderResponse(t *testing.T) {
	certificateServer := httptest.NewTLSServer(nil)
	secure := certificateServer.TLS.Clone()
	roots := x509.NewCertPool()
	roots.AddCert(certificateServer.Certificate())
	certificateServer.Close()
	for _, mode := range []string{"supported", "unsupported", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				raw, err := listener.Accept()
				if err != nil {
					return
				}
				defer raw.Close()
				raw.SetDeadline(time.Now().Add(5 * time.Second))
				connection := textproto.NewConn(tls.Server(raw, secure))
				caps := "IMAP4rev1"
				if mode != "unsupported" {
					caps += " QUOTA"
				}
				connection.PrintfLine("* OK [CAPABILITY %s] ready", caps)
				for {
					line, err := connection.ReadLine()
					if err != nil {
						return
					}
					parts := strings.SplitN(line, " ", 3)
					if len(parts) < 2 {
						return
					}
					tag, command := parts[0], strings.ToUpper(parts[1])
					switch command {
					case "LOGIN":
						connection.PrintfLine("%s OK authenticated", tag)
					case "CAPABILITY":
						connection.PrintfLine("* CAPABILITY %s", caps)
						connection.PrintfLine("%s OK", tag)
					case "GETQUOTAROOT":
						if mode == "oversized" {
							connection.PrintfLine("* QUOTAROOT INBOX %s", strings.Repeat("x", 100000))
							return
						}
						connection.PrintfLine("* QUOTAROOT INBOX shared-root")
						connection.PrintfLine("* QUOTA shared-root (STORAGE 1024 4096 MESSAGE 25 100)")
						connection.PrintfLine("%s OK", tag)
					default:
						connection.PrintfLine("%s BAD unexpected", tag)
						return
					}
				}
			}()
			port := uint16(listener.Addr().(*net.TCPAddr).Port)
			receiver := Receiver{RootCAs: roots, Policy: outbound.Policy{AllowedPorts: map[uint16]struct{}{port: {}}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}}
			result, err := receiver.Quota(context.Background(), Configuration{Host: "127.0.0.1", Port: port, Security: "tls", Username: "user", Password: []byte("secret")})
			if mode == "oversized" {
				if err == nil {
					t.Fatal("unbounded quota response accepted")
				}
			} else if err != nil {
				t.Fatal(err)
			} else if mode == "unsupported" {
				if result.Supported || len(result.Resources) != 0 {
					t.Fatal(result)
				}
			} else {
				if !result.Supported || len(result.Resources) != 2 || result.Resources[0].Unit != "KiB" || result.Resources[0].Usage != 1024 || result.Resources[1].Unit != "items" {
					t.Fatal(fmt.Sprint(result))
				}
			}
			<-done
		})
	}
}
