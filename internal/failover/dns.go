package failover

import (
	"context"
	"errors"
	"sync"
	"time"
)

type DNSRecord struct {
	Name  string
	Type  string
	Value string
	TTL   int
}

type DNSChange struct {
	Name               string
	Type               string
	Value              string
	TTL                int
	DryRun             bool
	ProviderMode       DNSProviderMode `json:"-"`
	PreviousValue      string          `json:"-"`
	PreviousValueKnown bool            `json:"-"`
}

type DNSPlan struct {
	ID             string          `json:"dry_run_id"`
	TargetNodeID   string          `json:"target_node_id"`
	ProviderMode   DNSProviderMode `json:"provider_mode"`
	GeneratedAt    time.Time       `json:"generated_at"`
	ExpiresAt      time.Time       `json:"expires_at"`
	PreviousRecord *DNSRecord      `json:"-"`
	Change         DNSChange       `json:"change"`
	Note           string          `json:"note"`
}

type DNSPropagation struct {
	Verified bool   `json:"verified"`
	Detail   string `json:"detail"`
}

type DNSProvider interface {
	GetRecord(ctx context.Context, name, recordType string) (DNSRecord, error)
	UpdateARecord(ctx context.Context, name, value string, ttl int) error
	UpdateAAAARecord(ctx context.Context, name, value string, ttl int) error
	UpdateCNAMERecord(ctx context.Context, name, value string, ttl int) error
	DryRunUpdate(ctx context.Context, change DNSChange) (DNSPlan, error)
	VerifyPropagation(ctx context.Context, change DNSChange) (DNSPropagation, error)
}

type MockDNSProvider struct {
	mu         sync.RWMutex
	records    map[string]DNSRecord
	FailApply  bool
	FailVerify bool
	FailDryRun bool
	ApplyCount int
}

func NewMockDNSProvider() *MockDNSProvider {
	return &MockDNSProvider{records: make(map[string]DNSRecord)}
}

func (m *MockDNSProvider) ProviderMode() DNSProviderMode { return DNSProviderModeMock }

func (m *MockDNSProvider) key(name, recordType string) string { return name + "|" + recordType }

func (m *MockDNSProvider) GetRecord(_ context.Context, name, recordType string) (DNSRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.records[m.key(name, recordType)]
	if !ok {
		return DNSRecord{}, errors.New("record_not_found")
	}
	return record, nil
}

func (m *MockDNSProvider) update(name, recordType, value string, ttl int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailApply {
		return errors.New("mock_apply_failed")
	}
	m.records[m.key(name, recordType)] = DNSRecord{Name: name, Type: recordType, Value: value, TTL: ttl}
	m.ApplyCount++
	return nil
}

func (m *MockDNSProvider) UpdateARecord(ctx context.Context, name, value string, ttl int) error {
	return m.update(name, "A", value, ttl)
}
func (m *MockDNSProvider) UpdateAAAARecord(ctx context.Context, name, value string, ttl int) error {
	return m.update(name, "AAAA", value, ttl)
}
func (m *MockDNSProvider) UpdateCNAMERecord(ctx context.Context, name, value string, ttl int) error {
	return m.update(name, "CNAME", value, ttl)
}

func (m *MockDNSProvider) DryRunUpdate(_ context.Context, change DNSChange) (DNSPlan, error) {
	if m.FailDryRun {
		return DNSPlan{}, errors.New("mock_dry_run_failed")
	}
	return DNSPlan{Change: change, Note: "mock provider; no remote mutation"}, nil
}

func (m *MockDNSProvider) VerifyPropagation(_ context.Context, change DNSChange) (DNSPropagation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.FailVerify {
		return DNSPropagation{Verified: false, Detail: "mock_propagation_failed"}, errors.New("mock_propagation_failed")
	}
	record, ok := m.records[m.key(change.Name, change.Type)]
	if !ok || record.Value != change.Value {
		return DNSPropagation{Verified: false, Detail: "record_not_observed"}, errors.New("record_not_observed")
	}
	return DNSPropagation{Verified: true, Detail: "mock_propagation_verified"}, nil
}

func ApplyDNS(ctx context.Context, provider DNSProvider, change DNSChange) (DNSPropagation, error) {
	if provider == nil {
		return DNSPropagation{}, errors.New("dns_provider_unavailable")
	}
	normalized, err := normalizeDNSChange(change)
	if err != nil {
		return DNSPropagation{Verified: false, Detail: "invalid_change"}, err
	}
	change = normalized
	if change.DryRun {
		_, err := provider.DryRunUpdate(ctx, change)
		return DNSPropagation{Verified: false, Detail: "dry_run"}, err
	}
	err = nil
	switch change.Type {
	case "A":
		err = provider.UpdateARecord(ctx, change.Name, change.Value, change.TTL)
	case "AAAA":
		err = provider.UpdateAAAARecord(ctx, change.Name, change.Value, change.TTL)
	case "CNAME":
		err = provider.UpdateCNAMERecord(ctx, change.Name, change.Value, change.TTL)
	default:
		err = errors.New("unsupported_record_type")
	}
	if err != nil {
		return DNSPropagation{Verified: false, Detail: "apply_failed"}, err
	}
	return provider.VerifyPropagation(ctx, change)
}

func validateDNSChange(change DNSChange) error {
	_, err := normalizeDNSChange(change)
	return err
}
