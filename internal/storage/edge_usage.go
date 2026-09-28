package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

type EdgeUsageEvent struct {
	ID        string `json:"event_id"`
	Bytes     int64  `json:"response_bytes"`
	SampledAt int64  `json:"sampled_at"`
}

func (s *Store) InitEdgeUsageOutbox(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS edge_usage_outbox (
event_id TEXT PRIMARY KEY, response_bytes INTEGER NOT NULL, sampled_at INTEGER NOT NULL,
next_attempt_at INTEGER NOT NULL DEFAULT 0
)`)
	if err == nil {
		err = s.ensureEdgeUsageOutboxColumn(ctx)
	}
	return err
}

func (s *Store) QueueEdgeUsage(ctx context.Context, bytes int64, when time.Time) error {
	if bytes <= 0 || bytes > 1<<40 {
		return errors.New("invalid_usage")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO edge_usage_outbox(event_id,response_bytes,sampled_at) VALUES(?,?,?)`, hex.EncodeToString(random[:]), bytes, when.Unix())
	return err
}

func (s *Store) NextEdgeUsage(ctx context.Context) (*EdgeUsageEvent, error) {
	var event EdgeUsageEvent
	err := s.db.QueryRowContext(ctx, `SELECT event_id,response_bytes,sampled_at FROM edge_usage_outbox WHERE next_attempt_at<=? ORDER BY rowid LIMIT 1`, time.Now().Unix()).Scan(&event.ID, &event.Bytes, &event.SampledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (s *Store) AcknowledgeEdgeUsage(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM edge_usage_outbox WHERE event_id=?`, id)
	return err
}
func (s *Store) DelayEdgeUsage(ctx context.Context, id string, delay time.Duration) error {
	if delay <= 0 || delay > 24*time.Hour {
		return errors.New("invalid_usage_delay")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE edge_usage_outbox SET next_attempt_at=? WHERE event_id=?`, time.Now().Add(delay).Unix(), id)
	return err
}

func (s *Store) ensureEdgeUsageOutboxColumn(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(edge_usage_outbox)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var position, required, primary int
		var name, typ string
		var defaultValue sql.NullString
		if err := rows.Scan(&position, &name, &typ, &required, &defaultValue, &primary); err != nil {
			_ = rows.Close()
			return err
		}
		if name == "next_attempt_at" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE edge_usage_outbox ADD COLUMN next_attempt_at INTEGER NOT NULL DEFAULT 0`)
	return err
}

func (s *Store) EdgeUsagePending(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM edge_usage_outbox`).Scan(&count)
	return count, err
}
