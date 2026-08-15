package storage

import (
	"context"
	"database/sql"
)

const (
	PublicationSavedUnpublished = "saved_unpublished"
	PublicationPublishing       = "publishing"
	PublicationPublished        = "published"
	PublicationUnpublishing     = "unpublishing"
	PublicationFailed           = "publish_failed"
	PublicationNeedsSync        = "needs_sync"
)

type Publication struct {
	UID         string `json:"uid"`
	NodeName    string `json:"node_name"`
	RouteSlug   string `json:"route_slug"`
	PublicURL   string `json:"public_url,omitempty"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
	FailedStep  string `json:"failed_step,omitempty"`
	NOSLAStatus string `json:"nosla_status"`
	BWGStatus   string `json:"bwg_status"`
	UpdatedAt   int64  `json:"updated_at"`
}

func (s *Store) InitPublicationSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS emby_publications (
  uid TEXT NOT NULL,
  node_name TEXT NOT NULL,
  route_slug TEXT NOT NULL,
  public_url TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  failed_step TEXT NOT NULL DEFAULT '',
  nosla_status TEXT NOT NULL DEFAULT 'unknown',
  bwg_status TEXT NOT NULL DEFAULT 'unknown',
  updated_at INTEGER NOT NULL,
  PRIMARY KEY(uid, node_name)
);
CREATE INDEX IF NOT EXISTS idx_emby_publications_status ON emby_publications(uid, status);
`)
	return err
}

func (s *Store) GetPublication(ctx context.Context, uid, nodeName string) (*Publication, error) {
	var p Publication
	err := s.db.QueryRowContext(ctx, `
SELECT uid, node_name, route_slug, public_url, status, reason, failed_step,
       nosla_status, bwg_status, updated_at
FROM emby_publications WHERE uid = ? AND node_name = ?
`, uid, nodeName).Scan(
		&p.UID, &p.NodeName, &p.RouteSlug, &p.PublicURL, &p.Status, &p.Reason,
		&p.FailedStep, &p.NOSLAStatus, &p.BWGStatus, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) SavePublication(ctx context.Context, p Publication) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO emby_publications
  (uid, node_name, route_slug, public_url, status, reason, failed_step,
   nosla_status, bwg_status, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(uid, node_name) DO UPDATE SET
  route_slug = excluded.route_slug,
  public_url = excluded.public_url,
  status = excluded.status,
  reason = excluded.reason,
  failed_step = excluded.failed_step,
  nosla_status = excluded.nosla_status,
  bwg_status = excluded.bwg_status,
  updated_at = excluded.updated_at
`, p.UID, p.NodeName, p.RouteSlug, p.PublicURL, p.Status, p.Reason,
		p.FailedStep, p.NOSLAStatus, p.BWGStatus, p.UpdatedAt)
	return err
}

func (s *Store) DeletePublication(ctx context.Context, uid, nodeName string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM emby_publications WHERE uid = ? AND node_name = ?`, uid, nodeName)
	return err
}
