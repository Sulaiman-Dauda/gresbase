.PHONY: all build build-frontend build-backend dev clean test frontend-dev

# Version — override with make VERSION=1.0.0
VERSION ?= 0.3.0
BUILD_TIME = $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT = $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS = -s -w \
	-X 'github.com/gresbase/gresbase.Version=$(VERSION)' \
	-X 'github.com/gresbase/gresbase.BuildTime=$(BUILD_TIME)' \
	-X 'github.com/gresbase/gresbase.GitCommit=$(GIT_COMMIT)'

# ──────────────────────────────────────────────
# ALL-IN-ONE: Build frontend + embed + backend
# Produces a SINGLE STATIC BINARY with everything inside.
# ──────────────────────────────────────────────
all: build

# Build the single binary (frontend embedded)
build: build-frontend embed-frontend build-backend
	@echo ""
	@echo "✅ Single binary built: backend/gresbase"
	@echo "   Size: $$(du -h backend/gresbase | cut -f1)"
	@echo "   Run:  cd backend && DATABASE_URL='' ./gresbase serve"
	@echo ""

# Build just the Go backend (requires frontend to be embedded first)
build-backend:
	cd backend && CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o gresbase ./cmd/gresbase

# Build the Next.js frontend as static files
build-frontend:
	@echo "Building frontend (Next.js static export)..."
	cd frontend && npm install --silent && npm run build
	@echo "Frontend built: frontend/out/"

# Copy built frontend into the embed location
embed-frontend:
	@rm -rf backend/internal/ui/dist
	@mkdir -p backend/internal/ui/dist
	@if [ -d frontend/out ]; then \
		cp -r frontend/out/* backend/internal/ui/dist/; \
		echo "Frontend embedded at backend/internal/ui/dist/"; \
	else \
		echo "⚠️  No frontend build found — binary will use built-in fallback UI"; \
		echo "   (Login/register/dashboard still works, but without Next.js)"; \
	fi

# ──────────────────────────────────────────────
# Development
# ──────────────────────────────────────────────
dev:
	cd backend && DATABASE_URL="" JWT_SECRET="gresbase-dev-secret" \
		go run ./cmd/gresbase serve --dev

frontend-dev:
	cd frontend && npm run dev

# ──────────────────────────────────────────────
# Testing
# ──────────────────────────────────────────────
test:
	cd backend && go test ./... -count=1 -timeout=120s

test-race:
	cd backend && go test -race ./... -count=1 -timeout=120s

# ──────────────────────────────────────────────
# Cross-compile (all platforms)
# ──────────────────────────────────────────────
build-all: build-frontend embed-frontend
	@echo "Building for all platforms..."
	cd backend && \
		GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o gresbase_linux_amd64   ./cmd/gresbase && \
		GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o gresbase_linux_arm64   ./cmd/gresbase && \
		GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o gresbase_darwin_amd64  ./cmd/gresbase && \
		GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o gresbase_darwin_arm64  ./cmd/gresbase && \
		GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o gresbase_windows_amd64.exe ./cmd/gresbase
	@echo "All platforms built in backend/"
	@ls -lh backend/gresbase_*

# ──────────────────────────────────────────────
# Utilities
# ──────────────────────────────────────────────
clean:
	rm -rf backend/gresbase backend/gresbase_* backend/internal/ui/dist frontend/out frontend/.next frontend/node_modules

banner:
	@echo ""
	@echo "  ⚡ Gresbase Build System"
	@echo "  ─────────────────────────"
	@echo "  make build       — single binary (embedded frontend)"
	@echo "  make dev         — run server in dev mode"
	@echo "  make test        — run all tests"
	@echo "  make build-all   — cross-compile for all platforms"
	@echo "  make clean       — remove build artifacts"
	@echo ""
