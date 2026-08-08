package storage

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type FailoverEventRecord struct {
	CreatedAt  int64
	EventType  string
	FromNodeID string
	ToNodeID   string
	Mode       string
	ReasonCode string
	Success    bool
}

type FailoverStateRecord struct {
	ActiveNodeID           string
	DesiredNodeID          string
	ObservedDNSNodeID      string
	Mode                   string
	CooldownUntil          int64
	LastTransitionAt       int64
	LastEvaluationAt       int64
	CurrentCycleKey        string
	ReconciliationRequired bool
}

func (s *Store) LoadFailoverState(ctx context.Context) (FailoverStateRecord, bool, error) {
	var record FailoverStateRecord
	var cooldown, transition, evaluation, reconciliation int64
	err := s.db.QueryRowContext(ctx, `
		SELECT active_node_id, desired_node_id, observed_dns_node_id, mode,
		cooldown_until, last_transition_at, last_evaluation_at, current_cycle_key, reconciliation_required
		FROM failover_state WHERE scope = 'default'
	`).Scan(&record.ActiveNodeID, &record.DesiredNodeID, &record.ObservedDNSNodeID, &record.Mode,
		&cooldown, &transition, &evaluation, &record.CurrentCycleKey, &reconciliation)
	if err == sql.ErrNoRows {
		return FailoverStateRecord{}, false, nil
	}
	if err != nil {
		return FailoverStateRecord{}, false, err
	}
	record.CooldownUntil, record.LastTransitionAt, record.LastEvaluationAt = cooldown, transition, evaluation
	record.ReconciliationRequired = reconciliation != 0
	return record, true, nil
}

func (s *Store) LoadFailoverEvents(ctx context.Context, limit int) ([]FailoverEventRecord, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT created_at, event_type, from_node_id, to_node_id, mode, reason_code, success
		FROM failover_events ORDER BY id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reversed []FailoverEventRecord
	for rows.Next() {
		var event FailoverEventRecord
		if err := rows.Scan(&event.CreatedAt, &event.EventType, &event.FromNodeID, &event.ToNodeID, &event.Mode, &event.ReasonCode, &event.Success); err != nil {
			return nil, err
		}
		reversed = append(reversed, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed, nil
}

// RecordFailoverHealthCheck stores only probe metadata and counters. It never
// stores request headers or credentials.
func (s *Store) RecordFailoverHealthCheck(ctx context.Context, nodeID string, checkedAt int64, kind string, success bool, statusCode int, latencyMS int64, failureCount, successCount int, errorSummary string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO failover_health_checks
		(node_id, checked_at, check_kind, success, status_code, latency_ms, consecutive_failure_count, consecutive_success_count, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, nodeID, checkedAt, kind, success, statusCode, latencyMS, failureCount, successCount, redactFailoverStorageText(errorSummary))
	return err
}

func (s *Store) RecordFailoverTrafficSample(ctx context.Context, sample TrafficSampleRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO failover_traffic_samples
		(node_id, sampled_at, cycle_key, source_type, inbound_bytes, outbound_bytes, total_bytes, quota_bytes, usage_percent, quality, source_ref)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, sample.NodeID, sample.SampledAt, sample.CycleKey, sample.SourceType,
		nullableInt64(sample.InboundBytes), nullableInt64(sample.OutboundBytes), nullableInt64(sample.TotalBytes), nullableInt64(sample.QuotaBytes), nullableFloat(sample.UsagePercent), sample.Quality, redactFailoverStorageText(sample.SourceRef))
	return err
}

type TrafficSampleRecord struct {
	NodeID        string
	SampledAt     int64
	CycleKey      string
	SourceType    string
	InboundBytes  *int64
	OutboundBytes *int64
	TotalBytes    *int64
	QuotaBytes    *int64
	UsagePercent  *float64
	Quality       string
	SourceRef     string
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func redactFailoverStorageText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{"authorization", "cookie", "api_key", "token", "password", "session", "secret", "://", "?", "&", "="} {
		if strings.Contains(lower, marker) {
			return "redacted"
		}
	}
	if len(value) > 80 {
		return value[:80]
	}
	return value
}

func (s *Store) UpsertFailoverNode(ctx context.Context, nodeID, name, role, publicHost string, enabled, maintenance bool, priority int) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO failover_nodes (node_id, name, role, public_host, health_url, proxy_base_url, enabled, maintenance_mode, priority, updated_at)
		VALUES (?, ?, ?, ?, '', '', ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET name=excluded.name, role=excluded.role,
		public_host=excluded.public_host, enabled=excluded.enabled,
		maintenance_mode=excluded.maintenance_mode, priority=excluded.priority, updated_at=excluded.updated_at
	`, nodeID, name, role, publicHost, enabled, maintenance, priority, time.Now().Unix())
	return err
}

func (s *Store) UpsertFailoverNodeConfig(ctx context.Context, nodeID, name, role, publicHost, healthURL, proxyBaseURL string, enabled, maintenance bool, priority, resetDay int, resetTimezone string, quotaBytes int64, thresholdPercent float64, sourceType string) error {
	if resetTimezone == "" {
		resetTimezone = "UTC"
	}
	if sourceType == "" {
		sourceType = "unknown"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO failover_nodes
		(node_id, name, role, public_host, health_url, proxy_base_url, enabled, maintenance_mode, priority, monthly_quota_bytes, traffic_threshold_percent, reset_day, reset_timezone, traffic_source_type, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET name=excluded.name, role=excluded.role,
		public_host=excluded.public_host, health_url=excluded.health_url, proxy_base_url=excluded.proxy_base_url,
		enabled=excluded.enabled, maintenance_mode=excluded.maintenance_mode, priority=excluded.priority,
		monthly_quota_bytes=excluded.monthly_quota_bytes, traffic_threshold_percent=excluded.traffic_threshold_percent,
		reset_day=excluded.reset_day, reset_timezone=excluded.reset_timezone, traffic_source_type=excluded.traffic_source_type,
		updated_at=excluded.updated_at
	`, nodeID, name, role, publicHost, healthURL, proxyBaseURL, enabled, maintenance, priority, quotaBytes, thresholdPercent, resetDay, resetTimezone, sourceType, time.Now().Unix())
	return err
}

func (s *Store) AppendRedactedFailoverEvent(ctx context.Context, createdAt int64, eventType, fromNodeID, toNodeID, mode, reason string, success bool) error {
	return s.AppendFailoverEvent(ctx, FailoverEventRecord{
		CreatedAt: createdAt, EventType: eventType, FromNodeID: fromNodeID,
		ToNodeID: toNodeID, Mode: mode, ReasonCode: reason, Success: success,
	})
}

func (s *Store) SaveFailoverState(ctx context.Context, active, desired, observed, mode, cycle string, cooldownUntil, lastTransition, lastEvaluation int64, reconciliation bool) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO failover_state (scope, active_node_id, desired_node_id, observed_dns_node_id, mode, current_cycle_key, cooldown_until, last_transition_at, last_evaluation_at, reconciliation_required, updated_at)
		VALUES ('default', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope) DO UPDATE SET active_node_id=excluded.active_node_id,
		desired_node_id=excluded.desired_node_id, observed_dns_node_id=excluded.observed_dns_node_id,
		mode=excluded.mode, current_cycle_key=excluded.current_cycle_key, cooldown_until=excluded.cooldown_until,
		last_transition_at=excluded.last_transition_at, last_evaluation_at=excluded.last_evaluation_at,
		reconciliation_required=excluded.reconciliation_required, updated_at=excluded.updated_at
	`, active, desired, observed, mode, cycle, cooldownUntil, lastTransition, lastEvaluation, reconciliation, time.Now().Unix())
	return err
}

func (s *Store) AppendFailoverEvent(ctx context.Context, event FailoverEventRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO failover_events
		(created_at, event_type, from_node_id, to_node_id, mode, reason_code, success)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, event.CreatedAt, event.EventType, event.FromNodeID, event.ToNodeID, event.Mode, event.ReasonCode, event.Success)
	return err
}

func (s *Store) RecordDNSUpdateRun(ctx context.Context, provider, name, recordType string, dryRun, success bool, providerResult, propagationResult string) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dns_update_runs
		(started_at, completed_at, provider_kind, record_name, record_type, dry_run, provider_result, propagation_result, success)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, now, now, provider, name, recordType, dryRun, providerResult, propagationResult, success)
	return err
}

func (s *Store) FailoverTableExists(ctx context.Context, table string) (bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&value)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}
