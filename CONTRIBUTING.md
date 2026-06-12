# Contributing to Gresbase

Thanks for your interest in contributing! Gresbase is a self-hosted
PostgreSQL backend platform: a Go backend plus a Next.js admin dashboard,
shipped as a single static binary. This guide covers how to build, test, and
submit changes.

## Prerequisites

- **Go 1.25+** — required. This is forced by the `pgx/v5` dependency; older Go
  toolchains will not compile the module (`go.mod` declares `go 1.25.0`).
- **Node.js 22** — for building and developing the frontend.
- **A local PostgreSQL install** — the backend test suite shells out to
  `initdb` and `pg_ctl` to spin up an ephemeral PostgreSQL instance, so those
  binaries must be on your `PATH`. Install PostgreSQL (e.g. `postgresql` /
  `postgresql-contrib`) and ensure `initdb` and `pg_ctl` are reachable. On
  some distros they live under `/usr/lib/postgresql/<version>/bin` — add that
  directory to your `PATH`.
- **GNU Make** — the repo's workflows are driven through the `Makefile`.

## Building

The default build produces a single static binary with the frontend embedded:

```bash
make build        # build-frontend → embed-frontend → build-backend
```

This runs `npm install && npm run build` in `frontend/`, copies the static
export into `backend/internal/ui/dist/`, then compiles
`backend/cmd/gresbase` with `CGO_ENABLED=0` into `backend/gresbase`.

Other useful targets:

```bash
make build-frontend   # Next.js static export only
make build-backend    # Go binary only (frontend must already be embedded)
make build-all        # cross-compile for linux/darwin/windows (amd64/arm64)
make clean            # remove build artifacts and node_modules
```

## Running in development

```bash
make dev          # runs the backend with embedded PostgreSQL + a dev JWT secret:
                  #   DATABASE_URL="" JWT_SECRET="gresbase-dev-secret"
                  #   go run ./cmd/gresbase serve --dev

make frontend-dev # runs the Next.js dev server (cd frontend && npm run dev)
```

`make dev` starts an embedded PostgreSQL (no external database needed) — handy
for local iteration. For anything resembling production, point `DATABASE_URL`
at a real PostgreSQL instance and set a strong `JWT_SECRET`.

## Testing

```bash
make test                       # cd backend && go test ./... -count=1 -timeout=120s
make test-race                  # same, with the race detector
cd backend && go test ./...     # equivalent direct invocation
```

The tests boot an ephemeral PostgreSQL, so `initdb` and `pg_ctl` must be on
your `PATH` (see Prerequisites). If tests fail immediately with errors about a
missing `initdb`/`pg_ctl`, that PATH is the usual cause.

CI additionally runs the suite with `-race` and coverage; run `make test-race`
locally if you touch concurrency-sensitive code.

## Lint & formatting gate

CI enforces all of the following — please run them before opening a PR:

- **`gofmt`** — code must be formatted. CI fails if `gofmt -l .` reports any
  files. Run `gofmt -w .` (or `go fmt ./...`) in `backend/`.
- **`go vet ./...`** — must be clean.
- **`go mod tidy`** — `go.mod`/`go.sum` must be tidy with no diff.
- **`golangci-lint`** — the linter (config in `backend/.golangci.yml`).

  > **Important:** `golangci-lint` must be **built with go1.25**. If your
  > installed `golangci-lint` was compiled against an older Go toolchain it can
  > report spurious errors or fail to load packages. Install/build it with your
  > go1.25 toolchain (CI pins `golangci-lint` `v1.64.8`):
  >
  > ```bash
  > cd backend
  > golangci-lint run --timeout=5m
  > ```

For the frontend, CI runs `npm run lint` and `npm run build`; run both before
submitting frontend changes.

## Code style

- **Go:** standard `gofmt` formatting (CI-enforced). Keep packages cohesive and
  prefer the existing patterns in `backend/internal/...`.
- **Commit messages:** use [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`, etc.), matching the
  existing history.
- Keep changes focused. Update or add tests for behavior changes.

## Pull request process

1. Fork the repo and create a topic branch off `main`.
2. Make your change, with tests where it makes sense.
3. Run the full local gate:
   ```bash
   cd backend && gofmt -l . && go vet ./... && go mod tidy && golangci-lint run --timeout=5m
   make test
   ```
   (and `npm run lint && npm run build` in `frontend/` for frontend changes).
4. Use Conventional Commit messages.
5. Open a PR against `main`, filling out the PR template. Describe what changed
   and why, and link any related issues.
6. CI must pass (backend test/lint/vuln + frontend lint/build). A maintainer
   will review; please be responsive to feedback.

## Reporting security issues

Do **not** open public issues for security vulnerabilities — see
[SECURITY.md](SECURITY.md).

By contributing, you agree that your contributions are licensed under the
project's [MIT License](LICENSE).
