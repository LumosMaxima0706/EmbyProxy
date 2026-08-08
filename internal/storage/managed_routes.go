package storage

import (
	"context"
	"database/sql"
)

type ManagedRoute struct {
	Slug        string
	NodeName    string
	Enabled     bool
	Public      bool
	DefaultLine string
}

type ManagedRouteLine struct {
	RouteSlug string
	LineSlug  string
	Target    string
	Enabled   bool
	Position  int
}

func (s *Store) InitManagedRoutesSchema(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS managed_routes (
			slug TEXT PRIMARY KEY,
			node_name TEXT NOT NULL UNIQUE,
			enabled INTEGER NOT NULL DEFAULT 0,
			public INTEGER NOT NULL DEFAULT 0,
			default_line TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS managed_route_lines (
			route_slug TEXT NOT NULL,
			line_slug TEXT NOT NULL,
			target TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 0,
			position INTEGER NOT NULL DEFAULT 0,
			note TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(route_slug, line_slug),
			FOREIGN KEY(route_slug) REFERENCES managed_routes(slug) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_managed_route_lines_order
			ON managed_route_lines(route_slug, enabled, position, line_slug)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetManagedRoute(ctx context.Context, slug string) (*ManagedRoute, error) {
	var route ManagedRoute
	var enabled, public int
	err := s.db.QueryRowContext(ctx, `
		SELECT slug, node_name, enabled, public, default_line
		FROM managed_routes
		WHERE slug = ?
	`, slug).Scan(&route.Slug, &route.NodeName, &enabled, &public, &route.DefaultLine)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	route.Enabled = enabled != 0
	route.Public = public != 0
	return &route, nil
}

func (s *Store) ListManagedRouteLines(ctx context.Context, slug string) ([]ManagedRouteLine, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT route_slug, line_slug, target, enabled, position
		FROM managed_route_lines
		WHERE route_slug = ?
		ORDER BY position ASC, line_slug ASC
	`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lines := []ManagedRouteLine{}
	for rows.Next() {
		var line ManagedRouteLine
		var enabled int
		if err := rows.Scan(&line.RouteSlug, &line.LineSlug, &line.Target, &enabled, &line.Position); err != nil {
			return nil, err
		}
		line.Enabled = enabled != 0
		lines = append(lines, line)
	}
	return lines, rows.Err()
}
