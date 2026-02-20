# Arbiter Reference

## Backend API Routes

All API routes are prefixed with `/api/v1` and served by the Arbiter Go binary. Middleware applied to all routes: `RequestID`, `RealIP`, `Recoverer`, `RequestLogger`, `AccessLogger`.

### Core Routes

#### `GET /api/v1/check`

**Purpose:** NGINX `auth_request` endpoint. This is the hot path — every proxied request hits this route. The decision engine evaluates whether to allow, deny, or redirect the request.

**Input:** HTTP headers forwarded by NGINX:
| Header | Description |
|--------|-------------|
| `X-Original-Host` | The host the client is requesting |
| `X-Original-Method` | HTTP method (GET, POST, etc.) |
| `X-Original-Uri` | Original URI (path + query) |
| `X-Policy-Host` | (Optional) Override host for policy lookup |
| `Cookie` (gk_sid, gk_csrf) | Session cookies forwarded for Gatekeeper auth |

**Response:** JSON body + response headers. HTTP status is the decision status (200, 401, 403, etc.).

Response headers set:
- `X-Auth-Decision` — `allow`, `unauth`, `forbid`, `killswitch`, `error`
- `X-Auth-Reason` — human-readable reason
- `X-Auth-Source` — `host`, `none`, `forced`
- `X-Auth-Policy` — e.g. `host:example.com` or `none`
- `X-Auth-Trace` — UUID trace ID
- `X-Arbiter-Latency-T` — total latency in ms
- `X-Arbiter-Latency-KS` — killswitch check latency (if applicable)
- `X-Arbiter-Latency-GK` — gatekeeper check latency (if applicable)
- Identity headers (`X-Identity-*`) — forwarded from Gatekeeper if present
- Killswitch headers — forwarded from Killswitch if present

**Side effect:** Emits a telemetry event to the Redis Stream (if telemetry is enabled).

---

#### `GET /api/v1/policies`

**Purpose:** List all host policies.

**Params:** None.

**Response:** JSON array of `HostPolicy` objects.

---

#### `POST /api/v1/policies`

**Purpose:** Create a new host policy.

**Body (JSON):**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `host` | string | yes | Hostname (lowercased, trimmed) |
| `killswitch_required` | bool | yes | Whether killswitch check is required |
| `gatekeeper_required` | bool | yes | Whether gatekeeper auth is required |
| `notes` | string | no | Optional notes |

**Response:** `201 Created` with the created `HostPolicy` JSON.

**Errors:** `409 Conflict` if a policy already exists for the host.

---

#### `GET /api/v1/policies/{id}`

**Purpose:** Get a single policy by ID.

**URL params:** `id` (integer) — policy ID.

**Response:** `HostPolicy` JSON. `404` if not found.

---

#### `PATCH /api/v1/policies/{id}`

**Purpose:** Update an existing policy. Managed policies (from packs) cannot be edited.

**URL params:** `id` (integer) — policy ID.

**Body (JSON):** Same fields as create (all optional except `host`).

**Response:** Updated `HostPolicy` JSON. `409 Conflict` if the policy is managed by a pack. `404` if not found.

---

#### `DELETE /api/v1/policies/{id}`

**Purpose:** Delete a policy. Managed policies cannot be deleted.

**URL params:** `id` (integer) — policy ID.

**Response:** `204 No Content`. `409 Conflict` if managed. `404` if not found.

---

#### `GET /api/v1/effective`

**Purpose:** Resolve the effective policy for a given host (what would actually apply at runtime, including forced constraints).

**Query params:**
| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `host` | string | yes | Hostname to look up |

**Response (JSON):**
| Field | Type | Description |
|-------|------|-------------|
| `host` | string | Normalized (lowercased) host |
| `killswitch_required` | bool | Effective killswitch requirement |
| `gatekeeper_required` | bool | Effective gatekeeper requirement |
| `source` | string | `host`, `none`, or `forced` |
| `forced_killswitch` | bool | True if killswitch is force-disabled (anti-recursion) |
| `forced_gatekeeper` | bool | True if gatekeeper is force-disabled (anti-recursion) |
| `killswitch_public_host` | string | Killswitch hostname (if configured) |
| `gatekeeper_public_host` | string | Gatekeeper hostname (if configured) |

---

#### `POST /api/v1/test/check`

**Purpose:** UI testing endpoint. Simulates a `/check` call without actually being called by NGINX. Always returns HTTP 200 with the decision in the JSON body.

**Body (JSON):**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `host` | string | yes | Host to check (www. prefix stripped automatically) |
| `method` | string | yes | HTTP method |
| `uri` | string | yes | Request URI |

**Response (JSON):** Full decision details including `decision`, `status`, `reason`, `source`, `policy`, `trace_id`, `normalized` values, latency breakdown, `nginx_headers`, `identity_headers`, `killswitch_headers`.

---

### Telemetry Query Routes

These routes are **optional** — they only exist when `ARB_TELEMETRY_API_ENABLED=true` and the MariaDB connection is configured. They are rate-limited (10 req/s per IP, burst 20).

#### `GET /api/v1/telemetry/hosts/{host}/summary`

**Purpose:** Aggregate traffic summary for a host within a time window.

**URL params:** `host` — hostname (validated, normalized).

**Query params:**
| Param | Type | Default | Max | Description |
|-------|------|---------|-----|-------------|
| `window_minutes` | int | 5 | 60 | Time window in minutes |
| `end_ts` | int | now | now+60 | End timestamp (epoch seconds) |

**Response (JSON):**
| Field | Type | Description |
|-------|------|-------------|
| `host` | string | Normalized host |
| `window_minutes` | int | Applied window |
| `start_ts` | int | 10s-aligned start timestamp |
| `end_ts` | int | 10s-aligned end timestamp |
| `total` | int | Total requests |
| `unique_ips` | int | Unique client IPs |
| `status` | object | `{ c_401, c_403, c_404, c_429, c_5xx }` |

---

#### `GET /api/v1/telemetry/hosts/{host}/top-ips`

**Purpose:** Top client IPs by request count for a host.

**URL params:** `host` — hostname.

**Query params:**
| Param | Type | Default | Max | Description |
|-------|------|---------|-----|-------------|
| `window_minutes` | int | 5 | 60 | Time window |
| `limit` | int | 20 | 100 | Max results |
| `end_ts` | int | now | now+60 | End timestamp |

**Response (JSON):**
| Field | Type | Description |
|-------|------|-------------|
| `host` | string | Normalized host |
| `window_minutes` | int | Applied window |
| `start_ts` | int | Start timestamp |
| `end_ts` | int | End timestamp |
| `items` | array | `[{ ip, total, status: { c_401, c_403, c_404, c_429, c_5xx } }]` |

---

#### `GET /api/v1/telemetry/hosts/{host}/ips/{ip}/top-paths`

**Purpose:** Top request paths for a specific IP on a host.

**URL params:** `host` — hostname, `ip` — client IP (validated via `net.ParseIP`).

**Query params:**
| Param | Type | Default | Max | Description |
|-------|------|---------|-----|-------------|
| `window_minutes` | int | 5 | 60 | Time window |
| `limit` | int | 20 | 100 | Max results |
| `end_ts` | int | now | now+60 | End timestamp |

**Response (JSON):**
| Field | Type | Description |
|-------|------|-------------|
| `host` | string | Normalized host |
| `ip` | string | Canonical IP |
| `window_minutes` | int | Applied window |
| `start_ts` | int | Start timestamp |
| `end_ts` | int | End timestamp |
| `items` | array | `[{ path, total, status: { c_401, c_403, c_404, c_429, c_5xx } }]` |

---

### Telemetry Error Response

All telemetry endpoints return errors as:
```json
{ "error": "message", "request_id": "chi-request-id" }
```

### Health Routes

| Route | Method | Purpose |
|-------|--------|---------|
| `/healthz` | GET | Liveness probe. Always returns `200 OK`. |
| `/readyz` | GET | Readiness probe. Tests DB connectivity. Returns `200 OK` or `503`. |

---

### CLI Subcommands

The `arbiter` binary also supports CLI subcommands (not HTTP):

| Command | Description |
|---------|-------------|
| `arbiter apply-pack --file <path>` | Apply a policy pack YAML file to the database. Managed policies are created/updated. |
| `arbiter nuke-db` | Delete the SQLite database, re-run migrations. Interactive confirmation required. |

---

## Frontend Routes

The Arbiter SPA is a React + TypeScript app using React Router, served from `frontend/dist/` as static files. All routes are within a shared `ArbiterLayout` shell (sidebar nav + mobile nav).

| Route | Page Component | Description |
|-------|---------------|-------------|
| `/` | `PoliciesPage` | CRUD for host policies. Table view with search, pagination, create/edit/delete dialogs. Managed policies are read-only. |
| `/effective` | `EffectivePage` | Look up the effective policy for a given host. Enter a host, see what Arbiter would actually enforce (including forced anti-recursion constraints). |
| `/tester` | `TesterPage` | Simulate an auth check from the UI. Enter host + method + URI, see the full decision result with latency breakdown, NGINX headers, identity headers, etc. |
| `/telemetry` | `TelemetryPage` | Read-only telemetry dashboard. View traffic summary, top IPs, and IP-to-path drilldown for any host. Supports time window selection, auto-refresh, and URL-shareable state. |
| `*` | `NotFound` (redirect) | Catch-all — shows error toast and redirects to `/`. |

---

## Directory Overview

```
arbiter/
├── cmd/
│   ├── arbiter/
│   │   └── main.go                    # Main binary entry point (server + CLI subcommands)
│   └── arbiter-telemetry-consumer/
│       ├── main.go                    # Telemetry consumer binary entry point
│       └── migrations.go             # Embedded MariaDB migrations for consumer
│
├── config/
│   └── packs/
│       └── arbiter.yml               # Default policy pack YAML
│
├── db/
│   └── migrations/
│       └── 000001_create_rollup_tables.*.sql  # MariaDB rollup table schema (telemetry)
│
├── docker-compose.dev.yml            # Dev Redis + MariaDB containers
├── env.example                       # Environment variable reference
│
├── frontend/
│   ├── package.json                  # React 18, react-router-dom, axios, lucide-react, tailwind
│   ├── vite.config.ts                # Vite config (dev server proxy to backend)
│   ├── tailwind.config.js            # Tailwind CSS config
│   └── src/
│       ├── App.tsx                   # Router setup, route definitions
│       ├── main.tsx                  # React DOM entry point
│       ├── index.css                 # Tailwind imports + CSS variables (theme)
│       ├── components/
│       │   ├── ArbiterLayout.tsx     # Shared shell: sidebar, mobile nav, admin site links, logout
│       │   ├── MobileNav.tsx         # Slide-in mobile navigation
│       │   ├── ErrorBoundary.tsx     # React error boundary
│       │   ├── TableCard.tsx         # Reusable card components for policy display
│       │   └── ui/                   # Primitive UI components
│       │       ├── button.tsx        # Button (default/secondary/danger/outline/ghost)
│       │       ├── input.tsx         # Text input with label + error
│       │       ├── select.tsx        # Select dropdown with label + error
│       │       ├── badge.tsx         # Badge (default/success/warning/danger)
│       │       ├── dialog.tsx        # Modal dialog
│       │       ├── loading.tsx       # Spinner
│       │       ├── toast.tsx         # Toast notification
│       │       └── tooltip.tsx       # Hover tooltip
│       ├── contexts/
│       │   ├── ThemeProvider.tsx      # Light/dark/system theme (next-themes)
│       │   └── ToastContext.tsx       # Toast notification system
│       ├── hooks/
│       │   ├── useAuth.ts            # Auth state from Gatekeeper identity headers / whoami
│       │   ├── useTheme.ts           # Theme hook wrapper
│       │   └── useToast.ts           # Toast hook wrapper
│       ├── lib/
│       │   ├── api.ts                # Axios instances (arbiterApi, gatekeeperApi), policies API, error helpers
│       │   ├── telemetryClient.ts    # Telemetry API client (getSummary, getTopIPs, getTopPaths)
│       │   ├── types.ts              # TypeScript types (HostPolicy, EffectivePolicy, TestCheckResponse)
│       │   └── utils.ts              # cn() — clsx + tailwind-merge
│       └── pages/
│           ├── PoliciesPage.tsx      # Policy CRUD (list, create, edit, delete)
│           ├── EffectivePage.tsx      # Effective policy lookup
│           ├── TesterPage.tsx        # Auth check tester
│           └── TelemetryPage.tsx     # Telemetry dashboard
│
├── internal/
│   ├── arbiter/
│   │   ├── engine.go                 # Decision engine (Check method — the core logic)
│   │   └── engine_test.go
│   ├── config/
│   │   └── config.go                 # All config loading from env vars
│   ├── downstream/
│   │   ├── client.go                 # HTTP client for Killswitch + Gatekeeper checks
│   │   └── client_test.go
│   ├── httpserver/
│   │   ├── server.go                 # Chi router setup, route registration
│   │   ├── handlers.go               # Core API handlers (check, policies CRUD, effective, test)
│   │   ├── telemetry_handlers.go     # Telemetry query API handlers (summary, top-ips, top-paths)
│   │   ├── ratelimit.go              # Per-IP token bucket rate limiter for telemetry API
│   │   ├── middleware.go             # Request + access logging middleware
│   │   ├── clientip.go               # Client IP extraction (X-Real-IP, X-Forwarded-For, RemoteAddr)
│   │   └── *_test.go                 # Tests
│   ├── pack/
│   │   ├── schema.go                 # PolicyPack / Policy YAML schema types
│   │   ├── parser.go                 # YAML parser
│   │   ├── expander.go               # Expand policies across common_domains
│   │   ├── applier.go                # Apply expanded policies to database (create/update/orphan)
│   │   └── applier_test.go
│   ├── policycache/
│   │   ├── cache.go                  # In-memory policy cache with TTL
│   │   └── cache_test.go
│   ├── store/
│   │   └── store.go                  # SQLite store (HostPolicy CRUD, Store interface)
│   └── telemetry/
│       ├── event.go                  # Event struct + wire format (Redis Stream payload)
│       ├── normalize.go              # Host + path normalization (lowercasing, stripping www.)
│       ├── publisher.go              # RedisPublisher (buffered channel → XADD) + NoopPublisher
│       ├── consumer/
│       │   ├── config.go             # Consumer-specific config (from env)
│       │   ├── consumer.go           # Redis Stream consumer (XREADGROUP loop, flush cycle)
│       │   ├── aggregator.go         # In-memory 10s-bucket aggregation
│       │   ├── rollupdb.go           # MariaDB upsert writer (host_ip + host_ip_path tables)
│       │   └── *_test.go             # Tests
│       └── query/
│           ├── models.go             # IPRow, PathRow, SummaryRow types
│           ├── validate.go           # ParseAndValidate — input validation + normalization
│           ├── repository.go         # Read-only MariaDB queries (TopIPs, TopPaths, Summary)
│           └── *_test.go             # Tests
│
├── migrations/
│   ├── 000001_create_host_policies.up.sql    # SQLite host_policies table
│   ├── 000001_create_host_policies.down.sql
│   ├── 000002_add_managed_fields_to_host_policies.up.sql  # Managed pack columns
│   └── 000002_add_managed_fields_to_host_policies.down.sql
│
├── scripts/
│   ├── telemetry_partitions.sh       # MariaDB partition maintenance (create daily partitions)
│   ├── test_telemetry.py             # Phase 1 telemetry publisher test
│   ├── test_telemetry_consumer.py    # Phase 2 consumer E2E test (Redis → MariaDB)
│   └── test_telemetry_api.py         # Phase 3 API E2E test (HTTP → MariaDB → JSON)
│
├── Makefile                          # Build, run, test, telemetry, frontend tasks
├── go.mod / go.sum                   # Go module
└── README.md
```

## SQL Schema

### SQLite — `host_policies` (Policy Store)

Migration `000001_create_host_policies.up.sql`:

```sql
CREATE TABLE host_policies (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  host TEXT NOT NULL UNIQUE,
  killswitch_required INTEGER NOT NULL,
  gatekeeper_required INTEGER NOT NULL,
  notes TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX idx_host_policies_host ON host_policies(host);
```

Migration `000002_add_managed_fields_to_host_policies.up.sql`:

```sql
ALTER TABLE host_policies ADD COLUMN managed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE host_policies ADD COLUMN managed_pack TEXT NULL;
ALTER TABLE host_policies ADD COLUMN managed_key TEXT NULL;
ALTER TABLE host_policies ADD COLUMN managed_version INTEGER NULL;
ALTER TABLE host_policies ADD COLUMN managed_name TEXT NULL;
ALTER TABLE host_policies ADD COLUMN managed_description TEXT NULL;
ALTER TABLE host_policies ADD COLUMN managed_at TEXT NULL;

CREATE INDEX idx_host_policies_managed_pack ON host_policies(managed_pack);
```

**Final schema:**

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Auto-increment row ID |
| `host` | TEXT UNIQUE | Hostname (e.g. `example.com`) |
| `killswitch_required` | INTEGER (bool) | 1 = killswitch check required |
| `gatekeeper_required` | INTEGER (bool) | 1 = gatekeeper auth required |
| `notes` | TEXT | Optional human notes |
| `created_at` | TEXT | ISO 8601 timestamp |
| `updated_at` | TEXT | ISO 8601 timestamp |
| `managed` | INTEGER (bool) | 1 = created/owned by a policy pack |
| `managed_pack` | TEXT | Pack name (e.g. `arbiter`) |
| `managed_key` | TEXT | Key within the pack |
| `managed_version` | INTEGER | Pack version |
| `managed_name` | TEXT | Human-readable policy name from pack |
| `managed_description` | TEXT | Description from pack |
| `managed_at` | TEXT | ISO 8601 timestamp of last pack apply |

---

### MariaDB — Telemetry Rollup Tables

Migration `000001_create_rollup_tables.up.sql`:

Tables are partitioned by `bucket_start` using `PARTITION BY RANGE`. A single `p_future` catchall partition is created at table creation time. The script `scripts/telemetry_partitions.sh` must be run to create daily partitions.

#### `arb_host_ip_10s` — Per-host, per-IP 10-second aggregation

```sql
CREATE TABLE IF NOT EXISTS arb_host_ip_10s (
  bucket_start INT UNSIGNED NOT NULL,
  host         VARCHAR(253) NOT NULL,
  ip           VARCHAR(45)  NOT NULL,
  total        INT UNSIGNED NOT NULL DEFAULT 0,
  c_401        INT UNSIGNED NOT NULL DEFAULT 0,
  c_403        INT UNSIGNED NOT NULL DEFAULT 0,
  c_404        INT UNSIGNED NOT NULL DEFAULT 0,
  c_429        INT UNSIGNED NOT NULL DEFAULT 0,
  c_5xx        INT UNSIGNED NOT NULL DEFAULT 0,
  m_get        INT UNSIGNED NOT NULL DEFAULT 0,
  m_post       INT UNSIGNED NOT NULL DEFAULT 0,
  m_put        INT UNSIGNED NOT NULL DEFAULT 0,
  m_patch      INT UNSIGNED NOT NULL DEFAULT 0,
  m_delete     INT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (bucket_start, host, ip),
  INDEX idx_host_bucket (host, bucket_start)
)
ENGINE=InnoDB
PARTITION BY RANGE (bucket_start) (
  PARTITION p_future VALUES LESS THAN MAXVALUE
);
```

| Column | Type | Description |
|--------|------|-------------|
| `bucket_start` | INT UNSIGNED | 10-second-aligned epoch timestamp (floor to nearest 10s) |
| `host` | VARCHAR(253) | Normalized hostname |
| `ip` | VARCHAR(45) | Client IP (IPv4 or IPv6) |
| `total` | INT UNSIGNED | Total request count in this bucket |
| `c_401` | INT UNSIGNED | Count of 401 responses |
| `c_403` | INT UNSIGNED | Count of 403 responses |
| `c_404` | INT UNSIGNED | Count of 404 responses |
| `c_429` | INT UNSIGNED | Count of 429 responses |
| `c_5xx` | INT UNSIGNED | Count of 5xx responses |
| `m_get` | INT UNSIGNED | Count of GET requests |
| `m_post` | INT UNSIGNED | Count of POST requests |
| `m_put` | INT UNSIGNED | Count of PUT requests |
| `m_patch` | INT UNSIGNED | Count of PATCH requests |
| `m_delete` | INT UNSIGNED | Count of DELETE requests |

**Primary key:** `(bucket_start, host, ip)` — one row per 10-second window per host per IP.

---

#### `arb_host_ip_path_10s` — Per-host, per-IP, per-path 10-second aggregation

```sql
CREATE TABLE IF NOT EXISTS arb_host_ip_path_10s (
  bucket_start INT UNSIGNED NOT NULL,
  host         VARCHAR(253) NOT NULL,
  ip           VARCHAR(45)  NOT NULL,
  path_hash    BINARY(16)   NOT NULL COMMENT 'MD5 of normalized path',
  path         VARCHAR(2048) NOT NULL,
  total        INT UNSIGNED NOT NULL DEFAULT 0,
  c_401        INT UNSIGNED NOT NULL DEFAULT 0,
  c_403        INT UNSIGNED NOT NULL DEFAULT 0,
  c_404        INT UNSIGNED NOT NULL DEFAULT 0,
  c_429        INT UNSIGNED NOT NULL DEFAULT 0,
  c_5xx        INT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (bucket_start, host, ip, path_hash),
  INDEX idx_host_ip_bucket (host, ip, bucket_start)
)
ENGINE=InnoDB
PARTITION BY RANGE (bucket_start) (
  PARTITION p_future VALUES LESS THAN MAXVALUE
);
```

| Column | Type | Description |
|--------|------|-------------|
| `bucket_start` | INT UNSIGNED | 10-second-aligned epoch timestamp |
| `host` | VARCHAR(253) | Normalized hostname |
| `ip` | VARCHAR(45) | Client IP |
| `path_hash` | BINARY(16) | MD5 hash of the normalized request path (used for PK deduplication) |
| `path` | VARCHAR(2048) | The actual normalized request path |
| `total` | INT UNSIGNED | Total request count |
| `c_401` | INT UNSIGNED | Count of 401 responses |
| `c_403` | INT UNSIGNED | Count of 403 responses |
| `c_404` | INT UNSIGNED | Count of 404 responses |
| `c_429` | INT UNSIGNED | Count of 429 responses |
| `c_5xx` | INT UNSIGNED | Count of 5xx responses |

**Primary key:** `(bucket_start, host, ip, path_hash)` — one row per 10-second window per host per IP per unique path.

**Note:** This table does not include method breakdown columns (`m_get`, etc.) unlike `arb_host_ip_10s`.

---

## Environment Variables

### Arbiter Server (required)
| Variable | Default | Description |
|----------|---------|-------------|
| `ARBITER_BIND_ADDR` | — | Listen address (e.g. `127.0.0.1:9100`) |
| `DATABASE_URL` | — | SQLite URL (e.g. `sqlite:///tmp/arbiter.db`) |
| `KILLSWITCH_BASE_URL` | — | Killswitch internal URL |
| `GATEKEEPER_BASE_URL` | — | Gatekeeper internal URL |

### Arbiter Server (optional)
| Variable | Default | Description |
|----------|---------|-------------|
| `KILLSWITCH_TIMEOUT_MS` | 1500 | Killswitch HTTP timeout |
| `GATEKEEPER_TIMEOUT_MS` | 1500 | Gatekeeper HTTP timeout |
| `CACHE_TTL_SECONDS` | 600 | Policy cache TTL |
| `KILLSWITCH_PUBLIC_HOST` | — | Killswitch public hostname (anti-recursion) |
| `GATEKEEPER_PUBLIC_HOST` | — | Gatekeeper public hostname (anti-recursion) |

### Telemetry Publisher
| Variable | Default | Description |
|----------|---------|-------------|
| `ARB_TELEMETRY_ENABLED` | false | Enable telemetry event emission |
| `ARB_TELEMETRY_REDIS_URL` | redis://localhost:6379/0 | Redis URL |
| `ARB_TELEMETRY_STREAM_KEY` | arb:v1:events | Redis Stream key |
| `ARB_TELEMETRY_TIMEOUT_MS` | 25 | XADD timeout |
| `ARB_TELEMETRY_BUFFER_SIZE` | 8192 | In-memory event buffer |

### Telemetry Query API
| Variable | Default | Description |
|----------|---------|-------------|
| `ARB_TELEMETRY_API_ENABLED` | false | Enable telemetry query endpoints |
| `ARB_TELEMETRY_API_DB_DSN` | — | MariaDB DSN (required when enabled) |
| `ARB_TELEMETRY_API_MAX_WINDOW_MINUTES` | 60 | Max query window |
| `ARB_TELEMETRY_API_MAX_LIMIT` | 100 | Max result limit |
| `ARB_TELEMETRY_API_TRUST_PROXY_HEADERS` | true | Trust X-Real-IP/X-Forwarded-For for rate limiting |
