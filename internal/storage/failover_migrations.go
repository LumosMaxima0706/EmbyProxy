package storage

import (
	"context"
	"database/sql"
)

// InitFailoverSchema is additive and idempotent. It is intentionally separate
// from the legacy proxy schema so migration review can remain scoped.
func (s *Store) InitFailoverSchema(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS failover_nodes (
			node_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			role TEXT NOT NULL CHECK (role IN ('primary','fallback')),
			public_host TEXT NOT NULL,
			health_url TEXT NOT NULL,
			proxy_base_url TEXT NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			maintenance_mode INTEGER NOT NULL DEFAULT 0,
			monthly_quota_bytes INTEGER NOT NULL DEFAULT 0,
			traffic_threshold_percent REAL NOT NULL DEFAULT 97,
			reset_day INTEGER NOT NULL DEFAULT 1,
			reset_timezone TEXT NOT NULL DEFAULT 'UTC',
			traffic_source_type TEXT NOT NULL DEFAULT 'unknown',
			traffic_source_config TEXT NOT NULL DEFAULT '',
			last_health_status TEXT NOT NULL DEFAULT 'unknown',
			last_traffic_usage INTEGER,
			last_switch_reason TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS failover_state (
			scope TEXT PRIMARY KEY,
			active_node_id TEXT,
			desired_node_id TEXT,
			observed_dns_node_id TEXT,
			mode TEXT NOT NULL DEFAULT 'auto',
			cooldown_until INTEGER NOT NULL DEFAULT 0,
			last_transition_at INTEGER NOT NULL DEFAULT 0,
			last_evaluation_at INTEGER NOT NULL DEFAULT 0,
			current_cycle_key TEXT NOT NULL DEFAULT '',
			reconciliation_required INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS failover_health_checks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id TEXT NOT NULL,
			checked_at INTEGER NOT NULL,
			check_kind TEXT NOT NULL,
			success INTEGER NOT NULL,
			status_code INTEGER NOT NULL DEFAULT 0,
			latency_ms INTEGER NOT NULL DEFAULT 0,
			consecutive_failure_count INTEGER NOT NULL DEFAULT 0,
			consecutive_success_count INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS failover_traffic_samples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id TEXT NOT NULL,
			sampled_at INTEGER NOT NULL,
			cycle_key TEXT NOT NULL DEFAULT '',
			source_type TEXT NOT NULL,
			inbound_bytes INTEGER,
			outbound_bytes INTEGER,
			total_bytes INTEGER,
			quota_bytes INTEGER,
			usage_percent REAL,
			quality TEXT NOT NULL DEFAULT 'unknown',
			source_ref TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS failover_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			from_node_id TEXT NOT NULL DEFAULT '',
			to_node_id TEXT NOT NULL DEFAULT '',
			mode TEXT NOT NULL,
			reason_code TEXT NOT NULL,
			policy_snapshot TEXT NOT NULL DEFAULT '{}',
			success INTEGER NOT NULL,
			error_summary TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS dns_update_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			started_at INTEGER NOT NULL,
			completed_at INTEGER,
			provider_kind TEXT NOT NULL,
			record_name TEXT NOT NULL,
			record_type TEXT NOT NULL,
			from_value_redacted TEXT NOT NULL DEFAULT '',
			to_value_redacted TEXT NOT NULL DEFAULT '',
			previous_value TEXT NOT NULL DEFAULT '',
			desired_value TEXT NOT NULL DEFAULT '',
			rollback_metadata_ready INTEGER NOT NULL DEFAULT 0,
			dry_run INTEGER NOT NULL DEFAULT 1,
			provider_result TEXT NOT NULL DEFAULT '',
			propagation_result TEXT NOT NULL DEFAULT '',
			success INTEGER NOT NULL DEFAULT 0,
			error_summary TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS traffic_quota_policies (
			node_id TEXT NOT NULL,
			quota_bytes INTEGER NOT NULL,
			threshold_percent REAL NOT NULL,
			reserve_bytes INTEGER NOT NULL DEFAULT 0,
			reset_day INTEGER NOT NULL,
			reset_timezone TEXT NOT NULL,
			unknown_blocks_recovery INTEGER NOT NULL DEFAULT 1,
			effective_from INTEGER NOT NULL,
			effective_to INTEGER,
			updated_by TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(node_id, effective_from)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_failover_health_node_time ON failover_health_checks(node_id, checked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_failover_traffic_node_time ON failover_traffic_samples(node_id, sampled_at)`,
		`CREATE INDEX IF NOT EXISTS idx_failover_events_created ON failover_events(created_at)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for _, column := range []struct {
		name       string
		definition string
	}{
		{name: "previous_value", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "desired_value", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "rollback_metadata_ready", definition: "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := ensureDNSUpdateRunColumn(ctx, tx, column.name, column.definition); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func ensureDNSUpdateRunColumn(ctx context.Context, tx *sql.Tx, column, definition string) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(dns_update_runs)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	switch column {
	case "previous_value", "desired_value", "rollback_metadata_ready":
		_, err = tx.ExecContext(ctx, `ALTER TABLE dns_update_runs ADD COLUMN `+column+` `+definition)
		return err
	default:
		return nil
	}
}
