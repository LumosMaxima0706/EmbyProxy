package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestManagedRoutesSchemaAndQueries(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.InitManagedRoutesSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO managed_routes
			(slug, node_name, enabled, public, default_line, created_at, updated_at)
		VALUES ('demo', 'node', 1, 1, 'main', 1, 1)
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO managed_route_lines
			(route_slug, line_slug, target, enabled, position)
		VALUES
			('demo', 'backup', 'https://backup.example', 1, 2),
			('demo', 'main', 'https://media.example/base', 1, 1)
	`); err != nil {
		t.Fatal(err)
	}
	route, err := store.GetManagedRoute(ctx, "demo")
	if err != nil || route == nil {
		t.Fatalf("route=%v err=%v", route, err)
	}
	if !route.Enabled || !route.Public || route.DefaultLine != "main" || route.NodeName != "node" {
		t.Fatalf("unexpected route metadata: %+v", route)
	}
	lines, err := store.ListManagedRouteLines(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0].LineSlug != "main" || lines[1].LineSlug != "backup" {
		t.Fatalf("unexpected route line order: %+v", lines)
	}
	missing, err := store.GetManagedRoute(ctx, "missing")
	if err != nil || missing != nil {
		t.Fatalf("missing route=%v err=%v", missing, err)
	}
}
