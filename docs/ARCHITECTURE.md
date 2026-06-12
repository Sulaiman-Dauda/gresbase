# Gresbase Architecture

This document explains how Gresbase is put together: the layers a request
passes through, the services behind them, the data model, and the decisions
that shaped them. It is written for contributors and for operators who want to
understand what they are running.

## Overview

Gresbase is a single, self-contained backend platform that compiles to **one
static binary**. That binary contains:

- A REST API over dynamic, PostgreSQL-backed collections.
- A full authentication suite (admin accounts and end-user "record" accounts).
- A realtime engine (Server-Sent Events with a WebSocket fallback).
- File storage (local disk or any S3-compatible service).
- An admin dashboard (a Next.js app embedded directly into the binary).

The governing principle is simple: **PostgreSQL is the only required piece of
infrastructure.** Anything a larger stack would hand to a separate service —
change-data-capture, image processing, metadata APIs, a message broker for
realtime fan-out — Gresbase does in-process or with a native PostgreSQL
feature. There are no sidecars to deploy and no second port to secure.

## Request lifecycle

Every HTTP request flows top-to-bottom through these layers:

```
Clients (TypeScript SDK · Dart SDK · REST · WebSocket/SSE · Admin dashboard)
        │
        ▼
HTTP server (chi router)
   Middleware stack, in order:
   RequestID → RealIP (trusted-proxy aware) → HTTP metrics → Logger →
   Recoverer → Timeout → CleanPath → Heartbeat → CORS →
   Security headers → Max body size → CSRF → (per-route) Auth
        │
        ▼
Route handlers (REST · admin · realtime upgrade · file serving)
        │
        ▼
Event / hook layer  (pre- and post- hooks fire around every operation)
        │
        ▼
Core services (Auth · Collections · Storage · Realtime · Migrations · Mailer)
        │
        ▼
Database layer (pgx connection pool · query builder · transactions ·
                migration runner · optional read-replica routing)
        │
        ▼
PostgreSQL (embedded for development, or external/managed for production)
```

The middleware order above is the actual order configured in
`backend/internal/api/server.go`. Two details matter for security:

- **RealIP is trusted-proxy aware.** Forwarded headers (`X-Forwarded-For`,
  `X-Real-IP`) are honored **only** when the request arrives from a CIDR listed
  in `TRUSTED_PROXIES`. Otherwise the direct socket peer is used, so a client
  cannot spoof its IP to defeat rate limiting or audit logging.
- **CSRF protection** uses a double-submit cookie for the dashboard's
  cookie-based sessions. Requests authenticated with a `Bearer` token or an API
  key are exempt, because they are not vulnerable to cross-site request forgery.

## Key design decisions

### PostgreSQL, accessed through pgx (not an ORM)

- Real concurrency and connection pooling (`pgxpool`) — no single-writer file
  lock to serialize against under load.
- Native `JSONB`, full-text search (`tsvector`), and row-level security.
- Direct, parameterized SQL — no ORM translation layer between the rule engine
  and the query that runs. Collection rules compile straight into `WHERE`
  clauses.

### chi for routing

- Built on the standard library's `net/http`, so middleware is ordinary
  `http.Handler` composition with no framework-specific abstractions.
- Explicit and predictable — the middleware chain reads as a list.

### Dynamic schemas become real tables

A collection definition is translated into a real PostgreSQL table. This:

```json
{
  "name": "posts",
  "schema": [
    { "name": "title", "type": "text" },
    { "name": "views", "type": "number" }
  ]
}
```

becomes:

```sql
CREATE TABLE posts (
  id         TEXT PRIMARY KEY DEFAULT gen_random_uuid(),
  title      TEXT,
  views      DOUBLE PRECISION,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Editing a collection's schema runs the matching `ALTER TABLE`. Records are
ordinary rows, so anything PostgreSQL can do to a table — indexes, full-text
search, views — applies to your data directly.

### Event / hook system

Every significant operation fires hooks before and after it runs. For a record
create, for example:

```
OnModelValidate → OnRecordCreate (pre) → execute → OnRecordCreate (post)
```

Hooks drive validation, audit logging, realtime emission, and any custom logic
you register (in Go when embedding the library, or in JavaScript via file
hooks). A panicking hook is recovered and logged; it cannot crash the server.

## Core services

| Service | Responsibility |
|---------|----------------|
| **Auth** | JWT issuance (HS256 or ES256), session tracking and revocation, OAuth, OTP, magic links, passkeys (WebAuthn), MFA (TOTP), API keys. |
| **Collection** | Schema definitions, table creation/alteration, access-rule compilation, import/export, generated PostgreSQL RLS scripts. |
| **Storage** | Local or S3-compatible file storage, signed URLs, on-the-fly image thumbnails, resumable (TUS) uploads. |
| **Realtime** | Per-subscriber rule-enforced record events, broadcast channels, presence, and optional cross-node fan-out via `LISTEN/NOTIFY`. |
| **Migrations** | Internal schema migrations (versioned Go migrations) plus user collection migrations (declarative JSON snapshots in `./gb_migrations`). |
| **Mailer** | SMTP delivery for all transactional email, with dashboard-editable templates and a built-in default fallback. |

## Database layer

- **Connection pool** via `pgxpool`, with configurable open/idle limits and idle
  timeout.
- **Query builder** — a small, readable, chainable helper for parameterized SQL.
- **Transactions** — multi-statement operations (such as transactional batch
  writes) run in a single transaction and roll back atomically on error.
- **Migration runner** — applies versioned internal migrations in order and
  records them in `_migrations_tracking`.
- **Read-replica routing (optional)** — when `DATABASE_REPLICA_URL` is set,
  replica-safe reads (record lists, aggregations, relation expansion) are routed
  to a read replica while writes and rule-feeding reads stay on the primary. A
  background health probe automatically fails reads back to the primary if the
  replica becomes unreachable, and resumes using it once it recovers — no
  restart required.

## Data model

### System tables

Gresbase owns a set of internal tables (all prefixed with `_`). User
collections never collide with them, and they are never written by collection
migrations.

| Table | Purpose |
|-------|---------|
| `_admins` | Superuser / admin accounts |
| `_collections` | Dynamic collection (schema) definitions |
| `_settings` | Instance settings, including email-template overrides |
| `_api_keys` | Programmatic access keys (hashed) |
| `_sessions` | Admin session tracking and revocation |
| `_audit_logs` | Activity trail |
| `_rate_limits`, `_collection_rate_limits` | Rate-limiting state |
| `_jobs`, `_job_runs` | Scheduled jobs (e.g. backups) and their run history |
| `_migrations_tracking` | Applied internal migrations |
| `_otp`, `_magic_links`, `_password_resets`, `_verifications`, `_email_changes`, `_external_auths`, `_oauth_states`, `_mfa_secrets` | Admin auth flows |
| `_record_sessions`, `_record_otp`, `_record_passkeys`, `_record_password_resets`, `_record_verifications`, `_record_email_changes`, `_record_external_auths`, `_record_mfa` | End-user (record) auth flows |
| `_webauthn_sessions` | In-progress passkey ceremonies (short TTL) |
| `{collection_name}` | Your dynamic collection tables |

## Security model

### Authentication

- JWT access + refresh tokens (HS256 by default; ES256 with an ECDSA key pair).
- Sessions are tracked and can be revoked; logout invalidates the session.
- The dashboard authenticates with **httpOnly, SameSite cookies plus CSRF
  protection** — tokens are never placed in `localStorage`. SDK and API-key
  clients use `Authorization: Bearer` and are CSRF-exempt.
- Auth endpoints are rate limited by client IP.
- API keys carry a `gb_` prefix and are stored hashed.

### Authorization — locked by default

Every collection has five access rules (`list`, `view`, `create`, `update`,
`delete`) with tri-state semantics:

- `null` (the default) → **locked**: only superusers may perform the operation.
- `""` → **public**: anyone may perform it.
- `"<expression>"` → evaluated per request/record (e.g. `owner = @request.auth.id`).

Rules are enforced on **every** read and write path — REST list/view,
create/update/delete, transactional batch, full-text search, file downloads, and
realtime event delivery — and they fail closed. The same rules can be compiled
into native PostgreSQL `CREATE POLICY` statements (`GET /api/v1/rls/script`) so
that direct database connections are guarded too.

### Transport and headers

- Content-Security-Policy, CORS (explicit allow-list in production), HSTS when
  TLS is enabled, and standard anti-clickjacking / MIME-sniffing headers.
- A request-body size cap on non-upload routes.
- Per-request IDs, propagated to logs and responses for tracing.

## Realtime protocol

Clients connect over SSE (primary) or WebSocket (fallback) and subscribe to
record topics — `posts/*` for a whole collection, `posts/<id>` for one record.
Messages are JSON:

```json
{
  "type": "subscribe | unsubscribe | message | presence",
  "channel": "posts",
  "event": "record:create | record:update | record:delete",
  "data": {}
}
```

Delivery is rule-checked per subscriber: wildcard topics use the collection's
list rule, single-record topics use its view rule, and a subscriber only
receives events for records its rules allow. Across multiple app nodes, events
fan out over PostgreSQL `LISTEN/NOTIFY` — no external broker.

## Frontend architecture

The admin dashboard is a Next.js application exported to static files and
embedded into the Go binary with `go:embed`. It is served from the same process
and port as the API.

### Stack

- **Next.js** (App Router) — static export.
- **Tailwind CSS** — dark-mode-first design system.
- **shadcn/ui** — accessible component primitives.
- **TanStack Query** — server-state fetching and caching.
- **Zustand** — local client state.

### Pages

Login, Overview, Collections (schema editor, records, rules, API preview),
Users (admins), API Keys, Realtime tester, Logs, Metrics, and Settings.
Navigation is keyboard-first, with a command palette (⌘K) and a sidebar toggle
(⌘B).

## Migrations

Two independent mechanisms, by design:

1. **Internal schema migrations** — versioned Go migrations
   (`backend/migrations`) that create and evolve the system tables. They run in
   order on boot and are recorded in `_migrations_tracking`.
2. **User collection migrations** — declarative JSON snapshots in
   `./gb_migrations`, applied in filename order after the internal migrations.
   In development these are written automatically as you change collections, so
   your data model lives in version control and deploys reproducibly. See
   [FEATURES.md](FEATURES.md#schema-migrations-gb_migrations).

## Extensibility

Gresbase has no separate plugin runtime or edge-function service. Extension
happens in-process, two ways:

- **Embedding as a Go library.** Import the package, register hooks in real Go,
  and add custom routes. Hooks bound before `Start()` attach to the live event
  bus:

  ```go
  gb := gresbase.New()

  gb.OnRecordCreate().BindFunc(func(e events.Event) error {
      // custom logic
      return e.Next()
  })

  gb.OnServe().BindFunc(func(e events.Event) error {
      // register a custom route
      return e.Next()
  })

  gb.Start()
  ```

  Available hooks include lifecycle (`OnBootstrap`, `OnServe`, `OnTerminate`),
  records (`OnRecordCreate/Update/Delete`), collections
  (`OnCollectionCreate/Update/Delete`), models (`OnModelValidate`,
  `OnModelCreate/Update/Delete`), auth (`OnAuthLogin`, `OnAuthRefresh`),
  realtime, files, and more. See
  [`backend/examples/embed`](../backend/examples/embed) for a runnable example.

- **File-based JavaScript hooks.** Drop `*.js` files into `./gb_hooks` and they
  load at boot and hot-reload on change. These run unsandboxed in the server
  process and are intended for trusted, first-party code only. See
  [FEATURES.md](FEATURES.md#js-file-hooks-gb_hooks).

## Deployment modes

| Mode | Command | Notes |
|------|---------|-------|
| Single binary | `./gresbase serve` | Frontend embedded; pair with external PostgreSQL for production. |
| Docker / Compose | `docker compose up` | Example stack ships in `docker/`. |
| Kubernetes | Deployment + external PostgreSQL | Standard manifests; use S3 storage for multi-node. |
| Fly.io / Railway | Platform config | Single binary plus a managed PostgreSQL add-on. |

For full operational guidance — TLS, backups, read replicas, multi-node,
monitoring, and a hardening checklist — see [DEPLOYMENT.md](DEPLOYMENT.md).

## Testing

- **Integration tests** boot an ephemeral PostgreSQL and exercise the live HTTP
  surface end to end — auth, collections, records, rule enforcement on every
  path, and realtime.
- **Unit tests** cover services with table-driven Go tests.
- **Realtime tests** drive the SSE/WebSocket engine directly.
- CI runs the full suite with the race detector and coverage on every change,
  alongside `gofmt`, `go vet`, `go mod tidy`, `golangci-lint`, and
  `govulncheck`. See [CONTRIBUTING.md](../CONTRIBUTING.md).
