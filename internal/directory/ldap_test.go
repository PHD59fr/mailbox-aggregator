package directory

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"net"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/example/mail-aggregator/internal/outbound"
	ber "github.com/go-asn1-ber/asn1-ber"
	"github.com/go-ldap/ldap/v3"
)

func TestLDAPVerifiedTLSReadOnlyEscapedFilter(t *testing.T) {
	certificateServer := httptest.NewTLSServer(nil)
	tlsConfig := certificateServer.TLS.Clone()
	roots := x509.NewCertPool()
	roots.AddCert(certificateServer.Certificate())
	certificateServer.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		raw, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer raw.Close()
		raw.SetDeadline(time.Now().Add(5 * time.Second))
		conn := tls.Server(raw, tlsConfig)
		result := func(id int64, tag ber.Tag, children ...*ber.Packet) error {
			packet := ber.NewSequence("")
			packet.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, id, ""))
			body := ber.Encode(ber.ClassApplication, ber.TypeConstructed, tag, nil, "")
			for _, c := range children {
				body.AppendChild(c)
			}
			packet.AppendChild(body)
			if tag == 5 {
				controls := ber.Encode(ber.ClassContext, ber.TypeConstructed, 0, nil, "")
				paging := ldap.NewControlPaging(50)
				paging.SetCookie([]byte("opaque-page"))
				controls.AppendChild(paging.Encode())
				packet.AppendChild(controls)
			}
			_, err := conn.Write(packet.Bytes())
			return err
		}
		str := func(value string) *ber.Packet {
			return ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, value, "")
		}
		status := func(id int64, tag ber.Tag) error {
			return result(id, tag, ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, 0, ""), str(""), str(""))
		}
		bind, err := ber.ReadPacket(conn)
		if err != nil {
			done <- err
			return
		}
		operation := bind.Children[1]
		if operation.Tag != 0 || operation.Children[1].Value != "uid=alice" || operation.Children[2].Data.String() != "secret" {
			done <- ErrDirectory
			return
		}
		if err = status(bind.Children[0].Value.(int64), 1); err != nil {
			done <- err
			return
		}
		search, err := ber.ReadPacket(conn)
		if err != nil {
			done <- err
			return
		}
		operation = search.Children[1]
		filter, err := ldap.DecompileFilter(operation.Children[6])
		if err != nil || filter != "(&(mail=*)(|(cn=*alice\\2a\\29\\28mail=\\2a*)(mail=*alice\\2a\\29\\28mail=\\2a*)))" {
			done <- ErrDirectory
			return
		}
		if operation.Tag != 3 || operation.Children[3].Value != int64(50) {
			done <- ErrDirectory
			return
		}
		attributes := ber.NewSequence("")
		for _, attribute := range [][2]string{{"cn", "Alice"}, {"mail", "alice@example.test"}} {
			a := ber.NewSequence("")
			a.AppendChild(str(attribute[0]))
			values := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSet, nil, "")
			values.AppendChild(str(attribute[1]))
			a.AppendChild(values)
			attributes.AppendChild(a)
		}
		id := search.Children[0].Value.(int64)
		if err = result(id, 4, str("uid=alice,dc=example"), attributes); err == nil {
			err = status(id, 5)
		}
		done <- err
	}()
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	client := Client{RootCAs: roots, Policy: outbound.Policy{AllowedPorts: map[uint16]struct{}{port: {}}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}}
	result, err := client.Read(context.Background(), Config{Kind: "ldap", Host: "127.0.0.1", Port: port, Security: "tls", Username: "uid=alice", Password: "secret", BaseDN: "dc=example", Query: "alice*)(mail=*"})
	if err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal("directory wire contract", err)
	}
	if result.NextPage != base64.RawURLEncoding.EncodeToString([]byte("opaque-page")) {
		t.Fatal("pagination cookie lost")
	}
	if len(result.Contacts) != 1 || result.Contacts[0].Email != "alice@example.test" {
		t.Fatal(result)
	}
}
