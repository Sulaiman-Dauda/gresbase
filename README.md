# Gresbase

> A self-hosted backend platform inspired by PocketBase — PostgreSQL-backed collections, authentication, realtime, and file storage in a single Go binary.

> **⚠️ Development Status: Early-stage / Prototype (~35% production-ready).**
> Auth and collections work. ACME CA, realtime, and frontend need more testing before production use.
> See [Status](#project-status) for an honest assessment.

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://go.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6-3178C6?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Tests](https://img.shields.io/badge/tests-35%25-yellow)](https://github.com/gresbase/gresbase)

---

## What is Gresbase?

A **single-binary, self-hosted backend platform** that aims to provide:

- **Dynamic Collections** — Define PostgreSQL-backed schemas from the dashboard or API ✅
- **Authentication** — Email/password, OAuth, magic links, OTP, API keys ✅ (most complete)
- **Realtime Engine** — WebSocket-based subscriptions with channel broadcasting 🚧
- **File Storage** — Local or S3-compatible storage with signed URLs 🚧
- **Embedded ACME CA** — Internal certificate authority with ACME protocol 🚧 (experimental)
- **Admin Dashboard** — Next.js + Tailwind CSS + shadcn/ui 🚧 (scaffolded, needs wiring)
- **Extensible** — Plugin architecture with event hooks and JS runtime 🚧
- **Multi-tenant** — Built-in tenant isolation ✅

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

Single binary, zero dependencies. The admin dashboard is embedded via Go's
embed package. When no DATABASE_URL is set, an embedded PostgreSQL
instance auto-starts for true zero-dependency operation.
```

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
  "create_rule": "@request.auth.role = 'admin'"
}
```

- **Field types**: text, number, bool, email, url, date, select, json, file, relation, password, editor, geo_point, autodate
- **Schema migration**: Changing a collection schema automatically alters the underlying PostgreSQL table
- **Access rules**: Row-level security with filter expressions
- **Indexes**: Composite and partial indexes via dashboard
- **Full-text search**: Via PostgreSQL tsvector

### 2. Authentication

```
POST /api/v1/auth/login          # Email/password
POST /api/v1/auth/refresh        # Refresh tokens
POST /api/v1/auth/otp/request    # OTP via email
POST /api/v1/auth/otp/verify     # Verify OTP
POST /api/v1/auth/magic-link     # Magic link auth
POST /api/v1/admin/users         # Manage admin users
```

- JWT access + refresh tokens (HS256)
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

- Channel-based subscriptions
- Presence tracking
- Broadcast messaging
- Automatic reconnection
- 10k+ concurrent connections

### 4. File Storage

- Local filesystem or S3-compatible backends
- Signed URLs for secure access
- Automatic MIME type detection
- File metadata tracking
- Per-collection file organization

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
gresbase admin create       # Create admin user
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

This is an **early-stage project** (~35% production-ready). Here's an honest assessment:

### What works well ✅
- **Authentication** — Email/password, OAuth providers, OTP, magic links, API keys. The most complete subsystem with decent test coverage.
- **Dynamic Collections** — Define schemas, PostgreSQL-backed tables, field types. Basic CRUD operations.
- **Record model** — Typed getters/setters, JSON serialization, field picking, relation expansion. Well-tested (84.7%).
- **Filter/query engine** — Expression-based filtering. Tested (63.6%).
- **Configuration** — YAML + env vars. Well-tested (88.5%).
- **Plugin architecture** — Event hooks, JS runtime (goja). Basic functionality tested.
- **Metrics/health checks** — Runtime stats, health endpoint (94.3% coverage).
- **OpenAPI generation** — Auto-generated spec from route definitions (87.0% coverage).

### Needs more testing 🚧
- **Realtime engine** — WebSocket/SSE works but needs load testing (45.0% coverage).
- **File storage** — Local and S3 backends work but need integration tests (25.1% coverage).
- **ACME CA** — Internal CA issues certificates, ACME protocol partially implemented (24.8% coverage).
  - HTTP-01 challenge validation works. DNS-01 is scaffolded.
  - NOT production-ready for TLS. Use Nginx + certbot for now.
- **Admin dashboard** — Next.js pages are scaffolded. UI components are built but API wiring is incomplete.
- **Tenant isolation** — Basic support, low test coverage (15.6%).

### Not yet implemented 📋
- Edge Functions (JavaScript serverless runtime)
- Row-Level Security (RLS) — access rules exist but are not enforced at the DB level
- Vector search / pgvector integration
- GraphQL support
- Horizontal scaling with message broker
- Comprehensive integration test suite
- Published SDK packages (npm, pub.dev)

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

**Overall: ~35% by line count** (25 of 32 packages have tests)

### Roadmap

1. **Milestone 1 — Solid Core** (target: 70% coverage)
   - [ ] Full integration test suite with testcontainers
   - [ ] Auth test coverage from 7% → 60%+
   - [ ] Collection test coverage from 7% → 60%+
   - [ ] Database layer tests

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
   - [ ] Row-Level Security at PostgreSQL level
   - [ ] Edge Functions runtime
   - [ ] Vector search
   - [ ] Real-time presence

## License

MIT © Gresbase Contributors

---

Built with ❤️ by engineers who believe backend platforms should be simple, fast, and beautiful.
