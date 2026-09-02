package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// NextMonthlyReset returns the next reset instant for the requested billing
// day in the supplied IANA timezone. Invalid timezone/day values are rejected
// instead of silently using the host timezone.
func NextMonthlyReset(now time.Time, resetDay int, timezone string) (time.Time, error) {
	if resetDay < 1 || resetDay > 31 {
		return time.Time{}, errors.New("invalid_reset_day")
	}
	loc, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return time.Time{}, err
	}
	local := now.In(loc)
	for monthOffset := 0; monthOffset < 2; monthOffset++ {
		month := local.Month() + time.Month(monthOffset)
		year := local.Year()
		for month > 12 {
			month -= 12
			year++
		}
		day := resetDay
		last := time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
		if day > last {
			day = last
		}
		candidate := time.Date(year, month, day, 0, 0, 0, 0, loc)
		if candidate.After(local) {
			return candidate, nil
		}
	}
	return time.Time{}, errors.New("reset_date_unavailable")
}

// ProxyNode is an independently enrolled data-plane node. Secrets never leave
// this package after enrollment and are stored only as SHA-256 verifiers.
type ProxyNode struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	PublicAddress     string `json:"public_address"`
	Enabled           bool   `json:"enabled"`
	State             string `json:"state"`
	Priority          int    `json:"priority"`
	QuotaBytes        int64  `json:"quota_bytes"`
	UsedBytes         int64  `json:"used_bytes"`
	ResetDay          int    `json:"reset_day"`
	ResetTimezone     string `json:"reset_timezone"`
	NextResetAt       int64  `json:"next_reset_at"`
	LastHeartbeatAt   int64  `json:"last_heartbeat_at"`
	PlaybackHealthy   bool   `json:"playback_healthy"`
	ConfigSynced      bool   `json:"config_synced"`
	AgentVersion      string `json:"agent_version"`
	AgentCommit       string `json:"agent_commit"`
	LastError         string `json:"last_error,omitempty"`
	ActiveConnections int    `json:"active_connections"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

type Enrollment struct {
	ID        string `json:"id"`
	NodeID    string `json:"node_id"`
	ExpiresAt int64  `json:"expires_at"`
	Revoked   bool   `json:"revoked"`
}

func (s *Store) InitProxyNodeSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
 version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS proxy_nodes (
 id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, public_address TEXT NOT NULL DEFAULT '',
 enabled INTEGER NOT NULL DEFAULT 1, state TEXT NOT NULL DEFAULT 'registered', priority INTEGER NOT NULL DEFAULT 0,
 quota_bytes INTEGER NOT NULL DEFAULT 0, used_bytes INTEGER NOT NULL DEFAULT 0,
 reset_day INTEGER NOT NULL DEFAULT 1, reset_timezone TEXT NOT NULL DEFAULT 'Asia/Shanghai', next_reset_at INTEGER NOT NULL DEFAULT 0,
 last_heartbeat_at INTEGER NOT NULL DEFAULT 0, playback_healthy INTEGER NOT NULL DEFAULT 0, config_synced INTEGER NOT NULL DEFAULT 0,
 agent_version TEXT NOT NULL DEFAULT '', agent_commit TEXT NOT NULL DEFAULT '', credential_hash TEXT NOT NULL DEFAULT '',
 last_error TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS proxy_node_enrollments (
 id TEXT PRIMARY KEY, node_id TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE, expires_at INTEGER NOT NULL,
 consumed_at INTEGER NOT NULL DEFAULT 0, revoked INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL,
 FOREIGN KEY(node_id) REFERENCES proxy_nodes(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_proxy_nodes_priority ON proxy_nodes(enabled, state, priority);
CREATE INDEX IF NOT EXISTS idx_proxy_node_enrollments_node ON proxy_node_enrollments(node_id);
CREATE TABLE IF NOT EXISTS proxy_node_connections (
 node_id TEXT PRIMARY KEY, active INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL,
 FOREIGN KEY(node_id) REFERENCES proxy_nodes(id) ON DELETE CASCADE
);
`)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES ('proxy_nodes_v1', ?)`, time.Now().Unix())
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES ('proxy_nodes_v2_connections', ?)`, time.Now().Unix())
	if err != nil {
		return err
	}
	return s.backfillProxyNodeResetSchedules(ctx, time.Now())
}

func (s *Store) backfillProxyNodeResetSchedules(ctx context.Context, now time.Time) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, reset_day, reset_timezone FROM proxy_nodes WHERE next_reset_at=0 AND state!='revoked'`)
	if err != nil {
		return err
	}
	type reset struct {
		id   string
		next int64
	}
	resets := make([]reset, 0)
	for rows.Next() {
		var id, timezone string
		var day int
		if err := rows.Scan(&id, &day, &timezone); err != nil {
			_ = rows.Close()
			return err
		}
		next, err := NextMonthlyReset(now, day, timezone)
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("proxy node %s reset schedule: %w", id, err)
		}
		resets = append(resets, reset{id: id, next: next.Unix()})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, item := range resets {
		if _, err := s.db.ExecContext(ctx, `UPDATE proxy_nodes SET next_reset_at=?,updated_at=? WHERE id=? AND next_reset_at=0`, item.next, now.Unix(), item.id); err != nil {
			return err
		}
	}
	return nil
}

// BeginProxyNodeConnection reserves one active request for drain accounting.
// Draining/revoked nodes are rejected so no new stream can race a removal.
func (s *Store) BeginProxyNodeConnection(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM proxy_nodes WHERE id=?`, id).Scan(&state); err != nil {
		return err
	}
	if state == "draining" || state == "revoked" {
		return errors.New("node_draining")
	}
	now := time.Now().Unix()
	if _, err = tx.ExecContext(ctx, `INSERT INTO proxy_node_connections(node_id,active,updated_at) VALUES(?,?,?) ON CONFLICT(node_id) DO UPDATE SET active=active+1,updated_at=excluded.updated_at`, id, 1, now); err != nil {
		return err
	}
	return tx.Commit()
}

// EndProxyNodeConnection decrements the active count. A drained node is
// revoked automatically once its final response has completed.
func (s *Store) EndProxyNodeConnection(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if _, err = tx.ExecContext(ctx, `UPDATE proxy_node_connections SET active=CASE WHEN active>0 THEN active-1 ELSE 0 END,updated_at=? WHERE node_id=?`, now, id); err != nil {
		return err
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT active FROM proxy_node_connections WHERE node_id=?`, id).Scan(&active); err != nil {
		return err
	}
	if active == 0 {
		var state string
		if err = tx.QueryRowContext(ctx, `SELECT state FROM proxy_nodes WHERE id=?`, id).Scan(&state); err == nil && state == "draining" {
			if _, err = tx.ExecContext(ctx, `UPDATE proxy_nodes SET enabled=0,state='revoked',credential_hash='',updated_at=? WHERE id=?`, now, id); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE proxy_node_enrollments SET revoked=1 WHERE node_id=?`, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) ProxyNodeActiveConnections(ctx context.Context, id string) (int, error) {
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT active FROM proxy_node_connections WHERE node_id=?`, id).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return active, err
}

func randomNodeToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func nodeHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func validNodeName(value string) bool {
	if len(value) < 2 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return !strings.HasPrefix(value, "-") && !strings.HasSuffix(value, "-")
}

func (s *Store) CreateProxyNode(ctx context.Context, node ProxyNode, enrollmentTTL time.Duration) (Enrollment, string, error) {
	node.Name = strings.ToLower(strings.TrimSpace(node.Name))
	if !validNodeName(node.Name) || node.QuotaBytes < 0 || node.ResetDay < 1 || node.ResetDay > 31 || enrollmentTTL <= 0 || enrollmentTTL > 24*time.Hour {
		return Enrollment{}, "", errors.New("invalid_proxy_node")
	}
	if node.ResetTimezone == "" {
		node.ResetTimezone = "Asia/Shanghai"
	}
	node.ID, _ = randomNodeToken()
	node.ID = node.ID[:24]
	now := time.Now().Unix()
	if node.NextResetAt == 0 {
		next, err := NextMonthlyReset(time.Unix(now, 0), node.ResetDay, node.ResetTimezone)
		if err != nil {
			return Enrollment{}, "", errors.New("invalid_reset_timezone")
		}
		node.NextResetAt = next.Unix()
	}
	node.State = "registered"
	node.Enabled = true
	node.CreatedAt, node.UpdatedAt = now, now
	token, err := randomNodeToken()
	if err != nil {
		return Enrollment{}, "", err
	}
	enrollment := Enrollment{ID: node.ID + "-" + token[:12], NodeID: node.ID, ExpiresAt: time.Now().Add(enrollmentTTL).Unix()}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Enrollment{}, "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO proxy_nodes (id,name,public_address,enabled,state,priority,quota_bytes,used_bytes,reset_day,reset_timezone,next_reset_at,last_heartbeat_at,playback_healthy,config_synced,agent_version,agent_commit,credential_hash,last_error,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, node.ID, node.Name, node.PublicAddress, 1, node.State, node.Priority, node.QuotaBytes, 0, node.ResetDay, node.ResetTimezone, node.NextResetAt, 0, 0, 0, "", "", "", "", now, now); err != nil {
		return Enrollment{}, "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO proxy_node_enrollments (id,node_id,token_hash,expires_at,created_at) VALUES (?,?,?,?,?)`, enrollment.ID, node.ID, nodeHash(token), enrollment.ExpiresAt, now); err != nil {
		return Enrollment{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return Enrollment{}, "", err
	}
	return enrollment, token, nil
}

func scanProxyNode(row interface{ Scan(...any) error }) (ProxyNode, error) {
	var n ProxyNode
	var enabled, playback, synced int
	err := row.Scan(&n.ID, &n.Name, &n.PublicAddress, &enabled, &n.State, &n.Priority, &n.QuotaBytes, &n.UsedBytes, &n.ResetDay, &n.ResetTimezone, &n.NextResetAt, &n.LastHeartbeatAt, &playback, &synced, &n.AgentVersion, &n.AgentCommit, &n.LastError, &n.CreatedAt, &n.UpdatedAt)
	n.Enabled, n.PlaybackHealthy, n.ConfigSynced = enabled != 0, playback != 0, synced != 0
	return n, err
}

const proxyNodeFields = `id,name,public_address,enabled,state,priority,quota_bytes,used_bytes,reset_day,reset_timezone,next_reset_at,last_heartbeat_at,playback_healthy,config_synced,agent_version,agent_commit,last_error,created_at,updated_at`

func (s *Store) ListProxyNodes(ctx context.Context) ([]ProxyNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+proxyNodeFields+` FROM proxy_nodes ORDER BY priority, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProxyNode{}
	for rows.Next() {
		n, err := scanProxyNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	for i := range out {
		out[i].ActiveConnections, _ = s.ProxyNodeActiveConnections(ctx, out[i].ID)
	}
	return out, nil
}
func (s *Store) GetProxyNode(ctx context.Context, id string) (*ProxyNode, error) {
	n, err := scanProxyNode(s.db.QueryRowContext(ctx, `SELECT `+proxyNodeFields+` FROM proxy_nodes WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	n.ActiveConnections, _ = s.ProxyNodeActiveConnections(ctx, n.ID)
	return &n, nil
}
func (s *Store) UpdateProxyNode(ctx context.Context, n ProxyNode) error {
	if n.ID == "" || !validNodeName(n.Name) || n.QuotaBytes < 0 || n.UsedBytes < 0 || n.ResetDay < 1 || n.ResetDay > 31 {
		return errors.New("invalid_proxy_node")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE proxy_nodes SET name=?,public_address=?,enabled=?,state=?,priority=?,quota_bytes=?,used_bytes=?,reset_day=?,reset_timezone=?,next_reset_at=?,playback_healthy=?,config_synced=?,last_error=?,updated_at=? WHERE id=?`, n.Name, n.PublicAddress, boolInt(n.Enabled), n.State, n.Priority, n.QuotaBytes, n.UsedBytes, n.ResetDay, n.ResetTimezone, n.NextResetAt, boolInt(n.PlaybackHealthy), boolInt(n.ConfigSynced), redactFailoverStorageText(n.LastError), time.Now().Unix(), n.ID)
	return err
}

// RecordProxyNodeUsage is the single write path for node traffic accounting.
// Values are monotonic within a billing cycle so an agent restart cannot erase
// previously observed usage.
func (s *Store) RecordProxyNodeUsage(ctx context.Context, id string, usedBytes int64, sampledAt time.Time) error {
	if usedBytes < 0 {
		return errors.New("invalid_usage")
	}
	if err := s.advanceProxyNodeCycle(ctx, id, sampledAt); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE proxy_nodes SET used_bytes=CASE WHEN used_bytes>? THEN used_bytes ELSE ? END,updated_at=? WHERE id=? AND state!='revoked'`, usedBytes, usedBytes, sampledAt.Unix(), id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// AddProxyNodeUsage atomically adds bytes observed by the local proxy. This
// avoids lost updates when concurrent streams finish at the same time.
func (s *Store) AddProxyNodeUsage(ctx context.Context, id string, bytes int64, sampledAt time.Time) error {
	if bytes < 0 {
		return errors.New("invalid_usage")
	}
	if err := s.advanceProxyNodeCycle(ctx, id, sampledAt); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE proxy_nodes SET used_bytes=used_bytes+?,updated_at=? WHERE id=? AND state!='revoked'`, bytes, sampledAt.Unix(), id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// advanceProxyNodeCycle persists the next reset before admitting a new usage
// sample, so process restarts cannot silently retain an expired billing cycle.
func (s *Store) advanceProxyNodeCycle(ctx context.Context, id string, now time.Time) error {
	node, err := s.GetProxyNode(ctx, id)
	if err != nil || node == nil {
		if err == nil {
			return sql.ErrNoRows
		}
		return err
	}
	if node.NextResetAt == 0 || now.Unix() < node.NextResetAt {
		return nil
	}
	next, err := NextMonthlyReset(now, node.ResetDay, node.ResetTimezone)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE proxy_nodes SET used_bytes=0,next_reset_at=?,updated_at=? WHERE id=? AND next_reset_at<=?`, next.Unix(), now.Unix(), id, now.Unix())
	return err
}

func (s *Store) ResetProxyNodeUsage(ctx context.Context, id string, now time.Time) error {
	node, err := s.GetProxyNode(ctx, id)
	if err != nil || node == nil {
		if err == nil {
			return sql.ErrNoRows
		}
		return err
	}
	next, err := NextMonthlyReset(now, node.ResetDay, node.ResetTimezone)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE proxy_nodes SET used_bytes=0,next_reset_at=?,updated_at=? WHERE id=? AND state!='revoked'`, next.Unix(), now.Unix(), id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return sql.ErrNoRows
	}
	return nil
}
func (s *Store) ReorderProxyNodes(ctx context.Context, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err = tx.ExecContext(ctx, `UPDATE proxy_nodes SET priority=?,updated_at=? WHERE id=?`, i+1, time.Now().Unix(), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) RevokeProxyNode(ctx context.Context, id string, force bool) error {
	state := "revoked"
	if !force {
		return s.DrainProxyNode(ctx, id)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE proxy_nodes SET enabled=0,state=?,credential_hash='',updated_at=? WHERE id=?`, state, time.Now().Unix(), id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE proxy_node_enrollments SET revoked=1 WHERE node_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DrainProxyNode removes a node from new-session eligibility while preserving
// its credential so existing streams and a later finalization can complete.
func (s *Store) DrainProxyNode(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	result, err := tx.ExecContext(ctx, `UPDATE proxy_nodes SET enabled=0,state='draining',updated_at=? WHERE id=? AND state!='revoked'`, now, id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return sql.ErrNoRows
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT active FROM proxy_node_connections WHERE node_id=?`, id).Scan(&active); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if active == 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE proxy_nodes SET state='revoked',credential_hash='',updated_at=? WHERE id=?`, now, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE proxy_node_enrollments SET revoked=1 WHERE node_id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) CompleteEnrollment(ctx context.Context, enrollmentID, token, version, commit string) (ProxyNode, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProxyNode{}, "", err
	}
	defer tx.Rollback()
	var nodeID, hash string
	var expires, used, revoked int64
	if err = tx.QueryRowContext(ctx, `SELECT node_id,token_hash,expires_at,consumed_at,revoked FROM proxy_node_enrollments WHERE id=?`, enrollmentID).Scan(&nodeID, &hash, &expires, &used, &revoked); err != nil {
		return ProxyNode{}, "", err
	}
	if used != 0 || revoked != 0 || expires <= time.Now().Unix() || nodeHash(token) != hash {
		return ProxyNode{}, "", errors.New("enrollment_denied")
	}
	credential, err := randomNodeToken()
	if err != nil {
		return ProxyNode{}, "", err
	}
	now := time.Now().Unix()
	if _, err = tx.ExecContext(ctx, `UPDATE proxy_node_enrollments SET consumed_at=? WHERE id=?`, now, enrollmentID); err != nil {
		return ProxyNode{}, "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE proxy_nodes SET state='installing',credential_hash=?,agent_version=?,agent_commit=?,updated_at=? WHERE id=?`, nodeHash(credential), version, commit, now, nodeID); err != nil {
		return ProxyNode{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return ProxyNode{}, "", err
	}
	n, e := s.GetProxyNode(ctx, nodeID)
	if e != nil || n == nil {
		return ProxyNode{}, "", fmt.Errorf("node_read_failed: %w", e)
	}
	return *n, credential, nil
}

// ValidateEnrollment verifies a bootstrap request without consuming it. The
// token is consumed only by CompleteEnrollment, so a transient download
// failure can be retried until the short expiry window closes.
func (s *Store) ValidateEnrollment(ctx context.Context, enrollmentID, token string) error {
	var hash string
	var expires, used, revoked int64
	err := s.db.QueryRowContext(ctx, `SELECT token_hash,expires_at,consumed_at,revoked FROM proxy_node_enrollments WHERE id=?`, enrollmentID).Scan(&hash, &expires, &used, &revoked)
	if err != nil {
		return err
	}
	if used != 0 || revoked != 0 || expires <= time.Now().Unix() || nodeHash(token) != hash {
		return errors.New("enrollment_denied")
	}
	return nil
}
func (s *Store) HeartbeatProxyNode(ctx context.Context, id, credential, version, commit, state string, playback, synced bool, lastError string) error {
	if state != "online" && state != "healthy" && state != "degraded" {
		return errors.New("invalid_node_state")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE proxy_nodes SET state=?,last_heartbeat_at=?,playback_healthy=?,config_synced=?,agent_version=?,agent_commit=?,last_error=?,updated_at=? WHERE id=? AND credential_hash=? AND state!='revoked'`, state, time.Now().Unix(), boolInt(playback), boolInt(synced), version, commit, redactFailoverStorageText(lastError), time.Now().Unix(), id, nodeHash(credential))
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return errors.New("node_credential_denied")
	}
	return nil
}

// ValidateProxyNodeCredential verifies a node-scoped long-lived credential
// without returning it or exposing any verifier material.
func (s *Store) ValidateProxyNodeCredential(ctx context.Context, id, credential string) bool {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(credential) == "" {
		return false
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM proxy_nodes WHERE id=? AND credential_hash=? AND state!='revoked'`, id, nodeHash(credential)).Scan(&count)
	return err == nil && count == 1
}
