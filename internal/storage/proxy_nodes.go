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
	ID                      string `json:"id"`
	Name                    string `json:"name"`
	PublicAddress           string `json:"public_address"`
	Enabled                 bool   `json:"enabled"`
	State                   string `json:"state"`
	Priority                int    `json:"priority"`
	QuotaBytes              int64  `json:"quota_bytes"`
	UsedBytes               int64  `json:"used_bytes"`
	ResetDay                int    `json:"reset_day"`
	ResetTimezone           string `json:"reset_timezone"`
	NextResetAt             int64  `json:"next_reset_at"`
	LastHeartbeatAt         int64  `json:"last_heartbeat_at"`
	PlaybackHealthy         bool   `json:"playback_healthy"`
	IngressHealthy          bool   `json:"ingress_healthy"`
	ConfigSynced            bool   `json:"config_synced"`
	AgentVersion            string `json:"agent_version"`
	AgentCommit             string `json:"agent_commit"`
	LastError               string `json:"last_error,omitempty"`
	ActiveConnections       int    `json:"active_connections"`
	CreatedAt               int64  `json:"created_at"`
	UpdatedAt               int64  `json:"updated_at"`
	DNSRecordID             string `json:"dns_record_id,omitempty"`
	DNSOwned                bool   `json:"dns_owned"`
	CaddyInstalledByProject bool   `json:"caddy_installed_by_project"`
	CaddyConfigOwned        bool   `json:"caddy_config_owned"`
	TLSStateOwned           bool   `json:"tls_state_owned"`
	EdgeUnitOwned           bool   `json:"edge_unit_owned"`
	RemoteCleanupPending    bool   `json:"remote_cleanup_pending"`
}

type ProxyNodeDecommissionJob struct {
	ID                   string                      `json:"id"`
	NodeID               string                      `json:"node_id"`
	State                string                      `json:"state"`
	Force                bool                        `json:"force"`
	CurrentStep          string                      `json:"current_step"`
	Error                string                      `json:"error,omitempty"`
	RemoteCleanupPending bool                        `json:"remote_cleanup_pending"`
	CleanupCommand       string                      `json:"cleanup_command,omitempty"`
	CreatedAt            int64                       `json:"created_at"`
	UpdatedAt            int64                       `json:"updated_at"`
	CompletedAt          int64                       `json:"completed_at,omitempty"`
	Steps                []ProxyNodeDecommissionStep `json:"steps,omitempty"`
}

type ProxyNodeDecommissionStep struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	Error     string `json:"error,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

var proxyNodeDecommissionSteps = []string{"switching_traffic", "draining", "scheduler_exclude", "revoking", "cleaning_remote", "removing_dns", "removing_controller_state", "complete"}

const proxyNodeHeartbeatFreshness = 5 * time.Minute

// ProxyRedirectEndpoint is a canary-validated media redirect origin. It is
// control-plane data, never derived from a client request.
type ProxyRedirectEndpoint struct {
	RouteSlug  string `json:"route_slug"`
	Scheme     string `json:"scheme"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	PathPrefix string `json:"path_prefix,omitempty"`
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
 last_heartbeat_at INTEGER NOT NULL DEFAULT 0, playback_healthy INTEGER NOT NULL DEFAULT 0, ingress_healthy INTEGER NOT NULL DEFAULT 0, config_synced INTEGER NOT NULL DEFAULT 0,
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
CREATE TABLE IF NOT EXISTS proxy_node_ownership (
 node_id TEXT PRIMARY KEY, dns_record_id TEXT NOT NULL DEFAULT '', dns_owned INTEGER NOT NULL DEFAULT 0,
 caddy_installed_by_project INTEGER NOT NULL DEFAULT 0, caddy_config_owned INTEGER NOT NULL DEFAULT 0,
 tls_state_owned INTEGER NOT NULL DEFAULT 0, edge_unit_owned INTEGER NOT NULL DEFAULT 0,
 FOREIGN KEY(node_id) REFERENCES proxy_nodes(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS proxy_node_decommission_jobs (
 id TEXT PRIMARY KEY, node_id TEXT NOT NULL, state TEXT NOT NULL, force INTEGER NOT NULL DEFAULT 0,
 current_step TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', remote_cleanup_pending INTEGER NOT NULL DEFAULT 0,
 cleanup_command TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, completed_at INTEGER NOT NULL DEFAULT 0,
 FOREIGN KEY(node_id) REFERENCES proxy_nodes(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS proxy_node_decommission_steps (
 job_id TEXT NOT NULL, name TEXT NOT NULL, state TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL,
 PRIMARY KEY(job_id, name), FOREIGN KEY(job_id) REFERENCES proxy_node_decommission_jobs(id) ON DELETE CASCADE
);
`)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS proxy_redirect_endpoints (
 route_slug TEXT NOT NULL,
 scheme TEXT NOT NULL,
 host TEXT NOT NULL,
 port INTEGER NOT NULL,
 path_prefix TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 PRIMARY KEY(route_slug, scheme, host, port, path_prefix)
);
CREATE INDEX IF NOT EXISTS idx_proxy_redirect_endpoints_route ON proxy_redirect_endpoints(route_slug)
`); err != nil {
		return err
	}
	if err := s.ensureProxyNodeColumn(ctx, "ingress_healthy", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureProxyNodeColumn(ctx, "drain_finalize", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES ('proxy_nodes_v4_lifecycle', ?)`, time.Now().Unix()); err != nil {
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
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES ('proxy_nodes_v3_ingress_health', ?)`, time.Now().Unix())
	if err != nil {
		return err
	}
	return s.backfillProxyNodeResetSchedules(ctx, time.Now())
}

func (s *Store) ReplaceProxyRedirectEndpoints(ctx context.Context, endpoints map[string][]ProxyRedirectEndpoint) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM proxy_redirect_endpoints`); err != nil {
		return err
	}
	now := time.Now().Unix()
	for slug, values := range endpoints {
		for _, endpoint := range values {
			if _, err := tx.ExecContext(ctx, `INSERT INTO proxy_redirect_endpoints(route_slug,scheme,host,port,path_prefix,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, slug, endpoint.Scheme, endpoint.Host, endpoint.Port, endpoint.PathPrefix, now, now); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) ReplaceProxyRedirectEndpointsForRoute(ctx context.Context, slug string, endpoints []ProxyRedirectEndpoint) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM proxy_redirect_endpoints WHERE route_slug=?`, slug); err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, endpoint := range endpoints {
		if endpoint.RouteSlug != slug {
			return errors.New("invalid_redirect_endpoint_route")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO proxy_redirect_endpoints(route_slug,scheme,host,port,path_prefix,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, slug, endpoint.Scheme, endpoint.Host, endpoint.Port, endpoint.PathPrefix, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListProxyRedirectEndpoints(ctx context.Context, slug string) ([]ProxyRedirectEndpoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT route_slug,scheme,host,port,path_prefix FROM proxy_redirect_endpoints WHERE route_slug=? ORDER BY host,port,path_prefix`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ProxyRedirectEndpoint{}
	for rows.Next() {
		var endpoint ProxyRedirectEndpoint
		if err := rows.Scan(&endpoint.RouteSlug, &endpoint.Scheme, &endpoint.Host, &endpoint.Port, &endpoint.PathPrefix); err != nil {
			return nil, err
		}
		result = append(result, endpoint)
	}
	return result, rows.Err()
}

func (s *Store) ListAllProxyRedirectEndpoints(ctx context.Context) (map[string][]ProxyRedirectEndpoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT route_slug,scheme,host,port,path_prefix FROM proxy_redirect_endpoints ORDER BY route_slug,host,port,path_prefix`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]ProxyRedirectEndpoint{}
	for rows.Next() {
		var e ProxyRedirectEndpoint
		if err := rows.Scan(&e.RouteSlug, &e.Scheme, &e.Host, &e.Port, &e.PathPrefix); err != nil {
			return nil, err
		}
		result[e.RouteSlug] = append(result[e.RouteSlug], e)
	}
	return result, rows.Err()
}

func (s *Store) ensureProxyNodeColumn(ctx context.Context, name, definition string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info(proxy_nodes)")
	if err != nil {
		return err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var column, typ string
		var notNull, primary int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &column, &typ, &notNull, &defaultValue, &primary); err != nil {
			return err
		}
		if column == name {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	if err := rows.Close(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "ALTER TABLE proxy_nodes ADD COLUMN "+name+" "+definition)
	return err
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
	if state == "draining" || state == "disabled" || state == "revoked" || state == "decommissioning" || state == "removed" {
		return errors.New("node_draining")
	}
	now := time.Now().Unix()
	if _, err = tx.ExecContext(ctx, `INSERT INTO proxy_node_connections(node_id,active,updated_at) VALUES(?,?,?) ON CONFLICT(node_id) DO UPDATE SET active=active+1,updated_at=excluded.updated_at`, id, 1, now); err != nil {
		return err
	}
	return tx.Commit()
}

// EndProxyNodeConnection decrements the active count. Legacy DrainProxyNode
// callers may request finalization; lifecycle drains never revoke credentials.
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
	var state string
	var finalize int
	if err = tx.QueryRowContext(ctx, `SELECT state,drain_finalize FROM proxy_nodes WHERE id=?`, id).Scan(&state, &finalize); err == nil && active == 0 && state == "draining" && finalize != 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE proxy_nodes SET enabled=0,state='revoked',credential_hash='',updated_at=? WHERE id=?`, now, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE proxy_node_enrollments SET revoked=1 WHERE node_id=?`, id); err != nil {
			return err
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

func (s *Store) loadProxyNodeOwnership(ctx context.Context, n *ProxyNode) error {
	if n == nil {
		return nil
	}
	var dnsID string
	var dnsOwned, caddyInstalled, caddyConfig, tlsOwned, unitOwned int
	err := s.db.QueryRowContext(ctx, `SELECT dns_record_id,dns_owned,caddy_installed_by_project,caddy_config_owned,tls_state_owned,edge_unit_owned FROM proxy_node_ownership WHERE node_id=?`, n.ID).Scan(&dnsID, &dnsOwned, &caddyInstalled, &caddyConfig, &tlsOwned, &unitOwned)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	n.DNSRecordID, n.DNSOwned = dnsID, dnsOwned != 0
	n.CaddyInstalledByProject, n.CaddyConfigOwned = caddyInstalled != 0, caddyConfig != 0
	n.TLSStateOwned, n.EdgeUnitOwned = tlsOwned != 0, unitOwned != 0
	return nil
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
	if _, err = tx.ExecContext(ctx, `INSERT INTO proxy_nodes (id,name,public_address,enabled,state,priority,quota_bytes,used_bytes,reset_day,reset_timezone,next_reset_at,last_heartbeat_at,playback_healthy,ingress_healthy,config_synced,agent_version,agent_commit,credential_hash,last_error,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, node.ID, node.Name, node.PublicAddress, 1, node.State, node.Priority, node.QuotaBytes, 0, node.ResetDay, node.ResetTimezone, node.NextResetAt, 0, 0, 0, 0, "", "", "", "", now, now); err != nil {
		return Enrollment{}, "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO proxy_node_enrollments (id,node_id,token_hash,expires_at,created_at) VALUES (?,?,?,?,?)`, enrollment.ID, node.ID, nodeHash(token), enrollment.ExpiresAt, now); err != nil {
		return Enrollment{}, "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO proxy_node_ownership(node_id) VALUES(?)`, node.ID); err != nil {
		return Enrollment{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return Enrollment{}, "", err
	}
	return enrollment, token, nil
}

// RegenerateProxyNodeEnrollment issues a fresh short-lived bootstrap token for
// an existing node. Older unconsumed enrollments are revoked so the operator
// never has multiple valid commands for the same node. A healthy node keeps
// its stable node ID until the new installer exchanges this one-time token.
func (s *Store) RegenerateProxyNodeEnrollment(ctx context.Context, nodeID string, enrollmentTTL time.Duration) (Enrollment, string, error) {
	if strings.TrimSpace(nodeID) == "" || enrollmentTTL <= 0 || enrollmentTTL > 24*time.Hour {
		return Enrollment{}, "", errors.New("invalid_enrollment")
	}
	token, err := randomNodeToken()
	if err != nil {
		return Enrollment{}, "", err
	}
	now := time.Now().Unix()
	enrollment := Enrollment{ID: nodeID + "-" + token[:12], NodeID: nodeID, ExpiresAt: time.Now().Add(enrollmentTTL).Unix()}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Enrollment{}, "", err
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM proxy_nodes WHERE id=?`, nodeID).Scan(&state); err != nil {
		return Enrollment{}, "", err
	}
	if state != "registered" && state != "revoked" && state != "installing" && state != "healthy" && state != "degraded" {
		return Enrollment{}, "", errors.New("node_not_registered")
	}
	if state == "revoked" {
		if _, err := tx.ExecContext(ctx, `UPDATE proxy_nodes SET state='registered',credential_hash='',last_heartbeat_at=0,playback_healthy=0,config_synced=0,last_error='',updated_at=? WHERE id=?`, now, nodeID); err != nil {
			return Enrollment{}, "", err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE proxy_node_enrollments SET revoked=1 WHERE node_id=? AND consumed_at=0 AND revoked=0`, nodeID); err != nil {
		return Enrollment{}, "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO proxy_node_enrollments (id,node_id,token_hash,expires_at,created_at) VALUES (?,?,?,?,?)`, enrollment.ID, nodeID, nodeHash(token), enrollment.ExpiresAt, now); err != nil {
		return Enrollment{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return Enrollment{}, "", err
	}
	return enrollment, token, nil
}

func scanProxyNode(row interface{ Scan(...any) error }) (ProxyNode, error) {
	var n ProxyNode
	var enabled, playback, synced int
	var ingress int
	err := row.Scan(&n.ID, &n.Name, &n.PublicAddress, &enabled, &n.State, &n.Priority, &n.QuotaBytes, &n.UsedBytes, &n.ResetDay, &n.ResetTimezone, &n.NextResetAt, &n.LastHeartbeatAt, &playback, &ingress, &synced, &n.AgentVersion, &n.AgentCommit, &n.LastError, &n.CreatedAt, &n.UpdatedAt)
	n.Enabled, n.PlaybackHealthy, n.IngressHealthy, n.ConfigSynced = enabled != 0, playback != 0, ingress != 0, synced != 0
	n.RemoteCleanupPending = n.LastError == "remote_cleanup_pending"
	return n, err
}

const proxyNodeFields = `id,name,public_address,enabled,state,priority,quota_bytes,used_bytes,reset_day,reset_timezone,next_reset_at,last_heartbeat_at,playback_healthy,ingress_healthy,config_synced,agent_version,agent_commit,last_error,created_at,updated_at`

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
		_ = s.loadProxyNodeOwnership(ctx, &out[i])
	}
	return out, nil
}

// ReplaceProxyNodeSnapshot stores the controller's redacted scheduling view on
// an edge. Node credentials are intentionally not part of ProxyNode and never
// cross this boundary.
func (s *Store) ReplaceProxyNodeSnapshot(ctx context.Context, nodes []ProxyNode) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seen := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		if n.ID == "" || !validNodeName(n.Name) || n.ResetDay < 1 || n.ResetDay > 31 {
			return errors.New("invalid_proxy_node_snapshot")
		}
		seen[n.ID] = struct{}{}
		_, err = tx.ExecContext(ctx, `INSERT INTO proxy_nodes (id,name,public_address,enabled,state,priority,quota_bytes,used_bytes,reset_day,reset_timezone,next_reset_at,last_heartbeat_at,playback_healthy,ingress_healthy,config_synced,agent_version,agent_commit,credential_hash,last_error,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,public_address=excluded.public_address,enabled=excluded.enabled,state=excluded.state,priority=excluded.priority,quota_bytes=excluded.quota_bytes,used_bytes=excluded.used_bytes,reset_day=excluded.reset_day,reset_timezone=excluded.reset_timezone,next_reset_at=excluded.next_reset_at,last_heartbeat_at=excluded.last_heartbeat_at,playback_healthy=excluded.playback_healthy,ingress_healthy=excluded.ingress_healthy,config_synced=excluded.config_synced,agent_version=excluded.agent_version,agent_commit=excluded.agent_commit,last_error=excluded.last_error,updated_at=excluded.updated_at`, n.ID, n.Name, n.PublicAddress, boolInt(n.Enabled), n.State, n.Priority, n.QuotaBytes, n.UsedBytes, n.ResetDay, n.ResetTimezone, n.NextResetAt, n.LastHeartbeatAt, boolInt(n.PlaybackHealthy), boolInt(n.IngressHealthy), boolInt(n.ConfigSynced), n.AgentVersion, n.AgentCommit, "", redactFailoverStorageText(n.LastError), n.CreatedAt, n.UpdatedAt)
		if err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM proxy_nodes`)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		if _, ok := seen[id]; !ok {
			stale = append(stale, id)
		}
	}
	_ = rows.Close()
	for _, id := range stale {
		if _, err = tx.ExecContext(ctx, `DELETE FROM proxy_nodes WHERE id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
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
	_ = s.loadProxyNodeOwnership(ctx, &n)
	return &n, nil
}

// GetProxyNodeForEnrollment returns the node bound to an enrollment without
// exposing token material. It is used to bind bootstrap preflight to the
// controller's persisted edge origin.
func (s *Store) GetProxyNodeForEnrollment(ctx context.Context, enrollmentID string) (*ProxyNode, error) {
	fields := "n." + strings.ReplaceAll(proxyNodeFields, ",", ",n.")
	row := s.db.QueryRowContext(ctx, `SELECT `+fields+` FROM proxy_nodes n JOIN proxy_node_enrollments e ON e.node_id=n.id WHERE e.id=?`, enrollmentID)
	node, err := scanProxyNode(row)
	if err != nil {
		return nil, err
	}
	return &node, nil
}
func (s *Store) UpdateProxyNode(ctx context.Context, n ProxyNode) error {
	if n.ID == "" || !validNodeName(n.Name) || n.QuotaBytes < 0 || n.UsedBytes < 0 || n.ResetDay < 1 || n.ResetDay > 31 {
		return errors.New("invalid_proxy_node")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE proxy_nodes SET name=?,public_address=?,enabled=?,state=?,priority=?,quota_bytes=?,used_bytes=?,reset_day=?,reset_timezone=?,next_reset_at=?,playback_healthy=?,ingress_healthy=?,config_synced=?,last_error=?,updated_at=? WHERE id=?`, n.Name, n.PublicAddress, boolInt(n.Enabled), n.State, n.Priority, n.QuotaBytes, n.UsedBytes, n.ResetDay, n.ResetTimezone, n.NextResetAt, boolInt(n.PlaybackHealthy), boolInt(n.IngressHealthy), boolInt(n.ConfigSynced), redactFailoverStorageText(n.LastError), time.Now().Unix(), n.ID)
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

// SetProxyNodeScheduling changes scheduler admission without revoking the
// node's identity. Revoked and removed nodes must be re-enrolled instead.
func (s *Store) SetProxyNodeScheduling(ctx context.Context, id string, enabled bool) error {
	node, err := s.GetProxyNode(ctx, id)
	if err != nil || node == nil {
		if err == nil {
			return sql.ErrNoRows
		}
		return err
	}
	if node.State == "revoked" || node.State == "removed" || node.State == "decommissioning" {
		return errors.New("node_requires_reenrollment")
	}
	state := node.State
	if enabled {
		if state == "disabled" || state == "draining" {
			state = "registered"
			if node.PlaybackHealthy && node.IngressHealthy && node.ConfigSynced {
				state = "healthy"
			}
		}
	} else if state != "draining" {
		state = "disabled"
	}
	_, err = s.db.ExecContext(ctx, `UPDATE proxy_nodes SET enabled=?,state=?,updated_at=? WHERE id=?`, boolInt(enabled), state, time.Now().Unix(), id)
	return err
}

// BeginProxyNodeDrain is the lifecycle drain operation. It excludes a node
// from new traffic while keeping credentials and existing sessions intact.
func (s *Store) BeginProxyNodeDrain(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE proxy_nodes SET enabled=0,state='draining',drain_finalize=0,updated_at=? WHERE id=? AND state NOT IN ('revoked','removed','decommissioning')`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SetProxyNodeOwnership(ctx context.Context, nodeID, dnsRecordID string, dnsOwned, caddyInstalled, caddyConfig, tlsOwned, unitOwned bool) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO proxy_node_ownership(node_id,dns_record_id,dns_owned,caddy_installed_by_project,caddy_config_owned,tls_state_owned,edge_unit_owned) VALUES(?,?,?,?,?,?,?) ON CONFLICT(node_id) DO UPDATE SET dns_record_id=excluded.dns_record_id,dns_owned=excluded.dns_owned,caddy_installed_by_project=excluded.caddy_installed_by_project,caddy_config_owned=excluded.caddy_config_owned,tls_state_owned=excluded.tls_state_owned,edge_unit_owned=excluded.edge_unit_owned`, nodeID, dnsRecordID, boolInt(dnsOwned), boolInt(caddyInstalled), boolInt(caddyConfig), boolInt(tlsOwned), boolInt(unitOwned))
	return err
}

func (s *Store) SetProxyNodeRemoteCleanupPending(ctx context.Context, id string, pending bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE proxy_nodes SET last_error=CASE WHEN ? THEN 'remote_cleanup_pending' ELSE CASE WHEN last_error='remote_cleanup_pending' THEN '' ELSE last_error END END,updated_at=? WHERE id=?`, boolInt(pending), time.Now().Unix(), id)
	return err
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
	result, err := tx.ExecContext(ctx, `UPDATE proxy_nodes SET enabled=0,state='draining',drain_finalize=1,updated_at=? WHERE id=? AND state!='revoked'`, now, id)
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
		if _, err := tx.ExecContext(ctx, `UPDATE proxy_nodes SET enabled=0,state='revoked',credential_hash='',updated_at=? WHERE id=?`, now, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE proxy_node_enrollments SET revoked=1 WHERE node_id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) setDecommissionStep(ctx context.Context, jobID, name, state, stepErr string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE proxy_node_decommission_steps SET state=?,error=?,updated_at=? WHERE job_id=? AND name=?`, state, redactFailoverStorageText(stepErr), time.Now().Unix(), jobID, name)
	return err
}

func (s *Store) GetProxyNodeDecommissionJob(ctx context.Context, jobID string) (*ProxyNodeDecommissionJob, error) {
	var j ProxyNodeDecommissionJob
	var force, pending int
	err := s.db.QueryRowContext(ctx, `SELECT id,node_id,state,force,current_step,error,remote_cleanup_pending,cleanup_command,created_at,updated_at,completed_at FROM proxy_node_decommission_jobs WHERE id=?`, jobID).Scan(&j.ID, &j.NodeID, &j.State, &force, &j.CurrentStep, &j.Error, &pending, &j.CleanupCommand, &j.CreatedAt, &j.UpdatedAt, &j.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j.Force, j.RemoteCleanupPending = force != 0, pending != 0
	rows, err := s.db.QueryContext(ctx, `SELECT name,state,error,updated_at FROM proxy_node_decommission_steps WHERE job_id=? ORDER BY rowid`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var step ProxyNodeDecommissionStep
		if err := rows.Scan(&step.Name, &step.State, &step.Error, &step.UpdatedAt); err != nil {
			return nil, err
		}
		j.Steps = append(j.Steps, step)
	}
	return &j, rows.Err()
}

func (s *Store) LatestProxyNodeDecommissionJob(ctx context.Context, nodeID string) (*ProxyNodeDecommissionJob, error) {
	var jobID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM proxy_node_decommission_jobs WHERE node_id=? ORDER BY created_at DESC LIMIT 1`, nodeID).Scan(&jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetProxyNodeDecommissionJob(ctx, jobID)
}

// DecommissionProxyNode completes the control-plane half of removal in an
// idempotent job. Remote cleanup is reported pending when the last heartbeat
// is stale; it never prevents credential/routing cleanup or archival.
func (s *Store) DecommissionProxyNode(ctx context.Context, id string, force bool) (*ProxyNodeDecommissionJob, error) {
	node, err := s.GetProxyNode(ctx, id)
	if err != nil || node == nil {
		if err == nil {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	if node.State == "removed" {
		if job, err := s.LatestProxyNodeDecommissionJob(ctx, id); err == nil && job != nil {
			return job, nil
		}
		return nil, errors.New("node_already_removed")
	}
	jobID, _ := randomNodeToken()
	jobID = "decom-" + jobID[:20]
	now := time.Now().Unix()
	remotePending := node.LastHeartbeatAt == 0 || time.Since(time.Unix(node.LastHeartbeatAt, 0)) > proxyNodeHeartbeatFreshness
	cleanupCommand := "sudo sh -c 'systemctl stop embyproxy-edge.service 2>/dev/null || true; rm -f /usr/local/bin/embyproxy-edge /etc/systemd/system/embyproxy-edge.service; for d in /etc/embyproxy-edge /var/lib/embyproxy-edge; do [ -d \"$d\" ] && find \"$d\" -xdev -depth -type f -delete && find \"$d\" -xdev -depth -type d -empty -delete; done'"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO proxy_node_decommission_jobs(id,node_id,state,force,current_step,remote_cleanup_pending,cleanup_command,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, jobID, id, "running", boolInt(force), "switching_traffic", boolInt(remotePending), cleanupCommand, now, now); err != nil {
		return nil, err
	}
	for _, name := range proxyNodeDecommissionSteps {
		if _, err = tx.ExecContext(ctx, `INSERT INTO proxy_node_decommission_steps(job_id,name,state,updated_at) VALUES(?,?,?,?)`, jobID, name, "pending", now); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE proxy_nodes SET enabled=0,state='decommissioning',credential_hash='',last_error=CASE WHEN ? THEN 'remote_cleanup_pending' ELSE '' END,updated_at=? WHERE id=?`, boolInt(remotePending), now, id); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE proxy_node_enrollments SET revoked=1 WHERE node_id=?`, id); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM proxy_redirect_endpoints WHERE route_slug IN (SELECT name FROM proxy_nodes WHERE id=?)`, id); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE proxy_node_decommission_steps SET state='complete',updated_at=? WHERE job_id=? AND name IN ('switching_traffic','draining','scheduler_exclude','revoking','removing_dns','removing_controller_state')`, now, jobID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE proxy_node_decommission_steps SET state=?,updated_at=? WHERE job_id=? AND name='cleaning_remote'`, map[bool]string{true: "pending", false: "complete"}[remotePending], now, jobID); err != nil {
		return nil, err
	}
	finalState := "removed"
	if remotePending {
		finalState = "removed"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE proxy_nodes SET state=?,enabled=0,updated_at=? WHERE id=?`, finalState, now, id); err != nil {
		return nil, err
	}
	jobState := "complete"
	currentStep := "complete"
	if remotePending {
		jobState = "partial"
		currentStep = "cleaning_remote"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE proxy_node_decommission_jobs SET state=?,current_step=?,updated_at=?,completed_at=CASE WHEN ?='complete' THEN ? ELSE 0 END WHERE id=?`, jobState, currentStep, now, jobState, now, jobID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetProxyNodeDecommissionJob(ctx, jobID)
}

func (s *Store) RetryProxyNodeDecommission(ctx context.Context, jobID string) (*ProxyNodeDecommissionJob, error) {
	job, err := s.GetProxyNodeDecommissionJob(ctx, jobID)
	if err != nil || job == nil {
		if err == nil {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	if job.State != "partial" && job.State != "failed" {
		return job, nil
	}
	if job.RemoteCleanupPending {
		now := time.Now().Unix()
		if _, err := s.db.ExecContext(ctx, `UPDATE proxy_node_decommission_steps SET state='complete',error='',updated_at=? WHERE job_id=? AND name='cleaning_remote'`, now, jobID); err != nil {
			return nil, err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE proxy_node_decommission_jobs SET state='complete',current_step='complete',remote_cleanup_pending=0,error='',updated_at=?,completed_at=? WHERE id=?`, now, now, jobID); err != nil {
			return nil, err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE proxy_nodes SET last_error='',updated_at=? WHERE id=?`, now, job.NodeID); err != nil {
			return nil, err
		}
		return s.GetProxyNodeDecommissionJob(ctx, jobID)
	}
	return s.DecommissionProxyNode(ctx, job.NodeID, job.Force)
}
func (s *Store) CompleteEnrollment(ctx context.Context, enrollmentID, token, version, commit string) (ProxyNode, string, error) {
	return s.completeEnrollment(ctx, enrollmentID, token, version, commit, "")
}

// CompleteEnrollmentWithPublicAddress records the HTTPS ingress origin chosen
// by the installer before the node can become selectable. An empty origin
// preserves the node's existing value for older installers.
func (s *Store) CompleteEnrollmentWithPublicAddress(ctx context.Context, enrollmentID, token, version, commit, publicAddress string) (ProxyNode, string, error) {
	return s.completeEnrollment(ctx, enrollmentID, token, version, commit, strings.TrimSpace(publicAddress))
}

func (s *Store) completeEnrollment(ctx context.Context, enrollmentID, token, version, commit, publicAddress string) (ProxyNode, string, error) {
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
	if publicAddress == "" {
		if _, err = tx.ExecContext(ctx, `UPDATE proxy_nodes SET state='installing',credential_hash=?,agent_version=?,agent_commit=?,updated_at=? WHERE id=?`, nodeHash(credential), version, commit, now, nodeID); err != nil {
			return ProxyNode{}, "", err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE proxy_nodes SET state='installing',public_address=?,ingress_healthy=0,credential_hash=?,agent_version=?,agent_commit=?,updated_at=? WHERE id=?`, publicAddress, nodeHash(credential), version, commit, now, nodeID); err != nil {
			return ProxyNode{}, "", err
		}
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
	// Agent heartbeats report local capability only. Public ingress health is
	// written by the controller-side probe and remains an independent gate.
	if !playback || !synced {
		state = "degraded"
	} else {
		var ingress int
		if err := s.db.QueryRowContext(ctx, `SELECT ingress_healthy FROM proxy_nodes WHERE id=? AND credential_hash=? AND state!='revoked'`, id, nodeHash(credential)).Scan(&ingress); err == nil && ingress == 0 {
			state = "online"
		}
	}
	now := time.Now().Unix()
	result, err := s.db.ExecContext(ctx, `UPDATE proxy_nodes SET state=CASE WHEN state IN ('disabled','draining','decommissioning','removed','revoked') THEN state ELSE ? END,last_heartbeat_at=?,playback_healthy=?,config_synced=?,agent_version=?,agent_commit=?,last_error=?,updated_at=? WHERE id=? AND credential_hash=? AND state NOT IN ('revoked','decommissioning','removed')`, state, now, boolInt(playback), boolInt(synced), version, commit, redactFailoverStorageText(lastError), now, id, nodeHash(credential))
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return errors.New("node_credential_denied")
	}
	return nil
}

// SetProxyNodeIngressHealth records the controller's independent HTTPS
// reachability probe. It never changes playback_healthy.
func (s *Store) SetProxyNodeIngressHealth(ctx context.Context, id string, healthy bool, lastError string) error {
	now := time.Now().Unix()
	state := "degraded"
	if healthy {
		var playback, synced int
		if err := s.db.QueryRowContext(ctx, `SELECT playback_healthy,config_synced FROM proxy_nodes WHERE id=? AND state!='revoked'`, id).Scan(&playback, &synced); err != nil {
			return err
		}
		if playback != 0 && synced != 0 {
			state = "healthy"
		} else {
			state = "online"
		}
	}
	result, err := s.db.ExecContext(ctx, `UPDATE proxy_nodes SET ingress_healthy=?,state=CASE WHEN state IN ('registered','installing','disabled','draining','decommissioning','removed','revoked') THEN state ELSE ? END,last_error=CASE WHEN ?='' THEN last_error ELSE ? END,updated_at=? WHERE id=? AND state NOT IN ('revoked','decommissioning','removed')`, boolInt(healthy), state, lastError, redactFailoverStorageText(lastError), now, id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return sql.ErrNoRows
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
