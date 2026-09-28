package admin

import (
	"net/netip"
	"strings"

	"embyproxy/internal/spaceship"
)

type ingressProviderView struct {
	Address string   `json:"address,omitempty"`
	TTL     int      `json:"ttl,omitempty"`
	IPv6    []string `json:"ipv6,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func providerIngressRecord(records []spaceship.Record, fqdn, managed string) ingressProviderView {
	view := ingressProviderView{}
	zone := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(managed)), ".")
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	if zone == "" || host == zone || !strings.HasSuffix(host, "."+zone) {
		view.Error = "record_outside_zone"
		return view
	}
	name := strings.TrimSuffix(host, "."+zone)
	count := 0
	for _, record := range records {
		got := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(record.Name)), ".")
		if got != name && got != host {
			continue
		}
		ip, err := netip.ParseAddr(record.Address)
		if strings.EqualFold(record.Type, "A") {
			count++
			if err == nil && ip.Is4() {
				view.Address, view.TTL = ip.String(), record.TTL
			} else {
				view.Error = "provider_a_invalid"
			}
		}
		if strings.EqualFold(record.Type, "AAAA") {
			if err == nil && ip.Is6() {
				view.IPv6 = append(view.IPv6, ip.String())
			} else {
				view.Error = "provider_aaaa_invalid"
			}
		}
	}
	if count != 1 {
		view.Address = ""
		view.Error = "provider_a_not_unique"
	}
	return view
}
