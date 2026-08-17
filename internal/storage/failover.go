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

type DNSUpdateRunRecord struct {
	StartedAt         int64
	CompletedAt       int64
	ProviderKind      string
	RecordName        string
	RecordType        string
	PreviousValue     string
	DesiredValue      string
	RollbackReady     bool
	DryRun            bool
	ProviderResult    string
	PropagationResult string
	Success           bool
}

type FailoverNodeRuntime struct {
	NodeID               string
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
	Traffic              TrafficSampleRecord
}

func (s *Store) LoadFailoverNodeRuntime(ctx context.Context) (map[string]FailoverNodeRuntime, error) {
	result := make(map[string]FailoverNodeRuntime)
	healthRows, err := s.db.QueryContext(ctx, `
		SELECT node_id, success, consecutive_failure_count, consecutive_success_count, last_error
		FROM failover_health_checks ORDER BY id DESC
	`)
	if err != nil {
		return nil, err
	}
	for healthRows.Next() {
		var nodeID, lastError string
		var success, failures, successes int
		if err := healthRows.Scan(&nodeID, &success, &failures, &successes, &lastError); err != nil {
			healthRows.Close()
			return nil, err
		}
		if _, exists := result[nodeID]; exists {
			continue
		}
		result[nodeID] = FailoverNodeRuntime{NodeID: nodeID, ConsecutiveFailures: failures, ConsecutiveSuccesses: successes}
	}
	if err := healthRows.Err(); err != nil {
		healthRows.Close()
		return nil, err
	}
	healthRows.Close()
	trafficRows, err := s.db.QueryContext(ctx, `
		SELECT node_id, sampled_at, cycle_key, source_type, inbound_bytes, outbound_bytes, total_bytes, quota_bytes, usage_percent, quality, source_ref
		FROM failover_traffic_samples ORDER BY id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer trafficRows.Close()
	for trafficRows.Next() {
		var sample TrafficSampleRecord
		var inbound, outbound, total, quota sql.NullInt64
		var usage sql.NullFloat64
		if err := trafficRows.Scan(&sample.NodeID, &sample.SampledAt, &sample.CycleKey, &sample.SourceType, &inbound, &outbound, &total, &quota, &usage, &sample.Quality, &sample.SourceRef); err != nil {
			return nil, err
		}
		if _, exists := result[sample.NodeID]; !exists {
			result[sample.NodeID] = FailoverNodeRuntime{NodeID: sample.NodeID}
		}
		runtime := result[sample.NodeID]
		if runtime.Traffic.SampledAt != 0 {
			continue
		}
		if inbound.Valid {
			value := inbound.Int64
			sample.InboundBytes = &value
		}
		if outbound.Valid {
			value := outbound.Int64
			sample.OutboundBytes = &value
		}
		if total.Valid {
			value := total.Int64
			sample.TotalBytes = &value
		}
		if quota.Valid {
			value := quota.Int64
			sample.QuotaBytes = &value
		}
		if usage.Valid {
			value := usage.Float64
			sample.UsagePercent = &value
		}
		runtime.Traffic = sample
		result[sample.NodeID] = runtime
	}
	return result, trafficRows.Err()
}

// CommitFailoverTransition keeps an in-memory transition unpublished until
// both its event and state rows commit in one SQLite transaction.
func (s *Store) CommitFailoverTransition(ctx context.Context, state FailoverStateRecord, event FailoverEventRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := appendFailoverEventTx(ctx, tx, event); err != nil {
		return err
	}
	if err := saveFailoverStateTx(ctx, tx, state); err != nil {
		return err
	}
	return tx.Commit()
}

// CommitDNSUpdate atomically records the provider result, audit event, and
// resulting local state. The provider call itself occurs before this method.
func (s *Store) CommitDNSUpdate(ctx context.Context, run DNSUpdateRunRecord, state FailoverStateRecord, event FailoverEventRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := recordDNSUpdateRunTx(ctx, tx, run); err != nil {
		return err
	}
	if err := appendFailoverEventTx(ctx, tx, event); err != nil {
		return err
	}
	if err := saveFailoverStateTx(ctx, tx, state); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) LoadFailoverState(ctx context.Context) (FailoverStateRecord, bool, error) {
	var record FailoverStateRecord
	var active, desired, observed, cycle sql.NullString
	var mode sql.NullString
	var cooldown, transition, evaluation, reconciliation sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT active_node_id, desired_node_id, observed_dns_node_id, mode,
		cooldown_until, last_transition_at, last_evaluation_at, current_cycle_key, reconciliation_required
		FROM failover_state WHERE scope = 'default'
	`).Scan(&active, &desired, &observed, &mode,
		&cooldown, &transition, &evaluation, &cycle, &reconciliation)
	if err == sql.ErrNoRows {
		return FailoverStateRecord{}, false, nil
	}
	if err != nil {
		return FailoverStateRecord{}, false, err
	}
	if active.Valid {
		record.ActiveNodeID = active.String
	}
	if desired.Valid {
		record.DesiredNodeID = desired.String
	}
	if observed.Valid {
		record.ObservedDNSNodeID = observed.String
	}
	if mode.Valid {
		record.Mode = mode.String
	}
	if cooldown.Valid {
		record.CooldownUntil = cooldown.Int64
	}
	if transition.Valid {
		record.LastTransitionAt = transition.Int64
	}
	if evaluation.Valid {
		record.LastEvaluationAt = evaluation.Int64
	}
	if cycle.Valid {
		record.CurrentCycleKey = cycle.String
	}
	record.ReconciliationRequired = reconciliation.Valid && reconciliation.Int64 != 0
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
	return saveFailoverStateTx(ctx, s.db, FailoverStateRecord{ActiveNodeID: active, DesiredNodeID: desired, ObservedDNSNodeID: observed, Mode: mode, CurrentCycleKey: cycle, CooldownUntil: cooldownUntil, LastTransitionAt: lastTransition, LastEvaluationAt: lastEvaluation, ReconciliationRequired: reconciliation})
}

type sqlFailoverExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func saveFailoverStateTx(ctx context.Context, exec sqlFailoverExecutor, state FailoverStateRecord) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO failover_state (scope, active_node_id, desired_node_id, observed_dns_node_id, mode, current_cycle_key, cooldown_until, last_transition_at, last_evaluation_at, reconciliation_required, updated_at)
		VALUES ('default', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope) DO UPDATE SET active_node_id=excluded.active_node_id,
		desired_node_id=excluded.desired_node_id, observed_dns_node_id=excluded.observed_dns_node_id,
		mode=excluded.mode, current_cycle_key=excluded.current_cycle_key, cooldown_until=excluded.cooldown_until,
		last_transition_at=excluded.last_transition_at, last_evaluation_at=excluded.last_evaluation_at,
		reconciliation_required=excluded.reconciliation_required, updated_at=excluded.updated_at
	`, state.ActiveNodeID, state.DesiredNodeID, state.ObservedDNSNodeID, state.Mode, state.CurrentCycleKey, state.CooldownUntil, state.LastTransitionAt, state.LastEvaluationAt, state.ReconciliationRequired, time.Now().Unix())
	return err
}

func (s *Store) AppendFailoverEvent(ctx context.Context, event FailoverEventRecord) error {
	return appendFailoverEventTx(ctx, s.db, event)
}

func appendFailoverEventTx(ctx context.Context, exec sqlFailoverExecutor, event FailoverEventRecord) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO failover_events
		(created_at, event_type, from_node_id, to_node_id, mode, reason_code, success)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, event.CreatedAt, event.EventType, event.FromNodeID, event.ToNodeID, event.Mode, event.ReasonCode, event.Success)
	return err
}

func (s *Store) RecordDNSUpdateRun(ctx context.Context, provider, name, recordType string, dryRun, success bool, providerResult, propagationResult string) error {
	now := time.Now().Unix()
	return recordDNSUpdateRunTx(ctx, s.db, DNSUpdateRunRecord{StartedAt: now, CompletedAt: now, ProviderKind: provider, RecordName: name, RecordType: recordType, DryRun: dryRun, ProviderResult: providerResult, PropagationResult: propagationResult, Success: success})
}

func (s *Store) RecordDNSUpdateRunRecord(ctx context.Context, run DNSUpdateRunRecord) error {
	return recordDNSUpdateRunTx(ctx, s.db, run)
}

func (s *Store) LoadLatestDNSUpdateRun(ctx context.Context) (DNSUpdateRunRecord, bool, error) {
	var run DNSUpdateRunRecord
	var dryRun, success, rollbackReady int
	err := s.db.QueryRowContext(ctx, `
		SELECT started_at, completed_at, provider_kind, record_name, record_type,
		previous_value, desired_value, rollback_metadata_ready,
		dry_run, provider_result, propagation_result, success
		FROM dns_update_runs ORDER BY id DESC LIMIT 1
	`).Scan(&run.StartedAt, &run.CompletedAt, &run.ProviderKind, &run.RecordName, &run.RecordType,
		&run.PreviousValue, &run.DesiredValue, &rollbackReady,
		&dryRun, &run.ProviderResult, &run.PropagationResult, &success)
	if err == sql.ErrNoRows {
		return DNSUpdateRunRecord{}, false, nil
	}
	if err != nil {
		return DNSUpdateRunRecord{}, false, err
	}
	run.DryRun, run.Success, run.RollbackReady = dryRun != 0, success != 0, rollbackReady != 0
	return run, true, nil
}

func recordDNSUpdateRunTx(ctx context.Context, exec sqlFailoverExecutor, run DNSUpdateRunRecord) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO dns_update_runs
		(started_at, completed_at, provider_kind, record_name, record_type,
		previous_value, desired_value, rollback_metadata_ready,
		dry_run, provider_result, propagation_result, success)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.StartedAt, run.CompletedAt, run.ProviderKind, run.RecordName, run.RecordType,
		run.PreviousValue, run.DesiredValue, run.RollbackReady,
		run.DryRun, run.ProviderResult, run.PropagationResult, run.Success)
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
