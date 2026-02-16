#!/usr/bin/env python3
"""
Arbiter Telemetry Integration Test

Sends requests to Arbiter's /api/v1/check endpoint, then reads the Redis Stream
to verify telemetry events were published with the correct shape and normalization.

Prerequisites:
  1. Redis running:      make redis-up
  2. Arbiter running:    make run   (with ARB_TELEMETRY_ENABLED=true in .env)

Usage:
  python3 scripts/test_telemetry.py
  python3 scripts/test_telemetry.py --arbiter http://127.0.0.1:9100 --redis redis://localhost:6379/0
  python3 scripts/test_telemetry.py --loop   # continuous mode (like simulate_blocks.py)
"""

import argparse
import json
import sys
import time
from datetime import datetime
from typing import Any, Dict, List, Optional, Tuple

try:
    import redis
    import requests
except ImportError:
    print("Missing dependencies. Install with:")
    print("  pip3 install -r scripts/requirements.txt")
    sys.exit(1)


# ── Configuration ────────────────────────────────────────────────────────────

ARBITER_URL = "http://127.0.0.1:9100/api/v1/check"
REDIS_URL = "redis://localhost:6379/0"
STREAM_KEY = "arb:v1:events"

# Required fields in every telemetry event
REQUIRED_FIELDS = {
    "v", "ts_ms", "ip", "host", "host_raw", "method",
    "path", "path_raw", "status", "decision", "engine_decision",
}

# ── Test Cases ───────────────────────────────────────────────────────────────

TEST_CASES = [
    {
        "name": "Basic allow (no policy)",
        "headers": {
            "X-Original-Host": "no-policy-host.example.com",
            "X-Original-Method": "GET",
            "X-Original-Uri": "/healthcheck",
            "X-Policy-Host": "no-policy-host.example.com",
            "X-Real-Ip": "10.0.0.1",
        },
        "expect": {
            "host": "no-policy-host.example.com",
            "host_raw": "no-policy-host.example.com",
            "method": "GET",
            "path": "/healthcheck",
            "path_raw": "/healthcheck",
            "ip": "10.0.0.1",
            "decision": "allow",  # no policy => default allow
        },
    },
    {
        "name": "Host normalization (WWW + uppercase + port)",
        "headers": {
            "X-Original-Host": "WWW.Example.COM:8080",
            "X-Original-Method": "POST",
            "X-Original-Uri": "/api/v1/data",
            "X-Policy-Host": "example.com",
            "X-Real-Ip": "192.168.1.50",
        },
        "expect": {
            "host": "example.com",
            "host_raw": "WWW.Example.COM:8080",
            "method": "POST",
            "path": "/api/v1/data",
            "path_raw": "/api/v1/data",
            "ip": "192.168.1.50",
        },
    },
    {
        "name": "Path normalization — numeric ID",
        "headers": {
            "X-Original-Host": "api.example.com",
            "X-Original-Method": "GET",
            "X-Original-Uri": "/api/v1/users/42/profile",
            "X-Policy-Host": "api.example.com",
            "X-Real-Ip": "172.16.0.5",
        },
        "expect": {
            "host": "api.example.com",
            "path": "/api/v1/users/:id/profile",
            "path_raw": "/api/v1/users/42/profile",
        },
    },
    {
        "name": "Path normalization — UUID",
        "headers": {
            "X-Original-Host": "api.example.com",
            "X-Original-Method": "DELETE",
            "X-Original-Uri": "/api/v1/sessions/550e8400-e29b-41d4-a716-446655440000",
            "X-Policy-Host": "api.example.com",
            "X-Real-Ip": "172.16.0.6",
        },
        "expect": {
            "path": "/api/v1/sessions/:uuid",
            "path_raw": "/api/v1/sessions/550e8400-e29b-41d4-a716-446655440000",
        },
    },
    {
        "name": "Path normalization — long hex token",
        "headers": {
            "X-Original-Host": "auth.example.com",
            "X-Original-Method": "GET",
            "X-Original-Uri": "/verify/a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
            "X-Policy-Host": "auth.example.com",
            "X-Real-Ip": "10.10.10.10",
        },
        "expect": {
            "path": "/verify/:token",
            "path_raw": "/verify/a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
        },
    },
    {
        "name": "Query param stripping",
        "headers": {
            "X-Original-Host": "app.example.com",
            "X-Original-Method": "GET",
            "X-Original-Uri": "/search?q=secret&api_key=abc123",
            "X-Policy-Host": "app.example.com",
            "X-Real-Ip": "10.0.0.99",
        },
        "expect": {
            "path": "/search",
            "path_raw": "/search",  # query stripped before storage
        },
    },
    {
        "name": "Pipe replacement in path",
        "headers": {
            "X-Original-Host": "app.example.com",
            "X-Original-Method": "GET",
            "X-Original-Uri": "/filter|sort/results",
            "X-Policy-Host": "app.example.com",
            "X-Real-Ip": "10.0.0.100",
        },
        "expect": {
            "path": "/filter_sort/results",
            "path_raw": "/filter|sort/results",  # raw preserves pipe but query stripped
        },
    },
    {
        "name": "Schema version check",
        "headers": {
            "X-Original-Host": "version-check.example.com",
            "X-Original-Method": "GET",
            "X-Original-Uri": "/",
            "X-Policy-Host": "version-check.example.com",
            "X-Real-Ip": "1.2.3.4",
        },
        "expect": {
            "v": 1,
        },
    },
]


# ── Helpers ──────────────────────────────────────────────────────────────────

def send_check(arbiter_url: str, headers: Dict[str, str]) -> Tuple[bool, Dict[str, Any]]:
    """Send a request to Arbiter's check endpoint. Returns (success, result_dict)."""
    try:
        resp = requests.get(arbiter_url, headers=headers, timeout=5)
        return True, {
            "status_code": resp.status_code,
            "headers": dict(resp.headers),
            "body": resp.text,
        }
    except requests.exceptions.RequestException as e:
        return False, {"error": str(e)}


def read_stream_events(
    r: redis.Redis,
    stream_key: str,
    since_id: str = "0-0",
    count: int = 100,
) -> List[Tuple[str, Dict[str, Any]]]:
    """Read events from the Redis Stream after the given ID.
    Returns list of (stream_id, parsed_event_dict).
    """
    results = []
    raw = r.xrange(stream_key, min=f"({since_id}", count=count)
    for stream_id, fields in raw:
        # stream_id and field keys come back as bytes or strings depending on decode_responses
        sid = stream_id if isinstance(stream_id, str) else stream_id.decode()
        event_raw = fields.get("event") or fields.get(b"event", b"")
        if isinstance(event_raw, bytes):
            event_raw = event_raw.decode()
        try:
            event = json.loads(event_raw)
        except (json.JSONDecodeError, TypeError):
            event = {"_raw": event_raw, "_parse_error": True}
        results.append((sid, event))
    return results


def validate_event(event: Dict[str, Any], expect: Dict[str, Any]) -> List[str]:
    """Validate a single event against expected values. Returns list of error strings."""
    errors = []

    # Check required fields
    missing = REQUIRED_FIELDS - set(event.keys())
    if missing:
        errors.append(f"missing fields: {', '.join(sorted(missing))}")

    # Check expected values
    for key, expected_val in expect.items():
        actual_val = event.get(key)
        if actual_val != expected_val:
            errors.append(f"{key}: expected {expected_val!r}, got {actual_val!r}")

    # ts_ms should be a reasonable timestamp (within last 60 seconds)
    ts_ms = event.get("ts_ms")
    if isinstance(ts_ms, (int, float)):
        age_s = (time.time() * 1000 - ts_ms) / 1000
        if age_s > 60 or age_s < -5:
            errors.append(f"ts_ms age looks wrong: {age_s:.1f}s old")

    return errors


# ── Main ─────────────────────────────────────────────────────────────────────

def run_once(
    arbiter_url: str,
    redis_client: redis.Redis,
    stream_key: str,
) -> Tuple[int, int]:
    """Run all test cases once. Returns (passed, failed)."""
    ts = datetime.now().strftime("%H:%M:%S")

    # Record the current stream high-water mark before sending
    try:
        info = redis_client.xinfo_stream(stream_key)
        last_id = info.get("last-generated-id", "0-0")
        if isinstance(last_id, bytes):
            last_id = last_id.decode()
    except redis.exceptions.ResponseError:
        # Stream doesn't exist yet
        last_id = "0-0"

    print(f"\n[{ts}] Sending {len(TEST_CASES)} requests to Arbiter...")
    print("-" * 70)

    # Send all requests
    send_results = []
    for tc in TEST_CASES:
        ok, result = send_check(arbiter_url, tc["headers"])
        status = f"HTTP {result['status_code']}" if ok else f"ERROR: {result.get('error', '?')}"
        print(f"  → {tc['name']:50s} {status}")
        send_results.append((tc, ok, result))

    # Brief pause for the worker to process
    time.sleep(0.5)

    # Read new events from stream
    events = read_stream_events(redis_client, stream_key, since_id=last_id)
    print(f"\n[{ts}] Read {len(events)} new event(s) from stream '{stream_key}'")
    print("-" * 70)

    if not events:
        print("  ✗ NO EVENTS FOUND — telemetry may not be reaching Redis")
        print("    Check that ARB_TELEMETRY_ENABLED=true and Redis is reachable")
        return 0, len(TEST_CASES)

    passed = 0
    failed = 0

    for tc, send_ok, send_result in send_results:
        if not send_ok:
            print(f"  ✗ {tc['name']}: skipped (request failed)")
            failed += 1
            continue

        expect = tc.get("expect", {})

        # Find a matching event by host_raw + method + path_raw
        h = tc["headers"]
        match_host = h.get("X-Original-Host", "")
        match_method = h.get("X-Original-Method", "")

        # Path raw should have query stripped
        raw_uri = h.get("X-Original-Uri", "")
        match_path_raw = raw_uri.split("?")[0].split("#")[0] or "/"
        # Pipe replacement happens in normalized path only, not path_raw
        # path_raw is the query-stripped original

        matched_event = None
        for _sid, ev in events:
            if (
                ev.get("host_raw") == match_host
                and ev.get("method") == match_method
                and ev.get("path_raw") == match_path_raw
            ):
                matched_event = ev
                break

        if matched_event is None:
            print(f"  ✗ {tc['name']}: no matching event in stream")
            failed += 1
            continue

        errors = validate_event(matched_event, expect)
        if errors:
            print(f"  ✗ {tc['name']}:")
            for e in errors:
                print(f"      {e}")
            failed += 1
        else:
            dec = matched_event.get("decision", "?")
            eng = matched_event.get("engine_decision", "?")
            nhost = matched_event.get("host", "?")
            npath = matched_event.get("path", "?")
            print(f"  ✓ {tc['name']}")
            print(f"      decision={dec} engine={eng} host={nhost} path={npath}")
            passed += 1

    return passed, failed


def main():
    parser = argparse.ArgumentParser(description="Arbiter Telemetry Integration Test")
    parser.add_argument(
        "--arbiter",
        default=ARBITER_URL,
        help=f"Arbiter check URL (default: {ARBITER_URL})",
    )
    parser.add_argument(
        "--redis",
        default=REDIS_URL,
        help=f"Redis URL (default: {REDIS_URL})",
    )
    parser.add_argument(
        "--stream",
        default=STREAM_KEY,
        help=f"Redis Stream key (default: {STREAM_KEY})",
    )
    parser.add_argument(
        "--loop",
        action="store_true",
        help="Run continuously (like simulate_blocks.py)",
    )
    parser.add_argument(
        "--interval",
        type=int,
        default=10,
        help="Seconds between rounds in loop mode (default: 10)",
    )
    args = parser.parse_args()

    print("=" * 70)
    print("  Arbiter Telemetry Integration Test")
    print("=" * 70)
    print(f"  Arbiter:  {args.arbiter}")
    print(f"  Redis:    {args.redis}")
    print(f"  Stream:   {args.stream}")
    print(f"  Mode:     {'loop (Ctrl+C to stop)' if args.loop else 'single run'}")
    print("=" * 70)

    # Connect to Redis
    try:
        r = redis.from_url(args.redis, decode_responses=True)
        r.ping()
        print("  Redis:    connected ✓")
    except redis.exceptions.ConnectionError as e:
        print(f"\n  ✗ Cannot connect to Redis: {e}")
        print("    Run: make redis-up")
        sys.exit(1)

    # Quick check that Arbiter is up
    try:
        resp = requests.get(args.arbiter.replace("/api/v1/check", "/healthz"), timeout=2)
        if resp.status_code == 200:
            print("  Arbiter:  healthy ✓")
        else:
            print(f"  Arbiter:  responded {resp.status_code} (may still work)")
    except requests.exceptions.RequestException:
        print("  ✗ Cannot reach Arbiter — is it running? (make run)")
        sys.exit(1)

    print("=" * 70)

    total_passed = 0
    total_failed = 0
    rounds = 0

    try:
        while True:
            rounds += 1
            passed, failed = run_once(args.arbiter, r, args.stream)
            total_passed += passed
            total_failed += failed

            print()
            print(f"  Round {rounds}: {passed} passed, {failed} failed")

            if not args.loop:
                break

            print(f"  Next round in {args.interval}s... (Ctrl+C to stop)")
            time.sleep(args.interval)

    except KeyboardInterrupt:
        print("\n\nStopped by user.")

    # Summary
    print()
    print("=" * 70)
    print(f"  TOTAL: {total_passed} passed, {total_failed} failed ({rounds} round(s))")
    print("=" * 70)

    sys.exit(1 if total_failed > 0 else 0)


if __name__ == "__main__":
    main()
