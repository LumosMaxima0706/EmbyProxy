package failover

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type testModeDNSProvider struct {
	*MockDNSProvider
	mode DNSProviderMode
}

func (p *testModeDNSProvider) ProviderMode() DNSProviderMode { return p.mode }

func TestDNSProviderModeDefaultsFailClosed(t *testing.T) {
	controller := NewController(testNodes(), DefaultPolicyConfig(), NewMockDNSProvider())
	_, err := controller.PrepareDNSApply(context.Background(), validGuardChange(), "bwg")
	if !errors.Is(err, ErrDNSProviderModeDenied) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseDNSRecordAllowlist(t *testing.T) {
	rules, err := ParseDNSRecordAllowlist("Stream.Example.:a,target.example:CNAME")
	if err != nil || len(rules) != 2 || rules[0].Name != "stream.example" || rules[0].Type != "A" {
		t.Fatalf("rules=%+v err=%v", rules, err)
	}
	for _, value := range []string{"stream.example", "stream.example:TXT", "bad_name.example:A", "192.0.2.1:A"} {
		if _, err := ParseDNSRecordAllowlist(value); err == nil {
			t.Fatalf("allowlist %q unexpectedly accepted", value)
		}
	}
}

func TestDNSProviderModesRequireExplicitSafeConfiguration(t *testing.T) {
	for _, mode := range []DNSProviderMode{DNSProviderModeMock, DNSProviderModeNoop, DNSProviderModeLocalOnly} {
		t.Run(string(mode), func(t *testing.T) {
			provider := &testModeDNSProvider{MockDNSProvider: NewMockDNSProvider(), mode: mode}
			controller := NewController(testNodes(), DefaultPolicyConfig(), provider)
			controller.ConfigureDNSGuard(DNSGuardConfig{ProviderMode: mode, Allowlist: guardAllowlist()})
			if _, err := controller.PrepareDNSApply(context.Background(), validGuardChange(), "bwg"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRealDNSProviderModeRequiresEnableAndRollbackCapability(t *testing.T) {
	for _, mode := range []DNSProviderMode{DNSProviderModeReal, DNSProviderModeExternal} {
		t.Run(string(mode), func(t *testing.T) {
			provider := &testModeDNSProvider{MockDNSProvider: NewMockDNSProvider(), mode: mode}
			controller := NewController(testNodes(), DefaultPolicyConfig(), provider)
			controller.ConfigureDNSGuard(DNSGuardConfig{ProviderMode: mode, Allowlist: guardAllowlist()})
			if _, err := controller.PrepareDNSApply(context.Background(), validGuardChange(), "bwg"); !errors.Is(err, ErrDNSProviderModeDenied) {
				t.Fatalf("without enable err = %v", err)
			}
			controller.ConfigureDNSGuard(DNSGuardConfig{ProviderMode: mode, AllowRealProvider: true, Allowlist: guardAllowlist()})
			if _, err := controller.PrepareDNSApply(context.Background(), validGuardChange(), "bwg"); !errors.Is(err, ErrDNSRollbackMetadataRequired) {
				t.Fatalf("without rollback capability err = %v", err)
			}
		})
	}
}

func TestDNSAllowlistAndRecordValueValidation(t *testing.T) {
	tests := []struct {
		name      string
		allowlist []DNSRecordRule
		change    DNSChange
		wantErr   error
	}{
		{name: "empty allowlist", change: validGuardChange(), wantErr: ErrDNSRecordNotAllowed},
		{name: "different name", allowlist: []DNSRecordRule{{Name: "other.example", Type: "A"}}, change: validGuardChange(), wantErr: ErrDNSRecordNotAllowed},
		{name: "different type", allowlist: guardAllowlist(), change: DNSChange{Name: "stream.example", Type: "AAAA", Value: "2001:db8::1", TTL: 60}, wantErr: ErrDNSRecordNotAllowed},
		{name: "A rejects IPv6", allowlist: guardAllowlist(), change: DNSChange{Name: "stream.example", Type: "A", Value: "2001:db8::1", TTL: 60}},
		{name: "AAAA rejects IPv4", allowlist: []DNSRecordRule{{Name: "stream.example", Type: "AAAA"}}, change: DNSChange{Name: "stream.example", Type: "AAAA", Value: "192.0.2.1", TTL: 60}},
		{name: "CNAME rejects IP", allowlist: []DNSRecordRule{{Name: "stream.example", Type: "CNAME"}}, change: DNSChange{Name: "stream.example", Type: "CNAME", Value: "192.0.2.1", TTL: 60}},
		{name: "CNAME rejects invalid hostname", allowlist: []DNSRecordRule{{Name: "stream.example", Type: "CNAME"}}, change: DNSChange{Name: "stream.example", Type: "CNAME", Value: "bad_name.example", TTL: 60}},
		{name: "valid A", allowlist: guardAllowlist(), change: validGuardChange()},
		{name: "valid AAAA", allowlist: []DNSRecordRule{{Name: "stream.example", Type: "AAAA"}}, change: DNSChange{Name: "stream.example", Type: "AAAA", Value: "2001:db8::1", TTL: 60}},
		{name: "valid CNAME", allowlist: []DNSRecordRule{{Name: "stream.example", Type: "CNAME"}}, change: DNSChange{Name: "stream.example", Type: "CNAME", Value: "target.example", TTL: 60}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := NewController(testNodes(), DefaultPolicyConfig(), NewMockDNSProvider())
			controller.ConfigureDNSGuard(DNSGuardConfig{ProviderMode: DNSProviderModeMock, Allowlist: test.allowlist})
			_, err := controller.PrepareDNSApply(context.Background(), test.change, "bwg")
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("err = %v, want %v", err, test.wantErr)
				}
				return
			}
			if strings.HasPrefix(test.name, "valid ") && err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(test.name, "valid ") && err == nil {
				t.Fatal("invalid record unexpectedly accepted")
			}
		})
	}
}

func TestDNSDryRunBindingMismatchExpiryAndSingleUse(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	newController := func() *Controller {
		controller := NewController(testNodes(), DefaultPolicyConfig(), NewMockDNSProvider())
		controller.ConfigureDNSGuard(DNSGuardConfig{ProviderMode: DNSProviderModeMock, Allowlist: guardAllowlist(), DryRunTTL: time.Minute})
		controller.SetNow(func() time.Time { return now })
		return controller
	}

	t.Run("missing id", func(t *testing.T) {
		controller := newController()
		if err := controller.ApplyDNSAndCommit(context.Background(), validGuardChange(), "bwg", ""); !errors.Is(err, ErrDNSDryRunRequired) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("mismatch consumes id", func(t *testing.T) {
		controller := newController()
		plan, err := controller.PrepareDNSApply(context.Background(), validGuardChange(), "bwg")
		if err != nil {
			t.Fatal(err)
		}
		mismatch := validGuardChange()
		mismatch.Value = "192.0.2.11"
		if err := controller.ApplyDNSAndCommit(context.Background(), mismatch, "bwg", plan.ID); !errors.Is(err, ErrDNSDryRunMismatch) {
			t.Fatalf("mismatch err = %v", err)
		}
		if err := controller.ApplyDNSAndCommit(context.Background(), validGuardChange(), "bwg", plan.ID); !errors.Is(err, ErrDNSDryRunRequired) {
			t.Fatalf("reuse err = %v", err)
		}
	})

	t.Run("target node mismatch", func(t *testing.T) {
		controller := newController()
		plan, err := controller.PrepareDNSApply(context.Background(), validGuardChange(), "bwg")
		if err != nil {
			t.Fatal(err)
		}
		if err := controller.ApplyDNSAndCommit(context.Background(), validGuardChange(), "nosla", plan.ID); !errors.Is(err, ErrDNSDryRunMismatch) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("expired", func(t *testing.T) {
		controller := newController()
		plan, err := controller.PrepareDNSApply(context.Background(), validGuardChange(), "bwg")
		if err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Minute)
		if err := controller.ApplyDNSAndCommit(context.Background(), validGuardChange(), "bwg", plan.ID); !errors.Is(err, ErrDNSDryRunExpired) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("successful apply consumes id", func(t *testing.T) {
		now = now.Add(time.Minute)
		controller := newController()
		plan, err := controller.PrepareDNSApply(context.Background(), validGuardChange(), "bwg")
		if err != nil {
			t.Fatal(err)
		}
		if err := controller.ApplyDNSAndCommit(context.Background(), validGuardChange(), "bwg", plan.ID); err != nil {
			t.Fatal(err)
		}
		if err := controller.ApplyDNSAndCommit(context.Background(), validGuardChange(), "bwg", plan.ID); !errors.Is(err, ErrDNSDryRunRequired) {
			t.Fatalf("reuse err = %v", err)
		}
	})

	t.Run("failed provider apply consumes id", func(t *testing.T) {
		now = now.Add(time.Minute)
		provider := NewMockDNSProvider()
		controller := NewController(testNodes(), DefaultPolicyConfig(), provider)
		controller.ConfigureDNSGuard(DNSGuardConfig{ProviderMode: DNSProviderModeMock, Allowlist: guardAllowlist(), DryRunTTL: time.Minute})
		controller.SetNow(func() time.Time { return now })
		plan, err := controller.PrepareDNSApply(context.Background(), validGuardChange(), "bwg")
		if err != nil {
			t.Fatal(err)
		}
		provider.FailApply = true
		if err := controller.ApplyDNSAndCommit(context.Background(), validGuardChange(), "bwg", plan.ID); err == nil {
			t.Fatal("expected provider apply failure")
		}
		provider.FailApply = false
		if err := controller.ApplyDNSAndCommit(context.Background(), validGuardChange(), "bwg", plan.ID); !errors.Is(err, ErrDNSDryRunRequired) {
			t.Fatalf("reuse err = %v", err)
		}
	})
}

func TestDNSDryRunCapturesPreviousRecordMetadata(t *testing.T) {
	provider := NewMockDNSProvider()
	if err := provider.UpdateARecord(context.Background(), "stream.example", "192.0.2.9", 60); err != nil {
		t.Fatal(err)
	}
	controller := NewController(testNodes(), DefaultPolicyConfig(), provider)
	controller.ConfigureDNSGuard(DNSGuardConfig{ProviderMode: DNSProviderModeMock, Allowlist: guardAllowlist()})
	plan, err := controller.PrepareDNSApply(context.Background(), validGuardChange(), "bwg")
	if err != nil {
		t.Fatal(err)
	}
	if plan.PreviousRecord == nil || plan.PreviousRecord.Value != "192.0.2.9" || !plan.Change.PreviousValueKnown {
		t.Fatalf("previous metadata = %+v change=%+v", plan.PreviousRecord, plan.Change)
	}
}

func validGuardChange() DNSChange {
	return DNSChange{Name: "stream.example", Type: "A", Value: "192.0.2.10", TTL: 60}
}

func guardAllowlist() []DNSRecordRule {
	return []DNSRecordRule{{Name: "stream.example", Type: "A"}}
}
