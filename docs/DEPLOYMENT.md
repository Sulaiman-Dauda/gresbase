# Gresbase Deployment Guide

A concise, production-focused guide to self-hosting Gresbase. Gresbase ships
as a **single static binary** with the Next.js dashboard embedded — no runtime
dependencies beyond a database.

## Table of Contents

- [Database: external vs embedded](#database-external-vs-embedded)
- [Required & important environment variables](#required--important-environment-variables)
- [Running with Docker Compose](#running-with-docker-compose)
- [Running the single binary directly](#running-the-single-binary-directly)
- [TLS](#tls)
- [Backups](#backups)
- [Read replicas & multi-node](#read-replicas--multi-node)
- [Monitoring](#monitoring)
- [Hardening checklist](#hardening-checklist)
- [Environment variable reference](#environment-variable-reference)
- [Troubleshooting](#troubleshooting)

---

## Database: external vs embedded

Gresbase supports two database modes, selected by the presence of
`DATABASE_URL`:

- **External PostgreSQL (`DATABASE_URL` set)** — **required for production.**
  Use a managed or self-run **PostgreSQL 14+** instance. This is the only mode
  that supports multiple app nodes, read replicas, and WAL-based realtime.
- **Embedded PostgreSQL (`DATABASE_URL` empty)** — the binary downloads and
  runs a private PostgreSQL on first boot, storing data under the data
  directory. This is a **dev / small single-node convenience only**: it is
  single-node, the first-boot download needs outbound network access, and the
  data directory must be writable and persistent. **Do not use it for
  production.**

In containers, always set `DATABASE_URL` to an external PostgreSQL. The
provided `docker/docker-compose.yml` does exactly this.

---

## Required & important environment variables

These are the names the binary actually reads (bound in
`backend/internal/config/config.go`). YAML config keys exist too, but env vars
are the recommended way to supply secrets.

### Required in production

| Variable       | Notes                                                                                                     |
| -------------- | --------------------------------------------------------------------------------------------------------- |
| `JWT_SECRET`   | HS256 signing key. **Must be explicit, ≥ 32 chars, and not a placeholder.** The server refuses to serve otherwise (a generated/ephemeral secret would invalidate all sessions on restart). Generate with `openssl rand -hex 32`. |
| `DATABASE_URL` | External PostgreSQL DSN, e.g. `postgres://user:pass@host:5432/gresbase?sslmode=require`. Omit only for embedded dev mode. |
| `CORS_ALLOWED_ORIGINS` | Comma-separated list of browser origins allowed to call the API. **Must be explicit (no `*`) in production**; the server validates this at boot. |

> ES256 alternative: set `JWT_ALGORITHM=ES256` and provide a PEM
> `JWT_PRIVATE_KEY` (and optionally `JWT_PUBLIC_KEY` for the JWKS endpoint).
> In that case `JWT_SECRET` is not used.

### Important when behind a reverse proxy

- **`DOMAIN`** — the public base URL (e.g. `https://app.example.com`). Used for
  email links and as the WebAuthn relying party. Set it to your real domain.
- **`TRUSTED_PROXIES`** (comma-separated CIDRs) — by default Gresbase **ignores**
  `X-Forwarded-For` / `X-Real-IP` and uses the direct socket peer for rate
  limiting and audit logs, so a direct client cannot spoof its IP. When you run
  behind a reverse proxy, set `TRUSTED_PROXIES` to the proxy's address range(s)
  (e.g. `10.0.0.0/8,127.0.0.1/32`); only then are forwarding headers honored, and
  only when the request actually arrives from one of those peers. Configure the
  proxy to **overwrite** (not append) these headers. Never expose the port to the
  internet — a client could otherwise spoof its IP to defeat rate limiting and
  audit logging.

### Commonly useful

- `ADDR` (default `:8080`) — listen address.
- `STORAGE_BACKEND` (`local` | `s3`) and `STORAGE_PATH` for local storage.
  For S3: `S3_ENDPOINT`, `S3_BUCKET`, `S3_REGION`, `S3_ACCESS_KEY`,
  `S3_SECRET_KEY`, `S3_USE_SSL`. **Use S3 for any multi-node deployment.**
- `DATABASE_MAX_OPEN_CONNS`, `DATABASE_MAX_IDLE_CONNS`, `DATABASE_MAX_IDLE_TIME`
  — connection pool tuning.
- `LOG_LEVEL` (`debug` | `info` | `warn` | `error`).
- `BACKUP_CRON`, `BACKUP_MAX_KEEP`, `BACKUP_UPLOAD_S3` — see [Backups](#backups).

---

## Running with Docker Compose

`docker/docker-compose.yml` is a production-leaning example: an external
`postgres:17` plus the Gresbase image built from `docker/Dockerfile`, with all
secrets read from the environment.

```bash
# 1. Provide secrets (never commit the real .env).
cp docker/.env.example docker/.env
#    Then edit docker/.env and set at least:
#      JWT_SECRET        (openssl rand -hex 32)
#      POSTGRES_PASSWORD (a strong password)
#      CORS_ALLOWED_ORIGINS, DOMAIN

# 2. Build and start.
docker compose -f docker/docker-compose.yml up -d --build

# 3. Run schema migrations (idempotent).
docker compose -f docker/docker-compose.yml exec gresbase gresbase migrate

# 4. Create the first superuser.
docker compose -f docker/docker-compose.yml exec gresbase \
  gresbase superuser create admin@example.com 'a-strong-password'
```

The compose file:

- binds Gresbase to `127.0.0.1:8080` only (put your reverse proxy in front);
- does **not** publish PostgreSQL to the host;
- fails fast if `JWT_SECRET` or `POSTGRES_PASSWORD` are unset
  (`${VAR:?...}` syntax);
- waits for PostgreSQL to be healthy before starting Gresbase
  (`depends_on: condition: service_healthy`);
- runs as a non-root user and sets `no-new-privileges`.

### Building the image standalone

```bash
docker build -f docker/Dockerfile -t gresbase:latest .
```

The Dockerfile is multi-stage: it builds the Next.js static export, embeds it
into the Go module, compiles a static `CGO_ENABLED=0` binary, and ships only
that binary on a digest-pinned `alpine:3.21` base running as uid 10001. No
secrets are baked in.

---

## Running the single binary directly

Same binary, no container. Suitable for a VM with systemd.

```bash
# 1. Build (Go 1.25 + Node 22 toolchains required) or download a release.
make build                       # → backend/gresbase (frontend embedded)

# 2. Configure (external PostgreSQL recommended).
export DATABASE_URL="postgres://gresbase:pass@localhost:5432/gresbase?sslmode=require"
export JWT_SECRET="$(openssl rand -hex 32)"
export CORS_ALLOWED_ORIGINS="https://app.example.com"
export DOMAIN="https://app.example.com"

# 3. Migrate, create an admin, serve.
./gresbase migrate
./gresbase superuser create admin@example.com 'a-strong-password'
./gresbase serve
```

### systemd unit

`/etc/systemd/system/gresbase.service`:

```ini
[Unit]
Description=Gresbase
After=network.target postgresql.service

[Service]
Type=simple
User=gresbase
Group=gresbase
# Secrets live here (mode 0600), not in the unit file.
EnvironmentFile=/etc/gresbase/env
WorkingDirectory=/var/lib/gresbase
ExecStart=/usr/local/bin/gresbase serve
Restart=always
RestartSec=5
LimitNOFILE=65535
# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/gresbase

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now gresbase
```

---

## TLS

**Recommended: terminate TLS at a reverse proxy** and forward plaintext to
Gresbase on `127.0.0.1:8080`. The proxy handles certificates and renewal.

### Caddy (automatic Let's Encrypt)

```caddyfile
app.example.com {
    reverse_proxy 127.0.0.1:8080
    # Caddy streams WebSocket/SSE through reverse_proxy automatically.
}
```

### nginx + certbot

```nginx
server {
    listen 443 ssl;
    server_name app.example.com;

    ssl_certificate     /etc/letsencrypt/live/app.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/app.example.com/privkey.pem;

    client_max_body_size 100M;   # file uploads

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        # WebSocket / SSE realtime
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400;
        # Forwarded headers — OVERWRITE so clients cannot spoof them.
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Obtain/renew certs with `certbot --nginx -d app.example.com`.

### Direct TLS (no proxy)

Gresbase can serve HTTPS itself from operator-provided files:

```bash
export ENABLE_TLS=true
export TLS_CERT_FILE=/etc/gresbase/tls/fullchain.pem
export TLS_KEY_FILE=/etc/gresbase/tls/privkey.pem
```

You are then responsible for certificate renewal. For most deployments,
terminating TLS at a reverse proxy is simpler and is the recommended approach.

---

## Backups

Gresbase has a **built-in backup system** — prefer it over ad-hoc `pg_dump`.

### Scheduled backups

Set a cron expression and the server snapshots the database on schedule:

```bash
export BACKUP_CRON="0 2 * * *"   # daily at 02:00
export BACKUP_MAX_KEEP=7         # prune to the 7 most recent
export BACKUP_UPLOAD_S3=true     # also mirror to the configured S3 backend
```

`BACKUP_UPLOAD_S3=true` requires the S3 settings (`S3_*`) to be configured.

### Manual backups (CLI)

```bash
gresbase backup create [name]    # create a snapshot
gresbase backup list             # list snapshots
gresbase backup restore <name>   # restore from a snapshot
```

In Docker, prefix with
`docker compose -f docker/docker-compose.yml exec gresbase`.

Also back up your file storage if using the `local` backend (the `/data`
volume), or rely on S3 versioning when using the `s3` backend.

---

## Read replicas & multi-node

- **Read replicas:** set `DATABASE_REPLICA_URL` alongside an external
  `DATABASE_URL` to route replica-safe reads (record lists, aggregations,
  relation expansion) to a PostgreSQL read replica. Writes and rule-feeding
  reads stay on the primary; expect normal replication lag. If the replica is
  unreachable at boot, Gresbase logs a warning and serves all reads from the
  primary. At runtime a background health probe (every 10s) automatically
  fails reads over to the primary if the replica becomes unreachable and
  resumes routing to it once it recovers — no restart needed. Ignored in
  embedded mode.
- **Multi-node realtime:** set `REALTIME_MULTI_NODE=true` so record events fan
  out across nodes via PostgreSQL `LISTEN/NOTIFY` (no external broker). Requires
  external PostgreSQL.
- **File storage:** use S3 (`STORAGE_BACKEND=s3`) so every node can serve files.

---

## Monitoring

- **Liveness / readiness:** `GET /api/v1/health` and `GET /api/v1/ready`.
  The Docker image and compose healthchecks already hit `/api/v1/health`.
- **Prometheus metrics:** Gresbase exposes a Prometheus text endpoint at
  `/metrics` (enabled by default, `METRICS_ENABLED`). Gate scrapes with a
  static bearer token via `METRICS_TOKEN`; an empty token leaves it open for
  trusted networks.
- **Logs:** structured JSON to stdout (zerolog). Tune with `LOG_LEVEL`.

---

## Hardening checklist

- [ ] **Strong, explicit `JWT_SECRET`** (≥ 32 chars, from `openssl rand -hex 32`),
      stored as a secret — never committed, never a placeholder.
- [ ] **External PostgreSQL** with `sslmode=require` (or `verify-full`) in the DSN.
- [ ] **Explicit `CORS_ALLOWED_ORIGINS`** — no `*`. If you enable
      `CORS_ALLOW_CREDENTIALS`, a wildcard origin is rejected at boot.
- [ ] **Reverse proxy in front**, terminating TLS; Gresbase bound to
      `127.0.0.1` only. The container/host port is **not** exposed to the
      internet directly.
- [ ] **Proxy overwrites `X-Forwarded-For` / `X-Real-IP`** so client IPs (used
      for rate limiting and audit logs) cannot be spoofed.
- [ ] **Rate limiting on** (`RATE_LIMIT_ENABLED=true`, the default).
- [ ] **`DEV_MODE` is off** in production (it relaxes auth/validation).
- [ ] **`/metrics` protected** with `METRICS_TOKEN` if reachable beyond a
      trusted network.
- [ ] **JS file hooks are trusted code** — the hook runtime is **not**
      sandboxed and runs in-process. Only load first-party `HOOKS_DIR` files
      that you wrote and trust; set `HOOKS_DIR=""` to disable hooks entirely.
- [ ] **Run as non-root** (the image already uses uid 10001;
      add `no-new-privileges` / read-only root FS where possible).
- [ ] **Backups configured** (`BACKUP_CRON` + `BACKUP_MAX_KEEP`, ideally
      `BACKUP_UPLOAD_S3`) and **periodically test-restored**.
- [ ] **Keep secrets out of images and compose files** — supply them via
      `.env` / a secrets manager at runtime.

---

## Environment variable reference

Names below are the actual env bindings from
`backend/internal/config/config.go`.

| Variable                  | Required        | Default      | Description                                                  |
| ------------------------- | --------------- | ------------ | ------------------------------------------------------------ |
| `DATABASE_URL`            | Prod            | — (embedded) | External PostgreSQL DSN. Empty → embedded dev PostgreSQL.    |
| `DATABASE_REPLICA_URL`    | No              | —            | Read replica for lists/aggregations/expansion.              |
| `DATABASE_MAX_OPEN_CONNS` | No              | `25`         | Max open connections.                                        |
| `DATABASE_MAX_IDLE_CONNS` | No              | `5`          | Max idle connections.                                        |
| `DATABASE_MAX_IDLE_TIME`  | No              | `5m`         | Max connection idle time.                                    |
| `JWT_SECRET`              | Yes (HS256)     | —            | HS256 signing key, ≥ 32 chars, explicit.                     |
| `JWT_ALGORITHM`           | No              | `HS256`      | `HS256` or `ES256`.                                          |
| `JWT_KEY_ID`              | No              | `gresbase-default` | `kid` header / JWKS key id.                            |
| `JWT_PRIVATE_KEY`         | Yes (ES256)     | —            | PEM ECDSA P-256 private key (ES256 only).                    |
| `JWT_PUBLIC_KEY`          | No              | —            | PEM ECDSA P-256 public key for JWKS.                         |
| `CORS_ALLOWED_ORIGINS`    | Prod            | `*`          | Comma-separated browser origins. No `*` in prod.             |
| `CORS_ALLOW_CREDENTIALS`  | No              | `false`      | Allow credentialed cross-origin requests.                   |
| `ADDR`                    | No              | `:8080`      | Listen address.                                              |
| `DOMAIN`                  | Recommended     | —            | Public base URL (email links, WebAuthn RP).                 |
| `DATA_DIR`                | No              | `./gresbase_data` | Base dir for storage/embedded-PG/backups (image: `/data`). |
| `ENABLE_TLS`              | No              | `false`      | Serve HTTPS directly from cert/key files.                    |
| `TLS_CERT_FILE`           | If `ENABLE_TLS` | —            | PEM certificate (full chain).                                |
| `TLS_KEY_FILE`            | If `ENABLE_TLS` | —            | PEM private key.                                             |
| `STORAGE_BACKEND`         | No              | `local`      | `local` or `s3`.                                             |
| `STORAGE_PATH`            | No              | `./storage`  | Local storage path (resolved under `DATA_DIR`).              |
| `S3_ENDPOINT`             | No              | —            | S3-compatible endpoint.                                      |
| `S3_BUCKET`               | No              | —            | S3 bucket.                                                   |
| `S3_REGION`               | No              | —            | S3 region.                                                   |
| `S3_ACCESS_KEY`           | No              | —            | S3 access key.                                               |
| `S3_SECRET_KEY`           | No              | —            | S3 secret key.                                               |
| `S3_USE_SSL`              | No              | —            | Use TLS for S3.                                              |
| `BACKUP_CRON`             | No              | —            | Cron expr for scheduled DB backups.                          |
| `BACKUP_MAX_KEEP`         | No              | `7`          | Snapshots to retain.                                         |
| `BACKUP_UPLOAD_S3`        | No              | `false`      | Mirror backups to S3 (needs `S3_*`).                         |
| `RATE_LIMIT_ENABLED`      | No              | `true`       | Enable rate limiting.                                        |
| `RATE_LIMIT_RPS`          | No              | `100`        | Requests/sec.                                                |
| `RATE_LIMIT_BURST`        | No              | `200`        | Burst size.                                                  |
| `METRICS_ENABLED`         | No              | `true`       | Expose `/metrics`.                                           |
| `METRICS_TOKEN`           | No              | —            | Bearer token gating `/metrics` (empty = open).               |
| `REALTIME_MULTI_NODE`     | No              | `false`      | Cross-node realtime via LISTEN/NOTIFY.                       |
| `HOOKS_DIR`               | No              | `./gb_hooks` | Directory of `*.js` hooks (`""` disables).                   |
| `HOOKS_WATCH`             | No              | `true`       | Hot-reload hooks on change.                                  |
| `MIGRATIONS_DIR`          | No              | `./gb_migrations` | Schema migration files applied on boot.                 |
| `LOG_LEVEL`               | No              | `info`       | `debug` / `info` / `warn` / `error`.                         |
| `DEV_MODE`                | No              | `false`      | Development mode — **off in production**.                     |
| `TRUSTED_PROXIES`         | If behind proxy | (none)       | Comma-separated CIDRs whose `X-Forwarded-For`/`X-Real-IP` are trusted. |
| `MAX_REQUEST_BODY_BYTES`  | No              | `10485760`   | Max request body for non-upload routes (10 MiB).             |
| `BCRYPT_COST`             | No              | `10`         | Password hash work factor (clamped 10–14).                   |
| `ENABLE_TLS` / `TLS_CERT_FILE` / `TLS_KEY_FILE` | No | (none) | Serve HTTPS directly from a cert/key pair (else use a reverse proxy). |

> Note: set **`TRUSTED_PROXIES`** (CIDRs) when behind a reverse proxy so
> forwarded client IPs are honored — but only from those trusted peers. Unset,
> forwarding headers are ignored and the socket peer is used.

---

## Troubleshooting

**"JWT_SECRET is required / must be set explicitly in production"** — set a real
`JWT_SECRET` (≥ 32 chars, not a placeholder). The server refuses to use a
generated secret because it would invalidate sessions on every restart.

**"cors_allowed_origins must include at least one origin in production"** — set
`CORS_ALLOWED_ORIGINS` to your real origin(s); `*` is not allowed with
`CORS_ALLOW_CREDENTIALS=true`.

**Cannot connect to PostgreSQL** — verify `DATABASE_URL`, that PostgreSQL is
reachable, and `sslmode`. In Compose, ensure the `postgres` service is healthy
(`depends_on` waits for it).

**Client IPs all look like the proxy** — make the proxy set `X-Real-IP` /
`X-Forwarded-For` to the real client address (and overwrite, not append).

**WebSocket / SSE drops** — ensure the proxy upgrades connections and uses a
long `proxy_read_timeout` (see the nginx snippet).
