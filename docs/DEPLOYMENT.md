# Gresbase Deployment Guide

This guide covers deploying Gresbase in production across multiple platforms.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Single Binary](#single-binary)
- [Docker](#docker)
- [Docker Compose](#docker-compose)
- [Kubernetes](#kubernetes)
- [Fly.io](#flyio)
- [Railway](#railway)
- [Nginx Reverse Proxy](#nginx-reverse-proxy)
- [TLS Configuration](#tls-configuration)
- [Backup & Restore](#backup--restore)
- [Monitoring](#monitoring)
- [Scaling Considerations](#scaling-considerations)

## Prerequisites

- **PostgreSQL 14+** (external or embedded)
- **JWT Secret**: A random 64-character hex string. Generate with `openssl rand -hex 32`
- **Ports**: 8080 (API), optional 443 (HTTPS with TLS)

---

## Single Binary

The simplest deployment — one static binary, zero dependencies:

```bash
# 1. Download the binary
curl -L https://github.com/gresbase/gresbase/releases/latest/download/gresbase_linux_amd64 -o /usr/local/bin/gresbase
chmod +x /usr/local/bin/gresbase

# 2. Set environment variables
export DATABASE_URL="postgres://user:password@localhost:5432/gresbase?sslmode=disable"
export JWT_SECRET="$(openssl rand -hex 32)"
export ADDR=":8080"

# 3. Run migrations
gresbase migrate

# 4. Create admin user
gresbase superuser create admin@example.com "your-secure-password"

# 5. Start the server
gresbase serve
```

### Systemd Service

Create `/etc/systemd/system/gresbase.service`:

```ini
[Unit]
Description=Gresbase Backend Platform
After=network.target postgresql.service

[Service]
Type=simple
User=gresbase
Group=gresbase
EnvironmentFile=/etc/gresbase/env
ExecStart=/usr/local/bin/gresbase serve
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now gresbase
```

---

## Docker

### Build

```bash
docker build -t gresbase:latest -f docker/Dockerfile .
```

### Run with external PostgreSQL

```bash
docker run -d \
  --name gresbase \
  -p 8080:8080 \
  -e DATABASE_URL="postgres://gresbase:password@host.docker.internal:5432/gresbase?sslmode=disable" \
  -e JWT_SECRET="$(openssl rand -hex 32)" \
  -e STORAGE_PATH="/app/storage" \
  -v gresbase_storage:/app/storage \
  gresbase:latest
```

---

## Docker Compose

The included `docker/docker-compose.yml` provides a complete stack with PostgreSQL:

```bash
cd docker
docker compose up -d
```

This starts:
- **gresbase**: The Gresbase server on port 8080
- **db**: PostgreSQL 16 on port 5432

### Production Configuration

Edit `docker/gresbase.example.yaml`:

```yaml
addr: ":8080"
database_url: "postgres://gresbase:${DB_PASSWORD}@db:5432/gresbase?sslmode=disable"
jwt_secret: "${JWT_SECRET}"
storage_backend: "s3"
storage_local_path: "/app/storage"
log_level: "info"
```

---

## Kubernetes

### Namespace & Secret

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: gresbase
---
apiVersion: v1
kind: Secret
metadata:
  name: gresbase-secrets
  namespace: gresbase
type: Opaque
stringData:
  database-url: "postgres://gresbase:${DB_PASSWORD}@postgres:5432/gresbase?sslmode=disable"
  jwt-secret: "${JWT_SECRET}"
```

### Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gresbase
  namespace: gresbase
spec:
  replicas: 1
  selector:
    matchLabels:
      app: gresbase
  template:
    metadata:
      labels:
        app: gresbase
    spec:
      containers:
        - name: gresbase
          image: gresbase:latest
          imagePullPolicy: IfNotPresent
          ports:
            - containerPort: 8080
              protocol: TCP
          env:
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: gresbase-secrets
                  key: database-url
            - name: JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: gresbase-secrets
                  key: jwt-secret
            - name: ADDR
              value: ":8080"
            - name: LOG_LEVEL
              value: "info"
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 500m
              memory: 512Mi
          livenessProbe:
            httpGet:
              path: /api/v1/health
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /api/v1/health
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
```

### Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: gresbase
  namespace: gresbase
spec:
  selector:
    app: gresbase
  ports:
    - port: 8080
      targetPort: 8080
      protocol: TCP
  type: ClusterIP
```

### Ingress (with cert-manager)

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: gresbase
  namespace: gresbase
  annotations:
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
spec:
  tls:
    - hosts:
        - api.example.com
      secretName: gresbase-tls
  rules:
    - host: api.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: gresbase
                port:
                  number: 8080
```

---

## Fly.io

```toml
# fly.toml
app = "gresbase"
primary_region = "iad"
kill_signal = "SIGINT"
kill_timeout = 5

[build]
  image = "gresbase:latest"

[env]
  DATABASE_URL = "postgres://..."
  JWT_SECRET = "..."

[[services]]
  protocol = "tcp"
  internal_port = 8080
  processes = ["app"]

  [[services.ports]]
    port = 80
    handlers = ["http"]

  [[services.ports]]
    port = 443
    handlers = ["tls", "http"]

  [services.concurrency]
    type = "connections"
    hard_limit = 25
    soft_limit = 20
```

---

## Railway

1. Create a new project from the GitHub repo
2. Add a PostgreSQL plugin
3. Set environment variables:
   - `DATABASE_URL`: Use the Railway PostgreSQL connection string
   - `JWT_SECRET`: Generate a random secret
4. Deploy — Railway auto-detects the Dockerfile

---

## Nginx Reverse Proxy

```nginx
server {
    listen 80;
    server_name api.example.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 86400;  # For WebSocket connections
    }

    # Increase body size for file uploads
    client_max_body_size 100M;
}
```

---

## TLS Configuration

### Option 1: Nginx + Let's Encrypt (Recommended)

Use certbot to obtain and auto-renew certificates:

```bash
sudo apt install certbot python3-certbot-nginx
sudo certbot --nginx -d api.example.com
```

### Option 2: Gresbase ACME CA (Experimental)

Gresbase can issue certificates via its embedded ACME CA. This feature is under active development and not recommended for production yet. See the ACME CA section in the README for details.

---

## Backup & Restore

### PostgreSQL Backup

```bash
# Backup
pg_dump -U gresbase -h localhost gresbase > gresbase_backup_$(date +%Y%m%d).sql

# Restore
psql -U gresbase -h localhost gresbase < gresbase_backup.sql
```

### Storage Backup

```bash
# Local storage
tar -czf storage_backup_$(date +%Y%m%d).tar.gz /app/storage/

# S3 (via AWS CLI)
aws s3 sync s3://my-bucket/ s3-backup/
```

### Automated Backup Cron

```bash
# /etc/cron.d/gresbase-backup
0 2 * * * gresbase pg_dump -U gresbase gresbase | gzip > /backups/gresbase_$(date +\%Y\%m\%d).sql.gz
0 3 * * * gresbase find /backups/ -name "*.sql.gz" -mtime +30 -delete
```

---

## Monitoring

### Health Check Endpoint

```
GET /api/v1/health
```

Returns JSON with server status, uptime, database status, and runtime metrics.

### Prometheus Metrics (Planned)

Not yet implemented — on the roadmap. For now, use the health endpoint and PostgreSQL monitoring tools.

### Logging

Gresbase uses structured logging with zerolog. Set `LOG_LEVEL=debug` for verbose output. Logs are written to stdout by default.

```bash
gresbase serve 2>&1 | tee gresbase.log
```

---

## Scaling Considerations

- **Gresbase is single-node.** The embedded PostgreSQL data directory is not designed for multi-node setups.
- **External PostgreSQL is required** for any multi-node scaling.
- **Realtime connections** are in-memory on a single instance. For horizontal scaling of realtime, use a separate set of stateless nodes with a shared message broker (NATS/Redis) — on the roadmap.
- **File storage** should use S3 for multi-node deployments so all nodes can access files.

---

## Environment Variables Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `JWT_SECRET` | Yes | — | HS256 signing key (min 32 chars) |
| `ADDR` | No | `:8080` | Listen address |
| `STORAGE_BACKEND` | No | `local` | `local` or `s3` |
| `STORAGE_LOCAL_PATH` | No | `./storage` | Local file storage path |
| `S3_ENDPOINT` | No | — | S3-compatible endpoint |
| `S3_ACCESS_KEY` | No | — | S3 access key |
| `S3_SECRET_KEY` | No | — | S3 secret key |
| `S3_BUCKET` | No | — | S3 bucket name |
| `S3_REGION` | No | `us-east-1` | S3 region |
| `LOG_LEVEL` | No | `info` | `debug`, `info`, `warn`, `error` |
| `DEV_MODE` | No | `false` | Enable development mode |

---

## Troubleshooting

### "Failed to connect to PostgreSQL"

- Verify `DATABASE_URL` is correct
- Ensure PostgreSQL is running and accessible
- Check firewall rules
- For embedded PostgreSQL, ensure the data directory is writable

### "JWT verification failed"

- Ensure `JWT_SECRET` is consistent across restarts
- Check token expiry (default: 15 minutes for access, 7 days for refresh)

### "File upload fails"

- Check `STORAGE_BACKEND` configuration
- For local storage, ensure the directory exists and is writable
- For S3, verify credentials and bucket permissions

### "WebSocket connection closed"

- Ensure the reverse proxy is configured for WebSocket upgrades
- Check `proxy_read_timeout` is high enough
- Verify the realtime feature is enabled in settings
