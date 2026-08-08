package failover

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrInvalidMode = errors.New("invalid failover mode")
var ErrUnknownNode = errors.New("unknown failover node")
var ErrCooldown = errors.New("failover cooldown active")

type Controller struct {
	mu           sync.RWMutex
	nodes        map[string]Node
	state        State
	config       PolicyConfig
	events       []Event
	eventWriter  func(Event) error
	dnsRunWriter func(DNSChange, bool) error
	stateWriter  func(State) error
	now          func() time.Time
	dns          DNSProvider
	record       string
}

func (c *Controller) SetEventWriter(writer func(Event) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eventWriter = writer
}

func (c *Controller) SetDNSRunWriter(writer func(DNSChange, bool) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dnsRunWriter = writer
}

func (c *Controller) SetStateWriter(writer func(State) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stateWriter = writer
}

func NewController(nodes []Node, cfg PolicyConfig, dns DNSProvider) *Controller {
	if cfg.FailureThreshold <= 0 {
		cfg = DefaultPolicyConfig()
	}
	byID := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	active := ""
	for _, node := range nodes {
		if node.Role == RolePrimary && node.Enabled {
			active = node.ID
			break
		}
	}
	return &Controller{
		nodes:  byID,
		state:  State{Mode: ModeAuto, ActiveNodeID: active},
		config: cfg,
		now:    time.Now,
		dns:    dns,
		record: "stream",
	}
}

func (c *Controller) SetDNSProvider(provider DNSProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dns = provider
}

// ApplyDNSAndCommit demonstrates the safe transaction boundary: local active
// state changes only after the mock provider applies and verifies propagation.
func (c *Controller) ApplyDNSAndCommit(ctx context.Context, change DNSChange, nodeID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dns == nil {
		return errors.New("dns_provider_unavailable")
	}
	if _, ok := c.nodes[nodeID]; !ok {
		return ErrUnknownNode
	}
	propagation, err := ApplyDNS(ctx, c.dns, change)
	if err != nil || !propagation.Verified {
		c.state.ReconciliationRequired = true
		if c.dnsRunWriter != nil {
			_ = c.dnsRunWriter(change, false)
		}
		c.persistStateLocked()
		if err == nil {
			err = errors.New("dns_propagation_unverified")
		}
		return err
	}
	previous := c.state
	c.state.ObservedDNSNodeID = nodeID
	c.state.ActiveNodeID = nodeID
	c.state.DesiredNodeID = nodeID
	c.state.ReconciliationRequired = false
	c.state.LastTransitionAt = c.now()
	if c.dnsRunWriter != nil {
		_ = c.dnsRunWriter(change, true)
	}
	if err := c.appendEventLocked(Event{EventType: "dns_switch_committed", ToNodeID: nodeID, Mode: c.state.Mode, ReasonCode: "dns_apply_verified", Success: true}); err != nil {
		c.state = previous
		c.state.ReconciliationRequired = true
		c.persistStateLocked()
		return err
	}
	c.persistStateLocked()
	return nil
}

func (c *Controller) SetNow(now func() time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now != nil {
		c.now = now
	}
}

func (c *Controller) Status() (State, []Node) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	state := c.state
	nodes := make([]Node, 0, len(c.nodes))
	for _, node := range c.nodes {
		nodes = append(nodes, node)
	}
	return state, nodes
}

func (c *Controller) Events(limit int) []Event {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if limit <= 0 || limit > len(c.events) {
		limit = len(c.events)
	}
	start := len(c.events) - limit
	result := append([]Event(nil), c.events[start:]...)
	return result
}

func (c *Controller) SetMode(mode Mode) error {
	if !validMode(mode) {
		return ErrInvalidMode
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := c.state
	c.state.Mode = mode
	if err := c.appendEventLocked(Event{EventType: "mode_changed", Mode: mode, ReasonCode: string(mode), Success: true}); err != nil {
		c.state = previous
		return err
	}
	c.persistStateLocked()
	return nil
}

func (c *Controller) SetTraffic(sample TrafficSample) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	node, ok := c.nodes[sample.NodeID]
	if !ok {
		return ErrUnknownNode
	}
	node.Traffic = sample
	c.nodes[sample.NodeID] = node
	return nil
}

func (c *Controller) SetHealth(result HealthResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	node, ok := c.nodes[result.NodeID]
	if !ok {
		return ErrUnknownNode
	}
	if result.Success {
		node.HealthStatus = HealthHealthy
		node.ConsecutiveSuccesses++
		node.ConsecutiveFailures = 0
	} else {
		node.HealthStatus = HealthFailed
		node.ConsecutiveFailures++
		node.ConsecutiveSuccesses = 0
	}
	c.nodes[result.NodeID] = node
	return nil
}

func (c *Controller) Evaluate() Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	state := c.state
	state.LastEvaluationAt = now
	c.state = state
	nodes := make([]Node, 0, len(c.nodes))
	for _, node := range c.nodes {
		nodes = append(nodes, node)
	}
	return Evaluate(nodes, state, c.config, now)
}

// ApplyDecision updates local state only. Production DNS orchestration is
// intentionally not wired in Phase 2A.
func (c *Controller) ApplyDecision(decision Decision, reason string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if decision.NodeID == "" {
		return ErrUnknownNode
	}
	if _, ok := c.nodes[decision.NodeID]; !ok {
		return ErrUnknownNode
	}
	now := c.now()
	if decision.NodeID == c.state.ActiveNodeID {
		return nil
	}
	if c.state.Mode == ModeAuto && !c.state.CooldownUntil.IsZero() && now.Before(c.state.CooldownUntil) {
		return ErrCooldown
	}
	from := c.state.ActiveNodeID
	previous := c.state
	c.state.ActiveNodeID = decision.NodeID
	c.state.DesiredNodeID = decision.NodeID
	c.state.LastTransitionAt = now
	c.state.CooldownUntil = now.Add(c.config.Cooldown)
	if err := c.appendEventLocked(Event{EventType: "node_switched", CreatedAt: now, FromNodeID: from, ToNodeID: decision.NodeID, Mode: c.state.Mode, ReasonCode: RedactReason(reason), Success: true}); err != nil {
		c.state = previous
		return err
	}
	c.persistStateLocked()
	return nil
}

func (c *Controller) persistStateLocked() {
	if c.stateWriter != nil {
		_ = c.stateWriter(c.state)
	}
}

func (c *Controller) appendEventLocked(event Event) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = c.now()
	}
	event.ID = int64(len(c.events) + 1)
	if c.eventWriter != nil {
		if err := c.eventWriter(event); err != nil {
			return err
		}
	}
	c.events = append(c.events, event)
	return nil
}

func validMode(mode Mode) bool {
	switch mode {
	case ModeAuto, ModeForceNOSLA, ModeForceBWG, ModeMaintenanceNOSLA:
		return true
	default:
		return false
	}
}
