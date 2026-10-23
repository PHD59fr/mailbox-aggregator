package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

var (
	ErrUnsafeDestination = errors.New("unsafe outbound destination")
	ErrPortNotAllowed    = errors.New("outbound port is not allowed")
)

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}
type Policy struct {
	Resolver     Resolver
	AllowPrivate bool
	AllowedCIDRs []netip.Prefix
	AllowedPorts map[uint16]struct{}
	Timeout      time.Duration
}

func (p Policy) Resolve(ctx context.Context, host string, port uint16) ([]netip.AddrPort, error) {
	if _, ok := p.AllowedPorts[port]; !ok {
		return nil, ErrPortNotAllowed
	}
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if !validHostname(host) {
		return nil, ErrUnsafeDestination
	}
	var addresses []netip.Addr
	if literal, err := netip.ParseAddr(host); err == nil {
		addresses = []netip.Addr{literal}
	} else {
		resolver := p.Resolver
		if resolver == nil {
			resolver = net.DefaultResolver
		}
		resolved, lookupErr := resolver.LookupNetIP(ctx, "ip", host)
		if lookupErr != nil {
			return nil, fmt.Errorf("resolve mail host: %w", lookupErr)
		}
		addresses = resolved
	}
	if len(addresses) == 0 {
		return nil, errors.New("mail host resolved to no addresses")
	}
	var allowed []netip.AddrPort
	for _, address := range addresses {
		address = address.Unmap()
		if !p.allowed(address) {
			return nil, ErrUnsafeDestination
		}
		allowed = append(allowed, netip.AddrPortFrom(address, port))
	}
	return allowed, nil
}

func (p Policy) ResolveFirst(ctx context.Context, host string, port uint16) (netip.AddrPort, error) {
	addrs, err := p.Resolve(ctx, host, port)
	if err != nil {
		return netip.AddrPort{}, err
	}
	return addrs[0], nil
}

func (p Policy) DialContext(ctx context.Context, network, host string, port uint16) (net.Conn, error) {
	destinations, err := p.Resolve(ctx, host, port)
	if err != nil {
		return nil, err
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	dialer := net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, destination := range destinations {
		conn, err := dialer.DialContext(ctx, network, destination.String())
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (p Policy) allowed(address netip.Addr) bool {
	for _, prefix := range p.AllowedCIDRs {
		if prefix.Contains(address) {
			return true
		}
	}
	if address.IsUnspecified() || address.IsLoopback() || address.IsMulticast() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
		return false
	}
	if address.IsPrivate() && !p.AllowPrivate {
		return false
	}
	return address.IsGlobalUnicast()
}

func validHostname(value string) bool {
	if value == "" || len(value) > 253 || strings.ContainsAny(value, "\x00/\\:%[]") {
		if _, err := netip.ParseAddr(value); err != nil {
			return false
		}
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return true
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func HostPort(host string, port uint16) string {
	return net.JoinHostPort(host, strconv.Itoa(int(port)))
}
