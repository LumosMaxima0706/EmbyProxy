package failover

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrInvalidMode = errors.New("invalid failover mode")
var ErrUnknownNode = errors.New("unknown failover node")
var ErrCooldown = errors.New("failover cooldown active")
var ErrInvalidTraffic = errors.New("invalid traffic sample")
var ErrNodeNotEligible = errors.New("failover node is not eligible")
var ErrSwitchRateLimited = errors.New("failover switch rate limit active")
var ErrAtomicPersistenceRequired = errors.New("atomic failover persistence writer required")

type TransitionWriter func(State, Event) error
type DNSCommitWriter func(DNSChange, State, Event, bool) error

type Controller struct {
	mu               sync.RWMutex
	nodes            map[string]Node
	state            State
	config           PolicyConfig
	events           []Event
	eventWriter      func(Event) error
	dnsRunWriter     func(DNSChange, bool) error
	dnsPendingWriter func(DNSChange) error
	stateWriter      func(State) error
	trafficWriter    func(TrafficSample) error
	healthWriter     func(HealthResult, Node) error
	transitionWriter TransitionWriter
	dnsCommitWriter  DNSCommitWriter
	now              func() time.Time
	dns              DNSProvider
	dnsGuard         DNSGuardConfig
	pendingDNS       map[string]pendingDNSApply
	record           string
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

func (c *Controller) SetDNSPendingWriter(writer func(DNSChange) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dnsPendingWriter = writer
}

func (c *Controller) SetStateWriter(writer func(State) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stateWriter = writer
}

func (c *Controller) SetTrafficWriter(writer func(TrafficSample) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.trafficWriter = writer
}

func (c *Controller) SetHealthWriter(writer func(HealthResult, Node) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.healthWriter = writer
}

func (c *Controller) SetTransitionWriter(writer TransitionWriter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.transitionWriter = writer
}

func (c *Controller) SetDNSCommitWriter(writer DNSCommitWriter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dnsCommitWriter = writer
}

func NewController(nodes []Node, cfg PolicyConfig, dns DNSProvider) *Controller {
	cfg = normalizePolicyConfig(cfg)
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
		nodes:      byID,
		state:      State{Mode: ModeAuto, ActiveNodeID: active},
		config:     cfg,
		now:        time.Now,
		dns:        dns,
		pendingDNS: make(map[string]pendingDNSApply),
		record:     "stream",
	}
}

func (c *Controller) SetDNSProvider(provider DNSProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dns = provider
	c.pendingDNS = make(map[string]pendingDNSApply)
}

func (c *Controller) PrepareDNSApply(ctx context.Context, change DNSChange, nodeID string) (DNSPlan, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dns == nil {
		return DNSPlan{}, errors.New("dns_provider_unavailable")
	}
	if err := c.dnsProviderAllowedLocked(); err != nil {
		return DNSPlan{}, err
	}
	node, ok := c.nodes[nodeID]
	if !ok {
		return DNSPlan{}, ErrUnknownNode
	}
	if !node.Enabled || node.Maintenance {
		return DNSPlan{}, ErrNodeNotEligible
	}
	normalized, err := c.normalizeAllowedChangeLocked(change)
	if err != nil {
		return DNSPlan{}, err
	}
	change = normalized
	change.DryRun = true
	change.ProviderMode = c.dnsGuard.ProviderMode
	previous, previousErr := c.dns.GetRecord(ctx, change.Name, change.Type)
	if previousErr == nil {
		change.PreviousValue = previous.Value
		change.PreviousValueKnown = true
	}
	if (change.ProviderMode == DNSProviderModeReal || change.ProviderMode == DNSProviderModeExternal) && previousErr != nil {
		return DNSPlan{}, ErrDNSRollbackMetadataRequired
	}
	providerPlan, providerErr := c.dns.DryRunUpdate(ctx, change)
	if c.dnsRunWriter != nil {
		if writeErr := c.dnsRunWriter(change, providerErr == nil); writeErr != nil {
			return DNSPlan{}, errors.Join(providerErr, writeErr)
		}
	}
	if providerErr != nil {
		return DNSPlan{}, providerErr
	}
	id, err := newDNSDryRunID()
	if err != nil {
		return DNSPlan{}, err
	}
	now := c.now()
	expiresAt := now.Add(c.dnsGuard.DryRunTTL)
	plan := DNSPlan{
		ID:           id,
		TargetNodeID: nodeID,
		ProviderMode: change.ProviderMode,
		GeneratedAt:  now,
		ExpiresAt:    expiresAt,
		Change:       change,
		Note:         providerPlan.Note,
	}
	if previousErr == nil {
		plan.PreviousRecord = &previous
	}
	if c.pendingDNS == nil {
		c.pendingDNS = make(map[string]pendingDNSApply)
	}
	for existingID, pending := range c.pendingDNS {
		if !now.Before(pending.ExpiresAt) {
			delete(c.pendingDNS, existingID)
		}
	}
	c.pendingDNS[id] = pendingDNSApply{
		ID:             id,
		TargetNodeID:   nodeID,
		ProviderMode:   change.ProviderMode,
		Change:         change,
		PreviousRecord: previous,
		PreviousKnown:  previousErr == nil,
		GeneratedAt:    now,
		ExpiresAt:      expiresAt,
	}
	return plan, nil
}

// ApplyDNSAndCommit consumes a matching one-time dry-run before calling the
// configured provider. Real/external modes remain blocked until explicitly
// enabled with verified rollback metadata support.
func (c *Controller) ApplyDNSAndCommit(ctx context.Context, change DNSChange, nodeID, dryRunID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.TrimSpace(dryRunID) == "" {
		return ErrDNSDryRunRequired
	}
	if c.dns == nil {
		return errors.New("dns_provider_unavailable")
	}
	if err := c.dnsProviderAllowedLocked(); err != nil {
		return err
	}
	pending, ok := c.pendingDNS[dryRunID]
	if !ok {
		return ErrDNSDryRunRequired
	}
	delete(c.pendingDNS, dryRunID)
	if !c.now().Before(pending.ExpiresAt) {
		return ErrDNSDryRunExpired
	}
	normalized, err := c.normalizeAllowedChangeLocked(change)
	if err != nil {
		return err
	}
	if pending.TargetNodeID != nodeID || pending.ProviderMode != c.dnsGuard.ProviderMode || !sameDNSChange(pending.Change, normalized) {
		return ErrDNSDryRunMismatch
	}
	change = pending.Change
	change.DryRun = false
	if _, ok := c.nodes[nodeID]; !ok {
		return ErrUnknownNode
	}
	node := c.nodes[nodeID]
	if !node.Enabled || node.Maintenance {
		return c.rejectNodeLocked(nodeID)
	}
	if c.dnsCommitWriter == nil && c.hasDNSPersistenceLocked() {
		return ErrAtomicPersistenceRequired
	}
	if c.dnsPendingWriter != nil {
		if err := c.dnsPendingWriter(change); err != nil {
			return err
		}
	}
	propagation, err := ApplyDNS(ctx, c.dns, change)
	if err != nil || !propagation.Verified {
		next := c.state
		next.ReconciliationRequired = true
		if err == nil {
			err = errors.New("dns_propagation_unverified")
		}
		event := Event{EventType: "dns_switch_failed", ToNodeID: nodeID, Mode: c.state.Mode, ReasonCode: RedactReason(err.Error()), Success: false}
		if c.dnsCommitWriter != nil {
			event = appendEventCopy(c.events, event, c.now())[len(c.events)]
			if commitErr := c.dnsCommitWriter(change, next, event, false); commitErr != nil {
				return errors.Join(err, commitErr)
			}
			c.state = next
			c.events = appendEventCopy(c.events, event, c.now())
			return err
		}
		c.state = next
		c.events = appendEventCopy(c.events, event, c.now())
		return err
	}
	next := c.state
	next.ObservedDNSNodeID = nodeID
	next.ActiveNodeID = nodeID
	next.DesiredNodeID = nodeID
	next.ReconciliationRequired = false
	next.LastTransitionAt = c.now()
	next.LastEvaluationAt = next.LastTransitionAt
	if nodeID != c.state.ActiveNodeID {
		c.advanceCycleForTransitionLocked(&next)
	}
	event := Event{EventType: "dns_switch_committed", ToNodeID: nodeID, Mode: next.Mode, ReasonCode: "dns_apply_verified", Success: true}
	if c.dnsCommitWriter != nil {
		event = appendEventCopy(c.events, event, c.now())[len(c.events)]
		if commitErr := c.dnsCommitWriter(change, next, event, true); commitErr != nil {
			return commitErr
		}
		c.state = next
		c.events = appendEventCopy(c.events, event, c.now())
		return nil
	}
	c.state = next
	c.events = appendEventCopy(c.events, event, c.now())
	return nil
}

func (c *Controller) SetNow(now func() time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now != nil {
		c.now = now
	}
}

func (c *Controller) RestoreState(state State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if state.Mode == "" {
		state.Mode = ModeAuto
	}
	c.state = state
}

func (c *Controller) RestoreEvents(events []Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append([]Event(nil), events...)
}

func (c *Controller) RestoreNodeRuntime(nodeID string, failures, successes int, traffic TrafficSample) {
	c.mu.Lock()
	defer c.mu.Unlock()
	node, ok := c.nodes[nodeID]
	if !ok {
		return
	}
	node.ConsecutiveFailures = failures
	node.ConsecutiveSuccesses = successes
	node.HealthStatus = healthStatusForCounters(failures, successes, c.config.FailureThreshold)
	if traffic.NodeID != "" {
		node.Traffic = traffic
	}
	c.nodes[nodeID] = node
}

func (c *Controller) Status() (State, []Node) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.statusLocked()
}

func (c *Controller) StatusWithEligibility() (State, []Node, map[string]bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	state, nodes := c.statusLocked()
	eligibility := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		eligibility[node.ID] = failoverEligible(node, state, c.config)
	}
	return state, nodes, eligibility
}

func (c *Controller) statusLocked() (State, []Node) {
	state := c.state
	nodes := make([]Node, 0, len(c.nodes))
	for _, node := range c.nodes {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Priority != nodes[j].Priority {
			return nodes[i].Priority < nodes[j].Priority
		}
		return nodes[i].ID < nodes[j].ID
	})
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
	next := c.state
	next.Mode = mode
	event := Event{EventType: "mode_changed", Mode: mode, ReasonCode: string(mode), Success: true}
	if err := c.commitTransitionLocked(previous, next, event); err != nil {
		return err
	}
	return nil
}

func (c *Controller) SetTraffic(sample TrafficSample) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	node, ok := c.nodes[sample.NodeID]
	if !ok {
		return ErrUnknownNode
	}
	if sample.Quality == "" {
		sample.Quality = TrafficUnknown
	}
	if sample.Quality == TrafficKnown && (sample.InboundBytes < 0 || sample.OutboundBytes < 0 || sample.TotalBytes < 0 || sample.QuotaBytes < 0 || sample.ThresholdPct < 0 || sample.ThresholdPct > 100) {
		return ErrInvalidTraffic
	}
	if sample.SampledAt.IsZero() {
		sample.SampledAt = c.now()
	}
	// Sources that provide directional counters are normalized here. Persisted
	// or restored samples may only have TotalBytes, so preserve that value when
	// no directional counters were supplied.
	if sample.Quality == TrafficKnown && (sample.InboundBytes != 0 || sample.OutboundBytes != 0) {
		sample.TotalBytes = sample.InboundBytes + sample.OutboundBytes
	}
	if c.trafficWriter != nil {
		if err := c.trafficWriter(sample); err != nil {
			return err
		}
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
		node.ConsecutiveSuccesses++
		node.ConsecutiveFailures = 0
	} else {
		node.ConsecutiveFailures++
		node.ConsecutiveSuccesses = 0
	}
	node.HealthStatus = healthStatusForCounters(node.ConsecutiveFailures, node.ConsecutiveSuccesses, c.config.FailureThreshold)
	if c.healthWriter != nil {
		if err := c.healthWriter(result, node); err != nil {
			return err
		}
	}
	c.nodes[result.NodeID] = node
	return nil
}

func (c *Controller) SetMaintenance(nodeID string, maintenance bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	node, ok := c.nodes[nodeID]
	if !ok {
		return ErrUnknownNode
	}
	node.Maintenance = maintenance
	c.nodes[nodeID] = node
	return nil
}

func (c *Controller) Evaluate() (Decision, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	now := c.now()
	state := c.state
	nodes := make([]Node, 0, len(c.nodes))
	for _, node := range c.nodes {
		nodes = append(nodes, node)
	}
	primary, _ := findRoles(nodes)
	observedCycle := knownCycle(primary.Traffic)
	policyState := state
	// An empty cycle on a recovered fallback is unknown, never a new cycle.
	if policyState.ActiveNodeID != primary.ID && policyState.CurrentCycleKey == "" && observedCycle != "" {
		policyState.CurrentCycleKey = observedCycle
	}
	decision := Evaluate(nodes, policyState, c.config, now)
	if decision.Change && c.switchLimitReachedLocked(now) {
		decision = Decision{NodeID: state.ActiveNodeID, Reason: "switch_rate_limited"}
	}
	return decision, nil
}

// ApplyDecision updates local state only. Production DNS orchestration is
// intentionally not wired in Phase 2A.
func (c *Controller) ApplyDecision(decision Decision, reason string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if decision.NodeID == "" {
		return ErrUnknownNode
	}
	node, ok := c.nodes[decision.NodeID]
	if !ok {
		return ErrUnknownNode
	}
	if !node.Enabled || node.Maintenance {
		return c.rejectNodeLocked(decision.NodeID)
	}
	now := c.now()
	if decision.NodeID == c.state.ActiveNodeID {
		return nil
	}
	if c.state.Mode == ModeAuto && !c.state.CooldownUntil.IsZero() && now.Before(c.state.CooldownUntil) {
		return ErrCooldown
	}
	if c.state.Mode == ModeAuto && c.switchLimitReachedLocked(now) {
		return ErrSwitchRateLimited
	}
	from := c.state.ActiveNodeID
	previous := c.state
	next := c.state
	next.ActiveNodeID = decision.NodeID
	next.DesiredNodeID = decision.NodeID
	next.LastTransitionAt = now
	next.LastEvaluationAt = now
	next.CooldownUntil = now.Add(c.config.Cooldown)
	c.advanceCycleForTransitionLocked(&next)
	event := Event{EventType: "node_switched", CreatedAt: now, FromNodeID: from, ToNodeID: decision.NodeID, Mode: next.Mode, ReasonCode: RedactReason(reason), Success: true}
	return c.commitTransitionLocked(previous, next, event)
}

func (c *Controller) persistStateLocked(state State) error {
	if c.stateWriter != nil {
		return c.stateWriter(state)
	}
	return nil
}

func (c *Controller) commitTransitionLocked(previous, next State, event Event) error {
	event = appendEventCopy(c.events, event, c.now())[len(c.events)]
	if c.transitionWriter != nil {
		if err := c.transitionWriter(next, event); err != nil {
			return err
		}
		c.state = next
		c.events = append(c.events, event)
		return nil
	}
	if c.hasTransitionPersistenceLocked() {
		return ErrAtomicPersistenceRequired
	}
	c.state = next
	c.events = append(c.events, event)
	return nil
}

func (c *Controller) hasTransitionPersistenceLocked() bool {
	return c.eventWriter != nil || c.stateWriter != nil
}

func (c *Controller) hasDNSPersistenceLocked() bool {
	return c.dnsRunWriter != nil || c.dnsPendingWriter != nil || c.eventWriter != nil || c.stateWriter != nil
}

func appendEventCopy(events []Event, event Event, now time.Time) []Event {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = now
	}
	event.ID = int64(len(events) + 1)
	return append(events, event)
}

func (c *Controller) rejectNodeLocked(nodeID string) error {
	event := Event{EventType: "switch_rejected", ToNodeID: nodeID, Mode: c.state.Mode, ReasonCode: "target_not_eligible", Success: false}
	if err := c.appendEventLocked(event); err != nil {
		return err
	}
	return ErrNodeNotEligible
}

func (c *Controller) switchLimitReachedLocked(now time.Time) bool {
	if c.config.MaxSwitchesPerWindow <= 0 {
		return false
	}
	window := c.config.SwitchWindow
	if window <= 0 {
		window = DefaultPolicyConfig().SwitchWindow
	}
	count := 0
	for _, event := range c.events {
		if event.EventType == "node_switched" && !event.CreatedAt.Before(now.Add(-window)) {
			count++
		}
	}
	return count >= c.config.MaxSwitchesPerWindow
}

func (c *Controller) FailoverEligible(nodeID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	node, ok := c.nodes[nodeID]
	if !ok {
		return false
	}
	return failoverEligible(node, c.state, c.config)
}

func failoverEligible(node Node, state State, cfg PolicyConfig) bool {
	return state.Mode == ModeAuto && node.Enabled && !node.Maintenance && node.HealthStatus == HealthFailed && node.ConsecutiveFailures >= cfg.FailureThreshold
}

func (c *Controller) advanceCycleForTransitionLocked(state *State) {
	nodes := make([]Node, 0, len(c.nodes))
	for _, node := range c.nodes {
		nodes = append(nodes, node)
	}
	primary, _ := findRoles(nodes)
	if cycle := knownCycle(primary.Traffic); cycle != "" {
		state.CurrentCycleKey = cycle
	}
}

func knownCycle(sample TrafficSample) string {
	if sample.Quality != TrafficKnown || sample.CycleKey == "" {
		return ""
	}
	return sample.CycleKey
}

func (c *Controller) appendEventLocked(event Event) error {
	event = appendEventCopy(c.events, event, c.now())[len(c.events)]
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
