# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

## [1.0.0] - Planned

First stable release. Consolidates the changes listed under
[Unreleased](#unreleased): CI, cert-file TLS, and the removal of the embedded
ACME CA, the dynamic JS plugin runtime, and multi-tenant support.

[Unreleased]: https://github.com/gresbase/gresbase/compare/main...HEAD
[1.0.0]: https://github.com/gresbase/gresbase/releases/tag/v1.0.0
