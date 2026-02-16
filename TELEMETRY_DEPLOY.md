# Arbiter Telemetry — Deployment Guide

This document covers everything needed to deploy the telemetry pipeline in production. It is intended to be handed to whoever manages the deploy scripts.

---

## Architecture Overview

```
┌─────────────────┐       ┌─────────┐       ┌──────────────────────────┐       ┌──────────┐
│  Arbiter Server  │──────▶│  Redis  │──────▶│  Telemetry Consumer      │──────▶│ MariaDB  │
│  (publisher)     │ XADD  │ Stream  │ XREAD │  (separate binary)       │ UPSERT│ (rollups)│
└────────┬─────────┘       └─────────┘       └──────────────────────────┘       └─────┬────┘
         │                                                                            │
         │  read-only query (Telemetry API)                                           │
         └────────────────────────────────────────────────────────────────────────────┘
```

There are **three components** to deploy:

| # | Component | What | Runs as |
|---|-----------|------|---------|
| 1 | **Arbiter Server** (existing) | Publishes telemetry events to Redis; serves the Telemetry Query API | Existing process — just add new env vars |
| 2 | **Telemetry Consumer** (new) | Reads Redis stream, aggregates into MariaDB 10s rollup tables | **New long-running process** |
| 3 | **Partition Maintenance** (new) | Creates daily partitions, drops expired ones | **Daily cron job** |

You also need two new infrastructure pieces:
- **Redis instance** (for the event stream)
- **MariaDB instance** (for rollup storage)

---

## 1. Infrastructure: Redis

Any Redis ≥ 6.2 (needs Streams + XAUTOCLAIM support).

| Setting | Recommendation |
|---------|---------------|
| Memory | 256 MB is plenty (stream is ephemeral, consumer ACKs quickly) |
| Persistence | `appendonly no` is fine — telemetry is best-effort. RDB snapshots optional. |
| maxmemory-policy | `noeviction` (let the consumer drain the stream) |
| TLS | Not supported by the publisher in Phase 1. Use plain `redis://`. |

The Arbiter server and the Consumer must both be able to reach this Redis instance.

---

## 2. Infrastructure: MariaDB

MariaDB ≥ 10.6 recommended (partition support, `REORGANIZE PARTITION`).

### Database & Users

```sql
CREATE DATABASE IF NOT EXISTS arbiter_telemetry
  CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;

-- Consumer user (read-write, needs ALTER TABLE for migrations)
CREATE USER 'arb_consumer'@'%' IDENTIFIED BY '<STRONG_PASSWORD>';
GRANT ALL PRIVILEGES ON arbiter_telemetry.* TO 'arb_consumer'@'%';

-- API user (read-only, used by Arbiter server's Telemetry Query API)
CREATE USER 'arb_readonly'@'%' IDENTIFIED BY '<STRONG_PASSWORD>';
GRANT SELECT ON arbiter_telemetry.* TO 'arb_readonly'@'%';

-- Partition maintenance user (needs ALTER TABLE to add/drop partitions)
CREATE USER 'arb_maint'@'%' IDENTIFIED BY '<STRONG_PASSWORD>';
GRANT ALTER, SELECT ON arbiter_telemetry.* TO 'arb_maint'@'%';

FLUSH PRIVILEGES;
```

> You can combine consumer + maint into one user if you prefer. The API user **should** be read-only for defense in depth.

### Schema

Tables are created automatically by the consumer on first boot via `golang-migrate`. The migration files live in `arbiter/db/migrations/`. The consumer binary looks for them in `../db/migrations/` relative to the binary, or `./db/migrations/` relative to cwd.

**No manual DDL needed** — just make sure the migration files are deployed alongside the consumer binary (see directory layout below).

---

## 3. Component: Arbiter Server (existing process)

### New Environment Variables

Add these to the existing Arbiter server process:

#### Publisher (writes events to Redis)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ARB_TELEMETRY_ENABLED` | Yes | `false` | Set to `true` to enable telemetry publishing |
| `ARB_TELEMETRY_REDIS_URL` | Yes (when enabled) | `redis://localhost:6379/0` | Redis connection URL |
| `ARB_TELEMETRY_STREAM_KEY` | No | `arb:v1:events` | Redis stream key name |
| `ARB_TELEMETRY_TIMEOUT_MS` | No | `25` | Max time per XADD call (ms). Keep low — fires on every request. |
| `ARB_TELEMETRY_BUFFER_SIZE` | No | `8192` | Internal channel buffer. Events dropped silently when full. |

#### Query API (reads from MariaDB, serves to frontend)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ARB_TELEMETRY_API_ENABLED` | Yes | `false` | Set to `true` to mount `/api/v1/telemetry/*` routes |
| `ARB_TELEMETRY_API_DB_DSN` | Yes (when enabled) | — | MariaDB DSN for the **read-only** user. Format: `arb_readonly:PASSWORD@tcp(HOST:3306)/arbiter_telemetry` |
| `ARB_TELEMETRY_API_MAX_WINDOW_MINUTES` | No | `60` | Maximum query window (minutes) |
| `ARB_TELEMETRY_API_MAX_LIMIT` | No | `100` | Maximum `limit` parameter |
| `ARB_TELEMETRY_API_TRUST_PROXY_HEADERS` | No | `true` | Use `X-Forwarded-For` / `X-Real-IP` for rate limiting |

> **Graceful degradation**: If Redis is unreachable at startup, the publisher silently falls back to a no-op. The server always starts. If the telemetry API DB is unreachable, the server **will fail to start** (since you explicitly opted in with `ARB_TELEMETRY_API_ENABLED=true`).

### Example additions to existing env

```bash
# --- Telemetry Publisher ---
ARB_TELEMETRY_ENABLED=true
ARB_TELEMETRY_REDIS_URL=redis://your-redis-host:6379/0

# --- Telemetry Query API ---
ARB_TELEMETRY_API_ENABLED=true
ARB_TELEMETRY_API_DB_DSN="arb_readonly:PASSWORD@tcp(your-mariadb-host:3306)/arbiter_telemetry"
```

`parseTime=true&loc=UTC` will be auto-appended to the DSN if missing.

---

## 4. Component: Telemetry Consumer (new process)

### Binary

Build from the arbiter repo:

```bash
cd arbiter
go build -o arbiter-telemetry-consumer ./cmd/arbiter-telemetry-consumer
```

This produces a single static binary: `arbiter-telemetry-consumer`.

### Deployment Directory Layout

The consumer needs its migration files at startup. Deploy like this:

```
/opt/arbiter/                         # or wherever you deploy
├── arbiter                           # existing server binary
├── arbiter-telemetry-consumer        # NEW consumer binary
└── db/
    └── migrations/
        └── 000001_create_rollup_tables.up.sql
        └── 000001_create_rollup_tables.down.sql
```

The binary searches for migrations in:
1. `../db/migrations/` relative to the binary location
2. `./db/migrations/` relative to the working directory

> **Migrations are idempotent** — safe to run on every boot. They use `CREATE TABLE IF NOT EXISTS`.

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ARB_TELEMETRY_CONSUMER_ENABLED` | **Yes** | `false` | Must be `true` or the process exits immediately |
| `ARB_TELEMETRY_REDIS_URL` | Yes | `redis://localhost:6379/0` | Same Redis instance as the Arbiter publisher |
| `ARB_TELEMETRY_DB_DSN` | **Yes** | — | MariaDB DSN for the **read-write** consumer user. Format: `arb_consumer:PASSWORD@tcp(HOST:3306)/arbiter_telemetry` |
| `ARB_TELEMETRY_STREAM_KEY` | No | `arb:v1:events` | Must match the publisher's stream key |
| `ARB_TELEMETRY_CONSUMER_GROUP` | No | `arbiter-telemetry-v1` | Redis consumer group name |
| `ARB_TELEMETRY_CONSUMER_NAME` | No | `hostname-PID` | Unique consumer name (auto-generated if unset) |
| `ARB_TELEMETRY_GROUP_START_ID` | No | `$` | `$` = only new messages; `0` = replay from beginning |
| `ARB_TELEMETRY_READ_COUNT` | No | `1000` | Max messages per XREADGROUP call |
| `ARB_TELEMETRY_BLOCK_MS` | No | `200` | XREADGROUP block timeout (ms) |
| `ARB_TELEMETRY_FLUSH_MS` | No | `1000` | Buffer flush interval (ms). Lower = fresher data, more DB writes. |
| `ARB_TELEMETRY_PATH_CAP` | No | `50` | Max unique paths per (host, ip, bucket) tracked per flush. Prevents memory blowup from path enumeration attacks. |
| `ARB_TELEMETRY_RETENTION_DAYS` | No | `7` | Informational only (used by the partition maintenance cron) |
| `ARB_TELEMETRY_PEL_CLAIM_ENABLED` | No | `false` | Enable XAUTOCLAIM of stale PEL entries on startup |
| `ARB_TELEMETRY_PEL_IDLE_MS` | No | `300000` | Min idle time (ms) for PEL entries to be claimed (5 min default) |
| `ARB_TELEMETRY_PEL_CLAIM_COUNT` | No | `1000` | Max PEL entries to claim per batch |

### Example systemd unit

```ini
[Unit]
Description=Arbiter Telemetry Consumer
After=network.target
Wants=redis.service mariadb.service

[Service]
Type=simple
User=arbiter
WorkingDirectory=/opt/arbiter
ExecStart=/opt/arbiter/arbiter-telemetry-consumer
Restart=always
RestartSec=5

Environment=ARB_TELEMETRY_CONSUMER_ENABLED=true
Environment=ARB_TELEMETRY_REDIS_URL=redis://your-redis-host:6379/0
Environment=ARB_TELEMETRY_DB_DSN=arb_consumer:PASSWORD@tcp(your-mariadb-host:3306)/arbiter_telemetry
Environment=ARB_TELEMETRY_FLUSH_MS=1000
Environment=ARB_TELEMETRY_PATH_CAP=50

# Logging is JSON to stdout, let journald capture it
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

### Behavior

- On startup: connects to Redis, connects to MariaDB, runs migrations, creates consumer group (idempotent), optionally claims stale PEL entries, then enters read loop.
- Steady state: reads batches from Redis stream → buffers in memory → flushes aggregated 10s rollups to MariaDB every `FLUSH_MS` → ACKs messages.
- On SIGTERM/SIGINT: graceful shutdown (stops reading, flushes remaining buffer, exits).
- If Redis or MariaDB becomes unreachable during operation, the consumer logs errors and retries on the next loop iteration. It does **not** crash.

### Scaling

- You can run **multiple consumer instances** in the same consumer group. Redis will distribute messages across them. Each instance should have a unique `ARB_TELEMETRY_CONSUMER_NAME`.
- For most deployments, **a single consumer is sufficient**. It handles tens of thousands of events/second.

---

## 5. Component: Partition Maintenance Cron (new)

### What it does

MariaDB tables use `PARTITION BY RANGE (bucket_start)` — one partition per UTC day. The cron job:

1. **Creates** tomorrow's partition (and today's, for first-run safety).
2. **Drops** partitions older than `RETENTION_DAYS` (default 7).

Dropping a partition is an instant DDL operation (`O(1)`) — far faster than `DELETE FROM ... WHERE bucket_start < X`.

### The script

Located at `arbiter/scripts/telemetry_partitions.sh`. It is **idempotent** — safe to run multiple times. If a partition already exists, the REORGANIZE fails harmlessly and logs a warning.

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MYSQL_HOST` | No | `127.0.0.1` | MariaDB host |
| `MYSQL_PORT` | No | `3306` | MariaDB port |
| `MYSQL_USER` | No | `root` | MariaDB user (needs `ALTER TABLE` privilege) |
| `MYSQL_ROOT_PASSWORD` | **Yes** | — | Password for the maintenance user |
| `MYSQL_DATABASE` | No | `arbiter_telemetry` | Database name |
| `RETENTION_DAYS` | No | `7` | How many days of data to keep |

### Cron entry

```cron
# Run daily at 00:05 UTC — creates tomorrow's partition, drops expired ones
5 0 * * * MYSQL_HOST=your-mariadb-host MYSQL_ROOT_PASSWORD=PASSWORD RETENTION_DAYS=7 /opt/arbiter/scripts/telemetry_partitions.sh >> /var/log/arbiter-partitions.log 2>&1
```

Or as a systemd timer:

```ini
# /etc/systemd/system/arbiter-partitions.timer
[Unit]
Description=Arbiter telemetry partition maintenance (daily)

[Timer]
OnCalendar=*-*-* 00:05:00 UTC
Persistent=true

[Install]
WantedBy=timers.target
```

```ini
# /etc/systemd/system/arbiter-partitions.service
[Unit]
Description=Arbiter telemetry partition maintenance

[Service]
Type=oneshot
User=arbiter
WorkingDirectory=/opt/arbiter

Environment=MYSQL_HOST=your-mariadb-host
Environment=MYSQL_ROOT_PASSWORD=PASSWORD
Environment=MYSQL_DATABASE=arbiter_telemetry
Environment=RETENTION_DAYS=7

ExecStart=/opt/arbiter/scripts/telemetry_partitions.sh
```

### First Deploy

On the **very first deployment**, you must run the partition script once manually (or let the cron run) to create today's partition. Until that happens, all data lands in `p_future` (which works, but you can't drop old data by partition).

```bash
MYSQL_HOST=your-mariadb-host \
MYSQL_ROOT_PASSWORD=PASSWORD \
bash /opt/arbiter/scripts/telemetry_partitions.sh
```

---

## 6. Deploy Checklist

```
[ ] 1. Provision Redis instance
[ ] 2. Provision MariaDB instance
[ ] 3. Create MariaDB database + users (consumer r/w, API r/o, maint ALTER)
[ ] 4. Deploy consumer binary + db/migrations/ directory
[ ] 5. Deploy partition maintenance script (scripts/telemetry_partitions.sh)
[ ] 6. Add telemetry env vars to Arbiter server process
[ ] 7. Start consumer process (systemd / supervisor / etc.)
[ ] 8. Run partition maintenance script once manually (first deploy only)
[ ] 9. Install daily cron / systemd timer for partition maintenance
[ ] 10. Restart Arbiter server with new env vars
[ ] 11. Verify: visit /telemetry in the Arbiter UI
```

---

## 7. Quick Env Reference (all vars in one place)

### Arbiter Server Process

```bash
# Publisher (to Redis)
ARB_TELEMETRY_ENABLED=true
ARB_TELEMETRY_REDIS_URL=redis://REDIS_HOST:6379/0
ARB_TELEMETRY_STREAM_KEY=arb:v1:events          # optional
ARB_TELEMETRY_TIMEOUT_MS=25                      # optional
ARB_TELEMETRY_BUFFER_SIZE=8192                   # optional

# Query API (from MariaDB)
ARB_TELEMETRY_API_ENABLED=true
ARB_TELEMETRY_API_DB_DSN=arb_readonly:PASS@tcp(MARIADB_HOST:3306)/arbiter_telemetry
ARB_TELEMETRY_API_MAX_WINDOW_MINUTES=60          # optional
ARB_TELEMETRY_API_MAX_LIMIT=100                  # optional
ARB_TELEMETRY_API_TRUST_PROXY_HEADERS=true       # optional
```

### Consumer Process

```bash
ARB_TELEMETRY_CONSUMER_ENABLED=true
ARB_TELEMETRY_REDIS_URL=redis://REDIS_HOST:6379/0
ARB_TELEMETRY_DB_DSN=arb_consumer:PASS@tcp(MARIADB_HOST:3306)/arbiter_telemetry
ARB_TELEMETRY_STREAM_KEY=arb:v1:events           # optional, must match publisher
ARB_TELEMETRY_CONSUMER_GROUP=arbiter-telemetry-v1 # optional
ARB_TELEMETRY_FLUSH_MS=1000                      # optional
ARB_TELEMETRY_PATH_CAP=50                        # optional
```

### Partition Cron

```bash
MYSQL_HOST=MARIADB_HOST
MYSQL_PORT=3306
MYSQL_USER=arb_maint        # or root
MYSQL_ROOT_PASSWORD=PASS
MYSQL_DATABASE=arbiter_telemetry
RETENTION_DAYS=7
```

---

## 8. Monitoring Notes

- **Consumer health**: The consumer logs JSON to stdout. Key log lines:
  - `"telemetry consumer started"` — healthy start
  - `"flush complete"` — successful DB write (includes `rows` count)
  - `"XADD failed"` / `"flush failed"` — transient errors (auto-retries)
- **Redis stream length**: Monitor `XLEN arb:v1:events`. If it grows unbounded, the consumer is falling behind or down.
- **MariaDB partition count**: `SELECT PARTITION_NAME FROM INFORMATION_SCHEMA.PARTITIONS WHERE TABLE_NAME = 'arb_host_ip_10s';` should show ~7-8 partitions (today + tomorrow + retention days).
- **Publisher drops**: The Arbiter server logs `"telemetry: event channel full, dropping events"` if the publisher buffer overflows (meaning Redis writes are too slow). Increase `ARB_TELEMETRY_BUFFER_SIZE` or check Redis latency.
