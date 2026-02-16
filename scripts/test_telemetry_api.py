#!/usr/bin/env python3
"""
Arbiter Telemetry Query API – End-to-End Integration Test

Inserts sample rollup rows directly into MariaDB at known bucket timestamps
(bypassing Redis/consumer), then hits the three telemetry API endpoints via
HTTP and verifies JSON response shape, ordering, counter values, and that
start_ts/end_ts in responses match expectations.

Each test run uses a unique run-ID embedded in hostnames so results never
collide with other traffic or prior runs.

Prerequisites (handled by `make test-telemetry-api`):
  1. MariaDB container running with rollup tables created
  2. Partition maintenance has been run for today
  3. Arbiter is running with ARB_TELEMETRY_API_ENABLED=true

Usage:
  python3 scripts/test_telemetry_api.py --arbiter-url http://127.0.0.1:9199
"""

import argparse
import json
import sys
import time
import uuid
from typing import Any, Dict, List, Tuple

try:
    import pymysql
except ImportError:
    print("Missing pymysql. Install with: pip3 install pymysql")
    sys.exit(1)

try:
    import requests
except ImportError:
    print("Missing requests. Install with: pip3 install requests")
    sys.exit(1)


# ── Defaults ────────────────────────────────────────────────────────────────

DEFAULT_ARBITER_URL = "http://127.0.0.1:9199"
DEFAULT_MARIADB_HOST = "127.0.0.1"
DEFAULT_MARIADB_PORT = 3306
DEFAULT_MARIADB_USER = "arbiter"
DEFAULT_MARIADB_PASSWORD = "arbiter_dev"
DEFAULT_MARIADB_DB = "arbiter_telemetry"


# ── Test Data ───────────────────────────────────────────────────────────────

def insert_test_data(conn: pymysql.Connection, run_id: str) -> Dict[str, Any]:
    """
    Insert sample rollup rows into MariaDB for this run.
    Returns metadata about what was inserted (for assertions).
    """
    host = f"{run_id}.api-test.example.com"
    # Use a bucket time ~1 minute ago (floored to 10s)
    now = int(time.time())
    bucket = (now // 10) * 10 - 60  # 1 minute ago, 10s-aligned

    ip1 = "198.51.100.10"
    ip2 = "198.51.100.20"
    ip3 = "198.51.100.30"

    with conn.cursor() as cur:
        # Insert into arb_host_ip_10s
        # ip1: 100 total, some error codes
        cur.execute(
            """INSERT INTO arb_host_ip_10s
               (bucket_start, host, ip, total, c_401, c_403, c_404, c_429, c_5xx,
                m_get, m_post, m_put, m_patch, m_delete)
               VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)""",
            (bucket, host, ip1, 100, 5, 3, 10, 2, 1, 80, 10, 5, 3, 2),
        )
        # ip2: 50 total
        cur.execute(
            """INSERT INTO arb_host_ip_10s
               (bucket_start, host, ip, total, c_401, c_403, c_404, c_429, c_5xx,
                m_get, m_post, m_put, m_patch, m_delete)
               VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)""",
            (bucket, host, ip2, 50, 0, 1, 2, 0, 0, 40, 5, 3, 1, 1),
        )
        # ip3: 25 total
        cur.execute(
            """INSERT INTO arb_host_ip_10s
               (bucket_start, host, ip, total, c_401, c_403, c_404, c_429, c_5xx,
                m_get, m_post, m_put, m_patch, m_delete)
               VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)""",
            (bucket, host, ip3, 25, 1, 0, 0, 0, 0, 20, 3, 1, 1, 0),
        )

        # Insert into arb_host_ip_path_10s (for ip1)
        import hashlib

        paths = [
            ("/api/v1/users", 60, 3, 2, 5, 1, 0),
            ("/api/v1/items", 30, 2, 1, 3, 1, 1),
            ("/health", 10, 0, 0, 2, 0, 0),
        ]
        for path, total, c401, c403, c404, c429, c5xx in paths:
            path_hash = hashlib.md5(path.encode("utf-8")).digest()
            cur.execute(
                """INSERT INTO arb_host_ip_path_10s
                   (bucket_start, host, ip, path_hash, path, total,
                    c_401, c_403, c_404, c_429, c_5xx)
                   VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)""",
                (bucket, host, ip1, path_hash, path, total, c401, c403, c404, c429, c5xx),
            )

    conn.commit()

    return {
        "host": host,
        "bucket": bucket,
        "ip1": ip1,
        "ip2": ip2,
        "ip3": ip3,
    }


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


# ── Test Assertions ─────────────────────────────────────────────────────────

def test_top_ips(base_url: str, meta: Dict[str, Any]) -> Tuple[int, int, List[str]]:
    """Test GET /api/v1/telemetry/hosts/{host}/top-ips"""
    passed, failed, msgs = 0, 0, []
    host = meta["host"]

    url = f"{base_url}/api/v1/telemetry/hosts/{host}/top-ips?window_minutes=5&limit=10"
    resp = requests.get(url, timeout=5)

    # Check status code
    if resp.status_code != 200:
        msgs.append(f"  ✗ top-ips: expected 200, got {resp.status_code}: {resp.text}")
        return 0, 1, msgs

    data = resp.json()

    # Check Content-Type
    ct = resp.headers.get("Content-Type", "")
    if "application/json" not in ct:
        msgs.append(f"  ✗ top-ips: Content-Type = {ct}")
        failed += 1
    else:
        passed += 1

    # Check host field
    if data.get("host") != host:
        msgs.append(f"  ✗ top-ips: host = {data.get('host')}, expected {host}")
        failed += 1
    else:
        passed += 1

    # Check window_minutes
    if data.get("window_minutes") != 5:
        msgs.append(f"  ✗ top-ips: window_minutes = {data.get('window_minutes')}")
        failed += 1
    else:
        passed += 1

    # Check start_ts and end_ts present
    if "start_ts" not in data or "end_ts" not in data:
        msgs.append(f"  ✗ top-ips: missing start_ts or end_ts")
        failed += 1
    else:
        passed += 1

    # Check items
    items = data.get("items", [])
    if len(items) != 3:
        msgs.append(f"  ✗ top-ips: expected 3 items, got {len(items)}")
        failed += 1
    else:
        passed += 1

    # Check ordering (DESC by total)
    if len(items) >= 3:
        totals = [item["total"] for item in items]
        if totals != sorted(totals, reverse=True):
            msgs.append(f"  ✗ top-ips: items not ordered by total DESC: {totals}")
            failed += 1
        else:
            passed += 1

        # Check first item is ip1 with total 100
        if items[0]["ip"] != meta["ip1"] or items[0]["total"] != 100:
            msgs.append(f"  ✗ top-ips: first item = {items[0]}, expected ip={meta['ip1']}, total=100")
            failed += 1
        else:
            passed += 1

        # Check status fields exist
        status = items[0].get("status", {})
        for field in ["c_401", "c_403", "c_404", "c_429", "c_5xx"]:
            if field not in status:
                msgs.append(f"  ✗ top-ips: missing status field {field}")
                failed += 1
            else:
                passed += 1

    if failed == 0:
        msgs.append(f"  ✓ top-ips: all checks passed ({passed} assertions)")

    return passed, failed, msgs


def test_top_paths(base_url: str, meta: Dict[str, Any]) -> Tuple[int, int, List[str]]:
    """Test GET /api/v1/telemetry/hosts/{host}/ips/{ip}/top-paths"""
    passed, failed, msgs = 0, 0, []
    host = meta["host"]
    ip = meta["ip1"]

    url = f"{base_url}/api/v1/telemetry/hosts/{host}/ips/{ip}/top-paths?window_minutes=5&limit=10"
    resp = requests.get(url, timeout=5)

    if resp.status_code != 200:
        msgs.append(f"  ✗ top-paths: expected 200, got {resp.status_code}: {resp.text}")
        return 0, 1, msgs

    data = resp.json()

    # Check fields
    if data.get("host") != host:
        msgs.append(f"  ✗ top-paths: host = {data.get('host')}")
        failed += 1
    else:
        passed += 1

    if data.get("ip") != ip:
        msgs.append(f"  ✗ top-paths: ip = {data.get('ip')}")
        failed += 1
    else:
        passed += 1

    items = data.get("items", [])
    if len(items) != 3:
        msgs.append(f"  ✗ top-paths: expected 3 items, got {len(items)}")
        failed += 1
    else:
        passed += 1

    # Check ordering (DESC by total)
    if len(items) >= 3:
        totals = [item["total"] for item in items]
        if totals != sorted(totals, reverse=True):
            msgs.append(f"  ✗ top-paths: items not ordered DESC: {totals}")
            failed += 1
        else:
            passed += 1

        # First should be /api/v1/users with total 60
        if items[0]["path"] != "/api/v1/users" or items[0]["total"] != 60:
            msgs.append(f"  ✗ top-paths: first item = {items[0]}")
            failed += 1
        else:
            passed += 1

    if failed == 0:
        msgs.append(f"  ✓ top-paths: all checks passed ({passed} assertions)")

    return passed, failed, msgs


def test_summary(base_url: str, meta: Dict[str, Any]) -> Tuple[int, int, List[str]]:
    """Test GET /api/v1/telemetry/hosts/{host}/summary"""
    passed, failed, msgs = 0, 0, []
    host = meta["host"]

    url = f"{base_url}/api/v1/telemetry/hosts/{host}/summary?window_minutes=5"
    resp = requests.get(url, timeout=5)

    if resp.status_code != 200:
        msgs.append(f"  ✗ summary: expected 200, got {resp.status_code}: {resp.text}")
        return 0, 1, msgs

    data = resp.json()

    # Total should be 100 + 50 + 25 = 175
    if data.get("total") != 175:
        msgs.append(f"  ✗ summary: total = {data.get('total')}, expected 175")
        failed += 1
    else:
        passed += 1

    # Unique IPs should be 3
    if data.get("unique_ips") != 3:
        msgs.append(f"  ✗ summary: unique_ips = {data.get('unique_ips')}, expected 3")
        failed += 1
    else:
        passed += 1

    # Check status fields
    status = data.get("status", {})
    expected_status = {"c_401": 6, "c_403": 4, "c_404": 12, "c_429": 2, "c_5xx": 1}
    for field, want in expected_status.items():
        got = status.get(field, None)
        if got != want:
            msgs.append(f"  ✗ summary: status.{field} = {got}, expected {want}")
            failed += 1
        else:
            passed += 1

    if failed == 0:
        msgs.append(f"  ✓ summary: all checks passed ({passed} assertions)")

    return passed, failed, msgs


def test_with_end_ts(base_url: str, meta: Dict[str, Any]) -> Tuple[int, int, List[str]]:
    """Test endpoints with explicit end_ts targeting the inserted data."""
    passed, failed, msgs = 0, 0, []
    host = meta["host"]
    bucket = meta["bucket"]

    # Use end_ts = bucket + 10 (just after the data bucket)
    end_ts = bucket + 10

    url = f"{base_url}/api/v1/telemetry/hosts/{host}/top-ips?window_minutes=1&end_ts={end_ts}"
    resp = requests.get(url, timeout=5)

    if resp.status_code != 200:
        msgs.append(f"  ✗ end_ts top-ips: expected 200, got {resp.status_code}: {resp.text}")
        return 0, 1, msgs

    data = resp.json()

    # start_ts should be end_ts_aligned - 60
    expected_end = (end_ts // 10) * 10
    expected_start = expected_end - 60
    if data.get("start_ts") != expected_start:
        msgs.append(f"  ✗ end_ts: start_ts = {data.get('start_ts')}, expected {expected_start}")
        failed += 1
    else:
        passed += 1

    if data.get("end_ts") != expected_end:
        msgs.append(f"  ✗ end_ts: end_ts = {data.get('end_ts')}, expected {expected_end}")
        failed += 1
    else:
        passed += 1

    items = data.get("items", [])
    if len(items) != 3:
        msgs.append(f"  ✗ end_ts: expected 3 items, got {len(items)}")
        failed += 1
    else:
        passed += 1

    if failed == 0:
        msgs.append(f"  ✓ end_ts: all checks passed ({passed} assertions)")

    return passed, failed, msgs


def test_validation_errors(base_url: str) -> Tuple[int, int, List[str]]:
    """Test validation error responses."""
    passed, failed, msgs = 0, 0, []

    # Host with colon (port) → 400
    url = f"{base_url}/api/v1/telemetry/hosts/example.com%3A8080/top-ips"
    resp = requests.get(url, timeout=5)
    if resp.status_code == 400:
        data = resp.json()
        if "error" in data and "request_id" in data:
            passed += 1
        else:
            msgs.append(f"  ✗ validation: 400 response missing error/request_id fields")
            failed += 1
    else:
        msgs.append(f"  ✗ validation: host with port expected 400, got {resp.status_code}")
        failed += 1

    # Invalid window_minutes → 400
    url = f"{base_url}/api/v1/telemetry/hosts/example.com/top-ips?window_minutes=abc"
    resp = requests.get(url, timeout=5)
    if resp.status_code == 400:
        passed += 1
    else:
        msgs.append(f"  ✗ validation: invalid window expected 400, got {resp.status_code}")
        failed += 1

    # Invalid end_ts → 400
    url = f"{base_url}/api/v1/telemetry/hosts/example.com/summary?end_ts=not-a-number"
    resp = requests.get(url, timeout=5)
    if resp.status_code == 400:
        passed += 1
    else:
        msgs.append(f"  ✗ validation: invalid end_ts expected 400, got {resp.status_code}")
        failed += 1

    # Invalid IP → 400
    url = f"{base_url}/api/v1/telemetry/hosts/example.com/ips/not-an-ip/top-paths"
    resp = requests.get(url, timeout=5)
    if resp.status_code == 400:
        passed += 1
    else:
        msgs.append(f"  ✗ validation: invalid IP expected 400, got {resp.status_code}")
        failed += 1

    if failed == 0:
        msgs.append(f"  ✓ validation: all checks passed ({passed} assertions)")

    return passed, failed, msgs


# ── Main ────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        description="Arbiter Telemetry Query API E2E Integration Test"
    )
    parser.add_argument(
        "--arbiter-url", default=DEFAULT_ARBITER_URL, help="Arbiter base URL"
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
        "--no-cleanup",
        action="store_true",
        help="Don't delete test rows from MariaDB after verification",
    )
    args = parser.parse_args()

    # Generate unique run ID
    run_id = uuid.uuid4().hex[:8]

    print("=" * 70)
    print("  Arbiter Telemetry Query API – E2E Integration Test")
    print("=" * 70)
    print(f"  Run ID:   {run_id}")
    print(f"  Arbiter:  {args.arbiter_url}")
    print(f"  MariaDB:  {args.mariadb_user}@{args.mariadb_host}:{args.mariadb_port}/{args.mariadb_db}")
    print("=" * 70)

    # ── Connect to MariaDB ──────────────────────────────────────────────
    try:
        conn = pymysql.connect(
            host=args.mariadb_host,
            port=args.mariadb_port,
            user=args.mariadb_user,
            password=args.mariadb_password,
            database=args.mariadb_db,
            cursorclass=pymysql.cursors.DictCursor,
            connect_timeout=5,
        )
        print("  MariaDB:  connected ✓")
    except pymysql.Error as e:
        print(f"\n  ✗ Cannot connect to MariaDB: {e}")
        sys.exit(1)

    # ── Verify Arbiter is reachable ─────────────────────────────────────
    try:
        resp = requests.get(f"{args.arbiter_url}/healthz", timeout=5)
        if resp.status_code == 200:
            print("  Arbiter:  reachable ✓")
        else:
            print(f"  ⚠ Arbiter /healthz returned {resp.status_code}")
    except requests.exceptions.ConnectionError:
        print(f"\n  ✗ Cannot connect to Arbiter at {args.arbiter_url}")
        conn.close()
        sys.exit(1)

    print("=" * 70)

    # ── Phase 1: Insert test data directly into MariaDB ──────────────────
    print(f"\n[Phase 1] Inserting test rollup rows into MariaDB...")
    meta = insert_test_data(conn, run_id)
    print(f"  Host:    {meta['host']}")
    print(f"  Bucket:  {meta['bucket']} (epoch)")
    print(f"  IPs:     {meta['ip1']}, {meta['ip2']}, {meta['ip3']}")

    total_passed = 0
    total_failed = 0

    # ── Phase 2: Test endpoints ──────────────────────────────────────────
    print(f"\n[Phase 2] Testing API endpoints...")
    print("-" * 70)

    # Test top-ips
    print("\n  --- top-ips ---")
    p, f, msgs = test_top_ips(args.arbiter_url, meta)
    total_passed += p
    total_failed += f
    for m in msgs:
        print(m)

    # Test top-paths
    print("\n  --- top-paths ---")
    p, f, msgs = test_top_paths(args.arbiter_url, meta)
    total_passed += p
    total_failed += f
    for m in msgs:
        print(m)

    # Test summary
    print("\n  --- summary ---")
    p, f, msgs = test_summary(args.arbiter_url, meta)
    total_passed += p
    total_failed += f
    for m in msgs:
        print(m)

    # Test with explicit end_ts
    print("\n  --- end_ts parameter ---")
    p, f, msgs = test_with_end_ts(args.arbiter_url, meta)
    total_passed += p
    total_failed += f
    for m in msgs:
        print(m)

    # Test validation errors
    print("\n  --- validation errors ---")
    p, f, msgs = test_validation_errors(args.arbiter_url)
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
