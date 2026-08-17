#!/usr/bin/env python3
"""Exact-allowlist DNS adapter for the owner Admin hostname.

This wrapper reuses the protected provider implementation without accepting a
hostname, address, or credential on the command line.
"""

import argparse
import importlib.util
import json
import sys
from pathlib import Path


BASE_ADAPTER = Path("/opt/stream-failover/spaceship_dns.py")
NAME = "owner-admin"
FQDN = "owner-admin.149077530.xyz"
ADDRESS = "144.34.226.187"
TTL = 60


class OwnerAdminDNSError(RuntimeError):
    pass


def load_base():
    spec = importlib.util.spec_from_file_location("restricted_spaceship_dns", BASE_ADAPTER)
    if spec is None or spec.loader is None:
        raise OwnerAdminDNSError("restricted DNS adapter is unavailable")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def snapshot(base):
    records = base.records()
    matches = base.find(records, NAME)
    if len(matches) > 1:
        raise OwnerAdminDNSError("multiple owner Admin A records found")
    if not matches:
        return {"exists": False, "record": FQDN}
    item = matches[0]
    return {"exists": True, "record": FQDN, "address": item.get("address"),
            "ttl": int(item.get("ttl") or 0)}


def ensure(base):
    before = base.records()
    matches = base.find(before, NAME)
    if matches:
        current = snapshot(base)
        if current["address"] == ADDRESS and current["ttl"] == TTL:
            return {"ok": True, "created": False, "already_correct": True,
                    **current}
        raise OwnerAdminDNSError("owner Admin record exists unexpectedly")
    baseline = base.canonical(before)
    base.put([{"type": "A", "name": NAME, "address": ADDRESS, "ttl": TTL}])
    after = base.records()
    verified = base.find(after, NAME)
    if (len(verified) != 1 or verified[0].get("address") != ADDRESS
            or int(verified[0].get("ttl") or 0) != TTL):
        raise OwnerAdminDNSError("owner Admin record verification failed")
    unrelated = base.canonical([item for item in after if item not in verified])
    if unrelated != baseline:
        raise OwnerAdminDNSError("unrelated DNS record changed")
    return {"ok": True, "created": True, "record": FQDN,
            "address": ADDRESS, "ttl": TTL, "unrelated_records_unchanged": True}


def remove(base):
    before = base.records()
    matches = base.find(before, NAME)
    if not matches:
        return {"ok": True, "deleted": False, "record": FQDN}
    if (len(matches) != 1 or matches[0].get("address") != ADDRESS
            or int(matches[0].get("ttl") or 0) != TTL):
        raise OwnerAdminDNSError("owner Admin record is outside rollback allowlist")
    unrelated_before = base.canonical([item for item in before if item not in matches])
    base.delete([{"type": "A", "name": NAME, "address": ADDRESS}])
    after = base.records()
    if base.find(after, NAME):
        raise OwnerAdminDNSError("owner Admin record deletion failed")
    if base.canonical(after) != unrelated_before:
        raise OwnerAdminDNSError("unrelated DNS record changed")
    return {"ok": True, "deleted": True, "record": FQDN,
            "unrelated_records_unchanged": True}


def main():
    parser = argparse.ArgumentParser(description="restricted owner Admin DNS adapter")
    parser.add_argument("command", choices=("status", "ensure", "delete"))
    args = parser.parse_args()
    base = load_base()
    if args.command == "status":
        result = {"ok": True, "provider_ready": bool(base.credentials()),
                  "dns_apply_enabled": bool(base.dns_apply_enabled()),
                  **snapshot(base)}
    elif not base.dns_apply_enabled():
        result = {"ok": True, "applied": False, "blocked": True,
                  "message": "DNS apply is disabled", **snapshot(base)}
    elif args.command == "ensure":
        result = ensure(base)
    else:
        result = remove(base)
    print(json.dumps(result, ensure_ascii=True, sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(json.dumps({"ok": False, "error": type(exc).__name__}, sort_keys=True))
        sys.exit(1)
