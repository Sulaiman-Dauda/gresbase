# syntax=docker/dockerfile:1.7
#
# Gresbase production image — single static binary with the Next.js dashboard
# embedded, mirroring `make build`:
#   1. Build the Next.js dashboard as a static export (frontend/out/).
#   2. Embed it into the Go module (backend/internal/ui/dist/).
#   3. Compile one static, CGO-free Go binary (frontend baked in).
#   4. Ship only the binary on a minimal, non-root, pinned base image.
#
# Build from the REPO ROOT so both `frontend/` and `backend/` are in context:
#   docker build -f docker/Dockerfile -t gresbase:latest .
#
# Override the version stamp at build time:
#   docker build -f docker/Dockerfile --build-arg VERSION=1.0.0 -t gresbase:1.0.0 .

# ──────────────────────────────────────────────────────────────────────────
# Stage 1 — Build the Next.js dashboard (static export → frontend/out/)
# ──────────────────────────────────────────────────────────────────────────
# Pinned by digest for reproducibility. Refresh with:
#   skopeo inspect --no-tags docker://docker.io/library/node:22
FROM node:22@sha256:2d178f2785b96dfbf62a416ca2e40f50e30150b4ff3320d706f0d96e90600eb3 AS frontend-builder

WORKDIR /src/frontend

# Install deps first for better layer caching.
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci

COPY frontend/ ./
# next.config.js sets `output: 'export'`, so this produces frontend/out/.
RUN npm run build

# ──────────────────────────────────────────────────────────────────────────
# Stage 2 — Embed the dashboard and compile the static Go binary
# ──────────────────────────────────────────────────────────────────────────
# Backend module requires Go 1.25. Pinned by digest; refresh with:
#   skopeo inspect --no-tags docker://docker.io/library/golang:1.25
FROM golang:1.25@sha256:379065f16fe8cce7949001ba9cffc827cd4b93d69495dec405befd1c13e19bb3 AS backend-builder

# Version stamp — keep in sync with the Makefile LDFLAGS.
ARG VERSION=0.3.0
ARG BUILD_TIME
ARG GIT_COMMIT=unknown
# Provided automatically by buildkit; declared so it's usable in RUN and so
# `docker buildx build --platform linux/arm64` cross-compiles correctly.
ARG TARGETARCH

WORKDIR /src/backend

# Download modules first for better layer caching.
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY backend/ ./

# Embed the static dashboard into the Go module (same target as
# `make embed-frontend`: backend/internal/ui/dist/).
COPY --from=frontend-builder /src/frontend/out/ ./internal/ui/dist/

# Compute a build timestamp if one was not supplied.
RUN if [ -z "${BUILD_TIME}" ]; then BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"; fi; \
    echo "BUILD_TIME=${BUILD_TIME}" > /tmp/build_env

# Build flags match the Makefile (-s -w + version vars). Fully static,
# CGO disabled, so it runs on any minimal base.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    . /tmp/build_env && \
    CGO_ENABLED=0 GOOS=linux GOARCH="${TARGETARCH:-amd64}" \
    go build \
      -trimpath \
      -ldflags "-s -w \
        -X 'github.com/gresbase/gresbase.Version=${VERSION}' \
        -X 'github.com/gresbase/gresbase.BuildTime=${BUILD_TIME}' \
        -X 'github.com/gresbase/gresbase.GitCommit=${GIT_COMMIT}'" \
      -o /out/gresbase ./cmd/gresbase

# ──────────────────────────────────────────────────────────────────────────
# Stage 3 — Minimal runtime
# ──────────────────────────────────────────────────────────────────────────
# alpine:3.21 (not distroless) is used on purpose:
#   - the HEALTHCHECK shells out to wget, and
#   - if DATABASE_URL is empty the binary downloads/extracts embedded
#     PostgreSQL on first boot, which needs a real (if tiny) userland.
# For production we recommend an EXTERNAL PostgreSQL, so embedded mode is a
# convenience fallback, not the default for containers.
#
# Pinned by digest for reproducibility/supply-chain safety. Refresh with:
#   skopeo inspect --no-tags docker://docker.io/library/alpine:3.21
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d AS runtime

# ca-certificates: outbound TLS (S3, SMTP, OAuth, embedded-pg download).
# tzdata: correct timezone handling for scheduled backups / autodate fields.
# wget is part of busybox in alpine and is used by the HEALTHCHECK.
RUN apk add --no-cache ca-certificates tzdata

# Non-root runtime user (uid/gid 10001). No shell, no home write access
# beyond what we grant on the data dir.
RUN addgroup -g 10001 -S app && \
    adduser -u 10001 -S -G app -H -s /sbin/nologin app

# Writable data directory for storage, embedded PG (dev), backups, hooks, etc.
# DATA_DIR points here so relative paths resolve into the volume.
RUN mkdir -p /data && chown -R app:app /data

# Copy ONLY the binary. No source, no toolchain, no secrets.
COPY --from=backend-builder /out/gresbase /usr/local/bin/gresbase

WORKDIR /data
VOLUME ["/data"]

ENV ADDR=":8080" \
    DATA_DIR="/data" \
    STORAGE_PATH="./storage" \
    LOG_LEVEL="info"
# NOTE: no DATABASE_URL or JWT_SECRET baked in. Supply them at runtime.
# In production, set DATABASE_URL to an external PostgreSQL and JWT_SECRET
# to a strong, explicit value (the binary refuses to serve otherwise).

USER 10001:10001

EXPOSE 8080

# Hits the liveness endpoint mounted at /api/v1/health.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/api/v1/health || exit 1

ENTRYPOINT ["gresbase"]
CMD ["serve"]
