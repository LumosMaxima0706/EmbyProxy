package storage

import (
	"context"
	"database/sql"
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
