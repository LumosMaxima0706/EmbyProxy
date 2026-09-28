package admin

import (
	"context"
	"errors"
	"net/netip"
	"net/url"

	"embyproxy/internal/storage"
)

func (s *publicIngressSwitcher) proxyNodeIPv4(ctx context.Context, n storage.ProxyNode) (netip.Addr, error) {
	u, err := url.Parse(n.PublicAddress)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Port() != "" && u.Port() != "443") {
		return netip.Addr{}, errors.New("invalid_target_origin")
	}
	var expected netip.Addr
	if n.DNSExpectedIP != "" {
		expected, err = netip.ParseAddr(n.DNSExpectedIP)
		if err != nil || !expected.Is4() {
			return netip.Addr{}, errors.New("invalid_registered_ipv4")
		}
	}
	if direct, parseErr := netip.ParseAddr(u.Hostname()); parseErr == nil {
		if !direct.Is4() || (expected.IsValid() && direct != expected) {
			return netip.Addr{}, errors.New("target_address_mismatch")
		}
		return direct, nil
	}
	ips, err := s.lookupIP(ctx, "ip", u.Hostname())
	if err != nil || len(ips) == 0 {
		return netip.Addr{}, errors.New("target_dns_unavailable")
	}
	var unique netip.Addr
	for _, ip := range ips {
		if !ip.Is4() {
			return netip.Addr{}, errors.New("target_ipv6_present")
		}
		if unique.IsValid() && unique != ip {
			return netip.Addr{}, errors.New("target_multiple_ipv4")
		}
		unique = ip
	}
	if expected.IsValid() && unique != expected {
		return netip.Addr{}, errors.New("target_address_mismatch")
	}
	return unique, nil
}
