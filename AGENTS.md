# Gresbase — agent brief

Self-hosted backend platform: PostgreSQL-backed collections, authentication, realtime and file
storage in a single Go binary. README declares **status 1.0 — production-ready core**, with
locked-by-default rules and a dashboard. Go backend + embedded frontend + client SDK.

## Status: pre-launch, but the core is declared 1.0

No known production deployments yet, but the README makes a security claim ("secure by default",
"locked by default") that anything you change must continue to honour. This is an open-source-shaped
repo — it ships `LICENSE`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md` and `SECURITY.md` — so treat
the public API surface as something people would depend on.

## Run it

```sh
make dev            # backend + frontend for development
make frontend-dev   # frontend only
make build          # embeds the frontend into the binary
make build-all      # all targets
make build-backend
make embed-frontend
make clean
```

Layout: `backend/` (Go — `cmd/`, `cli/`, `examples/`), `frontend/` dashboard, `sdk/` client SDK,
`docker/` container assets, `docs/`.

## Tests

```sh
make test
make test-race      # run this for anything touching realtime or concurrent access
```

## Hard rules

1. **Never weaken the default access rules to make something work.** Collections are locked by
   default on purpose; opening them up is a security regression, not a convenience.
2. **Embedded single-binary PostgreSQL is for development and small deployments only.** Don't
   write code or docs that imply it's appropriate for production — the README is explicit that
   production uses an external PostgreSQL.
3. **Stage and show the diff; get an explicit go before committing or pushing.**
4. Changes to `sdk/` are public API. Breaking them needs a `CHANGELOG.md` entry.

## Gotchas

- `make build` embeds the frontend into the Go binary — if you change frontend code and only run
  `make build-backend`, you'll be testing a stale dashboard and wondering why.
