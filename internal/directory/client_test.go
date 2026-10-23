package directory

import (
	"context"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/example/mail-aggregator/internal/outbound"
)

func TestCardDAVVerifiedTLSReadOnlyAndNoRedirect(t *testing.T) {
	var method string
	redirect := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		user, password, ok := r.BasicAuth()
		if !ok || user != "alice" || password != "secret" {
			t.Error("missing requested credentials")
		}
		if r.Header.Get("Depth") != "1" {
			t.Error("unbounded depth")
		}
		if redirect {
			w.Header().Set("Location", "https://evil.example/")
			w.WriteHeader(302)
			return
		}
		w.WriteHeader(207)
		w.Write([]byte(`<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav"><d:response><d:href>/contacts/alice.vcf</d:href><d:propstat><d:prop><d:getetag>&quot;v1&quot;</d:getetag><c:address-data>BEGIN:VCARD&#10;VERSION:3.0&#10;FN:Alice&#10;EMAIL:alice@example.test&#10;END:VCARD</c:address-data></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`))
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	host, port, _ := net.SplitHostPort(u.Host)
	p, _ := strconv.Atoi(port)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client := Client{Policy: outbound.Policy{AllowedPorts: map[uint16]struct{}{uint16(p): {}}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix(host + "/32")}}, RootCAs: roots}
	cfg := Config{Kind: "carddav", URL: server.URL + "/book/", Username: "alice", Password: "secret"}
	result, err := client.Read(context.Background(), cfg)
	if err != nil || len(result.Cards) != 1 || result.Cards[0].ETag != `"v1"` || method != "REPORT" {
		t.Fatal(result, err, method)
	}
	redirect = true
	if _, err = client.Read(context.Background(), cfg); err == nil {
		t.Fatal("redirect accepted")
	}
	client.RootCAs = nil
	if _, err = client.Read(context.Background(), cfg); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
	client.RootCAs = roots
	client.Policy.AllowedCIDRs = nil
	if _, err = client.Read(context.Background(), cfg); err == nil {
		t.Fatal("private destination accepted")
	}
}
func TestCardDAVBoundsParsing(t *testing.T) {
	for _, body := range []string{`<!DOCTYPE foo><x/>`, strings.Repeat("<x>", 30) + strings.Repeat("</x>", 30), `<x>`, `<d:multistatus xmlns:d="DAV:"><d:response><d:href>` + strings.Repeat("a", 65537) + `</d:href></d:response></d:multistatus>`} {
		if _, err := parseCards([]byte(body)); err == nil {
			t.Fatal("accepted malformed/oversized XML")
		}
	}
}
