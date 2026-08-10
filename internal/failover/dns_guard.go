package failover

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/netip"
	"strings"
	"time"
)

type DNSProviderMode string

const (
	DNSProviderModeUnknown   DNSProviderMode = ""
	DNSProviderModeMock      DNSProviderMode = "mock"
	DNSProviderModeNoop      DNSProviderMode = "noop"
	DNSProviderModeLocalOnly DNSProviderMode = "local-only"
	DNSProviderModeReal      DNSProviderMode = "real"
	DNSProviderModeExternal  DNSProviderMode = "external"
)

var (
	ErrDNSProviderModeDenied       = errors.New("dns provider mode is not allowed")
	ErrDNSRecordNotAllowed         = errors.New("dns record is not allowlisted")
	ErrDNSDryRunRequired           = errors.New("dns dry-run id is required")
	ErrDNSDryRunExpired            = errors.New("dns dry-run has expired")
	ErrDNSDryRunMismatch           = errors.New("dns dry-run does not match apply request")
	ErrDNSRollbackMetadataRequired = errors.New("dns rollback metadata is required")
	ErrDNSInvalidRecordName        = errors.New("invalid dns record name")
	ErrDNSInvalidRecordValue       = errors.New("invalid dns record value")
	ErrDNSInvalidTTL               = errors.New("invalid dns ttl")
	ErrDNSUnsupportedRecordType    = errors.New("unsupported dns record type")
)

type DNSRecordRule struct {
	Name string
	Type string
}

type DNSGuardConfig struct {
	ProviderMode          DNSProviderMode
	AllowRealProvider     bool
	RollbackMetadataReady bool
	Allowlist             []DNSRecordRule
	DryRunTTL             time.Duration
}

type pendingDNSApply struct {
	ID             string
	TargetNodeID   string
	ProviderMode   DNSProviderMode
	Change         DNSChange
	PreviousRecord DNSRecord
	PreviousKnown  bool
	GeneratedAt    time.Time
	ExpiresAt      time.Time
}

type modeAwareDNSProvider interface {
	ProviderMode() DNSProviderMode
}

func ParseDNSRecordAllowlist(raw string) ([]DNSRecordRule, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	rules := make([]DNSRecordRule, 0, len(parts))
	for _, part := range parts {
		name, recordType, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok || strings.Contains(recordType, ":") {
			return nil, ErrDNSRecordNotAllowed
		}
		name, err := normalizeDNSName(name)
		if err != nil {
			return nil, ErrDNSRecordNotAllowed
		}
		recordType = strings.ToUpper(strings.TrimSpace(recordType))
		if recordType != "A" && recordType != "AAAA" && recordType != "CNAME" {
			return nil, ErrDNSRecordNotAllowed
		}
		rules = append(rules, DNSRecordRule{Name: name, Type: recordType})
	}
	return rules, nil
}

func normalizeDNSGuardConfig(cfg DNSGuardConfig) DNSGuardConfig {
	cfg.ProviderMode = DNSProviderMode(strings.ToLower(strings.TrimSpace(string(cfg.ProviderMode))))
	if cfg.DryRunTTL <= 0 || cfg.DryRunTTL > 15*time.Minute {
		cfg.DryRunTTL = 5 * time.Minute
	}
	rules := make([]DNSRecordRule, 0, len(cfg.Allowlist))
	for _, rule := range cfg.Allowlist {
		name, err := normalizeDNSName(rule.Name)
		if err != nil {
			continue
		}
		recordType := strings.ToUpper(strings.TrimSpace(rule.Type))
		if recordType != "A" && recordType != "AAAA" && recordType != "CNAME" {
			continue
		}
		rules = append(rules, DNSRecordRule{Name: name, Type: recordType})
	}
	cfg.Allowlist = rules
	return cfg
}

func (c *Controller) ConfigureDNSGuard(cfg DNSGuardConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dnsGuard = normalizeDNSGuardConfig(cfg)
	c.pendingDNS = make(map[string]pendingDNSApply)
}

func (c *Controller) DNSProviderMode() DNSProviderMode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.dnsGuard.ProviderMode
}

func (c *Controller) dnsProviderAllowedLocked() error {
	configured := c.dnsGuard.ProviderMode
	provider, ok := c.dns.(modeAwareDNSProvider)
	if !ok || configured == DNSProviderModeUnknown || provider.ProviderMode() != configured {
		return ErrDNSProviderModeDenied
	}
	switch configured {
	case DNSProviderModeMock, DNSProviderModeNoop, DNSProviderModeLocalOnly:
		return nil
	case DNSProviderModeReal, DNSProviderModeExternal:
		if !c.dnsGuard.AllowRealProvider {
			return ErrDNSProviderModeDenied
		}
		if !c.dnsGuard.RollbackMetadataReady {
			return ErrDNSRollbackMetadataRequired
		}
		return nil
	default:
		return ErrDNSProviderModeDenied
	}
}

func (c *Controller) normalizeAllowedChangeLocked(change DNSChange) (DNSChange, error) {
	normalized, err := normalizeDNSChange(change)
	if err != nil {
		return DNSChange{}, err
	}
	for _, rule := range c.dnsGuard.Allowlist {
		if rule.Name == normalized.Name && rule.Type == normalized.Type {
			return normalized, nil
		}
	}
	return DNSChange{}, ErrDNSRecordNotAllowed
}

func normalizeDNSChange(change DNSChange) (DNSChange, error) {
	name, err := normalizeDNSName(change.Name)
	if err != nil {
		return DNSChange{}, ErrDNSInvalidRecordName
	}
	change.Name = name
	change.Type = strings.ToUpper(strings.TrimSpace(change.Type))
	change.Value = strings.TrimSpace(change.Value)
	if change.TTL < 0 || change.TTL > 86400 {
		return DNSChange{}, ErrDNSInvalidTTL
	}
	switch change.Type {
	case "A":
		address, parseErr := netip.ParseAddr(change.Value)
		if parseErr != nil || !address.Is4() {
			return DNSChange{}, ErrDNSInvalidRecordValue
		}
		change.Value = address.String()
	case "AAAA":
		address, parseErr := netip.ParseAddr(change.Value)
		if parseErr != nil || !address.Is6() || address.Is4() || address.Zone() != "" {
			return DNSChange{}, ErrDNSInvalidRecordValue
		}
		change.Value = address.String()
	case "CNAME":
		if address, parseErr := netip.ParseAddr(change.Value); parseErr == nil && address.IsValid() {
			return DNSChange{}, ErrDNSInvalidRecordValue
		}
		value, nameErr := normalizeDNSName(change.Value)
		if nameErr != nil {
			return DNSChange{}, ErrDNSInvalidRecordValue
		}
		change.Value = value
	default:
		return DNSChange{}, ErrDNSUnsupportedRecordType
	}
	return change, nil
}

func normalizeDNSName(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
	if name == "" || len(name) > 253 {
		return "", errors.New("invalid dns name")
	}
	if address, err := netip.ParseAddr(name); err == nil && address.IsValid() {
		return "", errors.New("dns name cannot be an address")
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("invalid dns label")
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return "", errors.New("invalid dns label")
			}
		}
	}
	return name, nil
}

func newDNSDryRunID() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func sameDNSChange(left, right DNSChange) bool {
	return left.Name == right.Name && left.Type == right.Type && left.Value == right.Value && left.TTL == right.TTL
}
