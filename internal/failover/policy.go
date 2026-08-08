package failover

import "time"

// Evaluate is pure policy logic. It never mutates state or calls a provider.
func Evaluate(nodes []Node, state State, cfg PolicyConfig, now time.Time) Decision {
	cfg = normalizePolicyConfig(cfg)
	byID := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	primary, fallback := findRoles(nodes)
	if primary.ID == "" || fallback.ID == "" {
		return Decision{Reason: "missing_primary_or_fallback"}
	}

	requested := ""
	switch state.Mode {
	case ModeForceNOSLA:
		if !primary.Enabled || primary.Maintenance || primary.HealthStatus == HealthFailed {
			return Decision{Reason: "forced_primary_not_eligible"}
		}
		requested = primary.ID
	case ModeForceBWG:
		if !fallback.Enabled || fallback.Maintenance || fallback.HealthStatus == HealthFailed {
			return Decision{Reason: "forced_fallback_not_eligible"}
		}
		requested = fallback.ID
	case ModeMaintenanceNOSLA:
		if !fallbackEligible(fallback) {
			return Decision{Reason: "no_eligible_fallback"}
		}
		requested = fallback.ID
	case ModeAuto, "":
		requested = autoDecision(primary, fallback, state, cfg, now)
	default:
		return Decision{Reason: "invalid_mode"}
	}
	if requested == "" {
		return Decision{Reason: "no_eligible_node"}
	}
	if _, ok := byID[requested]; !ok {
		return Decision{Reason: "selected_node_missing"}
	}
	return Decision{NodeID: requested, Change: requested != state.ActiveNodeID, Reason: reasonFor(requested, primary, fallback, state)}
}

func normalizePolicyConfig(cfg PolicyConfig) PolicyConfig {
	defaults := DefaultPolicyConfig()
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = defaults.FailureThreshold
	}
	if cfg.RecoverySuccesses <= 0 {
		cfg.RecoverySuccesses = defaults.RecoverySuccesses
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = defaults.Cooldown
	}
	if cfg.TrafficThresholdPct <= 0 {
		cfg.TrafficThresholdPct = defaults.TrafficThresholdPct
	}
	if cfg.MaxSwitchesPerWindow <= 0 {
		cfg.MaxSwitchesPerWindow = defaults.MaxSwitchesPerWindow
	}
	return cfg
}

func findRoles(nodes []Node) (Node, Node) {
	var primary, fallback Node
	for _, node := range nodes {
		if node.Role == RolePrimary && (primary.ID == "" || node.Priority < primary.Priority) {
			primary = node
		}
		if node.Role == RoleFallback && (fallback.ID == "" || node.Priority < fallback.Priority) {
			fallback = node
		}
	}
	return primary, fallback
}

func autoDecision(primary, fallback Node, state State, cfg PolicyConfig, now time.Time) string {
	primaryNeedsFailover := primary.Maintenance ||
		primary.ConsecutiveFailures >= cfg.FailureThreshold ||
		trafficAtThreshold(primary.Traffic, cfg.TrafficThresholdPct)
	fallbackEligible := fallbackEligible(fallback)
	primaryEligible := primary.Enabled && primary.HealthStatus != HealthFailed && !primary.Maintenance

	if state.ActiveNodeID == "" {
		if primaryEligible {
			return primary.ID
		}
		if fallbackEligible {
			return fallback.ID
		}
		return ""
	}
	if state.ActiveNodeID == primary.ID {
		if primaryNeedsFailover && fallbackEligible && cooldownExpired(state, now) {
			return fallback.ID
		}
		return primary.ID
	}
	if state.ActiveNodeID == fallback.ID && primaryEligible && fallbackEligible && cooldownExpired(state, now) {
		if primary.ConsecutiveSuccesses < cfg.RecoverySuccesses || !newKnownCycle(primary, state, cfg, now) {
			return fallback.ID
		}
		return primary.ID
	}
	if fallbackEligible {
		return fallback.ID
	}
	if primaryEligible {
		return primary.ID
	}
	return ""
}

func fallbackEligible(node Node) bool {
	return node.Enabled && node.HealthStatus != HealthFailed && !node.Maintenance
}

func trafficAtThreshold(sample TrafficSample, defaultThreshold float64) bool {
	if sample.Quality != TrafficKnown || sample.QuotaBytes <= 0 {
		return false
	}
	threshold := sample.ThresholdPct
	if threshold <= 0 {
		threshold = defaultThreshold
	}
	used, ok := sample.UsagePercent()
	return ok && used >= threshold
}

func newKnownCycle(node Node, state State, cfg PolicyConfig, now time.Time) bool {
	sample := node.Traffic
	if !cfg.AllowUnknownRecovery && (sample.Quality == TrafficUnknown || sample.Quality == TrafficStale) {
		return false
	}
	if node.ResetDay > 0 && sample.CycleKey != BillingCycleKey(now, node.ResetDay, node.ResetTimezone) {
		return false
	}
	return sample.CycleKey != "" && sample.CycleKey != state.CurrentCycleKey
}

func BillingCycleKey(now time.Time, resetDay int, timezone string) string {
	location := time.UTC
	if timezone != "" {
		if loaded, err := time.LoadLocation(timezone); err == nil {
			location = loaded
		}
	}
	local := now.In(location)
	if resetDay < 1 {
		resetDay = 1
	}
	year, month := local.Year(), local.Month()
	startDay := minInt(resetDay, daysInMonth(year, month, location))
	start := time.Date(year, month, startDay, 0, 0, 0, 0, location)
	if local.Before(start) {
		previous := start.AddDate(0, -1, 0)
		year, month = previous.Year(), previous.Month()
		startDay = minInt(resetDay, daysInMonth(year, month, location))
		start = time.Date(year, month, startDay, 0, 0, 0, 0, location)
	}
	return start.Format("2006-01-02")
}

func daysInMonth(year int, month time.Month, location *time.Location) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, location).Day()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func cooldownExpired(state State, now time.Time) bool {
	return state.CooldownUntil.IsZero() || !now.Before(state.CooldownUntil)
}

func reasonFor(selected string, primary, fallback Node, state State) string {
	if selected == fallback.ID {
		if primary.Maintenance {
			return "primary_maintenance"
		}
		if primary.ConsecutiveFailures > 0 {
			return "primary_health_failures"
		}
		return "primary_policy_threshold"
	}
	if state.ActiveNodeID == fallback.ID {
		return "primary_recovered_new_cycle"
	}
	return "primary_preferred"
}
