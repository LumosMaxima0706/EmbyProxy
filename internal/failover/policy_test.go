package failover

import (
	"errors"
	"testing"
	"time"
)

func testNodes() []Node {
	return []Node{
		{ID: "nosla", Name: "NOSLA", Role: RolePrimary, Enabled: true, HealthStatus: HealthHealthy, Priority: 1},
		{ID: "bwg", Name: "BWG", Role: RoleFallback, Enabled: true, HealthStatus: HealthHealthy, Priority: 2},
	}
}

func evaluateController(t *testing.T, controller *Controller) Decision {
	t.Helper()
	decision, err := controller.Evaluate()
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func TestHealthyPrimarySelectsNOSLA(t *testing.T) {
	decision := Evaluate(testNodes(), State{Mode: ModeAuto, ActiveNodeID: "nosla"}, DefaultPolicyConfig(), time.Unix(100, 0))
	if decision.NodeID != "nosla" || decision.Change {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestThreeFailuresSelectBWG(t *testing.T) {
	nodes := testNodes()
	nodes[0].HealthStatus = HealthFailed
	nodes[0].ConsecutiveFailures = 3
	decision := Evaluate(nodes, State{Mode: ModeAuto, ActiveNodeID: "nosla"}, DefaultPolicyConfig(), time.Unix(100, 0))
	if decision.NodeID != "bwg" || !decision.Change {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestTrafficThresholdSelectsBWG(t *testing.T) {
	nodes := testNodes()
	nodes[0].Traffic = TrafficSample{NodeID: "nosla", Quality: TrafficKnown, TotalBytes: 970, QuotaBytes: 1000}
	decision := Evaluate(nodes, State{Mode: ModeAuto, ActiveNodeID: "nosla"}, DefaultPolicyConfig(), time.Unix(100, 0))
	if decision.NodeID != "bwg" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestRecoveryRequiresNewKnownCycle(t *testing.T) {
	nodes := testNodes()
	nodes[0].ConsecutiveSuccesses = 3
	nodes[0].ResetDay = 21
	nodes[0].ResetTimezone = "UTC"
	nodes[0].Traffic = TrafficSample{NodeID: "nosla", CycleKey: "2026-08-21", Quality: TrafficKnown}
	state := State{Mode: ModeAuto, ActiveNodeID: "bwg", CurrentCycleKey: "2026-07-21"}
	decision := Evaluate(nodes, state, DefaultPolicyConfig(), time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC))
	if decision.NodeID != "nosla" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestSameCycleQuotaExceededCannotRecover(t *testing.T) {
	nodes := testNodes()
	nodes[0].ConsecutiveSuccesses = 3
	nodes[0].ResetDay = 21
	nodes[0].Traffic = TrafficSample{NodeID: "nosla", CycleKey: "2026-08-21", Quality: TrafficKnown, TotalBytes: 990, QuotaBytes: 1000}
	state := State{Mode: ModeAuto, ActiveNodeID: "bwg", CurrentCycleKey: "2026-08-21"}
	decision := Evaluate(nodes, state, DefaultPolicyConfig(), time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC))
	if decision.NodeID != "bwg" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestMissingCycleCannotRecover(t *testing.T) {
	nodes := testNodes()
	nodes[0].ConsecutiveSuccesses = 3
	nodes[0].Traffic = UnknownTraffic("nosla")
	state := State{Mode: ModeAuto, ActiveNodeID: "bwg", CurrentCycleKey: ""}
	decision := Evaluate(nodes, state, DefaultPolicyConfig(), time.Now())
	if decision.NodeID != "bwg" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestControllerTracksCycleAndRequiresResetBeforeRecovery(t *testing.T) {
	nodes := testNodes()
	nodes[0].ResetDay = 21
	nodes[0].ResetTimezone = "UTC"
	c := NewController(nodes, DefaultPolicyConfig(), nil)
	now := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	c.SetNow(func() time.Time { return now })
	if err := c.SetTraffic(TrafficSample{NodeID: "nosla", CycleKey: "2026-08-21", Quality: TrafficKnown, TotalBytes: 990, QuotaBytes: 1000}); err != nil {
		t.Fatal(err)
	}
	evaluateController(t, c)
	state, _ := c.Status()
	if state.CurrentCycleKey != "2026-08-21" {
		t.Fatalf("baseline state = %+v", state)
	}
	if err := c.ApplyDecision(Decision{NodeID: "bwg", Change: true}, "quota"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := c.SetHealth(HealthResultAt("nosla", "mock", true, 200, 0, "")); err != nil {
			t.Fatal(err)
		}
	}
	if decision := evaluateController(t, c); decision.NodeID != "bwg" {
		t.Fatalf("same-cycle decision = %+v", decision)
	}
	if err := c.SetTraffic(TrafficSample{NodeID: "nosla", CycleKey: "2026-09-21", Quality: TrafficKnown, TotalBytes: 1, QuotaBytes: 1000}); err != nil {
		t.Fatal(err)
	}
	now = time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	if decision := evaluateController(t, c); decision.NodeID != "nosla" {
		t.Fatalf("new-cycle decision = %+v", decision)
	}
	state, _ = c.Status()
	if state.CurrentCycleKey != "2026-09-21" {
		t.Fatalf("updated state = %+v", state)
	}
}

func TestRestartPreservesQuotaCycleAndFallbackDecision(t *testing.T) {
	nodes := testNodes()
	nodes[0].ResetDay = 21
	nodes[0].ResetTimezone = "UTC"
	c := NewController(nodes, DefaultPolicyConfig(), nil)
	now := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	c.SetNow(func() time.Time { return now })
	if err := c.SetTraffic(TrafficSample{NodeID: "nosla", CycleKey: "2026-08-21", Quality: TrafficKnown, TotalBytes: 990, QuotaBytes: 1000}); err != nil {
		t.Fatal(err)
	}
	evaluateController(t, c)
	if decision := evaluateController(t, c); decision.NodeID != "bwg" {
		t.Fatalf("initial decision = %+v", decision)
	}
	if err := c.ApplyDecision(Decision{NodeID: "bwg", Change: true}, "quota"); err != nil {
		t.Fatal(err)
	}
	savedState, savedNodes := c.Status()
	restarted := NewController(savedNodes, DefaultPolicyConfig(), nil)
	restarted.RestoreState(savedState)
	for _, node := range savedNodes {
		restarted.RestoreNodeRuntime(node.ID, node.ConsecutiveFailures, node.ConsecutiveSuccesses, node.Traffic)
	}
	restarted.SetNow(func() time.Time { return now })
	if decision := evaluateController(t, restarted); decision.NodeID != "bwg" {
		t.Fatalf("restarted decision = %+v", decision)
	}
}

func TestCooldownPreventsSwitch(t *testing.T) {
	nodes := testNodes()
	nodes[0].ConsecutiveFailures = 3
	state := State{Mode: ModeAuto, ActiveNodeID: "nosla", CooldownUntil: time.Unix(200, 0)}
	decision := Evaluate(nodes, state, DefaultPolicyConfig(), time.Unix(100, 0))
	if decision.NodeID != "nosla" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestManualModesAndMaintenance(t *testing.T) {
	for _, mode := range []Mode{ModeForceBWG, ModeMaintenanceNOSLA} {
		decision := Evaluate(testNodes(), State{Mode: mode, ActiveNodeID: "nosla"}, DefaultPolicyConfig(), time.Now())
		if decision.NodeID != "bwg" {
			t.Fatalf("mode %s decision = %+v", mode, decision)
		}
	}
	decision := Evaluate(testNodes(), State{Mode: ModeForceNOSLA, ActiveNodeID: "bwg"}, DefaultPolicyConfig(), time.Now())
	if decision.NodeID != "nosla" {
		t.Fatalf("force_nosla decision = %+v", decision)
	}
}

func TestUnknownTrafficDoesNotTriggerRecovery(t *testing.T) {
	nodes := testNodes()
	nodes[0].ConsecutiveSuccesses = 3
	nodes[0].Traffic = UnknownTraffic("nosla")
	state := State{Mode: ModeAuto, ActiveNodeID: "bwg", CurrentCycleKey: "old"}
	decision := Evaluate(nodes, state, DefaultPolicyConfig(), time.Now())
	if decision.NodeID != "bwg" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestExplicitPolicyCanAllowUnknownRecovery(t *testing.T) {
	nodes := testNodes()
	nodes[0].ConsecutiveSuccesses = 3
	nodes[0].Traffic = TrafficSample{NodeID: "nosla", CycleKey: "new", Quality: TrafficUnknown}
	state := State{Mode: ModeAuto, ActiveNodeID: "bwg", CurrentCycleKey: "old"}
	cfg := DefaultPolicyConfig()
	cfg.AllowUnknownRecovery = true
	decision := Evaluate(nodes, state, cfg, time.Now())
	if decision.NodeID != "nosla" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestControllerRecordsSwitchEvent(t *testing.T) {
	c := NewController(testNodes(), DefaultPolicyConfig(), NewMockDNSProvider())
	now := time.Unix(100, 0)
	c.SetNow(func() time.Time { return now })
	if err := c.ApplyDecision(Decision{NodeID: "bwg", Change: true}, "health failure"); err != nil {
		t.Fatal(err)
	}
	events := c.Events(10)
	if len(events) != 1 || events[0].EventType != "node_switched" || events[0].ToNodeID != "bwg" {
		t.Fatalf("events = %+v", events)
	}
}

func TestPolicyConfigPreservesExplicitCooldown(t *testing.T) {
	cfg := PolicyConfig{FailureThreshold: 3, Cooldown: time.Second}
	c := NewController(testNodes(), cfg, nil)
	now := time.Unix(100, 0)
	c.SetNow(func() time.Time { return now })
	if err := c.ApplyDecision(Decision{NodeID: "bwg", Change: true}, "health_failure"); err != nil {
		t.Fatal(err)
	}
	state, _ := c.Status()
	if !state.CooldownUntil.Equal(now.Add(time.Second)) {
		t.Fatalf("cooldown = %v", state.CooldownUntil)
	}
}

func TestSchedulerEmitsDecisionWithoutApplyingIt(t *testing.T) {
	c := NewController(testNodes(), DefaultPolicyConfig(), nil)
	called := make(chan Decision, 1)
	s := Scheduler{Controller: c, OnDecision: func(decision Decision) error { called <- decision; return nil }}
	decision, err := s.RunOnce()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case emitted := <-called:
		if emitted.NodeID != decision.NodeID {
			t.Fatalf("emitted = %+v decision = %+v", emitted, decision)
		}
	default:
		t.Fatal("scheduler did not emit decision")
	}
	state, _ := c.Status()
	if state.ActiveNodeID != "nosla" {
		t.Fatalf("scheduler changed active state: %+v", state)
	}
}

func TestSchedulerMockHealthTriggersFallbackAfterThreeFailures(t *testing.T) {
	c := NewController(testNodes(), DefaultPolicyConfig(), nil)
	probe := NewMockHealthProbe()
	probe.Set(HealthResultAt("nosla", "mock", false, 503, 0, "unavailable"))
	probe.Set(HealthResultAt("bwg", "mock", true, 200, 0, ""))
	s := Scheduler{Controller: c, Probe: probe}
	for i := 0; i < 2; i++ {
		if decision, err := s.RunOnce(); err != nil || decision.NodeID != "nosla" {
			t.Fatalf("decision %d = %+v err=%v", i, decision, err)
		}
	}
	decision, err := s.RunOnce()
	if err != nil {
		t.Fatal(err)
	}
	if decision.NodeID != "bwg" || !decision.Change {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestSchedulerMockTrafficTriggersFallbackAndUnknownDoesNot(t *testing.T) {
	c := NewController(testNodes(), DefaultPolicyConfig(), nil)
	source := NewMockTrafficSource()
	s := Scheduler{Controller: c, Traffic: source}
	if decision, err := s.RunOnce(); err != nil || decision.NodeID != "nosla" {
		t.Fatalf("unknown traffic decision = %+v err=%v", decision, err)
	}
	source.Set(TrafficSample{NodeID: "nosla", Quality: TrafficKnown, InboundBytes: 970, QuotaBytes: 1000})
	decision, err := s.RunOnce()
	if err != nil {
		t.Fatal(err)
	}
	if decision.NodeID != "bwg" || !decision.Change {
		t.Fatalf("threshold decision = %+v", decision)
	}
}

func TestControllerPersistsHealthAndTrafficBeforePublishing(t *testing.T) {
	c := NewController(testNodes(), DefaultPolicyConfig(), nil)
	trafficWrites, healthWrites := 0, 0
	c.SetTrafficWriter(func(sample TrafficSample) error {
		trafficWrites++
		if sample.SampledAt.IsZero() || sample.TotalBytes != 30 {
			t.Fatalf("sample = %+v", sample)
		}
		return nil
	})
	c.SetHealthWriter(func(result HealthResult, node Node) error {
		healthWrites++
		if !result.Success || node.ConsecutiveSuccesses != 1 {
			t.Fatalf("result=%+v node=%+v", result, node)
		}
		return nil
	})
	if err := c.SetTraffic(TrafficSample{NodeID: "nosla", InboundBytes: 10, OutboundBytes: 20, Quality: TrafficKnown}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetHealth(HealthResultAt("nosla", "mock", true, 200, 0, "")); err != nil {
		t.Fatal(err)
	}
	if trafficWrites != 1 || healthWrites != 1 {
		t.Fatalf("trafficWrites=%d healthWrites=%d", trafficWrites, healthWrites)
	}
}

func TestApplyDecisionRejectsDisabledAndMaintenanceTargets(t *testing.T) {
	for _, mutate := range []func(*Node){func(node *Node) { node.Enabled = false }, func(node *Node) { node.Maintenance = true }} {
		nodes := testNodes()
		mutate(&nodes[1])
		c := NewController(nodes, DefaultPolicyConfig(), nil)
		before, _ := c.Status()
		if err := c.ApplyDecision(Decision{NodeID: "bwg", Change: true}, "manual"); err != ErrNodeNotEligible {
			t.Fatalf("err = %v", err)
		}
		after, _ := c.Status()
		if after.ActiveNodeID != before.ActiveNodeID {
			t.Fatalf("state changed: before=%+v after=%+v", before, after)
		}
	}
}

func TestTransitionWriterFailureDoesNotPublishState(t *testing.T) {
	c := NewController(testNodes(), DefaultPolicyConfig(), nil)
	c.SetTransitionWriter(func(State, Event) error { return errors.New("write failed") })
	before, _ := c.Status()
	if err := c.ApplyDecision(Decision{NodeID: "bwg", Change: true}, "manual"); err == nil {
		t.Fatal("expected writer failure")
	}
	after, _ := c.Status()
	if after.ActiveNodeID != before.ActiveNodeID {
		t.Fatalf("state changed: before=%+v after=%+v", before, after)
	}
}

func TestEventWriterFailureDoesNotPublishState(t *testing.T) {
	c := NewController(testNodes(), DefaultPolicyConfig(), nil)
	eventWrites, stateWrites := 0, 0
	c.SetEventWriter(func(Event) error { eventWrites++; return errors.New("event write failed") })
	c.SetStateWriter(func(State) error { stateWrites++; return nil })
	before, _ := c.Status()
	if err := c.ApplyDecision(Decision{NodeID: "bwg", Change: true}, "manual"); err != ErrAtomicPersistenceRequired {
		t.Fatalf("err = %v", err)
	}
	after, _ := c.Status()
	if after.ActiveNodeID != before.ActiveNodeID {
		t.Fatalf("state changed: before=%+v after=%+v", before, after)
	}
	if eventWrites != 0 || stateWrites != 0 {
		t.Fatalf("non-atomic writers called: event=%d state=%d", eventWrites, stateWrites)
	}
}

func TestEvaluateReturnsStateWriterFailure(t *testing.T) {
	c := NewController(testNodes(), DefaultPolicyConfig(), nil)
	c.SetStateWriter(func(State) error { return errors.New("state write failed") })
	before, _ := c.Status()
	decision, err := c.Evaluate()
	if err == nil || decision.Reason != "state_persist_failed" {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	after, _ := c.Status()
	if !after.LastEvaluationAt.Equal(before.LastEvaluationAt) || after.ActiveNodeID != before.ActiveNodeID {
		t.Fatalf("state changed: before=%+v after=%+v", before, after)
	}
}

func TestSchedulerReturnsHealthPersistenceFailureBeforeEvaluation(t *testing.T) {
	c := NewController(testNodes(), DefaultPolicyConfig(), nil)
	c.SetHealthWriter(func(HealthResult, Node) error { return errors.New("health write failed") })
	probe := NewMockHealthProbe()
	probe.Set(HealthResultAt("nosla", "mock", false, 503, 0, "unavailable"))
	recorded := 0
	s := Scheduler{Controller: c, Probe: probe, OnError: func(error) { recorded++ }}
	decision, err := s.RunOnce()
	if err == nil || decision.Reason != "health_update_failed" || recorded != 1 {
		t.Fatalf("decision=%+v err=%v recorded=%d", decision, err, recorded)
	}
	state, _ := c.Status()
	if !state.LastEvaluationAt.IsZero() {
		t.Fatalf("evaluation continued: %+v", state)
	}
}

func TestSchedulerReturnsTrafficPersistenceFailureBeforeEvaluation(t *testing.T) {
	c := NewController(testNodes(), DefaultPolicyConfig(), nil)
	c.SetTrafficWriter(func(TrafficSample) error { return errors.New("traffic write failed") })
	source := NewMockTrafficSource()
	source.Set(TrafficSample{NodeID: "nosla", Quality: TrafficKnown})
	s := Scheduler{Controller: c, Traffic: source}
	decision, err := s.RunOnce()
	if err == nil || decision.Reason != "traffic_update_failed" {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	state, _ := c.Status()
	if !state.LastEvaluationAt.IsZero() {
		t.Fatalf("evaluation continued: %+v", state)
	}
}

func TestSchedulerReturnsCallbackFailureAndNeverCallsDNS(t *testing.T) {
	provider := NewMockDNSProvider()
	c := NewController(testNodes(), DefaultPolicyConfig(), provider)
	s := Scheduler{Controller: c, OnDecision: func(Decision) error { return errors.New("callback failed") }}
	decision, err := s.RunOnce()
	if err == nil || decision.NodeID != "nosla" {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	if provider.ApplyCount != 0 {
		t.Fatalf("DNS provider called %d times", provider.ApplyCount)
	}
}

func TestMaxSwitchesPerWindowIsEnforced(t *testing.T) {
	cfg := DefaultPolicyConfig()
	cfg.MaxSwitchesPerWindow = 1
	cfg.SwitchWindow = time.Hour
	c := NewController(testNodes(), cfg, nil)
	now := time.Unix(100, 0)
	c.SetNow(func() time.Time { return now })
	if err := c.ApplyDecision(Decision{NodeID: "bwg", Change: true}, "first"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	if err := c.ApplyDecision(Decision{NodeID: "nosla", Change: true}, "second"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Minute)
	if err := c.ApplyDecision(Decision{NodeID: "bwg", Change: true}, "third"); err != ErrSwitchRateLimited {
		t.Fatalf("err = %v", err)
	}
}
