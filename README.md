# Arbiter

Arbiter is an authorization gateway that orchestrates request authorization decisions by coordinating Killswitch and Gatekeeper checks in strict order. It provides a single decision endpoint for NGINX `auth_request` and includes a full admin API with SPA for managing host policies.

## Overview

Arbiter acts as a unified authorization layer that:
- Makes single authorization decisions per request
- Orchestrates Killswitch and Gatekeeper checks in strict order (Killswitch first, then Gatekeeper)
- Stores per-host policies in SQLite
- Provides fail-closed behavior when downstream services are unavailable
- Includes forced constraints to prevent recursion (killswitch/gatekeeper hosts cannot require themselves)

## Architecture

```
NGINX Request → Arbiter → Policy Cache → SQLite DB
                      ↓
              Killswitch Check (if required)
                      ↓
              Gatekeeper Check (if required)
                      ↓
              Decision + Headers → NGINX
```

### Key Features

- **Strict Ordering**: Killswitch checks always run before Gatekeeper checks
- **Fail-Closed**: Returns 500 on downstream errors when checks are required
- **Thread-Safe Caching**: In-memory policy cache with 10-minute TTL, invalidate-on-write
- **Forced Constraints**: Prevents recursion by forcing killswitch/gatekeeper hosts to disable their own checks
- **No Defaults**: If a host has no policy, it's allowed (no checks required)
- **Policy Packs**: Declarative YAML-based policy management with immutability guarantees

## Installation

### Prerequisites

- Go 1.23 or later
- Node.js 18+ (for frontend development)
- SQLite 3 (via modernc.org/sqlite, pure Go)

### Building

```bash
# Build the binary
make build

# Or manually
go build -o arbiter ./cmd/arbiter
```

### Frontend

```bash
# Install frontend dependencies
make frontend-install

# Build frontend for production
make frontend-build
```

## Configuration

Arbiter is configured via environment variables:

### Required

- `ARBITER_BIND_ADDR` - Address to bind the HTTP server (e.g., `127.0.0.1:9100`)
- `DATABASE_URL` - SQLite database URL (e.g., `sqlite:///var/lib/arbiter/arbiter.db`)
- `KILLSWITCH_BASE_URL` - Base URL for Killswitch service (e.g., `http://127.0.0.1:9090`)
- `GATEKEEPER_BASE_URL` - Base URL for Gatekeeper service (e.g., `http://192.168.4.201:3000`)

### Optional

- `KILLSWITCH_TIMEOUT_MS` - Timeout for Killswitch requests in milliseconds (default: 1500)
- `GATEKEEPER_TIMEOUT_MS` - Timeout for Gatekeeper requests in milliseconds (default: 1500)
- `CACHE_TTL_SECONDS` - Policy cache TTL in seconds (default: 600)
- `KILLSWITCH_PUBLIC_HOST` - Public hostname for Killswitch (for forced constraints, e.g., `killswitch.domostack.me`)
- `GATEKEEPER_PUBLIC_HOST` - Public hostname for Gatekeeper (for forced constraints, e.g., `gatekeeper.domostack.me`)

## Usage

### Running the Server

```bash
# Using Makefile
make run

# Or manually with environment variables
ARBITER_BIND_ADDR=127.0.0.1:9100 \
DATABASE_URL=sqlite:///tmp/arbiter.db \
KILLSWITCH_BASE_URL=http://127.0.0.1:9090 \
GATEKEEPER_BASE_URL=http://127.0.0.1:3000 \
./arbiter
```

### Database Setup

The database is automatically migrated on startup. Migrations include:
- `000001_create_host_policies.up.sql` - Creates the `host_policies` table
- `000002_add_managed_fields_to_host_policies.up.sql` - Adds managed policy metadata fields

## API Endpoints

### Authorization

#### `GET /api/v1/check`

Core decision endpoint for NGINX `auth_request`.

**Required Headers:**
- `X-Original-Host` - Hostname (lowercase)
- `X-Original-URI` - Path and query string
- `X-Original-Method` - HTTP method (uppercase)

**Optional Headers:**
- `X-Forwarded-For` - Client IP
- `X-Real-IP` - Real client IP
- `User-Agent` - User agent string
- `X-Request-Id` - Request ID (used as trace ID if present)
- `Cookie` - Session cookie (forwarded to Gatekeeper)

**Response Headers (always present):**
- `X-Auth-Decision` - Decision: `allow`, `unauth`, `forbid`, `killswitch`, or `error`
- `X-Auth-Reason` - Human-readable reason
- `X-Auth-Source` - Policy source: `host`, `none`, or `forced`
- `X-Auth-Policy` - Policy identifier: `host:<hostname>` or `none`
- `X-Auth-Trace` - Correlation ID (UUID or X-Request-Id)

**Response Headers (when Gatekeeper allows):**
- `X-Identity-User-Id` - User ID
- `X-Identity-Email` - User email
- `X-Identity-Groups` - Comma-separated groups

**Response Headers (when Killswitch blocks):**
- `X-Killswitch-Rule` - Rule that matched
- `X-Killswitch-Reason` - Block reason
- `X-Killswitch-Response-Type` - Response type
- `Retry-After` - Retry delay (if present)

**Status Codes:**
- `200` - Allow
- `401` - Unauthenticated (from Gatekeeper)
- `403` - Forbidden (from Gatekeeper)
- `503` - Killswitch block
- `500` - Internal error (missing headers, downstream errors)

### Policy Management

#### `GET /api/v1/policies`

List all host policies.

**Response:** Array of policy objects

#### `POST /api/v1/policies`

Create a new host policy.

**Request Body:**
```json
{
  "host": "example.com",
  "killswitch_required": true,
  "gatekeeper_required": false,
  "notes": "Optional notes"
}
```

**Response:** Created policy object

#### `GET /api/v1/policies/{id}`

Get a specific policy by ID.

**Response:** Policy object

#### `PATCH /api/v1/policies/{id}`

Update a policy.

**Request Body:** Same as POST (all fields optional)

**Response:** Updated policy object

#### `DELETE /api/v1/policies/{id}`

Delete a policy.

**Response:** `204 No Content`

**Note:** Managed policies (created via policy packs) cannot be updated or deleted via the API. Attempts will return `409 Conflict` with an error message indicating the policy is managed by a pack.

#### `GET /api/v1/effective?host=example.com`

Get the effective policy for a host (after forced constraints are applied).

**Response:**
```json
{
  "host": "example.com",
  "killswitch_required": false,
  "gatekeeper_required": true,
  "source": "forced",
  "forced_killswitch": true,
  "forced_gatekeeper": false,
  "killswitch_public_host": "killswitch.domostack.me",
  "gatekeeper_public_host": "gatekeeper.domostack.me"
}
```

### Testing

#### `POST /api/v1/test/check`

Test endpoint for evaluating authorization decisions. Always returns HTTP 200 with decision details in JSON body. Useful for UI testing and debugging.

**Prerequisites:**
- Killswitch service must be running and accessible at the URL configured in `KILLSWITCH_BASE_URL`
- Gatekeeper service must be running and accessible at the URL configured in `GATEKEEPER_BASE_URL`
- Both services must be properly configured and responding to requests

**Request Body:**
```json
{
  "host": "example.com",
  "method": "GET",
  "uri": "/api/v1/users"
}
```

**Response (HTTP 200):**
```json
{
  "decision": "allow",
  "status": 200,
  "reason": "authorized",
  "source": "host",
  "policy": "host:example.com",
  "trace_id": "550e8400-e29b-41d4-a716-446655440000",
  "normalized": {
    "host": "example.com",
    "method": "GET",
    "uri": "/api/v1/users"
  },
  "latency_ms": 45.123,
  "total_latency_ms": 45.123,
  "killswitch_latency_ms": 12.456,
  "gatekeeper_latency_ms": 28.789,
  "nginx_headers": {
    "X-Auth-Decision": "allow",
    "X-Auth-Reason": "authorized",
    "X-Auth-Source": "host",
    "X-Auth-Policy": "host:example.com",
    "X-Auth-Trace": "550e8400-e29b-41d4-a716-446655440000",
    "X-Identity-User-Id": "123",
    "X-Identity-Email": "user@example.com",
    "X-Identity-Groups": "admin,users"
  },
  "identity_headers": {
    "X-Identity-User-Id": "123",
    "X-Identity-Email": "user@example.com",
    "X-Identity-Groups": "admin,users"
  },
  "killswitch_headers": {
    "X-Killswitch-Rule": "GET:example.com:/api/v1/users",
    "X-Killswitch-Reason": "Maintenance"
  }
}
```

**Response Fields:**
- `decision` - Decision: `allow`, `unauth`, `forbid`, `killswitch`, or `error`
- `status` - HTTP status code that would be returned
- `reason` - Human-readable reason
- `source` - Policy source: `host`, `none`, or `forced`
- `policy` - Policy identifier: `host:<hostname>` or `none`
- `trace_id` - Correlation ID (UUID or X-Request-Id)
- `normalized` - Normalized request values (host, method, uri)
- `latency_ms` - Total latency (deprecated, use `total_latency_ms`)
- `total_latency_ms` - Total end-to-end latency in milliseconds
- `killswitch_latency_ms` - Killswitch check latency (0 if not called)
- `gatekeeper_latency_ms` - Gatekeeper check latency (0 if not called)
- `nginx_headers` - All headers that would be sent back to NGINX
- `identity_headers` - Identity headers (if present, from Gatekeeper)
- `killswitch_headers` - Killswitch headers (if present, when blocked)

**Note:** This endpoint uses your current session cookies (if authenticated) for Gatekeeper checks, making it useful for testing authorization decisions in the UI.

## Policy Packs

Policy packs allow you to manage host policies declaratively using YAML files. Policies defined in packs are **immutable** via the API and UI - they can only be modified by editing the YAML file and re-applying the pack.

### Features

- **Declarative Management**: Define policies in YAML files
- **Idempotent Application**: Safe to re-apply multiple times
- **Collision Detection**: Prevents conflicts between pack-managed and manually-managed policies
- **Domain Expansion**: Generate policies for multiple domains using `common_domains`
- **Anti-Recursion**: Automatically enforces constraints to prevent service recursion
- **Versioning**: Track pack versions and apply times

### YAML Schema

```yaml
version: 1                    # Pack version (integer)
pack: arbiter                # Pack name (string)
common_domains:              # List of domains for expansion
  - home.arpa
  - domostack.me

policies:
  - key: policy-key           # Stable identifier (required)
    name: Policy Name         # Human-readable name (required)
    description: Optional description
    required_services:        # List of required services (required)
      - killswitch
      - gatekeeper
    # Either specify explicit hosts:
    hosts:
      - api.example.com
      - api2.example.com
    # OR use domain expansion:
    expand_common_domains: true
    subdomain: api            # Creates api.home.arpa, api.domostack.me
```

### Policy Specification

Each policy must specify either:
- **Explicit hosts**: Use the `hosts` field with a list of FQDNs
- **Domain expansion**: Use `expand_common_domains: true` with a `subdomain` field

When using domain expansion, policies are created for each domain in `common_domains` with the specified subdomain prefix.

### Required Services

The `required_services` field accepts:
- `killswitch` - Requires Killswitch check
- `gatekeeper` - Requires Gatekeeper check
- Both can be specified for policies requiring both services
- Empty array `[]` means no services required (allow all)

### Applying Policy Packs

Use the CLI command to apply a policy pack:

```bash
arbiter apply-pack --file /path/to/pack.yml
```

The command will:
1. Parse and validate the YAML file
2. Expand policies into host-level entries
3. Apply anti-recursion constraints
4. Upsert policies in a transaction
5. Delete policies from the pack that are no longer present
6. Invalidate the policy cache

**Example:**
```bash
arbiter apply-pack --file config/packs/arbiter.yml
```

### Resetting the Database

To completely reset the database (delete all data and recreate schema):

```bash
arbiter nuke-db
```

This command will:
1. Show a warning with the database path
2. Require typing `yes` to confirm
3. Delete the database file (and WAL/SHM files if present)
4. Re-run migrations to create a fresh database

**Warning:** This permanently deletes all policies. This action cannot be undone.

**Example:**
```bash
DATABASE_URL=sqlite:///tmp/arbiter.db arbiter nuke-db
```

### Managed vs Unmanaged Policies

- **Managed Policies**: Created via policy packs. Shown with a "Managed" badge in the UI. Cannot be edited or deleted via API/UI.
- **Unmanaged Policies**: Created manually via API or UI. Fully editable and deletable.

**Collision Rules:**
- A host cannot be both managed and unmanaged
- A host cannot be managed by multiple packs
- Pack apply will fail with a clear error if collisions are detected

### Anti-Recursion Constraints

When applying packs, Arbiter automatically enforces:
- If `host == KILLSWITCH_PUBLIC_HOST` → `killswitch_required` is forced to `false`
- If `host == GATEKEEPER_PUBLIC_HOST` → `gatekeeper_required` is forced to `false`

This prevents infinite recursion where services try to check themselves.

### Example Policy Pack

See `config/packs/arbiter.yml` for a complete example with:
- Multiple policy types
- Both explicit hosts and domain expansion
- Various service requirement combinations

### Health Checks

#### `GET /healthz`

Process health check. Returns `200 OK` if the process is alive.

#### `GET /readyz`

Readiness check. Returns `200 OK` if the database is reachable and migrations are applied.

## Policy Semantics

### Policy Resolution

1. Look up policy by exact host match (case-insensitive)
2. If no policy exists → allow (no checks required)
3. Apply forced constraints:
   - If host == `KILLSWITCH_PUBLIC_HOST` → force `killswitch_required=false`
   - If host == `GATEKEEPER_PUBLIC_HOST` → force `gatekeeper_required=false`
4. Execute checks in order:
   - Killswitch (if required)
   - Gatekeeper (if required and Killswitch didn't block)

### Forced Constraints

Forced constraints prevent recursion by ensuring that:
- The Killswitch public host cannot require Killswitch checks
- The Gatekeeper public host cannot require Gatekeeper checks

These constraints are applied regardless of what's stored in the database.

## Frontend

The frontend is a React SPA that provides a UI for managing host policies.

### Development

```bash
# Start frontend dev server
make frontend-dev

# Or manually
cd frontend && npm run dev
```

The dev server runs on `http://localhost:5173` with API proxying to the backend.

### Production Build

```bash
# Build frontend
make frontend-build

# The built files are in frontend/dist/
# The backend serves these files automatically when present
```

## Development

### Project Structure

```
arbiter/
├── cmd/arbiter/          # Main entry point
├── internal/
│   ├── config/           # Configuration parsing
│   ├── store/            # SQLite store implementation
│   ├── policycache/      # Thread-safe policy cache
│   ├── arbiter/          # Decision engine
│   ├── downstream/       # Killswitch/Gatekeeper clients
│   ├── httpserver/       # HTTP server and handlers
│   └── pack/             # Policy pack parsing and application
├── migrations/           # Database migrations
├── config/
│   └── packs/            # Policy pack YAML files
├── frontend/             # React SPA
└── Makefile              # Build automation
```

### Running Tests

```bash
# Run all tests
make test

# Run tests with verbose output
go test ./... -v

# Run specific package tests
go test ./internal/store/... -v
```

### Code Quality

```bash
# Format code
make fmt

# Run linter
make lint

# Run all checks
make all
```

## Integration with NGINX

Arbiter is designed to be called by NGINX via `auth_request`. Example configuration:

```nginx
location / {
    auth_request /auth;
    auth_request_set $auth_decision $upstream_http_x_auth_decision;
    auth_request_set $auth_trace $upstream_http_x_auth_trace;

    # Forward identity headers if present
    auth_request_set $user_id $upstream_http_x_identity_user_id;
    auth_request_set $user_email $upstream_http_x_identity_email;
    auth_request_set $user_groups $upstream_http_x_identity_groups;

    proxy_pass http://backend;
    proxy_set_header X-Auth-Decision $auth_decision;
    proxy_set_header X-Auth-Trace $auth_trace;
    proxy_set_header X-Identity-User-Id $user_id;
    proxy_set_header X-Identity-Email $user_email;
    proxy_set_header X-Identity-Groups $user_groups;
}

location = /auth {
    internal;
    proxy_pass http://127.0.0.1:9100/api/v1/check;
    proxy_pass_request_body off;
    proxy_set_header Content-Length "";
    proxy_set_header X-Original-Host $host;
    proxy_set_header X-Original-URI $request_uri;
    proxy_set_header X-Original-Method $request_method;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header User-Agent $http_user_agent;
    proxy_set_header X-Request-Id $request_id;
    proxy_set_header Cookie $http_cookie;
}
```

## Caching

Arbiter uses an in-memory cache for host policies with the following characteristics:

- **TTL**: 10 minutes (configurable via `CACHE_TTL_SECONDS`)
- **Lazy Loading**: Policies are loaded from the database on first request, not at startup
- **Invalidate-on-Write**: Cache is invalidated after any policy create/update/delete
- **Thread-Safe**: Uses atomic pointer swaps to ensure readers never see partial state
- **Single-Flight**: Only one goroutine reloads from the database at a time

## Error Handling

Arbiter implements fail-closed behavior:

- **Missing Required Headers**: Returns 500 with `X-Auth-Decision=error`
- **Killswitch Required but Unavailable**: Returns 500 with `X-Auth-Decision=error`
- **Gatekeeper Required but Unavailable**: Returns 500 with `X-Auth-Decision=error`
- **Killswitch Blocks**: Returns 503 with `X-Auth-Decision=killswitch` (Gatekeeper is not called)
- **Gatekeeper Unauthenticated**: Returns 401 with `X-Auth-Decision=unauth`
- **Gatekeeper Forbidden**: Returns 403 with `X-Auth-Decision=forbid`

## Tracing

Arbiter generates correlation IDs for request tracing:

- Uses `X-Request-Id` header if present
- Otherwise generates a UUID
- Returns as `X-Auth-Trace` header
- Forwards to downstream services (Killswitch and Gatekeeper)
- Logged with structured logging (zerolog) keyed by trace ID

## License

[Add your license here]

## Contributing

[Add contributing guidelines here]
