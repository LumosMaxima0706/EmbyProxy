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

func TestManagedRouteSaveListUpdateAndDelete(t *testing.T) {
	ctx := context.Background()
	store, err := New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	route := ManagedRoute{Slug: "demo", NodeName: "node", Enabled: true, Public: true, DefaultLine: "main"}
	lines := []ManagedRouteLine{
		{LineSlug: "backup", Target: "https://backup.example", Enabled: true, Position: 2},
		{LineSlug: "main", Target: "https://media.example/base", Enabled: true, Position: 1},
	}
	if err := store.SaveManagedRoute(ctx, route, lines); err != nil {
		t.Fatal(err)
	}
	routes, err := store.ListManagedRoutes(ctx)
	if err != nil || len(routes) != 1 || routes[0] != route {
		t.Fatalf("routes=%+v err=%v", routes, err)
	}
	gotLines, err := store.ListManagedRouteLines(ctx, route.Slug)
	if err != nil || len(gotLines) != 2 || gotLines[0].LineSlug != "main" {
		t.Fatalf("lines=%+v err=%v", gotLines, err)
	}

	updated := route
	updated.NodeName = "node-updated"
	updated.DefaultLine = "backup"
	if err := store.SaveManagedRoute(ctx, updated, []ManagedRouteLine{{LineSlug: "backup", Target: "https://backup.example/v2", Enabled: true, Position: 1}}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetManagedRoute(ctx, route.Slug)
	if err != nil || got == nil || got.NodeName != "node-updated" || got.DefaultLine != "backup" {
		t.Fatalf("updated route=%+v err=%v", got, err)
	}
	gotLines, err = store.ListManagedRouteLines(ctx, route.Slug)
	if err != nil || len(gotLines) != 1 || gotLines[0].Target != "https://backup.example/v2" {
		t.Fatalf("updated lines=%+v err=%v", gotLines, err)
	}

	if err := store.DeleteManagedRoute(ctx, route.Slug); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.GetManagedRoute(ctx, route.Slug)
	if err != nil || deleted != nil {
		t.Fatalf("deleted route=%+v err=%v", deleted, err)
	}
	gotLines, err = store.ListManagedRouteLines(ctx, route.Slug)
	if err != nil || len(gotLines) != 0 {
		t.Fatalf("deleted lines=%+v err=%v", gotLines, err)
	}
}

func TestManagedRoutesSurviveStoreReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "proxy.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManagedRoute(ctx, ManagedRoute{Slug: "persisted", NodeName: "persisted-node", Enabled: true, Public: true, DefaultLine: "main"}, []ManagedRouteLine{{RouteSlug: "persisted", LineSlug: "main", Target: "https://media.example/base", Enabled: true, Position: 1}}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	route, err := reopened.GetManagedRoute(ctx, "persisted")
	if err != nil {
		t.Fatal(err)
	}
	if route == nil || route.NodeName != "persisted-node" || !route.Enabled || !route.Public {
		t.Fatalf("reopened route = %+v", route)
	}
	lines, err := reopened.ListManagedRouteLines(ctx, "persisted")
	if err != nil || len(lines) != 1 || lines[0].Target != "https://media.example/base" {
		t.Fatalf("reopened lines = %+v err=%v", lines, err)
	}
}
