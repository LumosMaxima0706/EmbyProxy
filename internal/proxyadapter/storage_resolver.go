package proxyadapter

import (
	"context"
	"fmt"
	"strings"

	"embyproxy/internal/mediaproxy"
	"embyproxy/internal/storage"
)

type RouteStore interface {
	GetNode(context.Context, string, string) (*storage.Node, error)
	GetManagedRoute(context.Context, string) (*storage.ManagedRoute, error)
	ListManagedRouteLines(context.Context, string) ([]storage.ManagedRouteLine, error)
}

type StorageResolver struct {
	store RouteStore
	uid   string
}

func NewStorageResolver(store RouteStore, uid string) *StorageResolver {
	if strings.TrimSpace(uid) == "" {
		uid = "admin"
	}
	return &StorageResolver{store: store, uid: uid}
}

func (r *StorageResolver) slug(ctx context.Context, slug string) (mediaproxy.Target, bool, string, error) {
	if r == nil || r.store == nil {
		return mediaproxy.Target{}, false, "", ErrResolver
	}
	route, err := r.store.GetManagedRoute(ctx, slug)
	if err != nil {
		return mediaproxy.Target{}, false, "", fmt.Errorf("%w: managed route lookup", ErrResolver)
	}
	if route == nil {
		return mediaproxy.Target{}, false, "", ErrNotFound
	}
	if !route.Enabled || !route.Public {
		return mediaproxy.Target{}, false, "", ErrRouteDisabled
	}
	lines, err := r.store.ListManagedRouteLines(ctx, slug)
	if err != nil {
		return mediaproxy.Target{}, false, "", fmt.Errorf("%w: managed line lookup", ErrResolver)
	}
	line, ok := selectManagedLine(route.DefaultLine, lines)
	if !ok {
		return mediaproxy.Target{}, false, "", ErrRouteDisabled
	}
	target, err := parseServerTarget(line.Target)
	if err != nil {
		return mediaproxy.Target{}, false, "", err
	}
	return target, true, "/s/" + slug + "/", nil
}

func (r *StorageResolver) node(ctx context.Context, name string) (storage.Node, mediaproxy.Target, error) {
	if r == nil || r.store == nil {
		return storage.Node{}, mediaproxy.Target{}, ErrResolver
	}
	node, err := r.store.GetNode(ctx, r.uid, name)
	if err != nil {
		return storage.Node{}, mediaproxy.Target{}, fmt.Errorf("%w: node lookup", ErrResolver)
	}
	if node == nil {
		return storage.Node{}, mediaproxy.Target{}, ErrNotFound
	}
	targets := storage.SplitTargets(node.Target)
	if len(targets) == 0 {
		return storage.Node{}, mediaproxy.Target{}, ErrInvalidTarget
	}
	if len(targets) != 1 {
		return storage.Node{}, mediaproxy.Target{}, ErrMultipleTarget
	}
	target, err := parseServerTarget(targets[0])
	if err != nil {
		return storage.Node{}, mediaproxy.Target{}, err
	}
	return *node, target, nil
}

func selectManagedLine(defaultLine string, lines []storage.ManagedRouteLine) (storage.ManagedRouteLine, bool) {
	if defaultLine != "" {
		for _, line := range lines {
			if line.Enabled && line.LineSlug == defaultLine {
				return line, true
			}
		}
		return storage.ManagedRouteLine{}, false
	}
	for _, line := range lines {
		if line.Enabled {
			return line, true
		}
	}
	return storage.ManagedRouteLine{}, false
}
