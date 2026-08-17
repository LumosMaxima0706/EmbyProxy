#!/usr/bin/env python3
import importlib.util
import tempfile
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).with_name("live-playback-capture.py")
SPEC = importlib.util.spec_from_file_location("live_playback_capture", MODULE_PATH)
CAPTURE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CAPTURE)


class LivePlaybackCaptureTest(unittest.TestCase):
    def test_fields_retain_only_query_free_allowlist(self):
        parsed = CAPTURE.fields(
            "uri=/https/media.example/443/Videos/redacted status=206 "
            "bytes_sent=1024 Authorization=redacted cookie=redacted"
        )
        self.assertEqual(parsed["status"], "206")
        self.assertEqual(parsed["bytes_sent"], "1024")
        self.assertNotIn("Authorization", parsed)
        self.assertNotIn("cookie", parsed)

    def test_sensitive_field_marker_rejects_line(self):
        self.assertTrue(CAPTURE.unsafe_log_line("status=206 authorization=redacted"))
        self.assertFalse(CAPTURE.unsafe_log_line("status=206 bytes_sent=1024"))

    def test_dynamic_fragment_and_legacy_route_attribution(self):
        with tempfile.TemporaryDirectory() as directory:
            Path(directory, "demo.conf").write_text(
                "location ^~ /https/media.example/443/ {\n"
                "  proxy_redirect https://cdn.example/ /https/cdn.example/443/;\n"
                "}\n",
                encoding="utf-8",
            )
            rules = CAPTURE.route_rules(
                directory, ["demo"], {"legacy": "/https/legacy.example/443"}
            )
        self.assertEqual(
            CAPTURE.route_owner("/https/media.example/443/Videos/redacted", rules)[:2],
            ("demo", "primary"),
        )
        self.assertEqual(
            CAPTURE.route_owner("/https/legacy.example/443/Videos/redacted", rules)[:2],
            ("legacy", "primary"),
        )


if __name__ == "__main__":
    unittest.main()
