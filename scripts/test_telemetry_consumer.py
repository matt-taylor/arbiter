#!/usr/bin/env python3
"""
Arbiter Telemetry Consumer – End-to-End Integration Test

Publishes synthetic events directly to the Redis Stream, waits for the
consumer to flush them into MariaDB, then verifies the rollup rows exist
with the expected counter values.

Each test run uses a unique run-ID embedded in hostnames so results never
collide with other traffic or prior runs.

Prerequisites (handled by `make test-telemetry-e2e`):
  1. Redis + MariaDB containers running
  2. Partition maintenance has been run for today
  3. The telemetry consumer binary is running in the background

Usage:
  python3 scripts/test_telemetry_consumer.py
  python3 scripts/test_telemetry_consumer.py --redis redis://localhost:6379/0 --mariadb-dsn "..."
"""

import argparse
import hashlib
import json
import sys
import time
import uuid
from typing import Any, Dict, List, Optional, Tuple

try:
    import redis
except ImportError:
    print("Missing redis. Install with: pip3 install redis")
    sys.exit(1)

try:
    import pymysql
except ImportError:
    print("Missing pymysql. Install with: pip3 install pymysql")
    sys.exit(1)


# ── Defaults ────────────────────────────────────────────────────────────────

REDIS_URL = "redis://localhost:6379/0"
STREAM_KEY = "arb:v1:events"

DEFAULT_MARIADB_HOST = "127.0.0.1"
DEFAULT_MARIADB_PORT = 3306
DEFAULT_MARIADB_USER = "root"
DEFAULT_MARIADB_PASSWORD = "changeme"
DEFAULT_MARIADB_DB = "arbiter_telemetry"

# How long to wait for the consumer to flush (seconds)
FLUSH_WAIT_SECS = 4
# Number of retries when polling MariaDB for rows
DB_POLL_RETRIES = 5
DB_POLL_INTERVAL = 1.0  # seconds between retries


# ── Test Events ─────────────────────────────────────────────────────────────

def build_test_events(run_id: str) -> List[Dict[str, Any]]:
    """
    Build a list of synthetic wireEvent dicts for this run.
    Each event has a unique host scoped by run_id.
    Returns list of dicts, each with:
        event: the wireEvent dict to XADD
        expect_host_ip: expected (host, ip) key for arb_host_ip_10s
        expect_counters: expected counter values for the host+IP row
        expect_path: (optional) expected row in arb_host_ip_path_10s
    """
    now_ms = int(time.time() * 1000)

    # Use a stable bucket so all events in this run land in the same row
    # (floor to 10s boundary)
    bucket_ms = (now_ms // 10000) * 10000

    host_a = f"{run_id}.host-a.test.example.com"
    host_b = f"{run_id}.host-b.test.example.com"
    ip1 = "198.51.100.1"  # RFC 5737 TEST-NET-2
    ip2 = "198.51.100.2"

    events = []

    # --- Group 1: 3 GETs to host_a from ip1, path /api/v1/users ---
    for _ in range(3):
        events.append({
            "event": {
                "v": 1,
                "ts_ms": bucket_ms + 1000,
                "ip": ip1,
                "host": host_a,
                "host_raw": host_a,
                "method": "GET",
                "path": "/api/v1/users",
                "path_raw": "/api/v1/users",
                "status": 200,
                "decision": "allow",
                "engine_decision": "allow",
            },
            "expect_host_ip": (host_a, ip1),
            "group": "A1",
        })

    # --- Group 2: 2 POSTs to host_a from ip1, path /api/v1/users, status 401 ---
    for _ in range(2):
        events.append({
            "event": {
                "v": 1,
                "ts_ms": bucket_ms + 2000,
                "ip": ip1,
                "host": host_a,
                "host_raw": host_a,
                "method": "POST",
                "path": "/api/v1/users",
                "path_raw": "/api/v1/users",
                "status": 401,
                "decision": "deny",
                "engine_decision": "unauth",
            },
            "expect_host_ip": (host_a, ip1),
            "group": "A2",
        })

    # --- Group 3: 1 DELETE to host_a from ip1, path /api/v1/items, status 500 ---
    events.append({
        "event": {
            "v": 1,
            "ts_ms": bucket_ms + 3000,
            "ip": ip1,
            "host": host_a,
            "host_raw": host_a,
            "method": "DELETE",
            "path": "/api/v1/items",
            "path_raw": "/api/v1/items",
            "status": 500,
            "decision": "error",
            "engine_decision": "error",
        },
        "expect_host_ip": (host_a, ip1),
        "group": "A3",
    })

    # --- Group 4: 1 GET to host_b from ip2, path /health, status 200 ---
    events.append({
        "event": {
            "v": 1,
            "ts_ms": bucket_ms + 4000,
            "ip": ip2,
            "host": host_b,
            "host_raw": host_b,
            "method": "GET",
            "path": "/health",
            "path_raw": "/health",
            "status": 200,
            "decision": "allow",
            "engine_decision": "allow",
        },
        "expect_host_ip": (host_b, ip2),
        "group": "B1",
    })

    # --- Group 5: 1 PUT to host_a from ip2, path /api/v1/users, status 403 ---
    events.append({
        "event": {
            "v": 1,
            "ts_ms": bucket_ms + 5000,
            "ip": ip2,
            "host": host_a,
            "host_raw": host_a,
            "method": "PUT",
            "path": "/api/v1/users",
            "path_raw": "/api/v1/users",
            "status": 403,
            "decision": "deny",
            "engine_decision": "forbid",
        },
        "expect_host_ip": (host_a, ip2),
        "group": "A4",
    })

    # --- Group 6: 1 PATCH to host_a from ip1, path /api/v1/settings, status 429 ---
    events.append({
        "event": {
            "v": 1,
            "ts_ms": bucket_ms + 6000,
            "ip": ip1,
            "host": host_a,
            "host_raw": host_a,
            "method": "PATCH",
            "path": "/api/v1/settings",
            "path_raw": "/api/v1/settings",
            "status": 429,
            "decision": "deny",
            "engine_decision": "killswitch",
        },
        "expect_host_ip": (host_a, ip1),
        "group": "A5",
    })

    return events


# ── Expected Rollup Values ──────────────────────────────────────────────────

def build_expected_host_ip(run_id: str) -> Dict[Tuple[str, str], Dict[str, int]]:
    """
    Compute expected arb_host_ip_10s rows for this run.
    Key: (host, ip) → expected counters.
    """
    host_a = f"{run_id}.host-a.test.example.com"
    host_b = f"{run_id}.host-b.test.example.com"
    ip1 = "198.51.100.1"
    ip2 = "198.51.100.2"

    return {
        # host_a + ip1: 3 GET/200 + 2 POST/401 + 1 DELETE/500 + 1 PATCH/429 = 7 total
        (host_a, ip1): {
            "total": 7,
            "c_401": 2,
            "c_403": 0,
            "c_404": 0,
            "c_429": 1,
            "c_5xx": 1,
            "m_get": 3,
            "m_post": 2,
            "m_put": 0,
            "m_patch": 1,
            "m_delete": 1,
        },
        # host_a + ip2: 1 PUT/403 = 1 total
        (host_a, ip2): {
            "total": 1,
            "c_401": 0,
            "c_403": 1,
            "c_404": 0,
            "c_429": 0,
            "c_5xx": 0,
            "m_get": 0,
            "m_post": 0,
            "m_put": 1,
            "m_patch": 0,
            "m_delete": 0,
        },
        # host_b + ip2: 1 GET/200 = 1 total
        (host_b, ip2): {
            "total": 1,
            "c_401": 0,
            "c_403": 0,
            "c_404": 0,
            "c_429": 0,
            "c_5xx": 0,
            "m_get": 1,
            "m_post": 0,
            "m_put": 0,
            "m_patch": 0,
            "m_delete": 0,
        },
    }


def build_expected_host_ip_path(run_id: str) -> Dict[Tuple[str, str, str], Dict[str, int]]:
    """
    Compute expected arb_host_ip_path_10s rows for this run.
    Key: (host, ip, path) → expected counters.
    """
    host_a = f"{run_id}.host-a.test.example.com"
    host_b = f"{run_id}.host-b.test.example.com"
    ip1 = "198.51.100.1"
    ip2 = "198.51.100.2"

    return {
        # host_a + ip1 + /api/v1/users: 3 GET/200 + 2 POST/401 = 5
        (host_a, ip1, "/api/v1/users"): {
            "total": 5, "c_401": 2, "c_403": 0, "c_404": 0, "c_429": 0, "c_5xx": 0,
        },
        # host_a + ip1 + /api/v1/items: 1 DELETE/500
        (host_a, ip1, "/api/v1/items"): {
            "total": 1, "c_401": 0, "c_403": 0, "c_404": 0, "c_429": 0, "c_5xx": 1,
        },
        # host_a + ip1 + /api/v1/settings: 1 PATCH/429
        (host_a, ip1, "/api/v1/settings"): {
            "total": 1, "c_401": 0, "c_403": 0, "c_404": 0, "c_429": 1, "c_5xx": 0,
        },
        # host_a + ip2 + /api/v1/users: 1 PUT/403
        (host_a, ip2, "/api/v1/users"): {
            "total": 1, "c_401": 0, "c_403": 1, "c_404": 0, "c_429": 0, "c_5xx": 0,
        },
        # host_b + ip2 + /health: 1 GET/200
        (host_b, ip2, "/health"): {
            "total": 1, "c_401": 0, "c_403": 0, "c_404": 0, "c_429": 0, "c_5xx": 0,
        },
    }


# ── Helpers ─────────────────────────────────────────────────────────────────

def md5_bytes(path: str) -> bytes:
    """Compute MD5 hash matching the Go consumer's PathHash (md5.Sum([]byte(path)))."""
    return hashlib.md5(path.encode("utf-8")).digest()


def xadd_events(r: redis.Redis, stream_key: str, events: List[Dict]) -> List[str]:
    """Publish events to the Redis Stream via XADD. Returns list of stream IDs."""
    ids = []
    for ev in events:
        wire_json = json.dumps(ev["event"])
        stream_id = r.xadd(stream_key, {"event": wire_json})
        if isinstance(stream_id, bytes):
            stream_id = stream_id.decode()
        ids.append(stream_id)
    return ids


def connect_mariadb(
    host: str, port: int, user: str, password: str, database: str
) -> pymysql.Connection:
    """Connect to MariaDB and return a connection."""
    return pymysql.connect(
        host=host,
        port=port,
        user=user,
        password=password,
        database=database,
        cursorclass=pymysql.cursors.DictCursor,
        connect_timeout=5,
    )


def query_host_ip_rows(
    conn: pymysql.Connection, run_id: str
) -> List[Dict[str, Any]]:
    """Query arb_host_ip_10s for rows matching this run_id."""
    with conn.cursor() as cur:
        cur.execute(
            "SELECT * FROM arb_host_ip_10s WHERE host LIKE %s",
            (f"{run_id}.%",),
        )
        return cur.fetchall()


def query_host_ip_path_rows(
    conn: pymysql.Connection, run_id: str
) -> List[Dict[str, Any]]:
    """Query arb_host_ip_path_10s for rows matching this run_id."""
    with conn.cursor() as cur:
        cur.execute(
            "SELECT * FROM arb_host_ip_path_10s WHERE host LIKE %s",
            (f"{run_id}.%",),
        )
        return cur.fetchall()


def cleanup_test_data(conn: pymysql.Connection, run_id: str) -> None:
    """Delete test rows from both rollup tables."""
    with conn.cursor() as cur:
        cur.execute(
            "DELETE FROM arb_host_ip_10s WHERE host LIKE %s", (f"{run_id}.%",)
        )
        cur.execute(
            "DELETE FROM arb_host_ip_path_10s WHERE host LIKE %s", (f"{run_id}.%",)
        )
    conn.commit()


# ── Verification ────────────────────────────────────────────────────────────

def verify_host_ip(
    rows: List[Dict], expected: Dict[Tuple[str, str], Dict[str, int]]
) -> Tuple[int, int, List[str]]:
    """Verify host+IP rollup rows against expected values.
    Returns (passed, failed, error_messages).
    """
    passed = 0
    failed = 0
    errors = []

    # Index rows by (host, ip) for lookup
    actual = {}
    for row in rows:
        key = (row["host"], row["ip"])
        actual[key] = row

    for key, counters in expected.items():
        host, ip = key
        label = f"host_ip ({host}, {ip})"

        if key not in actual:
            errors.append(f"  ✗ {label}: row NOT FOUND in MariaDB")
            failed += 1
            continue

        row = actual[key]
        row_errors = []
        for col, want in counters.items():
            got = row.get(col, None)
            if got != want:
                row_errors.append(f"{col}: expected {want}, got {got}")

        if row_errors:
            errors.append(f"  ✗ {label}:")
            for e in row_errors:
                errors.append(f"      {e}")
            failed += 1
        else:
            errors.append(f"  ✓ {label}: all counters match (total={counters['total']})")
            passed += 1

    # Check for unexpected rows
    for key in actual:
        if key not in expected:
            errors.append(f"  ⚠ unexpected row: {key}")

    return passed, failed, errors


def verify_host_ip_path(
    rows: List[Dict], expected: Dict[Tuple[str, str, str], Dict[str, int]]
) -> Tuple[int, int, List[str]]:
    """Verify host+IP+path rollup rows against expected values.
    Returns (passed, failed, error_messages).
    """
    passed = 0
    failed = 0
    errors = []

    # Index rows by (host, ip, path) for lookup
    actual = {}
    for row in rows:
        key = (row["host"], row["ip"], row["path"])
        actual[key] = row

    for key, counters in expected.items():
        host, ip, path = key
        label = f"path ({host}, {ip}, {path})"

        if key not in actual:
            errors.append(f"  ✗ {label}: row NOT FOUND in MariaDB")
            failed += 1
            continue

        row = actual[key]
        row_errors = []
        for col, want in counters.items():
            got = row.get(col, None)
            if got != want:
                row_errors.append(f"{col}: expected {want}, got {got}")

        # Verify path_hash matches MD5
        expected_hash = md5_bytes(path)
        actual_hash = row.get("path_hash", b"")
        if actual_hash != expected_hash:
            row_errors.append(
                f"path_hash: expected {expected_hash.hex()}, got {actual_hash.hex() if isinstance(actual_hash, bytes) else actual_hash}"
            )

        if row_errors:
            errors.append(f"  ✗ {label}:")
            for e in row_errors:
                errors.append(f"      {e}")
            failed += 1
        else:
            errors.append(f"  ✓ {label}: all counters + hash match (total={counters['total']})")
            passed += 1

    return passed, failed, errors


# ── Main ────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        description="Arbiter Telemetry Consumer E2E Integration Test"
    )
    parser.add_argument(
        "--redis", default=REDIS_URL, help=f"Redis URL (default: {REDIS_URL})"
    )
    parser.add_argument(
        "--stream", default=STREAM_KEY, help=f"Stream key (default: {STREAM_KEY})"
    )
    parser.add_argument(
        "--mariadb-host", default=DEFAULT_MARIADB_HOST, help="MariaDB host"
    )
    parser.add_argument(
        "--mariadb-port", type=int, default=DEFAULT_MARIADB_PORT, help="MariaDB port"
    )
    parser.add_argument(
        "--mariadb-user", default=DEFAULT_MARIADB_USER, help="MariaDB user"
    )
    parser.add_argument(
        "--mariadb-password", default=DEFAULT_MARIADB_PASSWORD, help="MariaDB password"
    )
    parser.add_argument(
        "--mariadb-db", default=DEFAULT_MARIADB_DB, help="MariaDB database"
    )
    parser.add_argument(
        "--flush-wait",
        type=float,
        default=FLUSH_WAIT_SECS,
        help=f"Seconds to wait for consumer flush (default: {FLUSH_WAIT_SECS})",
    )
    parser.add_argument(
        "--no-cleanup",
        action="store_true",
        help="Don't delete test rows from MariaDB after verification",
    )
    args = parser.parse_args()

    # Generate unique run ID
    run_id = uuid.uuid4().hex[:8]

    print("=" * 70)
    print("  Arbiter Telemetry Consumer – E2E Integration Test")
    print("=" * 70)
    print(f"  Run ID:   {run_id}")
    print(f"  Redis:    {args.redis}")
    print(f"  Stream:   {args.stream}")
    print(f"  MariaDB:  {args.mariadb_user}@{args.mariadb_host}:{args.mariadb_port}/{args.mariadb_db}")
    print("=" * 70)

    # ── Connect to Redis ────────────────────────────────────────────────
    try:
        r = redis.from_url(args.redis, decode_responses=True)
        r.ping()
        print("  Redis:    connected ✓")
    except redis.exceptions.ConnectionError as e:
        print(f"\n  ✗ Cannot connect to Redis: {e}")
        print("    Run: make redis-up")
        sys.exit(1)

    # ── Connect to MariaDB ──────────────────────────────────────────────
    try:
        conn = connect_mariadb(
            host=args.mariadb_host,
            port=args.mariadb_port,
            user=args.mariadb_user,
            password=args.mariadb_password,
            database=args.mariadb_db,
        )
        print("  MariaDB:  connected ✓")
    except pymysql.Error as e:
        print(f"\n  ✗ Cannot connect to MariaDB: {e}")
        print("    Run: make mariadb-up && make telemetry-partitions")
        sys.exit(1)

    print("=" * 70)

    # ── Phase 1: Publish events to Redis Stream ─────────────────────────
    events = build_test_events(run_id)
    print(f"\n[Phase 1] Publishing {len(events)} events to stream '{args.stream}'...")

    stream_ids = xadd_events(r, args.stream, events)
    print(f"  Published {len(stream_ids)} events (IDs: {stream_ids[0]} .. {stream_ids[-1]})")

    # ── Phase 2: Wait for consumer to flush ─────────────────────────────
    print(f"\n[Phase 2] Waiting {args.flush_wait}s for consumer to flush...")
    time.sleep(args.flush_wait)

    # ── Phase 3: Verify MariaDB rows ────────────────────────────────────
    print("\n[Phase 3] Verifying MariaDB rollup tables...")
    print("-" * 70)

    total_passed = 0
    total_failed = 0

    # --- host+IP table ---
    expected_hip = build_expected_host_ip(run_id)

    hip_rows = []
    for attempt in range(DB_POLL_RETRIES):
        hip_rows = query_host_ip_rows(conn, run_id)
        if len(hip_rows) >= len(expected_hip):
            break
        if attempt < DB_POLL_RETRIES - 1:
            print(f"  ... found {len(hip_rows)}/{len(expected_hip)} host_ip rows, retrying in {DB_POLL_INTERVAL}s...")
            time.sleep(DB_POLL_INTERVAL)

    print(f"\n  arb_host_ip_10s: {len(hip_rows)} rows found (expected {len(expected_hip)})")
    p, f, msgs = verify_host_ip(hip_rows, expected_hip)
    total_passed += p
    total_failed += f
    for m in msgs:
        print(m)

    # --- host+IP+path table ---
    expected_path = build_expected_host_ip_path(run_id)

    path_rows = []
    for attempt in range(DB_POLL_RETRIES):
        path_rows = query_host_ip_path_rows(conn, run_id)
        if len(path_rows) >= len(expected_path):
            break
        if attempt < DB_POLL_RETRIES - 1:
            print(f"  ... found {len(path_rows)}/{len(expected_path)} path rows, retrying in {DB_POLL_INTERVAL}s...")
            time.sleep(DB_POLL_INTERVAL)

    print(f"\n  arb_host_ip_path_10s: {len(path_rows)} rows found (expected {len(expected_path)})")
    p, f, msgs = verify_host_ip_path(path_rows, expected_path)
    total_passed += p
    total_failed += f
    for m in msgs:
        print(m)

    # ── Cleanup ─────────────────────────────────────────────────────────
    if not args.no_cleanup:
        print(f"\n[Cleanup] Deleting test rows for run {run_id}...")
        cleanup_test_data(conn, run_id)
        print("  Done.")
    else:
        print(f"\n[Cleanup] Skipped (--no-cleanup). Run ID: {run_id}")

    conn.close()

    # ── Summary ─────────────────────────────────────────────────────────
    print()
    print("=" * 70)
    print(f"  RESULT: {total_passed} passed, {total_failed} failed")
    print("=" * 70)

    sys.exit(1 if total_failed > 0 else 0)


if __name__ == "__main__":
    main()
