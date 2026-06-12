# Security Policy

Thank you for helping keep Gresbase and its users safe.

## Supported Versions

Gresbase is pre-1.0 software. Security fixes are applied to the latest
release and the `main` branch only. Older tagged releases are not
back-patched.

| Version | Supported          |
| ------- | ------------------ |
| `main` / latest release | ✅ |
| Older releases | ❌ |

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Report privately using either of the following:

- Open a private security advisory:
  [github.com/gresbase/gresbase → Security → Report a vulnerability](https://github.com/gresbase/gresbase/security/advisories/new)
  (preferred — keeps the report attached to the repo).
- Or email **security@gresbase.dev**.

Please include:

- A description of the issue and its impact.
- Steps to reproduce (a minimal proof of concept is ideal).
- Affected version / commit, and your environment (embedded vs. external
  PostgreSQL, reverse proxy, etc.).

### Response expectations

This is a single-maintainer project. As a best-effort target:

- **Acknowledgement** within 5 business days.
- **Triage and an initial assessment** within 10 business days.
- A fix or mitigation timeline communicated once the issue is confirmed.

Please give us a reasonable opportunity to address the issue before any
public disclosure. We will credit reporters who wish to be acknowledged.

## Deployment Hardening

Gresbase can auto-start an **embedded PostgreSQL** in a single binary when no
`DATABASE_URL` is set. This mode downloads platform-specific PostgreSQL
binaries on first run and is intended for **development and small
deployments** — not for production or air-gapped environments.

For production deployments:

- **Set a strong `JWT_SECRET`** (e.g. a 64-character hex string). Production
  refuses to start with a missing, generated, or weak secret.
- **Use an external, managed or self-run PostgreSQL** via `DATABASE_URL`
  rather than the embedded instance.
- **Terminate TLS at a reverse proxy** (nginx, Caddy, Traefik, a cloud load
  balancer) rather than relying on the experimental embedded ACME CA.
- **Set `TRUSTED_PROXIES`** when running behind a reverse proxy so that
  client IPs (used for rate limiting and audit logging) are derived from the
  correct forwarded headers and cannot be spoofed.
- Keep experimental subsystems (embedded ACME CA, JS plugin runtime) disabled
  unless you understand and accept their trust model.

See [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) for full deployment guidance.
