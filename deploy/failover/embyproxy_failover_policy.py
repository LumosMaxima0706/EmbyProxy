#!/usr/bin/env python3
"""Fail-closed NOSLA-primary/BWG-fallback DNS policy runner.

The runner performs only small health requests. It never probes media paths.
Provider credentials remain owned by the separately restricted DNS adapter.
"""

import argparse
import calendar
import datetime as dt
import fcntl
import json
import os
import socket
import subprocess
import sys
import tempfile
from pathlib import Path
from zoneinfo import ZoneInfo


DEFAULT_CONFIG = Path("/etc/embyproxy-failover-policy/config.json")
DEFAULT_STATE = Path("/var/lib/embyproxy-gsy-sidecar/failover-state.json")
DEFAULT_LOCK = Path("/run/embyproxy-failover-policy.lock")
GB = 1_000_000_000


def load_json(path):
    with Path(path).open(encoding="utf-8") as handle:
        value = json.load(handle)
    if not isinstance(value, dict):
        raise ValueError(f"{path} must contain a JSON object")
    return value


def atomic_json(path, value, mode=0o600):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix=path.name + ".", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(value, handle, ensure_ascii=True, indent=2, sort_keys=True)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(temporary, mode)
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def now_utc():
    return dt.datetime.now(dt.timezone.utc)


def parse_time(value):
    return dt.datetime.fromisoformat(str(value).replace("Z", "+00:00"))


def cycle_bounds(moment, reset_day, timezone):
    zone = ZoneInfo(timezone)
    local = moment.astimezone(zone)
    day = min(int(reset_day), calendar.monthrange(local.year, local.month)[1])
    start = dt.datetime(local.year, local.month, day, tzinfo=zone)
    if local < start:
        previous = (start.replace(day=1) - dt.timedelta(days=1))
        day = min(int(reset_day), calendar.monthrange(previous.year, previous.month)[1])
        start = dt.datetime(previous.year, previous.month, day, tzinfo=zone)
    next_month = (start.replace(day=28) + dt.timedelta(days=4)).replace(day=1)
    next_day = min(int(reset_day), calendar.monthrange(next_month.year, next_month.month)[1])
    end = dt.datetime(next_month.year, next_month.month, next_day, tzinfo=zone)
    return start, end


def cycle_key(moment, reset_day, timezone):
    return cycle_bounds(moment, reset_day, timezone)[0].strftime("%Y-%m-%d")


def run_command(command, timeout=30):
    return subprocess.run(command, capture_output=True, text=True, timeout=timeout,
                          check=False)


def curl_status(host, address, path, timeout):
    result = run_command([
        "curl", "--silent", "--show-error", "--output", "/dev/null",
        "--write-out", "%{http_code}", "--max-time", str(timeout),
        "--resolve", f"{host}:443:{address}", f"https://{host}{path}",
    ], timeout=timeout + 3)
    status = int(result.stdout) if result.stdout.isdigit() else 0
    return status


def health_target(config, target):
    statuses = [curl_status(config["stream_host"], config["node_ips"][target], path,
                            int(config["health_timeout_seconds"]))
                for path in config["health_paths"]]
    return {"healthy": bool(statuses) and all(code == 200 for code in statuses),
            "statuses": statuses}


def local_counter(interface):
    base = Path("/sys/class/net") / interface / "statistics"
    return int((base / "rx_bytes").read_text().strip()) + int(
        (base / "tx_bytes").read_text().strip())


def remote_counter(config):
    meter = config["nosla_meter"]
    result = run_command([
        "ssh", "-F", "/dev/null", "-i", meter["identity_file"],
        "-o", f"UserKnownHostsFile={meter['known_hosts_file']}",
        "-o", "StrictHostKeyChecking=yes", "-o", "BatchMode=yes",
        "-o", "IdentitiesOnly=yes", "-T",
        "-o", f"ConnectTimeout={int(meter['timeout_seconds'])}",
        f"{meter['user']}@{meter['host']}",
    ], timeout=int(meter["timeout_seconds"]) + 3)
    if result.returncode:
        raise RuntimeError("NOSLA restricted traffic meter failed")
    payload = json.loads(result.stdout)
    return int(payload["rx_bytes"]) + int(payload["tx_bytes"])


def collect_counter(config, target):
    if target == "nosla":
        return remote_counter(config)
    return local_counter(config["traffic"]["bwg"]["interface"])


def traffic_sample(config, state, target, moment, counter):
    traffic = config["traffic"][target]
    start, end = cycle_bounds(moment, traffic["reset_day"], config["timezone"])
    key = start.strftime("%Y-%m-%d")
    counters = state.setdefault("counter_baselines", {})
    baseline = counters.get(target)
    seed_cycle = str(traffic["seed_cycle_start"])[0:10]
    opening = float(traffic["opening_balance_gb"]) if seed_cycle == key else 0.0
    reset_baseline = not baseline or baseline.get("cycle_key") != key
    seed_age = moment - parse_time(config["usage_seed_observed_at"])
    if reset_baseline:
        baseline = {"cycle_key": key, "counter_bytes": counter,
                    "captured_at": moment.isoformat(),
                    "seed_aligned": (seed_cycle != key or seed_age <= dt.timedelta(
                        hours=int(config["usage_seed_max_age_hours"]))) }
        counters[target] = baseline
    counter_reset = counter < int(baseline["counter_bytes"])
    delta = max(0, counter - int(baseline["counter_bytes"]))
    usage_gb = opening + (delta / GB)
    quota_gb = float(traffic["quota_gb"])
    grace_end = start + dt.timedelta(hours=int(config["reset_grace_hours"]))
    quality = "fresh_estimate"
    if seed_cycle == key and not baseline.get("seed_aligned", False):
        quality = "stale"
    elif counter_reset:
        quality = "counter_reset"
    elif seed_cycle != key and moment.astimezone(start.tzinfo) < grace_end:
        quality = "reset_grace"
    return {
        "cycle_start": start.isoformat(), "cycle_end": end.isoformat(),
        "cycle_key": key, "counter_bytes": counter,
        "counter_baseline": int(baseline["counter_bytes"]),
        "opening_balance_gb": opening, "usage_gb": round(usage_gb, 6),
        "usage_bytes_decimal": int(round(usage_gb * GB)),
        "quota_gb": quota_gb, "usage_percent": round(usage_gb / quota_gb * 100, 6),
        "quality": quality, "source": "owner_provider_seed_plus_host_rx_tx_estimate",
        "sampled_at": moment.isoformat(), "traffic_direction": "rx_plus_tx",
        "new_cycle_baseline": seed_cycle != key,
    }


def dns_record(config):
    result = run_command(["python3", config["dns_adapter"], "read"], timeout=30)
    if result.returncode:
        raise RuntimeError("DNS provider read failed")
    payload = json.loads(result.stdout.splitlines()[-1])
    matches = [item for item in payload.get("project_records", [])
               if item.get("name") == "stream" and item.get("type") == "A"]
    if len(matches) != 1:
        raise RuntimeError("expected exactly one stream A record")
    address = matches[0].get("address")
    reverse = {value: key for key, value in config["node_ips"].items()}
    if address not in reverse:
        raise RuntimeError("stream A record is outside node allowlist")
    return {"target": reverse[address], "address": address,
            "ttl": int(matches[0].get("ttl") or 0)}


def apply_dns(config, target):
    result = run_command(["python3", config["dns_adapter"], "apply-stream", "--ip",
                          config["node_ips"][target]], timeout=90)
    if result.returncode:
        raise RuntimeError("DNS adapter apply failed")
    payload = json.loads(result.stdout.splitlines()[-1])
    if not payload.get("applied") and not payload.get("verified"):
        raise RuntimeError("DNS adapter did not verify apply")


def public_target_verified(config, target):
    expected = config["node_ips"][target]
    deadline = dt.datetime.now().timestamp() + int(config["dns_verify_timeout_seconds"])
    while dt.datetime.now().timestamp() < deadline:
        try:
            addresses = {row[4][0] for row in socket.getaddrinfo(
                config["stream_host"], 443, socket.AF_INET, socket.SOCK_STREAM)}
        except socket.gaierror:
            addresses = set()
        if expected in addresses:
            statuses = [curl_status(config["stream_host"], expected, path,
                                    int(config["health_timeout_seconds"]))
                        for path in config["health_paths"]]
            if statuses and all(status == 200 for status in statuses):
                return True
        __import__("time").sleep(5)
    return False


def evaluate(config, state, nosla_health, nosla_traffic, moment):
    hold = config["manual_hold"]
    active = state["active_target"]
    if hold == "nosla":
        return "nosla", "manual_hold_nosla"
    if hold == "bwg":
        return "bwg", "manual_hold_bwg"
    if hold != "none":
        raise ValueError("manual_hold must be none, nosla, or bwg")
    failures = int(state.get("nosla_consecutive_failures", 0))
    successes = int(state.get("nosla_consecutive_successes", 0))
    if failures >= int(config["health_failures_to_switch"]):
        return "bwg", "nosla_health_failure_threshold"
    if nosla_traffic.get("quality") != "fresh_estimate":
        if active == "nosla":
            return "nosla", "nosla_usage_not_fresh_hold_active"
        return "bwg", "nosla_usage_not_fresh_blocks_return"
    usage = float(nosla_traffic["usage_percent"])
    if usage >= float(config["nosla_switch_threshold_percent"]):
        return "bwg", "nosla_traffic_threshold"
    if active == "nosla":
        return "nosla", "nosla_healthy_below_threshold"
    if not nosla_health["healthy"] or successes < int(config["health_successes_to_return"]):
        return "bwg", "nosla_recovery_debounce"
    if nosla_traffic.get("new_cycle_baseline"):
        if usage < float(config["nosla_reset_return_threshold_percent"]):
            return "nosla", "nosla_healthy_new_cycle"
        return "bwg", "nosla_new_cycle_return_threshold"
    if usage < float(config["nosla_return_threshold_percent"]):
        return "nosla", "nosla_recovered_below_return_threshold"
    return "bwg", "nosla_return_hysteresis"


def update_health_state(state, healthy, moment, minimum_interval_seconds):
    previous = state.get("last_health_sample_at")
    if previous and moment < parse_time(previous) + dt.timedelta(
            seconds=int(minimum_interval_seconds)):
        state["last_healthcheck"] = {"healthy": healthy, "at": moment.isoformat(),
                                     "counted": False}
        return False
    if healthy:
        state["nosla_consecutive_successes"] = int(
            state.get("nosla_consecutive_successes", 0)) + 1
        state["nosla_consecutive_failures"] = 0
    else:
        state["nosla_consecutive_failures"] = int(
            state.get("nosla_consecutive_failures", 0)) + 1
        state["nosla_consecutive_successes"] = 0
    state["last_health_sample_at"] = moment.isoformat()
    state["last_healthcheck"] = {"healthy": healthy, "at": moment.isoformat(),
                                 "counted": True}
    return True


def cooldown_active(config, state, moment):
    value = state.get("last_switch_at")
    if not value:
        return False
    return moment < parse_time(value) + dt.timedelta(seconds=int(config["cooldown_seconds"]))


def switch_with_rollback(config, state, target, health_fn=health_target,
                         dns_apply_fn=apply_dns, dns_read_fn=dns_record,
                         public_verify_fn=public_target_verified):
    previous = dns_read_fn(config)
    backup_dir = Path(config["switch_backup_dir"])
    stamp = now_utc().strftime("%Y%m%dT%H%M%SZ")
    backup = backup_dir / f"dns-before-{stamp}.json"
    atomic_json(backup, previous)
    dns_apply_fn(config, target)
    after = dns_read_fn(config)
    verified = (after["target"] == target
                and health_fn(config, target)["healthy"]
                and public_verify_fn(config, target))
    if verified:
        state.setdefault("switch_history", []).append({
            "at": now_utc().isoformat(), "previous_target": previous["target"],
            "target": target, "result": "verified", "backup": str(backup)})
        state["switch_history"] = state["switch_history"][-50:]
        return str(backup), False
    try:
        dns_apply_fn(config, previous["target"])
        restored = dns_read_fn(config)
        if (restored["target"] != previous["target"]
                or not public_verify_fn(config, previous["target"])):
            raise RuntimeError("rollback DNS verification failed")
    except Exception as exc:
        raise RuntimeError("DNS rollback failed; owner intervention required") from exc
    state.setdefault("switch_history", []).append({
        "at": now_utc().isoformat(), "previous_target": previous["target"],
        "target": target, "result": "rolled_back", "backup": str(backup)})
    state["switch_history"] = state["switch_history"][-50:]
    state["last_rollback"] = {"at": now_utc().isoformat(),
                              "restored_target": previous["target"],
                              "result": "verified"}
    raise RuntimeError("post-switch verification failed; DNS rollback succeeded")


def initial_state(config, active, moment):
    return {
        "active_target": active, "previous_target": None,
        "decision_reason": "initial_discovery", "mode": config["mode"],
        "manual_hold": config["manual_hold"], "preferred_primary": "nosla",
        "fallback": "bwg", "last_switch_at": None, "last_healthcheck": None,
        "nosla_consecutive_failures": 0, "nosla_consecutive_successes": 0,
        "counter_baselines": {}, "updated_at": moment.isoformat(),
    }


def run_policy(config_path, output):
    config = load_json(config_path)
    required_mode = os.getenv("FAILOVER_MODE", config.get("mode", "dry-run"))
    config["mode"] = required_mode
    config["manual_hold"] = os.getenv(
        "MANUAL_HOLD", config.get("manual_hold", "none")).strip().lower()
    if required_mode not in ("dry-run", "auto"):
        raise ValueError("mode must be dry-run or auto")
    state_path = Path(config.get("state_file", DEFAULT_STATE))
    moment = now_utc()
    current = dns_record(config)
    state = load_json(state_path) if state_path.exists() else initial_state(
        config, current["target"], moment)
    state["active_target"] = current["target"]
    nosla_health = health_target(config, "nosla")
    bwg_health = health_target(config, "bwg")
    update_health_state(state, nosla_health["healthy"], moment,
                        config["health_min_sample_interval_seconds"])
    samples = {}
    for target in ("nosla", "bwg"):
        try:
            samples[target] = traffic_sample(config, state, target, moment,
                                             collect_counter(config, target))
        except Exception:
            samples[target] = {"quality": "unknown", "usage_gb": None,
                               "usage_percent": None,
                               "source": "counter_collection_failed",
                               "sampled_at": moment.isoformat()}
    desired, reason = evaluate(config, state, nosla_health, samples["nosla"], moment)
    change = desired != state["active_target"]
    blocked = None
    applied = False
    backup = None
    if change and cooldown_active(config, state, moment):
        blocked = "cooldown_active"
    elif change and desired == "bwg" and not bwg_health["healthy"]:
        blocked = "bwg_healthcheck_failed"
    elif change and config["mode"] == "auto":
        try:
            backup, _ = switch_with_rollback(config, state, desired)
            state["previous_target"] = state["active_target"]
            state["active_target"] = desired
            state["last_switch_at"] = moment.isoformat()
            applied = True
        except Exception:
            state["decision_reason"] = reason
            state["updated_at"] = moment.isoformat()
            atomic_json(state_path, state)
            raise
    state.update({
        "decision_reason": reason, "mode": config["mode"],
        "manual_hold": config["manual_hold"], "preferred_primary": "nosla",
        "fallback": "bwg", "nosla_quota_gb": config["traffic"]["nosla"]["quota_gb"],
        "bwg_quota_gb": config["traffic"]["bwg"]["quota_gb"],
        "traffic_direction": "rx_plus_tx",
        "nosla_threshold_percent": config["nosla_switch_threshold_percent"],
        "nosla_reset_day": config["traffic"]["nosla"]["reset_day"],
        "bwg_reset_day": config["traffic"]["bwg"]["reset_day"],
        "nosla_opening_balance_gb": config["traffic"]["nosla"]["opening_balance_gb"],
        "bwg_opening_balance_gb": config["traffic"]["bwg"]["opening_balance_gb"],
        "usage_seed_observed_at": config["usage_seed_observed_at"],
        "usage_seed_source": config["usage_seed_source"],
        "nosla_usage_gb": samples["nosla"].get("usage_gb"),
        "bwg_usage_gb": samples["bwg"].get("usage_gb"),
        "nosla_usage_bytes": samples["nosla"].get("usage_bytes_decimal"),
        "bwg_usage_bytes": samples["bwg"].get("usage_bytes_decimal"),
        "nosla_usage_percent": samples["nosla"].get("usage_percent"),
        "bwg_usage_percent": samples["bwg"].get("usage_percent"),
        "nosla_cycle_start": samples["nosla"].get("cycle_start"),
        "nosla_cycle_end": samples["nosla"].get("cycle_end"),
        "bwg_cycle_start": samples["bwg"].get("cycle_start"),
        "bwg_cycle_end": samples["bwg"].get("cycle_end"),
        "nosla_counter_baseline": state.get("counter_baselines", {}).get("nosla"),
        "bwg_counter_baseline": state.get("counter_baselines", {}).get("bwg"),
        "usage_state": samples["nosla"].get("quality"),
        "traffic_samples": samples, "updated_at": moment.isoformat(),
    })
    atomic_json(state_path, state)
    result = {
        "ok": True, "mode": config["mode"], "active_target": state["active_target"],
        "desired_target": desired, "decision_reason": reason, "change": change,
        "mutation_applied": applied, "blocked": blocked,
        "nosla_healthy": nosla_health["healthy"],
        "bwg_healthy": bwg_health["healthy"],
        "nosla_usage_percent": samples["nosla"].get("usage_percent"),
        "bwg_usage_percent": samples["bwg"].get("usage_percent"),
        "usage_state": samples["nosla"].get("quality"),
        "switch_backup": backup,
    }
    json.dump(result, output, ensure_ascii=True, sort_keys=True)
    output.write("\n")


def main():
    parser = argparse.ArgumentParser(description="EmbyProxy DNS failover policy")
    parser.add_argument("--config", default=os.getenv("FAILOVER_CONFIG", str(DEFAULT_CONFIG)))
    parser.add_argument("--lock", default=os.getenv("FAILOVER_LOCK_FILE", str(DEFAULT_LOCK)))
    args = parser.parse_args()
    lock_path = Path(args.lock)
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    with lock_path.open("a+", encoding="utf-8") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError("policy runner already active") from None
        run_policy(args.config, sys.stdout)


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(json.dumps({"ok": False, "error": type(exc).__name__}, sort_keys=True))
        sys.exit(1)
