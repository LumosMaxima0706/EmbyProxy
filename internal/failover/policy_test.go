package failover

import (
	"testing"
	"time"
)

func testNodes() []Node {
	return []Node{
		{ID: "nosla", Name: "NOSLA", Role: RolePrimary, Enabled: true, HealthStatus: HealthHealthy, Priority: 1},
		{ID: "bwg", Name: "BWG", Role: RoleFallback, Enabled: true, HealthStatus: HealthHealthy, Priority: 2},
	}
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
