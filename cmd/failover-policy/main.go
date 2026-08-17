package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"embyproxy/internal/failover"
)

type snapshot struct {
	ActiveTarget              string `json:"active_target"`
	CurrentCycleKey           string `json:"current_cycle_key"`
	Now                       string `json:"now,omitempty"`
	NOSLAHealthy              bool   `json:"nosla_healthy"`
	NOSLAConsecutiveFailures  int    `json:"nosla_consecutive_failures"`
	NOSLAConsecutiveSuccesses int    `json:"nosla_consecutive_successes"`
	NOSLACycleKey             string `json:"nosla_cycle_key"`
	NOSLAUsageBytes           int64  `json:"nosla_usage_bytes"`
	NOSLAQuotaBytes           int64  `json:"nosla_quota_bytes"`
	NOSLATrafficQuality       string `json:"nosla_traffic_quality"`
	BWGHealthy                bool   `json:"bwg_healthy"`
}

type result struct {
	Mode             string  `json:"mode"`
	ManualHold       string  `json:"manual_hold"`
	ActiveTarget     string  `json:"active_target"`
	DesiredTarget    string  `json:"desired_target"`
	Change           bool    `json:"change"`
	Reason           string  `json:"reason"`
	MutationApplied  bool    `json:"mutation_applied"`
	NOSLAResetDay    int     `json:"nosla_reset_day"`
	BWGResetDay      int     `json:"bwg_reset_day"`
	SwitchThreshold  float64 `json:"nosla_switch_threshold_percent"`
	ReturnThreshold  float64 `json:"nosla_return_threshold_percent"`
	ReturnAfterReset bool    `json:"nosla_return_after_reset"`
	ResetGraceHours  int     `json:"reset_grace_hours"`
	Timezone         string  `json:"timezone"`
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(input io.Reader, output io.Writer) error {
	lock, err := acquireLock(envString("FAILOVER_LOCK_FILE", "/tmp/embyproxy-failover-policy.lock"))
	if err != nil {
		return err
	}
	defer releaseLock(lock)

	mode := strings.ToLower(envString("FAILOVER_MODE", "dry-run"))
	if mode != "dry-run" && mode != "auto" {
		return errors.New("invalid FAILOVER_MODE")
	}
	if mode == "auto" {
		return errors.New("auto apply adapter is not configured; refusing mutation")
	}
	hold := strings.ToLower(envString("MANUAL_HOLD", "none"))
	stateMode, err := manualHoldMode(hold)
	if err != nil {
		return err
	}

	var in snapshot
	decoder := json.NewDecoder(io.LimitReader(input, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		return fmt.Errorf("invalid state snapshot: %w", err)
	}
	now := time.Now()
	if in.Now != "" {
		now, err = time.Parse(time.RFC3339, in.Now)
		if err != nil {
			return errors.New("invalid snapshot time")
		}
	}

	noslaReset, err := envInt("NOSLA_RESET_DAY", 21, 1, 31)
	if err != nil {
		return err
	}
	bwgReset, err := envInt("BWG_RESET_DAY", 7, 1, 31)
	if err != nil {
		return err
	}
	graceHours, err := envInt("FAILOVER_RESET_GRACE_HOURS", 6, 1, 72)
	if err != nil {
		return err
	}
	switchThreshold, err := envFloat("NOSLA_TRAFFIC_SWITCH_THRESHOLD_PERCENT", 85)
	if err != nil {
		return err
	}
	returnThreshold, err := envFloat("NOSLA_TRAFFIC_RETURN_THRESHOLD_PERCENT", 15)
	if err != nil {
		return err
	}
	returnAfterReset, err := envBool("NOSLA_TRAFFIC_RETURN_AFTER_RESET", true)
	if err != nil {
		return err
	}
	quality, err := trafficQuality(in.NOSLATrafficQuality)
	if err != nil {
		return err
	}

	noslaHealth := failover.HealthFailed
	if in.NOSLAHealthy {
		noslaHealth = failover.HealthHealthy
	}
	bwgHealth := failover.HealthFailed
	if in.BWGHealthy {
		bwgHealth = failover.HealthHealthy
	}
	nodes := []failover.Node{
		{ID: "nosla", Role: failover.RolePrimary, Enabled: true, Priority: 1, ResetDay: noslaReset, ResetTimezone: "Asia/Shanghai", HealthStatus: noslaHealth, ConsecutiveFailures: in.NOSLAConsecutiveFailures, ConsecutiveSuccesses: in.NOSLAConsecutiveSuccesses, Traffic: failover.TrafficSample{NodeID: "nosla", CycleKey: in.NOSLACycleKey, TotalBytes: in.NOSLAUsageBytes, QuotaBytes: in.NOSLAQuotaBytes, ThresholdPct: switchThreshold, Quality: quality}},
		{ID: "bwg", Role: failover.RoleFallback, Enabled: true, Priority: 2, ResetDay: bwgReset, ResetTimezone: "Asia/Shanghai", HealthStatus: bwgHealth},
	}
	cfg := failover.DefaultPolicyConfig()
	cfg.TrafficThresholdPct = switchThreshold
	cfg.ReturnThresholdPct = returnThreshold
	cfg.ResetGrace = time.Duration(graceHours) * time.Hour
	cfg.DisableReturnAfterReset = !returnAfterReset
	decision := failover.Evaluate(nodes, failover.State{ActiveNodeID: in.ActiveTarget, CurrentCycleKey: in.CurrentCycleKey, Mode: stateMode}, cfg, now)

	return json.NewEncoder(output).Encode(result{Mode: mode, ManualHold: hold, ActiveTarget: in.ActiveTarget, DesiredTarget: decision.NodeID, Change: decision.Change, Reason: decision.Reason, MutationApplied: false, NOSLAResetDay: noslaReset, BWGResetDay: bwgReset, SwitchThreshold: switchThreshold, ReturnThreshold: returnThreshold, ReturnAfterReset: returnAfterReset, ResetGraceHours: graceHours, Timezone: "Asia/Shanghai"})
}

func manualHoldMode(value string) (failover.Mode, error) {
	switch value {
	case "none":
		return failover.ModeAuto, nil
	case "nosla":
		return failover.ModeForceNOSLA, nil
	case "bwg":
		return failover.ModeForceBWG, nil
	default:
		return "", errors.New("invalid MANUAL_HOLD")
	}
}

func trafficQuality(value string) (failover.TrafficQuality, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "known":
		return failover.TrafficKnown, nil
	case "stale":
		return failover.TrafficStale, nil
	case "", "unknown":
		return failover.TrafficUnknown, nil
	default:
		return "", errors.New("invalid NOSLA traffic quality")
	}
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback, minValue, maxValue int) (int, error) {
	raw := envString(key, strconv.Itoa(fallback))
	value, err := strconv.Atoi(raw)
	if err != nil || value < minValue || value > maxValue {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return value, nil
}

func envFloat(key string, fallback float64) (float64, error) {
	raw := envString(key, strconv.FormatFloat(fallback, 'f', -1, 64))
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 || value > 100 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return value, nil
}

func envBool(key string, fallback bool) (bool, error) {
	raw := strings.ToLower(envString(key, strconv.FormatBool(fallback)))
	switch raw {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid %s", key)
	}
}

func acquireLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open policy lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("policy runner already active")
	}
	return file, nil
}

func releaseLock(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}
