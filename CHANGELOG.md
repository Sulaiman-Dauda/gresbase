# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-06-12

First stable release. Gresbase is a single-binary, self-hosted backend platform
built on PostgreSQL: dynamic collections with locked-by-default access rules, a
full authentication suite, a rule-enforced realtime engine, file storage, and an
embedded admin dashboard.

### Security

- httpOnly + SameSite cookie auth with CSRF (double-submit) for the dashboard;
  tokens no longer stored in `localStorage`.
- Trusted-proxy-aware client IP (`TRUSTED_PROXIES`) replacing blind
  `X-Forwarded-For` trust; request body-size cap; configurable bcrypt cost;
  login anti-enumeration; filter-expression DoS limits; token-lookup key
  derived from `JWT_SECRET`.
- Fixed latent bugs surfaced during hardening: webhook signing-secret entropy,
  MFA backup-code reuse, OAuth state replay.
- Resolved all `govulncheck` findings — pinned the Go 1.25.11 toolchain (std-lib
  fixes) and updated `golang.org/x/image` to v0.39.0.

### Added

- Continuous integration pipeline (GitHub Actions): backend test (race +
  coverage), `gofmt`/`go vet`/`go mod tidy` checks, `golangci-lint`,
  `govulncheck`, and frontend lint + build.
- Certificate-file TLS support — serve TLS directly from a key pair via
  `TLS_CERT_FILE` / `TLS_KEY_FILE`.

### Removed

- Embedded ACME CA — terminate TLS at a reverse proxy (nginx, Caddy, Traefik)
  instead.
- Dynamic JS plugin runtime routes — the unsandboxed runtime-route plugin
  system is gone. File-based JS hooks in `./gb_hooks` are kept.
- Multi-tenant support — Gresbase is now a single-instance deployment.

[1.0.0]: https://github.com/gresbase/gresbase/releases/tag/v1.0.0
