package mediaproxy

import (
	"context"
	"net"
	"net/netip"
	"strings"
)

func hostIsPrivate(ctx context.Context, host string) (bool, error) {
	return hostIsPrivateWithResolver(ctx, host, net.DefaultResolver)
}

func hostIsPrivateWithResolver(ctx context.Context, host string, resolver HostResolver) (bool, error) {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true, nil
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return addrIsPrivate(ip), nil
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	ips, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return false, err
	}
	for _, ip := range ips {
		if addrIsPrivate(ip) {
			return true, nil
		}
	}
	return false, nil
}

func addrIsPrivate(ip netip.Addr) bool {
	if !ip.IsValid() {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	if ip.Is4() {
		v4 := ip.As4()
		return v4[0] == 169 && v4[1] == 254 ||
			v4[0] == 100 && v4[1] == 100 ||
			v4[0] == 192 && v4[1] == 0 && v4[2] == 0
	}
	return ip.Is6() && (ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsPrivate() || strings.HasPrefix(ip.String(), "fc") || strings.HasPrefix(ip.String(), "fd"))
}
