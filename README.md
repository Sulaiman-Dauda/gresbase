# Gresbase

> **PocketBase meets Supabase meets Caddy** — a modern, insanely fast, elegant backend platform with PostgreSQL, embedded ACME CA, realtime engine, and Vercel-quality dashboard.

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.6-3178C6?style=flat&logo=typescript)](https://www.typescriptlang.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

---

## What is Gresbase?

A **single-binary, self-hosted backend platform** that combines:

- **Dynamic Collections** — Define PostgreSQL-backed schemas from the dashboard or API
- **Authentication** — Email/password, OAuth, magic links, OTP, API keys
- **Realtime Engine** — WebSocket-based subscriptions with channel broadcasting
- **File Storage** — Local or S3-compatible storage with signed URLs
- **Embedded ACME CA** — Issue TLS certificates from your own CA, ACME-compatible
- **Admin Dashboard** — Dark-mode-first, Vercel-grade UI built with Next.js
- **Extensible** — Plugin architecture with event hooks at every level
- **Multi-tenant** — Built-in tenant isolation

## Quick Start

### Single Binary (Zero Dependencies)

Download the single static binary and run:

```bash
# Download latest release
curl -L https://github.com/gresbase/gresbase/releases/latest/download/gresbase_linux_amd64 -o gresbase
chmod +x gresbase

# Start — embedded PostgreSQL auto-starts, no dependencies needed!
./gresbase serve
```

**That's it.** One binary. Embedded PostgreSQL auto-starts on first run. The admin dashboard is built into the binary. Open http://localhost:8080 and you're in.

### With External PostgreSQL

```bash
DATABASE_URL="postgres://user:pass@host:5432/gresbase?sslmode=disable" \
  JWT_SECRET="your-secret-key" \
  ./gresbase serve
```

### From Source

```bash
git clone https://github.com/gresbase/gresbase
cd gresbase

# Build the single binary
make build

# Start
cd backend && ./gresbase serve
```

### CLI Commands

```bash
./gresbase serve                  # Start server (embedded PostgreSQL)
./gresbase superuser create admin@example.com mypassword  # Create admin
./gresbase migrate                # Run database migrations
./gresbase cert issue example.com # Issue TLS certificate
./gresbase backup create          # Create backup
./gresbase info                   # Show system info
./gresbase version                # Show version
```

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

### Docker

```bash
docker build -t gresbase -f docker/Dockerfile .
docker run -d \
  -e DATABASE_URL="postgres://..." \
  -p 8080:8080 \
  gresbase
```

### Fly.io

```toml
# fly.toml
app = "gresbase"
kill_signal = "SIGINT"
kill_timeout = 5

[build]
  image = "gresbase:latest"

[env]
  DATABASE_URL = "postgres://..."

[[services]]
  internal_port = 8080
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gresbase
spec:
  replicas: 1
  template:
    spec:
      containers:
        - name: gresbase
          image: gresbase:latest
          ports:
            - containerPort: 8080
          env:
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: gresbase-secrets
                  key: database-url
```

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

## License

MIT © Gresbase Contributors

---

Built with ❤️ by engineers who believe backend platforms should be simple, fast, and beautiful.
