import datetime as dt
import importlib.util
import pathlib
import tempfile
import unittest
from unittest import mock


MODULE_PATH = pathlib.Path(__file__).with_name("embyproxy_failover_policy.py")
SPEC = importlib.util.spec_from_file_location("policy_runner", MODULE_PATH)
policy = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(policy)


def config(hold="none"):
    return {
        "manual_hold": hold,
        "health_failures_to_switch": 3,
        "health_successes_to_return": 3,
        "nosla_switch_threshold_percent": 85,
        "nosla_return_threshold_percent": 80,
        "nosla_reset_return_threshold_percent": 15,
    }


def state(active="nosla", failures=0, successes=3):
    return {"active_target": active, "nosla_consecutive_failures": failures,
            "nosla_consecutive_successes": successes}


def traffic(percent=44, quality="fresh_estimate", new_cycle=False):
    return {"usage_percent": percent, "quality": quality,
            "new_cycle_baseline": new_cycle}


class EvaluateTests(unittest.TestCase):
    now = dt.datetime(2026, 8, 12, tzinfo=dt.timezone.utc)
    healthy = {"healthy": True}

    def decision(self, cfg, current, sample, health=None):
        return policy.evaluate(cfg, current, health or self.healthy, sample, self.now)

    def test_healthy_below_threshold_prefers_nosla(self):
        self.assertEqual(self.decision(config(), state(), traffic())[0], "nosla")

    def test_over_threshold_uses_bwg(self):
        self.assertEqual(self.decision(config(), state(), traffic(85))[0], "bwg")

    def test_health_failure_threshold_uses_bwg(self):
        self.assertEqual(self.decision(config(), state(failures=3), traffic())[0], "bwg")

    def test_recovered_below_return_threshold_returns_to_nosla(self):
        self.assertEqual(self.decision(config(), state("bwg"), traffic(44))[0], "nosla")

    def test_reset_cycle_after_grace_returns_to_nosla(self):
        self.assertEqual(self.decision(config(), state("bwg"), traffic(3, new_cycle=True))[0], "nosla")

    def test_manual_holds(self):
        self.assertEqual(self.decision(config("nosla"), state("bwg"), traffic(99))[0], "nosla")
        self.assertEqual(self.decision(config("bwg"), state(), traffic(1))[0], "bwg")

    def test_stale_usage_holds_active_nosla_but_blocks_return(self):
        self.assertEqual(self.decision(config(), state(), traffic(44, "unknown"))[0], "nosla")
        self.assertEqual(self.decision(config(), state("bwg"), traffic(44, "unknown"))[0], "bwg")


class RollbackTests(unittest.TestCase):
    def test_failed_postcheck_restores_previous_target(self):
        calls = []
        records = [{"target": "bwg", "address": "b", "ttl": 60}]

        def read(_):
            return records[-1]

        def apply(_, target):
            calls.append(target)
            records.append({"target": target, "address": target[0], "ttl": 60})

        def failed_health(_, __):
            return {"healthy": False}

        with tempfile.TemporaryDirectory() as directory:
            cfg = {"switch_backup_dir": directory}
            with self.assertRaisesRegex(RuntimeError, "rollback succeeded"):
                policy.switch_with_rollback(cfg, {}, "nosla", failed_health, apply, read)
            self.assertEqual(calls, ["nosla", "bwg"])


class RunnerTests(unittest.TestCase):
    def test_dry_run_never_calls_dns_apply(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            cfg = {
                "mode": "dry-run", "manual_hold": "none",
                "state_file": str(root / "state.json"), "timezone": "Asia/Shanghai",
                "health_failures_to_switch": 3, "health_successes_to_return": 3,
                "cooldown_seconds": 3600, "nosla_switch_threshold_percent": 85,
                "nosla_return_threshold_percent": 80,
                "nosla_reset_return_threshold_percent": 15,
                "usage_seed_observed_at": "2026-08-12T16:21:00+08:00",
                "usage_seed_source": "owner_provider_panel",
                "traffic": {
                    "nosla": {"quota_gb": 1100, "opening_balance_gb": 484,
                              "reset_day": 21, "seed_cycle_start": "2026-07-21"},
                    "bwg": {"quota_gb": 2000, "opening_balance_gb": 217,
                            "reset_day": 7, "seed_cycle_start": "2026-08-07"},
                },
            }
            config_path = root / "config.json"
            config_path.write_text(__import__("json").dumps(cfg), encoding="utf-8")
            samples = {
                "nosla": {"quality": "fresh_estimate", "usage_gb": 484,
                          "usage_percent": 44, "new_cycle_baseline": False},
                "bwg": {"quality": "fresh_estimate", "usage_gb": 217,
                        "usage_percent": 10.85, "new_cycle_baseline": False},
            }
            with mock.patch.object(policy, "dns_record", return_value={"target": "bwg"}), \
                    mock.patch.object(policy, "health_target", return_value={"healthy": True}), \
                    mock.patch.object(policy, "collect_counter", return_value=1), \
                    mock.patch.object(policy, "traffic_sample",
                                      side_effect=lambda _c, _s, target, _m, _n: samples[target]), \
                    mock.patch.object(policy, "switch_with_rollback") as switch:
                policy.run_policy(config_path, __import__("io").StringIO())
                switch.assert_not_called()


if __name__ == "__main__":
    unittest.main()
