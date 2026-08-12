package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDryRunScenarioMatrix(t *testing.T) {
	tests := []struct {
		name, hold, input, target, reason string
	}{
		{"healthy below threshold", "none", `{"active_target":"nosla","nosla_healthy":true,"nosla_traffic_quality":"known","nosla_usage_bytes":840,"nosla_quota_bytes":1000,"bwg_healthy":true}`, "nosla", "primary_preferred"},
		{"usage threshold", "none", `{"active_target":"nosla","nosla_healthy":true,"nosla_traffic_quality":"known","nosla_usage_bytes":850,"nosla_quota_bytes":1000,"bwg_healthy":true}`, "bwg", "primary_policy_threshold"},
		{"health failure", "none", `{"active_target":"nosla","nosla_healthy":false,"nosla_consecutive_failures":3,"nosla_traffic_quality":"unknown","bwg_healthy":true}`, "bwg", "primary_health_failures"},
		{"reset return", "none", `{"active_target":"bwg","current_cycle_key":"2026-07-21","now":"2026-08-21T06:01:00+08:00","nosla_healthy":true,"nosla_consecutive_successes":3,"nosla_cycle_key":"2026-08-21","nosla_traffic_quality":"known","nosla_usage_bytes":10,"nosla_quota_bytes":1000,"bwg_healthy":true}`, "nosla", "primary_recovered_new_cycle"},
		{"hold nosla", "nosla", `{"active_target":"bwg","nosla_healthy":false,"nosla_consecutive_failures":3,"nosla_traffic_quality":"unknown","bwg_healthy":true}`, "nosla", "manual_hold_nosla"},
		{"hold bwg", "bwg", `{"active_target":"nosla","nosla_healthy":true,"nosla_traffic_quality":"known","nosla_usage_bytes":1,"nosla_quota_bytes":1000,"bwg_healthy":true}`, "bwg", "manual_hold_bwg"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FAILOVER_MODE", "dry-run")
			t.Setenv("MANUAL_HOLD", tc.hold)
			t.Setenv("FAILOVER_LOCK_FILE", filepath.Join(t.TempDir(), "runner.lock"))
			var output bytes.Buffer
			if err := run(bytes.NewBufferString(tc.input), &output); err != nil {
				t.Fatal(err)
			}
			var got result
			if err := json.Unmarshal(output.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.DesiredTarget != tc.target || got.Reason != tc.reason || got.MutationApplied {
				t.Fatalf("result = %+v", got)
			}
		})
	}
}

func TestAutoFailsClosedWithoutApplyAdapter(t *testing.T) {
	t.Setenv("FAILOVER_MODE", "auto")
	t.Setenv("FAILOVER_LOCK_FILE", filepath.Join(t.TempDir(), "runner.lock"))
	if err := run(bytes.NewBufferString(`{}`), &bytes.Buffer{}); err == nil {
		t.Fatal("expected auto mode refusal")
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
