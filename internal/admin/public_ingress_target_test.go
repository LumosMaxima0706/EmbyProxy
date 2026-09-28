package admin

import (
	"context"
	"net/netip"
	"testing"

	"embyproxy/internal/config"
	"embyproxy/internal/storage"
)

func TestPublicIngressTargetRequiresUniqueExpectedIP(t *testing.T) {
	h := newAuthTestHandler(t, config.Config{PublicIngressHost: "stream.example.com"})
	s := newPublicIngressSwitcher(h)
	n := storage.ProxyNode{PublicAddress: "https://edge.example.com", DNSExpectedIP: "1.1.1.1"}
	s.lookupIP = func(_ context.Context, _, _ string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	}
	if ip, err := s.proxyNodeIPv4(context.Background(), n); err != nil || ip.String() != "1.1.1.1" {
		t.Fatalf("unique target=%v err=%v", ip, err)
	}
	for _, answer := range [][]netip.Addr{{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("2.2.2.2")}, {netip.MustParseAddr("2001:db8::1")}, {netip.MustParseAddr("2.2.2.2")}} {
		s.lookupIP = func(_ context.Context, _, _ string) ([]netip.Addr, error) { return answer, nil }
		if _, err := s.proxyNodeIPv4(context.Background(), n); err == nil {
			t.Fatalf("unsafe target accepted: %v", answer)
		}
	}
	n.PublicAddress = "http://edge.example.com"
	if _, err := s.proxyNodeIPv4(context.Background(), n); err == nil {
		t.Fatal("HTTP origin accepted")
	}
	n.PublicAddress = "https://2.2.2.2"
	if _, err := s.proxyNodeIPv4(context.Background(), n); err == nil {
		t.Fatal("mismatched direct IP accepted")
	}
	n.PublicAddress = "https://1.1.1.1"
	if ip, err := s.proxyNodeIPv4(context.Background(), n); err != nil || ip.String() != "1.1.1.1" {
		t.Fatalf("direct IP rejected: %v %v", ip, err)
	}
	n.PublicAddress = "https://edge.example.com"
	n.DNSExpectedIP = ""
	s.lookupIP = func(_ context.Context, _, _ string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	}
	if ip, err := s.proxyNodeIPv4(context.Background(), n); err != nil || ip.String() != "1.1.1.1" {
		t.Fatalf("unique legacy domain rejected: %v %v", ip, err)
	}
	n.PublicAddress = "https://edge.example.com:443"
	if ip, err := s.proxyNodeIPv4(context.Background(), n); err != nil || ip.String() != "1.1.1.1" {
		t.Fatalf("explicit 443 rejected: %v %v", ip, err)
	}
	n.PublicAddress = "https://edge.example.com:8443"
	if _, err := s.proxyNodeIPv4(context.Background(), n); err == nil {
		t.Fatal("nonstandard port accepted")
	}
	n.PublicAddress = "https://edge.example.com/"
	if ip, err := s.proxyNodeIPv4(context.Background(), n); err != nil || ip.String() != "1.1.1.1" {
		t.Fatalf("trailing slash origin rejected: %v %v", ip, err)
	}
	n.PublicAddress = "https://edge.example.com/other"
	if _, err := s.proxyNodeIPv4(context.Background(), n); err == nil {
		t.Fatal("path-bearing origin accepted")
	}
}
