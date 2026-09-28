package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ACME leases deliberately survive expiry until an operator verifies provider
// state. Expiry alone cannot prove a submitted TXT value has disappeared.
type EdgeACMELease struct {
	NodeID    string
	Value     string
	ExpiresAt int64
}

func (s *Store) InitEdgeACMELease(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS edge_acme_lease (
	id INTEGER PRIMARY KEY CHECK (id=1),
	node_id TEXT NOT NULL,
	challenge_value TEXT NOT NULL,
	expires_at INTEGER NOT NULL
)`)
	return err
}

func (s *Store) GetEdgeACMELease(ctx context.Context) (*EdgeACMELease, error) {
	var lease EdgeACMELease
	err := s.db.QueryRowContext(ctx, `SELECT node_id,challenge_value,expires_at FROM edge_acme_lease WHERE id=1`).Scan(&lease.NodeID, &lease.Value, &lease.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &lease, nil
}
func (s *Store) AcquireEdgeACMELease(ctx context.Context, nodeID, value string, expires time.Time) error {
	if nodeID == "" || value == "" || !expires.After(time.Now()) || expires.After(time.Now().Add(time.Hour)) {
		return errors.New("invalid_acme_lease")
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO edge_acme_lease(id,node_id,challenge_value,expires_at) VALUES(1,?,?,?) ON CONFLICT(id) DO UPDATE SET expires_at=excluded.expires_at WHERE node_id=excluded.node_id AND challenge_value=excluded.challenge_value`, nodeID, value, expires.Unix())
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return errors.New("acme_lease_owned_by_other_challenge")
	}
	return nil
}
func (s *Store) ReleaseEdgeACMELease(ctx context.Context, nodeID, value string) error {
	if nodeID == "" || value == "" {
		return errors.New("invalid_acme_lease")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM edge_acme_lease WHERE id=1 AND node_id=? AND challenge_value=?`, nodeID, value)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return errors.New("acme_lease_not_owned")
	}
	return nil
}
