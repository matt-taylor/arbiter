#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# telemetry_partitions.sh
#
# Partition maintenance for the Arbiter telemetry rollup tables.
# Intended to be run daily via cron / systemd timer with privileged MariaDB
# credentials (ALTER TABLE privileges).
#
# What it does:
#   1. Creates tomorrow's partition (REORGANIZE p_future) for both tables.
#   2. Also ensures today's partition exists (for first-run scenarios).
#   3. Drops partitions older than RETENTION_DAYS.
#
# Idempotent: if a partition already exists, REORGANIZE will fail harmlessly
# (logged as a warning). DROP of a non-existent partition is also caught.
#
# Environment variables (or override on CLI):
#   MYSQL_HOST          default: 127.0.0.1
#   MYSQL_PORT          default: 3306
#   MYSQL_USER          default: root
#   MYSQL_ROOT_PASSWORD required
#   MYSQL_DATABASE      default: arbiter_telemetry
#   RETENTION_DAYS      default: 7
# ---------------------------------------------------------------------------
set -euo pipefail

MYSQL_HOST="${MYSQL_HOST:-127.0.0.1}"
MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_USER="${MYSQL_USER:-root}"
MYSQL_PASSWORD="${MYSQL_ROOT_PASSWORD:?MYSQL_ROOT_PASSWORD is required}"
MYSQL_DATABASE="${MYSQL_DATABASE:-arbiter_telemetry}"
RETENTION_DAYS="${RETENTION_DAYS:-7}"

TABLES=("arb_host_ip_10s" "arb_host_ip_path_10s")

mysql_exec() {
  if command -v mariadb >/dev/null 2>&1; then
    mariadb -h "$MYSQL_HOST" -P "$MYSQL_PORT" -u "$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE" -e "$1" 2>&1
  elif command -v mysql >/dev/null 2>&1; then
    mysql -h "$MYSQL_HOST" -P "$MYSQL_PORT" -u "$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE" -e "$1" 2>&1
  else
    # Fall back to running inside the Docker container (use localhost:3306 inside)
    docker compose -f docker-compose.dev.yml exec -T mariadb \
      mariadb -h localhost -P 3306 -u "$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE" -e "$1" 2>&1
  fi
}

# ---------------------------------------------------------------------------
# Helper: compute epoch seconds for UTC midnight of a given date
# Works on both GNU date (Linux) and BSD date (macOS).
# ---------------------------------------------------------------------------
utc_midnight_epoch() {
  local datestr="$1"
  if date --version >/dev/null 2>&1; then
    # GNU date
    date -u -d "${datestr}" +%s
  else
    # BSD date (macOS)
    date -u -j -f "%Y-%m-%d" "${datestr}" +%s
  fi
}

# ---------------------------------------------------------------------------
# Create a partition for a specific date if it doesn't already exist.
# Partition p_YYYYMMDD holds data for that UTC day.
# Its boundary = midnight of the NEXT day (epoch int).
# ---------------------------------------------------------------------------
create_partition() {
  local table="$1"
  local target_date="$2"  # YYYY-MM-DD

  local part_name
  part_name="p_$(echo "$target_date" | tr -d '-')"

  # Boundary = next day's midnight epoch
  local next_day
  if date --version >/dev/null 2>&1; then
    next_day=$(date -u -d "${target_date} + 1 day" +%Y-%m-%d)
  else
    next_day=$(date -u -j -v+1d -f "%Y-%m-%d" "${target_date}" +%Y-%m-%d)
  fi

  local boundary
  boundary=$(utc_midnight_epoch "$next_day")

  local sql="ALTER TABLE ${table} REORGANIZE PARTITION p_future INTO (
    PARTITION ${part_name} VALUES LESS THAN (${boundary}),
    PARTITION p_future VALUES LESS THAN MAXVALUE
  );"

  echo "[INFO] Creating partition ${part_name} on ${table} (boundary=${boundary})"
  if ! mysql_exec "$sql"; then
    echo "[WARN] Partition ${part_name} on ${table} may already exist (continuing)"
  fi
}

# ---------------------------------------------------------------------------
# Drop a partition for a specific date.
# ---------------------------------------------------------------------------
drop_partition() {
  local table="$1"
  local target_date="$2"  # YYYY-MM-DD

  local part_name
  part_name="p_$(echo "$target_date" | tr -d '-')"

  local sql="ALTER TABLE ${table} DROP PARTITION ${part_name};"

  echo "[INFO] Dropping partition ${part_name} from ${table}"
  if ! mysql_exec "$sql"; then
    echo "[WARN] Partition ${part_name} on ${table} may not exist (continuing)"
  fi
}

# ---------------------------------------------------------------------------
# Main logic
# ---------------------------------------------------------------------------

echo "=== Telemetry Partition Maintenance ==="
echo "Database: ${MYSQL_DATABASE} @ ${MYSQL_HOST}:${MYSQL_PORT}"
echo "Retention: ${RETENTION_DAYS} days"
echo ""

# Compute today and tomorrow
if date --version >/dev/null 2>&1; then
  TODAY=$(date -u +%Y-%m-%d)
  TOMORROW=$(date -u -d "+1 day" +%Y-%m-%d)
else
  TODAY=$(date -u +%Y-%m-%d)
  TOMORROW=$(date -u -j -v+1d +%Y-%m-%d)
fi

echo "Today:    ${TODAY}"
echo "Tomorrow: ${TOMORROW}"
echo ""

# --- Create today's + tomorrow's partitions ---
for TABLE in "${TABLES[@]}"; do
  create_partition "$TABLE" "$TODAY"
  create_partition "$TABLE" "$TOMORROW"
done

echo ""

# --- Drop expired partitions (older than RETENTION_DAYS) ---
for i in $(seq "$RETENTION_DAYS" 30); do
  if date --version >/dev/null 2>&1; then
    DROP_DATE=$(date -u -d "-${i} days" +%Y-%m-%d)
  else
    DROP_DATE=$(date -u -j -v-"${i}"d +%Y-%m-%d)
  fi

  for TABLE in "${TABLES[@]}"; do
    drop_partition "$TABLE" "$DROP_DATE"
  done
done

echo ""
echo "=== Partition maintenance complete ==="
