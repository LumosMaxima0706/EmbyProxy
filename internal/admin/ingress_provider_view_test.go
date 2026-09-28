package admin

import (
	"testing"

	"embyproxy/internal/spaceship"
)

func TestIngressProviderViewMatchesExactRecords(t *testing.T) {
	base := []spaceship.Record{{Name: "stream", Type: "A", Address: "1.1.1.1", TTL: 60}, {Name: "stream.example.com", Type: "AAAA", Address: "2001:db8::1", TTL: 60}, {Name: "other", Type: "A", Address: "9.9.9.9", TTL: 300}}
	view := providerIngressRecord(base, "stream.example.com", "example.com")
	if view.Address != "1.1.1.1" || view.TTL != 60 || len(view.IPv6) != 1 || view.Error != "" {
		t.Fatalf("provider view=%+v", view)
	}
	duplicates := append(append([]spaceship.Record{}, base...), spaceship.Record{Name: "stream.example.com", Type: "A", Address: "2.2.2.2"})
	if got := providerIngressRecord(duplicates, "stream.example.com", "example.com"); got.Address != "" || got.Error != "provider_a_not_unique" {
		t.Fatalf("duplicate A accepted: %+v", got)
	}
	if got := providerIngressRecord(base, "stream.other.com", "example.com"); got.Error != "record_outside_zone" {
		t.Fatalf("outside zone accepted: %+v", got)
	}
	invalid := []spaceship.Record{{Name: "stream", Type: "A", Address: "not-an-ip"}}
	if got := providerIngressRecord(invalid, "stream.example.com", "example.com"); got.Error != "provider_a_invalid" {
		t.Fatalf("invalid A accepted: %+v", got)
	}
}
