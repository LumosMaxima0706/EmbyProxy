package proxyadapter

import (
	"strings"
	"sync"

	"embyproxy/internal/mediaproxy"
	"embyproxy/internal/storage"
)

type SlugConfig struct {
	Slug       string
	RawTarget  string
	Enabled    bool
	PublicPath string
}

type slugRecord struct {
	target     mediaproxy.Target
	enabled    bool
	publicPath string
}

type nodeRecord struct {
	node   storage.Node
	target mediaproxy.Target
}

// Registry is an immutable-after-construction server-side route snapshot.
// It intentionally has no method accepting client-controlled target data.
type Registry struct {
	mu    sync.RWMutex
	slugs map[string]slugRecord
	nodes map[string]nodeRecord
}

func NewRegistry(slugs []SlugConfig, nodes []storage.Node) (*Registry, error) {
	registry := &Registry{slugs: make(map[string]slugRecord, len(slugs)), nodes: make(map[string]nodeRecord, len(nodes))}
	for _, config := range slugs {
		if err := validateSlug(config.Slug); err != nil {
			return nil, err
		}
		target, err := parseServerTarget(config.RawTarget)
		if err != nil {
			return nil, err
		}
		if _, exists := registry.slugs[config.Slug]; exists {
			return nil, ErrInvalidSlug
		}
		if config.PublicPath != "" && (!strings.HasPrefix(config.PublicPath, "/") || strings.ContainsAny(config.PublicPath, "?#")) {
			return nil, ErrInvalidSlug
		}
		registry.slugs[config.Slug] = slugRecord{target: target, enabled: config.Enabled, publicPath: config.PublicPath}
	}
	for _, node := range nodes {
		if err := validateNodeName(node.Name); err != nil {
			return nil, err
		}
		if node.Secret != "" && strings.ContainsAny(node.Secret, "/?#") {
			return nil, ErrInvalidNode
		}
		targets := storage.SplitTargets(node.Target)
		if len(targets) != 1 {
			if len(targets) == 0 {
				return nil, ErrInvalidTarget
			}
			return nil, ErrMultipleTarget
		}
		target, err := parseServerTarget(targets[0])
		if err != nil {
			return nil, err
		}
		name := strings.ToLower(node.Name)
		if _, exists := registry.nodes[name]; exists {
			return nil, ErrInvalidNode
		}
		registry.nodes[name] = nodeRecord{node: node, target: target}
	}
	return registry, nil
}

func (r *Registry) slug(slug string) (mediaproxy.Target, bool, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.slugs[slug]
	if !ok {
		return mediaproxy.Target{}, false, "", ErrNotFound
	}
	return record.target, record.enabled, record.publicPath, nil
}

func (r *Registry) node(name string) (storage.Node, mediaproxy.Target, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.nodes[strings.ToLower(name)]
	if !ok {
		return storage.Node{}, mediaproxy.Target{}, ErrNotFound
	}
	return record.node, record.target, nil
}
