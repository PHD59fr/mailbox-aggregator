package outbound

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

type resolver map[string][]netip.Addr

func (r resolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return r[host], nil
}

func TestPolicyRejectsSSRFAndMixedDNS(t *testing.T) {
	p := Policy{Resolver: resolver{"public.example": {netip.MustParseAddr("203.0.113.20")}, "mixed.example": {netip.MustParseAddr("203.0.113.20"), netip.MustParseAddr("127.0.0.1")}}, AllowedPorts: map[uint16]struct{}{993: {}}}
	if _, err := p.Resolve(context.Background(), "public.example", 993); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"127.0.0.1", "169.254.169.254", "::1", "mixed.example", "bad/name"} {
		if _, err := p.Resolve(context.Background(), host, 993); !errors.Is(err, ErrUnsafeDestination) {
			t.Errorf("%s: got %v", host, err)
		}
	}
	if _, err := p.Resolve(context.Background(), "public.example", 22); !errors.Is(err, ErrPortNotAllowed) {
		t.Fatalf("port allowed: %v", err)
	}
}

func TestPolicyExplicitPrivateAllowance(t *testing.T) {
	address := netip.MustParseAddr("10.0.0.5")
	p := Policy{Resolver: resolver{"mail.internal": {address}}, AllowedPorts: map[uint16]struct{}{993: {}}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}}
	if _, err := p.Resolve(context.Background(), "mail.internal", 993); err != nil {
		t.Fatal(err)
	}
}
