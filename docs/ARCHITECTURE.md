# Gresbase Architecture Document

## Overview

Gresbase is a production-grade, single-binary backend platform that combines:
- Dynamic PostgreSQL-backed collections
- Full authentication suite (like Supabase)
- Embedded ACME certificate authority (like Caddy)
- Realtime WebSocket engine
- File storage with S3 compatibility
- Vercel-quality admin dashboard

## System Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                        Client Layer                              │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐ │
│  │ TypeScript│  │   Go     │  │  REST    │  │   WebSocket      │ │
│  │   SDK     │  │   SDK    │  │  Client  │  │   Client         │ │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────────┬─────────┘ │
└───────┼─────────────┼─────────────┼─────────────────┼───────────┘
        │             │             │                 │
┌───────┴─────────────┴─────────────┴─────────────────┴───────────┐
│                      HTTP Server (chi)                           │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │                    Middleware Stack                       │    │
│  │  RequestID → RealIP → Logger → Recovery → CORS → CSP     │    │
│  │  → RateLimit → Auth → SecurityHeaders                    │    │
│  └──────────────────────────────────────────────────────────┘    │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────────┐    │
│  │  REST    │ │  Admin   │ │  ACME    │ │   Realtime/WS    │    │
│  │  APIs    │ │  Routes  │ │  Routes  │ │   Upgrader       │    │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └───────┬──────────┘    │
└───────┼─────────────┼─────────────┼──────────────┼──────────────┘
        │             │             │              │
┌───────┴─────────────┴─────────────┴──────────────┴──────────────┐
│                     Event / Hook System                          │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │  onBootstrap  onServe  onTerminate                       │    │
│  │  onModel{C,U,D}  onRecord{C,U,D}  onCollection{C,U,D}    │    │
│  │  onAuth{Login,Refresh}  onRealtime{Connect,Message}      │    │
│  │  onFile{Upload,Download} onCert{Issue,Renew}             │    │
│  └──────────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────────┘
        │
┌───────┴──────────────────────────────────────────────────────────┐
│                     Core Services                                │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────────┐    │
│  │   Auth   │ │Collection│ │ Storage  │ │   ACME CA        │    │
│  │ Service  │ │ Service  │ │ Service  │ │   Service        │    │
│  │          │ │          │ │          │ │                  │    │
│  │ JWT HS256│ │Dynamic   │ │Local/S3  │ │ X.509 ECDSA     │    │
│  │ Sessions │ │Schema→PG │ │SignedURLs│ │ ACME v2         │    │
│  │ OTP/ML   │ │Migration │ │Uploads   │ │ Auto-Renewal    │    │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └────────┬─────────┘    │
└───────┼─────────────┼─────────────┼──────────────┼──────────────┘
        │             │             │              │
┌───────┴─────────────┴─────────────┴──────────────┴──────────────┐
│                  Database Layer (pgx)                            │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │  Connection Pool → Query Builder → Transaction Support   │    │
│  │  Migration Runner → Lock Retry Logic                     │    │
│  └──────────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────────┘
        │
┌───────┴──────────────────────────────────────────────────────────┐
│                   PostgreSQL (v14+)                              │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────────┐    │
│  │ _tenants │ │ _admins  │ │_collections│ │ Dynamic Tables  │    │
│  │_api_keys │ │_sessions │ │_audit_logs│ │ _certificates  │    │
│  │_rate_limits│ │_acme_accounts│ │user tables│ │              │    │
│  └──────────┘ └──────────┘ └──────────┘ └──────────────────┘    │
└──────────────────────────────────────────────────────────────────┘
```

## Key Design Decisions

### 1. PostgreSQL over SQLite
- No file-locking issues in production
- Native JSONB for flexible schemas
- Connection pooling via pgxpool
- Row-level security support
- Full-text search with tsvector
- Better for horizontal scaling

### 2. chi Router over fiber/echo
- Standard library compatible (`net/http`)
- Clean middleware composition
- Excellent performance
- No magic — explicit and composable

### 3. pgx over GORM
- Direct SQL access, no ORM overhead
- Type-safe query building
- pgxpool for connection management
- Better performance for dynamic schemas

### 4. Event/Hook System
Every operation fires pre/post hooks:
```
Bootstrap → OnModelValidate → OnModelCreate → OnModelCreateExecute
→ OnModelAfterCreateSuccess | OnModelAfterCreateError
```

This enables:
- Custom validation logic
- Audit logging
- Triggered workflows
- Plugin extensions
- Multi-tenant isolation

### 5. Dynamic Schema → PostgreSQL
Collection schemas become real PostgreSQL tables:
```json
{
  "name": "posts",
  "schema": [
    {"name": "title", "type": "text"},
    {"name": "views", "type": "number"}
  ]
}
```
Becomes:
```sql
CREATE TABLE posts (
  id TEXT PRIMARY KEY DEFAULT gen_random_uuid(),
  title TEXT,
  views DOUBLE PRECISION,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### 6. ACME CA Architecture
- Internal CA using ECDSA P-384
- Full ACME v2 protocol support
- HTTP-01 challenge validation
- Certificate storage in PostgreSQL
- Auto-renewal 30 days before expiry
- Compatible with standard ACME clients

## Database Schema

### System Tables

| Table | Purpose |
|-------|---------|
| `_tenants` | Multi-tenant isolation |
| `_admins` | Superuser/admin accounts |
| `_collections` | Dynamic schema definitions |
| `_api_keys` | Programmatic access tokens |
| `_sessions` | JWT session tracking |
| `_audit_logs` | Activity trail |
| `_certificates` | TLS certificates |
| `_rate_limits` | Rate limiting state |
| `_acme_accounts` | ACME client accounts |
| `_gresbase_migrations` | Migration tracking |
| `{collection_name}` | User-defined dynamic tables |

## API Design

### REST Principles
- Plural resource names (`/collections`, `/records`)
- Consistent JSON error format
- Pagination via `page` and `perPage` params
- Field selection via `fields` param
- Sorting via `sort` param
- Filtering via `filter` param

### Realtime Protocol
WebSocket messages follow this format:
```json
{
  "type": "subscribe|unsubscribe|message|presence",
  "channel": "collection-name",
  "event": "record:create|record:update|record:delete",
  "data": {}
}
```

## Security Model

### Authentication
- JWT HS256 with configurable expiry
- Access + refresh token rotation
- Session invalidation on logout
- Rate limiting on auth endpoints (10/min for login, 5/min for registration)
- API keys with `gb_` prefix, hashed storage

### Authorization
- Collection-level access rules (list, view, create, update, delete)
- Admin role hierarchy (super_admin, admin, viewer)
- Multi-tenant isolation via `tenant_id`

### Transport Security
- CSP headers
- CORS configuration
- HSTS (when TLS enabled)
- XSS protection headers
- Request ID tracking

## Performance Characteristics

| Metric | Target |
|--------|--------|
| Startup time | <200ms |
| Memory (idle) | <50MB |
| Concurrent WebSocket | 10,000+ |
| API latency (p95) | <10ms |
| Database connections | 25 max (configurable) |

## Deployment Modes

### Single Binary
```bash
./gresbase serve
```

### Docker
```bash
docker compose up
```

### Kubernetes
Standard Deployment with PostgreSQL StatefulSet.

### Fly.io / Railway
Trivial deployment with `fly.toml` or Railway template.

## Extensibility

### Plugin Architecture
Plugins register hooks:
```go
app.OnRecordCreate().Bind(events.Handler{
    ID: "my-plugin",
    Func: func(e events.Event) error {
        // Custom logic
        return e.Next()
    },
})
```

### SDK Generation
OpenAPI spec auto-generated from route definitions.
TypeScript and Go SDKs maintained in `/sdk`.

## Frontend Architecture

### Tech Stack
- **Next.js 14** — App Router, Server Components
- **Tailwind CSS** — Dark-mode-first design system
- **shadcn/ui** — Accessible component primitives
- **Framer Motion** — Subtle, premium animations
- **TanStack Query** — Server state management
- **Zustand** — Client state management

### Design System
- 8px spacing grid
- Inter + JetBrains Mono fonts
- HSL color tokens with CSS variables
- Keyboard-first interactions
- Command palette (⌘K)
- Sidebar toggle (⌘B)
- Glass morphism effects (subtle)

### Pages
1. **Login** — Clean, minimal sign-in
2. **Overview** — Stats, quick actions, recent activity
3. **Collections** — Schema editor, CRUD
4. **Auth** — Providers, sessions, rate limiting
5. **APIs** — Endpoint reference, SDK info
6. **Realtime** — Connection monitor, code examples
7. **Storage** — File browser, S3 config
8. **Certificates** — ACME CA management
9. **Logs** — Audit trail with filtering
10. **Settings** — Platform configuration
11. **Users** — Admin management
12. **API Keys** — Key generation/revocation
13. **Metrics** — Performance dashboards

## Migration Strategy

Migrations are timestamped SQL files applied in order:

```
migrations/
├── 001_initial_schema.sql
├── 002_add_indexes.sql
└── 003_user_preferences.sql
```

The `_gresbase_migrations` table tracks applied migrations with checksums.

## Testing Strategy

- **Unit tests** — Go table-driven tests for services
- **Integration tests** — PostgreSQL testcontainers
- **API tests** — httptest for endpoints
- **WebSocket tests** — gorilla/websocket test helpers
- **Frontend tests** — Vitest + React Testing Library

## Future Roadmap

- [ ] Vector search extensions (pgvector)
- [ ] Edge Functions (JavaScript/TypeScript runtime)
- [ ] GraphQL subscription support
- [ ] Horizontal scaling with NATS
- [ ] DNS-01 ACME challenge
- [ ] Audit log retention policies
- [ ] Backup/restore to S3
- [ ] Multi-region replication
- [ ] Webhook triggers
- [ ] Custom email templates
- [ ] MFA (TOTP, WebAuthn)
- [ ] OAuth provider ecosystem
