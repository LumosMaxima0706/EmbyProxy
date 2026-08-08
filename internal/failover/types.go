package failover

import "time"

type Mode string

const (
	ModeAuto             Mode = "auto"
	ModeForceNOSLA       Mode = "force_nosla"
	ModeForceBWG         Mode = "force_bwg"
	ModeMaintenanceNOSLA Mode = "maintenance_nosla"
)

type NodeRole string

const (
	RolePrimary  NodeRole = "primary"
	RoleFallback NodeRole = "fallback"
)

type HealthStatus string

const (
	HealthHealthy  HealthStatus = "healthy"
	HealthDegraded HealthStatus = "degraded"
	HealthFailed   HealthStatus = "failed"
	HealthUnknown  HealthStatus = "unknown"
)

type TrafficQuality string

const (
	TrafficKnown   TrafficQuality = "known"
	TrafficStale   TrafficQuality = "stale"
	TrafficUnknown TrafficQuality = "unknown"
)

type Node struct {
	ID                   string        `json:"node_id"`
	Name                 string        `json:"name"`
	Role                 NodeRole      `json:"role"`
	PublicHost           string        `json:"public_host"`
	HealthURL            string        `json:"health_url"`
	Enabled              bool          `json:"enabled"`
	Maintenance          bool          `json:"maintenance_mode"`
	Priority             int           `json:"priority"`
	ResetDay             int           `json:"reset_day"`
	ResetTimezone        string        `json:"reset_timezone"`
	HealthStatus         HealthStatus  `json:"last_health_status"`
	ConsecutiveFailures  int           `json:"consecutive_health_failures"`
	ConsecutiveSuccesses int           `json:"consecutive_health_successes"`
	Traffic              TrafficSample `json:"traffic"`
}

type TrafficSample struct {
	NodeID        string         `json:"node_id"`
	CycleKey      string         `json:"cycle_key"`
	InboundBytes  int64          `json:"inbound_bytes"`
	OutboundBytes int64          `json:"outbound_bytes"`
	TotalBytes    int64          `json:"total_bytes"`
	QuotaBytes    int64          `json:"quota_bytes"`
	ThresholdPct  float64        `json:"threshold_percent"`
	Quality       TrafficQuality `json:"quality"`
	SampledAt     time.Time      `json:"sampled_at"`
}

func (s TrafficSample) UsagePercent() (float64, bool) {
	if s.Quality != TrafficKnown || s.QuotaBytes <= 0 {
		return 0, false
	}
	return float64(s.TotalBytes) * 100 / float64(s.QuotaBytes), true
}

type State struct {
	ActiveNodeID           string    `json:"active_node_id"`
	DesiredNodeID          string    `json:"desired_node_id"`
	ObservedDNSNodeID      string    `json:"observed_dns_node_id"`
	Mode                   Mode      `json:"mode"`
	CooldownUntil          time.Time `json:"cooldown_until"`
	LastTransitionAt       time.Time `json:"last_transition_at"`
	LastEvaluationAt       time.Time `json:"last_evaluation_at"`
	CurrentCycleKey        string    `json:"current_cycle_key"`
	ReconciliationRequired bool      `json:"reconciliation_required"`
}

type PolicyConfig struct {
	FailureThreshold     int
	RecoverySuccesses    int
	Cooldown             time.Duration
	TrafficThresholdPct  float64
	AllowUnknownRecovery bool
	MaxSwitchesPerWindow int
	SwitchWindow         time.Duration
}

func DefaultPolicyConfig() PolicyConfig {
	return PolicyConfig{
		FailureThreshold:     3,
		RecoverySuccesses:    3,
		Cooldown:             30 * time.Minute,
		TrafficThresholdPct:  97,
		AllowUnknownRecovery: false,
		MaxSwitchesPerWindow: 3,
		SwitchWindow:         24 * time.Hour,
	}
}

type Decision struct {
	NodeID string `json:"node_id"`
	Reason string `json:"reason"`
	Change bool   `json:"change"`
}

type Event struct {
	ID         int64     `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	EventType  string    `json:"event_type"`
	FromNodeID string    `json:"from_node_id,omitempty"`
	ToNodeID   string    `json:"to_node_id,omitempty"`
	Mode       Mode      `json:"mode"`
	ReasonCode string    `json:"reason_code"`
	Success    bool      `json:"success"`
}

type HealthResult struct {
	NodeID     string
	Kind       string
	Success    bool
	StatusCode int
	Latency    time.Duration
	ErrorCode  string
}
