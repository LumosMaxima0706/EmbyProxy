package proxyadapter

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"embyproxy/internal/mediaproxy"
	"embyproxy/internal/nodes"
	"embyproxy/internal/storage"
)

type RouteStore interface {
	GetNode(context.Context, string, string) (*storage.Node, error)
	GetManagedRoute(context.Context, string) (*storage.ManagedRoute, error)
	ListManagedRouteLines(context.Context, string) ([]storage.ManagedRouteLine, error)
}

// ProxyNodeStore is intentionally optional. Older stores and edge releases
// that do not expose the control-plane table continue to use managed routes.
type ProxyNodeStore interface {
	ListProxyNodes(context.Context) ([]storage.ProxyNode, error)
}

type ProxyNodeConnectionStore interface {
	BeginProxyNodeConnection(context.Context, string) error
	EndProxyNodeConnection(context.Context, string) error
}

type proxyNodeUsageStore interface {
	AddProxyNodeUsage(context.Context, string, int64, time.Time) error
}

func (r *StorageResolver) RecordProxyNodeBytes(ctx context.Context, id string, bytes int64) {
	if bytes <= 0 {
		return
	}
	store, ok := r.store.(proxyNodeUsageStore)
	if !ok {
		return
	}
	_ = store.AddProxyNodeUsage(ctx, id, bytes, time.Now())
}

func (r *StorageResolver) BeginProxyNodeConnection(ctx context.Context, id string) error {
	store, ok := r.store.(ProxyNodeConnectionStore)
	if !ok {
		return nil
	}
	return store.BeginProxyNodeConnection(ctx, id)
}

func (r *StorageResolver) EndProxyNodeConnection(ctx context.Context, id string) error {
	store, ok := r.store.(ProxyNodeConnectionStore)
	if !ok {
		return nil
	}
	return store.EndProxyNodeConnection(ctx, id)
}

const selectedNodeHeader = "X-EmbyProxy-Selected-Node"

type StorageResolver struct {
	store       RouteStore
	uid         string
	mode        string
	mu          sync.Mutex
	assignments map[string]nodeAssignment
}

type nodeAssignment struct {
	id    string
	since time.Time
}

// NewStorageResolver accepts an optional scheduler mode for compatibility
// with callers that predate proxy-node scheduling. The default is manual.
func NewStorageResolver(store RouteStore, uid string, schedulerMode ...string) *StorageResolver {
	if strings.TrimSpace(uid) == "" {
		uid = "admin"
	}
	mode := "manual"
	if len(schedulerMode) > 0 && strings.EqualFold(strings.TrimSpace(schedulerMode[0]), "smart") {
		mode = "smart"
	}
	return &StorageResolver{store: store, uid: uid, mode: mode, assignments: make(map[string]nodeAssignment)}
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
	// A selected edge receives the same public route and must resolve the
	// original managed line locally. The marker prevents a routing loop when
	// multiple edges run the same sidecar build.
	if _, marked := ctx.Value(selectedNodeContextKey{}).(bool); !marked {
		if selected, ok := r.selectEdge(ctx, slug, target); ok {
			target = selected
		}
	}
	return target, true, "/s/" + slug + "/", nil
}

type selectedNodeContextKey struct{}

type selectionMeta struct {
	selected bool
	nodeID   string
}

type selectionMetaContextKey struct{}

func MarkSelectedNodeRequest(ctx context.Context) context.Context {
	return context.WithValue(ctx, selectedNodeContextKey{}, true)
}

func (r *StorageResolver) selectEdge(ctx context.Context, slug string, fallback mediaproxy.Target) (mediaproxy.Target, bool) {
	provider, ok := r.store.(ProxyNodeStore)
	if !ok {
		return mediaproxy.Target{}, false
	}
	nodeList, err := provider.ListProxyNodes(ctx)
	if err != nil || len(nodeList) == 0 {
		return mediaproxy.Target{}, false
	}
	now := time.Now()
	key := r.uid + ":" + slug
	r.mu.Lock()
	current := r.assignments[key]
	decision, selected := nodes.SelectWithPolicy(nodeList, nodes.Policy{
		Mode: r.mode, CurrentID: current.id, CurrentSince: current.since,
		MinimumDwell: 2 * time.Minute, HysteresisScore: 0.08,
	}, now)
	if !selected {
		r.mu.Unlock()
		return mediaproxy.Target{}, false
	}
	if current.id != decision.NodeID {
		current = nodeAssignment{id: decision.NodeID, since: now}
		r.assignments[key] = current
	}
	r.mu.Unlock()
	var node storage.ProxyNode
	for _, candidate := range nodeList {
		if candidate.ID == decision.NodeID {
			node = candidate
			break
		}
	}
	if node.ID == "" {
		return mediaproxy.Target{}, false
	}
	target, err := parseProxyAddress(node.PublicAddress)
	if err != nil || sameOrigin(target, fallback) {
		return mediaproxy.Target{}, false
	}
	if target.BasePath == "" {
		target.BasePath = "/s/" + slug
	}
	if meta, ok := ctx.Value(selectionMetaContextKey{}).(*selectionMeta); ok && meta != nil {
		meta.selected = true
		meta.nodeID = node.ID
	}
	return target, true
}

func parseProxyAddress(raw string) (mediaproxy.Target, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return mediaproxy.Target{}, ErrInvalidTarget
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" {
		return mediaproxy.Target{}, ErrInvalidTarget
	}
	return parseServerTarget(value)
}

func sameOrigin(a, b mediaproxy.Target) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host) && a.Port == b.Port
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
