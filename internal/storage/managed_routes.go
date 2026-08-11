package storage

import (
	"context"
	"database/sql"
	"time"
)

type ManagedRoute struct {
	Slug        string `json:"slug"`
	NodeName    string `json:"node_name"`
	Enabled     bool   `json:"enabled"`
	Public      bool   `json:"public"`
	DefaultLine string `json:"default_line"`
}

type ManagedRouteLine struct {
	RouteSlug string `json:"route_slug"`
	LineSlug  string `json:"line_slug"`
	Target    string `json:"target"`
	Enabled   bool   `json:"enabled"`
	Position  int    `json:"position"`
}

func (s *Store) ListManagedRoutes(ctx context.Context) ([]ManagedRoute, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT slug, node_name, enabled, public, default_line
		FROM managed_routes
		ORDER BY slug ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	routes := []ManagedRoute{}
	for rows.Next() {
		var route ManagedRoute
		var enabled, public int
		if err := rows.Scan(&route.Slug, &route.NodeName, &enabled, &public, &route.DefaultLine); err != nil {
			return nil, err
		}
		route.Enabled = enabled != 0
		route.Public = public != 0
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

func (s *Store) SaveManagedRoute(ctx context.Context, route ManagedRoute, lines []ManagedRouteLine) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO managed_routes
			(slug, node_name, enabled, public, default_line, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(slug) DO UPDATE SET
			node_name = excluded.node_name,
			enabled = excluded.enabled,
			public = excluded.public,
			default_line = excluded.default_line,
			updated_at = excluded.updated_at
	`, route.Slug, route.NodeName, boolInt(route.Enabled), boolInt(route.Public), route.DefaultLine, now, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM managed_route_lines WHERE route_slug = ?`, route.Slug); err != nil {
		return err
	}
	for _, line := range lines {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO managed_route_lines
				(route_slug, line_slug, target, enabled, position)
			VALUES (?, ?, ?, ?, ?)
		`, route.Slug, line.LineSlug, line.Target, boolInt(line.Enabled), line.Position); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteManagedRoute(ctx context.Context, slug string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM managed_routes WHERE slug = ?`, slug)
	return err
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
