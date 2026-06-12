# Gresbase

> A self-hosted backend platform inspired by PocketBase — PostgreSQL-backed collections, authentication, realtime, and file storage in a single Go binary.

> **⚠️ Development Status: Beta.**
> Core (auth, collections, records, rules, realtime, dashboard) is tested and secure by default.
> The embedded ACME CA and JS plugins are experimental and disabled/gated by default.
> See [Status](#project-status) for an honest assessment.

## Secure by default

- **Collections are locked on creation** — every access rule starts as `null` (superusers only). You explicitly open what should be public.
- Access rules are enforced on **every read path**: list, view, search, batch, file downloads, and realtime event delivery.
- Production refuses to start with a missing, generated, or weak `JWT_SECRET`.
- Auth endpoints (admin and end-user) are rate limited by IP.
- The experimental ACME CA and the unsandboxed JS plugin runtime return 404 unless explicitly enabled.

## Postgres-native superpowers — one process, one database

The governing principle: **PostgreSQL is the only infrastructure.** Anything a
larger platform does with a sidecar service, Gresbase does with a Postgres feature.

- **Vector search (pgvector)** — add a `vector` field, store embeddings, run rule-aware similarity search. The AI/RAG use-case SQLite-based tools can't reach. → `POST /api/v1/records/{c}/search-vector`
- **RLS-grade rules without the footguns** — locked-by-default app-layer rules with a preset catalog and a **rule simulator** ("would user X pass this rule against this record?") so policies are testable before they ship.
- **Real Postgres RLS, generated** — compile your collection rules into `CREATE POLICY` statements for a restricted role, so direct database connections are guarded too. → `GET /api/v1/rls/script`, `POST /api/v1/rls/apply`
- **Webhooks without pg_net** — HMAC-SHA256-signed event forwarding (Stripe-style `t=,v1=` signatures) with retries and a delivery log, delivered by in-process workers. → `/api/v1/webhooks`
- **SQL console + schema introspection** — superuser SQL editor (read-only by default) and a structured schema API. The job Supabase runs a `postgres-meta` container for. → `POST /api/v1/sql`, `GET /api/v1/sql/schema`
- **Typed SDK from your schema** — `gresbase types` or `GET /api/v1/types.ts` generates a typed TypeScript client. PostgREST-style ergonomics, no extra service.
- **Horizontal realtime** — set `realtime_multi_node: true` and record events travel between app nodes over Postgres `LISTEN/NOTIFY`. No Redis, no broker.
- **Prometheus metrics in-process** — `GET /metrics` text exposition, optional bearer token. No log-shipping sidecars.
- **REST aggregations** — `count`, `sum`, `avg`, `min`, `max` with `groupBy`, filtered and **list-rule enforced** in SQL. Neither PocketBase nor PostgREST-less Supabase self-hosting gives you this for free. → `GET /api/v1/records/{c}/aggregate?aggregate=count,sum:amount&groupBy=status`
- **Rule-enforced relation expansion** — forward (`?expand=author`), back-relations (`?expand=comments_via_post`), and nested paths (`comments_via_post.user`, up to 6 levels). Every level honors the target collection's rules, so a public collection can never leak a locked one through expand.
- **Anonymous sign-in** — `POST /api/v1/collections/{c}/auth/auth-with-anonymous` mints a throwaway user for try-before-signup flows. Off by default per collection; gate rules with `@request.auth.anonymous = false`.
- **Passkeys (WebAuthn)** — phishing-resistant, passwordless sign-in for auth collections, in-process via go-webauthn. Off by default per collection (`allowPasskeys`); discoverable credentials, no email enumeration, SDK helpers included. → `POST /api/v1/collections/{c}/auth/passkey/login-begin`
- **WAL change capture** — opt-in logical-replication consumer (`realtime_wal_enabled`) so rows changed by *direct SQL* — psql, the SQL console, the generated RLS role — reach realtime subscribers too, rule-checked like everything else. The job Supabase runs an Elixir service for, in-process via pglogrepl.
- **Resumable uploads (TUS)** — `POST /api/v1/files/tus/` attaches large files to records over flaky connections with full rule + field-constraint enforcement. The job Supabase Storage runs a sidecar container for.
- **Schema migrations as files** — dev-mode collection changes are snapshotted to `./gb_migrations/*.json` and replayed on boot, so schemas live in git and deploy reproducibly (`gresbase migrations snapshot|list|apply`).
- **Editable email templates** — every transactional email customizable from the dashboard, validated at save time, with built-in defaults as a can't-break fallback.
- **Realtime broadcast + presence** — client-to-client channel messages (`POST /api/v1/realtime/broadcast`, auth required) and per-channel presence with join/leave events and member state. Travels across nodes over `LISTEN/NOTIFY` in multi-node mode.
- **On-the-fly image transforms** — `?thumb=400x300f&format=jpeg&quality=80` on file URLs, generated in-process and cached. The job Supabase runs an `imgproxy` container for.
- **File-based JS hooks with hot reload** — drop `*.js` files in `./gb_hooks` (PocketBase `pb_hooks`-style) to handle record/auth events; edits reload live. No Deno runtime, no cold starts. Scaffold with `gresbase hooks init` (includes `types.d.ts` for editor autocomplete).
- **Read-replica routing** — set `DATABASE_REPLICA_URL` and record lists, aggregations, and relation expansion route to a PostgreSQL read replica; writes and rule-feeding reads stay on the primary. The scale lever a single-writer SQLite backend structurally cannot offer.
- **Embed as a Go framework** — write hooks in real Go and add custom routes; see [`backend/examples/embed`](backend/examples/embed).

📖 **Full features guide**: [docs/FEATURES.md](docs/FEATURES.md)

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://go.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6-3178C6?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Tests](https://img.shields.io/badge/tests-passing-brightgreen)](https://github.com/gresbase/gresbase)

---

## What is Gresbase?

A **single-binary, self-hosted backend platform** that aims to provide:

- **Dynamic Collections** — Define PostgreSQL-backed schemas from the dashboard or API, locked-by-default access rules ✅
- **Authentication** — Email/password, OAuth, magic links, OTP, API keys, end-user record auth ✅
- **Realtime Engine** — SSE/WebSocket record subscriptions with per-subscriber rule enforcement ✅
- **File Storage** — Local or S3-compatible storage, downloads gated by collection view rules ✅
- **Admin Dashboard** — Next.js + Tailwind CSS + shadcn/ui, embedded in the binary ✅
- **Extensible** — Event hooks on every request path, JS runtime (goja) 🚧
- **Embedded ACME CA** — Internal certificate authority, disabled by default 🚧 (experimental)
- **Multi-tenant** — Built-in tenant isolation 🚧

**Legend**: ✅ = Working with tests | 🚧 = Implemented, needs more testing | 📋 = Planned

## Quick Start

### From Source (recommended for development)

```bash
git clone https://github.com/gresbase/gresbase
cd gresbase

# Build everything (frontend + backend → single binary)
make build

# Start with embedded PostgreSQL
cd backend && DATABASE_URL="" JWT_SECRET="dev-secret-change-me" ./gresbase serve
```

### With External PostgreSQL (production)

```bash
DATABASE_URL="postgres://user:pass@host:5432/gresbase?sslmode=disable" \
  JWT_SECRET="your-64-char-hex-secret" \
  ./gresbase serve
```

### CLI Commands

```bash
./gresbase serve                              # Start server
./gresbase superuser create admin@ex.com pass # Create admin
./gresbase migrate                            # Run database migrations
./gresbase migrations snapshot                # Snapshot collection schemas to ./gb_migrations
./gresbase update                             # Self-update from the latest release (--check to only report)
./gresbase cert issue example.com             # Issue TLS certificate (experimental)
./gresbase version                            # Show version
```

📖 **Full deployment guide**: [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)
📖 **API reference**: [docs/API_REFERENCE.md](docs/API_REFERENCE.md)
📖 **Architecture**: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│               Gresbase — Single Static Binary                 │
├──────────────────────────────────────────────────────────────┤
│  ┌─────────┐  ┌─────────┐  ┌──────────┐  ┌───────────────┐  │
│  │   API   │  │ Realtime│  │   ACME   │  │   Admin UI    │  │
│  │  (REST) │  │ (SSE/WS)│  │   (CA)   │  │ (Embedded SPA)│  │
│  └────┬────┘  └────┬────┘  └────┬─────┘  └──────┬────────┘  │
│       │            │            │                │           │
│  ┌────┴────────────┴────────────┴────────────────┴──────┐    │
│  │                  Event Hooks Layer                    │    │
│  └──────────────────────┬───────────────────────────────┘    │
│  ┌──────────────────────┴───────────────────────────────┐    │
│  │                    Core Engine                        │    │
│  ├──────────┬──────────┬──────────┬─────────────────────┤    │
│  │   Auth   │Collection│ Storage  │   Migrations        │    │
│  ├──────────┼──────────┼──────────┼─────────────────────┤    │
│  │    PostgreSQL (pgx) — embedded or external            │    │
│  └──────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘

Single binary, one database. The admin dashboard is embedded via Go's
embed package. When no DATABASE_URL is set, an embedded PostgreSQL
instance auto-starts for zero-setup development.
```

> **Honest note on embedded PostgreSQL:** the embedded mode downloads
> platform-specific PostgreSQL binaries on first run (internet required) and is
> intended for development and small deployments. For production — and for
> air-gapped environments — point `DATABASE_URL` at a managed or self-run
> PostgreSQL. pgvector features also require external PostgreSQL.

## Features

### 1. Collections / Database

Dynamic collections backed by real PostgreSQL tables:

```json
{
  "name": "posts",
  "type": "base",
  "schema": [
    { "name": "title", "type": "text", "required": true },
    { "name": "content", "type": "editor" },
    { "name": "published", "type": "bool" },
    { "name": "tags", "type": "json" }
  ],
  "list_rule": "published = true",
  "view_rule": "",
  "create_rule": "@request.auth.id != \"\""
}
```

- **Field types**: text, number, bool, email, url, date, select, json, file, relation, password, editor, geo_point, autodate
- **Schema migration**: Changing a collection schema automatically alters the underlying PostgreSQL table
- **Access rules — locked by default**:
  - `null` (or omitted) → **locked**: only superusers can perform the operation
  - `""` → **public**: anyone can perform the operation
  - `"owner = @request.auth.id"` → filter expression evaluated per request/record
  - Rules are enforced consistently across REST, batch, search, file downloads, and realtime delivery
- **Filter syntax**: `=`, `!=`, `>`, `>=`, `<`, `<=`, `~` (contains), `!~`, `?=` (in array), `&&`/`||` (or `AND`/`OR`)
- **Indexes**: Composite and partial indexes via dashboard
- **Full-text search**: Via PostgreSQL tsvector (honors list rules)
- **View collections**: SQL views (or materialized views via `options.materialized`) exposed as read-only, rule-guarded collections; refresh with `POST /api/v1/collections/{id}/refresh-view`

### 2. Authentication

```
POST /api/v1/auth/login          # Email/password
POST /api/v1/auth/refresh        # Refresh tokens
POST /api/v1/auth/otp/request    # OTP via email
POST /api/v1/auth/otp/verify     # Verify OTP
POST /api/v1/auth/magic-link     # Magic link auth
POST /api/v1/admin/users         # Manage admin users
```

- JWT access + refresh tokens (HS256 or ES256)
- **Passkeys (WebAuthn)** — phishing-resistant passwordless sign-in, per-collection opt-in
- **30 OAuth2 providers** — Google, GitHub, Microsoft, GitLab, Discord, Apple, Facebook, Spotify, Twitch, Notion, Slack, LinkedIn, Kakao, VK, Yandex, and more; configure any with `<PROVIDER>_CLIENT_ID` / `<PROVIDER>_CLIENT_SECRET`
- Session management with revocation
- Rate limiting on auth endpoints
- API keys with prefix `gb_`

### 3. Realtime Engine

```javascript
const ws = new WebSocket('ws://localhost:8080/api/v1/realtime')

ws.send(JSON.stringify({
  type: 'subscribe',
  channel: 'posts'
}))

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data)
  // { event: 'record:create', channel: 'posts', data: {...} }
}
```

- PocketBase-style record topics (`posts/*`, `posts/{id}`)
- **Access rules enforced per subscriber** — events from locked/filtered collections are only delivered to clients the rules allow (fail-closed)
- Per-subscription filter expressions and field picking
- Cryptographically random client IDs, per-client subscription caps, reserved server event names
- SSE primary, WebSocket fallback; automatic reconnection; 10k+ concurrent connections

### 4. File Storage

- Local filesystem or S3-compatible backends
- Signed URLs for secure access
- **Auto-thumbnails** — `?thumb=100x100` (center crop), `100x100t` (top), `100x100f` (fit); generated on demand, cached, concurrency-capped
- **File tokens** — `POST /api/v1/files/token` mints a 3-minute token so protected files work in `<img>`/`<video>` tags (`?token=`)
- **Resumable uploads** — TUS protocol at `/api/v1/files/tus/` for large files over unreliable connections; rules and field constraints enforced
- Automatic MIME type detection
- File metadata tracking
- Per-collection file organization
- **Scheduled backups** — set `backup_cron`; snapshots prune to `backup_max_keep` and mirror to S3 with `backup_upload_s3`

### 5. Embedded ACME CA

**Gresbase is an ACME-compatible Certificate Authority.**

```bash
# Issue a certificate
gresbase cert issue example.com

# The ACME directory endpoint:
GET http://localhost:8080/api/v1/acme/directory

# Compatible clients:
# Caddy, Traefik, certbot, acme.sh, nginx-acme, lego
```

- Internal X.509 CA (ECDSA P-384)
- HTTP-01 challenge validation
- DNS-01 challenge hooks (planned)
- Automatic renewal (30 days before expiry)
- Certificate storage in PostgreSQL
- Wildcard certificate support architecture

### 6. Admin Dashboard

Built with:
- **Next.js 14** (App Router)
- **Tailwind CSS** (dark-mode-first)
- **shadcn/ui** components
- **Framer Motion** animations
- **TanStack Query** for data fetching
- **Zustand** for state management

Keyboard-first with Command Palette (⌘K) and ⌘B for sidebar toggle.

## API Reference

### Base URL

```
http://localhost:8080/api/v1
```

### Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/health` | No | Health check |
| POST | `/auth/login` | No | Admin login |
| POST | `/auth/refresh` | No | Refresh token |
| GET | `/collections` | Yes | List collections |
| POST | `/collections` | Yes | Create collection |
| GET | `/records/{collection}` | * | List records |
| POST | `/records/{collection}` | * | Create record |
| GET | `/files/{collection}/{id}/{file}` | No | Download file |
| GET | `/realtime` | No | WebSocket endpoint |
| GET | `/acme/directory` | No | ACME directory |
| GET | `/certificates` | Yes | List certificates |
| POST | `/certificates/issue` | Yes | Issue certificate |
| GET | `/admin/users` | Yes | List admins |
| GET | `/api-keys` | Yes | List API keys |
| GET | `/logs` | Yes | View audit logs |
| GET | `/settings` | Yes | Get settings |

*\*Collection-level access rules apply*

## CLI

```bash
gresbase serve              # Start the server
gresbase migrate            # Run database migrations
gresbase migrations         # Collection schema migrations: snapshot | list | apply
gresbase update             # Self-update (checksum-verified, keeps a .bak rollback)
gresbase admin create       # Create admin user
gresbase hooks init         # Scaffold ./gb_hooks JS hooks
gresbase cert issue         # Issue TLS certificate
gresbase --help             # Help
```

## Configuration

Configuration via `gresbase.yaml`, environment variables, or CLI flags:

| Key | Env | Default | Description |
|-----|-----|---------|-------------|
| `addr` | `ADDR` | `:8080` | Server address |
| `database_url` | `DATABASE_URL` | — | PostgreSQL URL (**required**) |
| `jwt_secret` | `JWT_SECRET` | — | JWT signing secret |
| `storage_backend` | — | `local` | `local` or `s3` |
| `storage_local_path` | `STORAGE_PATH` | `./storage` | Local storage path |
| `acme_enabled` | — | `false` | Enable ACME CA |
| `dev_mode` | — | `false` | Development mode |
| `log_level` | `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

## Tech Stack

### Backend (Go)
- `chi` — HTTP routing
- `pgx` — PostgreSQL driver
- `golang-jwt` — JWT handling
- `gorilla/websocket` — WebSocket
- `certmagic` — ACME client
- `zerolog` — Structured logging
- `cobra` — CLI framework

### Frontend (TypeScript)
- `Next.js 14` — React framework
- `Tailwind CSS` — Utility-first CSS
- `shadcn/ui` — Component library
- `Framer Motion` — Animations
- `TanStack Query` — Server state
- `Zustand` — Client state

## Deployment

See [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) for comprehensive guides covering:
- Single binary with systemd
- Docker & Docker Compose
- Kubernetes with ingress
- Fly.io, Railway
- Nginx reverse proxy
- Backup & restore
- Monitoring & troubleshooting

## Development

```bash
# Backend
cd backend
go run ./cmd/gresbase serve --dev

# Frontend
cd frontend
npm install
npm run dev
```

### Running Tests

```bash
cd backend
go test ./...

cd frontend
npm test
```

## Project Structure

```
gresbase/
├── backend/
│   ├── cmd/gresbase/        # CLI entry point (→ single binary)
│   ├── internal/
│   │   ├── acme/            # ACME CA subsystem
│   │   ├── api/             # HTTP server and routes
│   │   ├── app/             # Core application with interfaces
│   │   ├── auth/            # Authentication service
│   │   ├── collection/      # Dynamic PostgreSQL collections
│   │   ├── config/          # Configuration (YAML + env)
│   │   ├── database/        # PostgreSQL + embedded PG
│   │   ├── events/          # Event/hook system
│   │   ├── mailer/          # SMTP email delivery
│   │   ├── realtime/        # SSE + WebSocket engine
│   │   ├── storage/         # Local and S3 file storage
│   │   └── ui/              # Embedded dashboard (go:embed)
│   ├── migrations/          # SQL migrations (embedded)
│   └── gresbase.go          # Library entry point
├── frontend/                # Next.js admin dashboard (built & embedded)
├── Makefile                 # Build system (make build → single binary)
├── docker/                  # Docker configs
└── sdk/                     # Client SDKs (TS + Dart)
```

## Project Status

This is a **beta project**. Here's an honest assessment:

### What works well ✅
- **Security model** — Locked-by-default access rules enforced across REST, batch, search, files, and realtime delivery. Covered by integration regression tests against a real PostgreSQL.
- **Authentication** — Email/password, OAuth providers, OTP, magic links, API keys, end-user (record) auth with rate limiting.
- **Dynamic Collections** — Schema editor, PostgreSQL-backed tables, 14 field types, locked-by-default rules, import/export.
- **Records** — CRUD with rule-aware list compilation to SQL WHERE, rule-enforced relation expansion (forward, back-relation, nested), aggregations, field picking, transactional batch.
- **Realtime engine** — SSE primary / WebSocket fallback with per-subscriber rule enforcement, channel broadcast + presence, subscription caps, and unguessable client IDs.
- **Admin dashboard** — Collections workbench (schema, records, rules, API preview), admins, logs, metrics, settings, API keys, certificates, realtime tester. Embedded into the single binary.
- **Filter/query engine** — Expression-based filtering (`&&`/`||`/`AND`/`OR`), compiled to parameterized SQL.
- **Configuration** — YAML + env vars; production refuses weak or generated JWT secrets.
- **Integration test suite** — End-to-end tests boot an ephemeral PostgreSQL and exercise the real HTTP surface.

### Experimental 🚧
- **ACME CA** — Disabled by default (`acme_enabled`). HTTP-01 works; DNS-01 scaffolded. NOT production-ready for TLS — use a reverse proxy with certbot/Caddy.
- **JS plugins** — goja runtime, admin-gated routes, but no sandboxing; treat plugins as trusted code.
- **Multi-tenant** — Basic isolation support, light test coverage.
- **S3 storage** — Implemented and configurable from the dashboard; needs more real-provider testing.

### Not yet implemented 📋
- Published SDK packages — both SDKs are publish-ready (`sdk/typescript`: dual CJS/ESM build, 43 tests; `sdk/dart`: parity feature set, 26 tests); npm and pub.dev publication pending.
- Presence member lists are node-local in multi-node mode (join/leave events do propagate).

### Deliberately not built ❌
- **GraphQL** — in Supabase this is a pg extension plus gateway plumbing; the filter/expand/aggregate REST surface covers the same CRUD ground without another query language to secure.
- **Edge functions / Deno runtime, API gateway, connection pooler service, analytics sidecar** — each one is a second process with its own port and secrets. That's the Supabase self-host failure mode this project exists to avoid. JS file hooks and Go hooks cover the extensibility need in-process.

### Test Coverage

| Package | Coverage | Status |
|---------|----------|--------|
| metrics | 94.3% | ✅ |
| config | 88.5% | ✅ |
| openapi | 87.0% | ✅ |
| record | 84.7% | ✅ |
| security | 84.8% | ✅ |
| subscriptions | 96.9% | ✅ |
| events | 60.8% | 🟨 |
| filter | 63.6% | 🟨 |
| middleware | 55.6% | 🟨 |
| realtime | 45.0% | 🟨 |
| mailer | 34.6% | 🟨 |
| fields | 29.6% | 🟨 |
| jsplugin | 28.3% | 🟨 |
| app | 27.4% | 🟨 |
| storage | 25.1% | 🟨 |
| acme | 24.8% | 🟨 |
| search | 20.0% | 🟨 |
| tenant | 15.6% | 🔴 |
| api | 13.3% | 🔴 |
| query | 11.7% | 🔴 |
| job | 8.5% | 🔴 |
| auth | 7.2% | 🔴 |
| collection | 7.0% | 🔴 |
| database | 3.5% | 🔴 |
| settings | 0.9% | 🔴 |

Coverage figures predate the security re-engineering pass; the api, collection, and realtime packages have since gained substantial integration coverage.

### Roadmap

1. **Milestone 1 — Solid Core**
   - [x] Integration test suite against ephemeral PostgreSQL
   - [x] Locked-by-default access rules enforced on every read/write path
   - [x] Realtime rule enforcement and hardening
   - [ ] Deeper unit coverage for auth/collection/database packages

2. **Milestone 2 — Working Dashboard**
   - [ ] Wire all Next.js pages to the API
   - [ ] Add frontend tests (Vitest)
   - [ ] Collection schema editor
   - [ ] File browser

3. **Milestone 3 — Production-grade ACME**
   - [ ] Full ACME v2 compliance
   - [ ] DNS-01 challenge with common providers
   - [ ] Integration tests

4. **Milestone 4 — SDK Releases**
   - [ ] Publish TypeScript SDK to npm
   - [ ] Publish Dart SDK to pub.dev
   - [ ] SDK documentation

5. **Milestone 5 — Advanced Features**
   - [x] Row-Level Security at PostgreSQL level (generated from collection rules)
   - [x] Vector search (pgvector)
   - [x] Webhooks (HMAC-signed event forwarding)
   - [ ] Real-time presence

## License

MIT © Gresbase Contributors

---

Built with ❤️ by engineers who believe backend platforms should be simple, fast, and beautiful.
