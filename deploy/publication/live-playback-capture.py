#!/usr/bin/env python3
"""Capture only redacted playback evidence from an edge for a bounded window."""

import argparse
import hashlib
import json
import os
import re
import subprocess
import time
from pathlib import Path


PLAYBACK_CLASSES = {
    "playbackinfo": "PlaybackInfo",
    "sessions/playing": "SessionsPlaying",
}

ALLOWED_LOG_FIELDS = {
    "uri", "host", "status", "request_length", "bytes_sent",
    "body_bytes_sent", "request_time", "upstream_response_time",
    "upstream_status", "range_seen", "content_range_seen", "location_class",
}
FORBIDDEN_LOG_MARKERS = (
    "authorization=", "cookie=", "x-emby-token=",
    "x-mediabrowser-token=", "access_token=",
)


def route_rules(fragment_dir, slugs, legacy_routes):
    rules = {}
    for slug in slugs:
        fragment = Path(fragment_dir) / (slug + ".conf")
        exact, patterns, redirects = [], [], 0
        for raw in fragment.read_text(encoding="utf-8").splitlines():
            line = raw.strip()
            match = re.match(r"location\s+(?:=|\^~)\s+(\S+)\s+\{", line)
            if match:
                value = match.group(1).rstrip("/")
                if value not in exact:
                    exact.append(value)
                continue
            match = re.match(r"location\s+~\s+(.+?)\s+\{", line)
            if match:
                try:
                    patterns.append(re.compile(match.group(1)))
                except re.error:
                    pass
            if line.startswith("proxy_redirect "):
                redirects += 1
        rules[slug] = {"exact": exact, "patterns": patterns, "redirects": redirects}
    for slug, prefix in legacy_routes.items():
        rules[slug] = {"exact": [prefix.rstrip("/")], "patterns": [], "redirects": 0}
    return rules


def route_owner(uri, rules):
    for slug, rule in rules.items():
        for index, prefix in enumerate(rule["exact"]):
            if uri == prefix or uri.startswith(prefix + "/"):
                return slug, "primary" if index == 0 else "redirect", prefix
        for pattern in rule["patterns"]:
            if pattern.search(uri):
                return slug, "redirect_pattern", route_prefix(uri)
    return "unattributed", "unknown", ""


def route_prefix(uri):
    parts = uri.split("/")
    if len(parts) >= 5 and parts[1] in ("http", "https"):
        return "/".join(parts[:5])
    return ""


def host_hash(prefix):
    parts = prefix.split("/")
    if len(parts) < 4:
        return "unavailable"
    return hashlib.sha256(parts[2].lower().encode("utf-8")).hexdigest()[:12]


def public_host_class(value, public_hosts):
    value = value.strip().lower()
    if value in public_hosts:
        return "stream"
    return "other" if value else "unavailable"


def path_class(uri, alias):
    lower = uri.lower()
    for marker, value in PLAYBACK_CLASSES.items():
        if marker in lower:
            return value
    if lower.endswith(".m3u8") or "master.m3u8" in lower:
        return "HLSManifest"
    if lower.endswith((".ts", ".m4s")):
        return "HLSSegment"
    if ("/videos/" in lower or "/audio/" in lower or "/stream" in lower or
            lower.endswith((".mp4", ".mkv", ".avi"))):
        return "VideoStream"
    if alias in ("redirect", "redirect_pattern"):
        return "RedirectAlias"
    if "/images/" in lower or "/image" in lower:
        return "Image"
    return "Other"


def fields(line):
    result = {}
    for part in line.split():
        if "=" in part:
            key, value = part.split("=", 1)
            if key in ALLOWED_LOG_FIELDS:
                result[key] = value
    return result


def unsafe_log_line(line):
    lower = line.lower()
    return any(marker in lower for marker in FORBIDDEN_LOG_MARKERS)


def parse_legacy_route(value):
    slug, separator, prefix = value.partition("=")
    slug = slug.strip().lower()
    prefix = prefix.strip().rstrip("/")
    if not separator or not re.fullmatch(r"[a-z0-9][a-z0-9_-]{0,63}", slug):
        raise argparse.ArgumentTypeError("legacy route must be SLUG=/http(s)/host/port")
    if not re.fullmatch(r"/https?/[^/?#\s]+/[0-9]{1,5}", prefix):
        raise argparse.ArgumentTypeError("legacy route prefix is invalid")
    return slug, prefix


def safe_int(value):
    try:
        return max(0, int(value))
    except (TypeError, ValueError):
        return 0


def safe_duration(value):
    try:
        return max(0, round(float(value) * 1000))
    except (TypeError, ValueError):
        return 0


def append_event(output, event):
    output.write(json.dumps(event, separators=(",", ":"), sort_keys=True) + "\n")
    output.flush()


def network_bytes():
    received = sent = 0
    with open("/proc/net/dev", encoding="ascii") as handle:
        for line in handle:
            if ":" not in line:
                continue
            name, counters = line.split(":", 1)
            if name.strip() == "lo":
                continue
            values = counters.split()
            if len(values) >= 9:
                received += safe_int(values[0])
                sent += safe_int(values[8])
    return received, sent


def established_https():
    command = ["/usr/bin/ss", "-Htn", "state", "established", "sport", "=", ":443"]
    result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                            check=False, text=True)
    return sum(1 for line in result.stdout.splitlines() if line.strip())


def classify_error(line):
    lower = line.lower()
    checks = (
        ("upstream_timeout", "upstream timed out"),
        ("upstream_connect_failed", "connect() failed"),
        ("upstream_closed", "upstream prematurely closed"),
        ("no_live_upstream", "no live upstreams"),
        ("permission_denied", "permission denied"),
        ("redirect_fallthrough", "access forbidden by rule"),
    )
    for category, marker in checks:
        if marker in lower:
            return category
    return "other_error"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--edge", required=True, choices=("nosla", "bwg"))
    parser.add_argument("--access-log", required=True)
    parser.add_argument("--error-log", default="/var/log/nginx/error.log")
    parser.add_argument("--fragment-dir", default="/etc/nginx/conf.d/embyproxy-publications")
    parser.add_argument("--slug", action="append", default=[])
    parser.add_argument("--legacy-route", action="append", default=[], type=parse_legacy_route)
    parser.add_argument("--public-host", action="append", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--duration", type=int, default=600)
    args = parser.parse_args()
    if args.duration < 60 or args.duration > 900:
        raise SystemExit("invalid capture duration")
    if not args.slug and not args.legacy_route:
        raise SystemExit("at least one --slug or --legacy-route is required")
    if any(not re.fullmatch(r"[a-z0-9][a-z0-9_-]{0,63}", slug) for slug in args.slug):
        raise SystemExit("invalid slug")
    public_hosts = {host.strip().lower() for host in args.public_host}
    if any(not re.fullmatch(r"[a-z0-9.-]+", host) for host in public_hosts):
        raise SystemExit("invalid public host")

    rules = route_rules(args.fragment_dir, args.slug, dict(args.legacy_route))
    access = open(args.access_log, encoding="utf-8", errors="strict")
    access.seek(0, os.SEEK_END)
    error = open(args.error_log, encoding="utf-8", errors="replace")
    error.seek(0, os.SEEK_END)
    output_path = Path(args.output)
    output_path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    with output_path.open("x", encoding="utf-8") as output:
        os.chmod(output_path, 0o600)
        started = int(time.time())
        append_event(output, {
            "edge": args.edge, "kind": "capture_start", "timestamp": started,
            "duration_seconds": args.duration,
            "access_offset": access.tell(), "error_offset": error.tell(),
            "route_count": len(rules),
            "redirect_rule_count": sum(rule["redirects"] for rule in rules.values()),
        })
        last_sample = 0
        deadline = time.monotonic() + args.duration
        while time.monotonic() < deadline:
            for line in access.readlines():
                if unsafe_log_line(line):
                    append_event(output, {
                        "edge": args.edge, "kind": "rejected_log_line",
                        "timestamp": int(time.time()), "reason": "forbidden_field",
                    })
                    continue
                item = fields(line)
                uri = item.get("uri", "")
                if not uri or "?" in uri or "#" in uri:
                    continue
                slug, alias, prefix = route_owner(uri, rules)
                status = safe_int(item.get("status"))
                append_event(output, {
                    "edge": args.edge, "kind": "access", "timestamp": int(time.time()),
                    "slug": slug, "route_alias": alias, "route_host_hash": host_hash(prefix),
                    "public_host_class": public_host_class(item.get("host", ""), public_hosts),
                    "stage": path_class(uri, alias), "status": status,
                    "bytes_in": safe_int(item.get("request_length")),
                    "bytes_out": safe_int(item.get("body_bytes_sent") or item.get("bytes_sent")),
                    "request_ms": safe_duration(item.get("request_time")),
                    "upstream_ms": safe_duration(item.get("upstream_response_time")),
                    "upstream_status": safe_int(item.get("upstream_status")) or "unavailable",
                    "is_206": status == 206, "is_redirect": status in (302, 307, 308),
                    "range_seen": item.get("range_seen", "unavailable"),
                    "content_range_seen": item.get("content_range_seen", "unavailable"),
                    "location_class": item.get("location_class", "unavailable"),
                })
            error_counts = {}
            for line in error.readlines():
                category = classify_error(line)
                error_counts[category] = error_counts.get(category, 0) + 1
            for category, count in error_counts.items():
                append_event(output, {
                    "edge": args.edge, "kind": "error_count", "timestamp": int(time.time()),
                    "category": category, "count": count,
                })
            now = time.monotonic()
            if now - last_sample >= 1:
                received, sent = network_bytes()
                append_event(output, {
                    "edge": args.edge, "kind": "sample", "timestamp": int(time.time()),
                    "established_https": established_https(),
                    "interface_bytes_in": received, "interface_bytes_out": sent,
                })
                last_sample = now
            time.sleep(0.2)
        append_event(output, {"edge": args.edge, "kind": "capture_end", "timestamp": int(time.time())})


if __name__ == "__main__":
    main()
