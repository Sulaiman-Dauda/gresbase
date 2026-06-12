# Gresbase Features Guide

This guide covers the capabilities that make Gresbase a PostgreSQL-native
self-hosted backend platform with serious power and minimal operational sprawl.
**The governing principle: PostgreSQL is the only piece of infrastructure.**
Everything below runs inside the single binary or inside your existing
PostgreSQL. No extra services, gateways, or sidecars.

- [Access rules (locked by default)](#access-rules-locked-by-default)
- [Rule presets & the rule simulator](#rule-presets--the-rule-simulator)
- [Aggregations](#aggregations)
- [Relation expansion](#relation-expansion)
- [Anonymous sign-in](#anonymous-sign-in)
- [Passkeys (WebAuthn)](#passkeys-webauthn)
- [Vector search (pgvector)](#vector-search-pgvector)
- [Typed SDK generation](#typed-sdk-generation)
- [Realtime](#realtime)
- [Broadcast channels & presence](#broadcast-channels--presence)
- [Image transforms](#image-transforms)
- [Resumable uploads (TUS)](#resumable-uploads-tus)
- [Customizable email templates](#customizable-email-templates)
- [JS file hooks (gb_hooks)](#js-file-hooks-gb_hooks)
- [Schema migrations (gb_migrations)](#schema-migrations-gb_migrations)
- [Scaling out: multi-node realtime with LISTEN/NOTIFY](#scaling-out-multi-node-realtime-with-listennotify)
- [Embedding Gresbase as a Go framework](#embedding-gresbase-as-a-go-framework)
- [Self-update](#self-update)
- [Operational hardening](#operational-hardening)
- [TLS](#tls)

---

## Access rules (locked by default)

Every collection has five rules — `list`, `view`, `create`, `update`, `delete` —
with **tri-state, locked-by-default** semantics:

| Value | Meaning |
|-------|---------|
| `null` (default) | **Locked** — only superusers can perform the operation |
| `""` | **Public** — anyone can perform the operation |
| `"<filter>"` | Evaluated per request/record (e.g. `owner = @request.auth.id`) |

A newly created collection is locked on every operation. You explicitly open what
should be public. Rules are enforced on **every** read/write path — REST list/view,
create/update/delete, transactional batch, full-text search, file downloads, and
realtime event delivery — and they fail closed.

Filter syntax:

```
status = "published"
owner = @request.auth.id
@request.auth.role = "admin" || owner = @request.auth.id
created_at > "2024-01-01" && published = true
tags ?= "go"          # value in array/JSON
title ~ "intro"       # contains
```

Operators: `=` `!=` `>` `>=` `<` `<=` `~` (contains) `!~` `?=` (in) `?!=` and
`&&` / `||` (also spelled `AND` / `OR`).

Request macros available in rules: `@request.auth.id`, `@request.auth.email`,
`@request.auth.role`, `@request.auth.verified`, `@request.auth.anonymous`,
`@request.auth.collection`, `@request.method`,
`@request.query.*`, `@request.body.*`.

---

## Rule presets & the rule simulator

Database row-level security is powerful but hard to test — a leaky policy is a
silent breach you only find in production. Gresbase keeps rules in the app layer
(easy to read and reason about) and adds two things that make them safe to
ship:

**Presets** — `GET /api/v1/collections/meta/rule-presets` returns a catalog:
`locked`, `public`, `authenticated`, `owner-only`, `verified-only`,
`admin-or-owner`. The dashboard schema editor exposes these as a dropdown.

**Simulator** — `POST /api/v1/collections/meta/rule-simulate` answers "would user
X pass this rule against this record?" *before* you ship it:

```bash
curl -X POST $API/collections/meta/rule-simulate \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{
        "rule": "owner = @request.auth.id",
        "auth": { "id": "u_123", "role": "member", "verified": true },
        "record": { "owner": "u_123", "title": "Hello" }
      }'
# → { "valid": true, "allowed": true, "resolvedFilter": "owner = \"u_123\"", "locked": false }
```

Invalid rules return `{ "valid": false, "error": "..." }` so you catch mistakes at
authoring time, not in production. The dashboard wires this into each rule editor
as a "Test" panel.

---

## Aggregations

`GET /api/v1/records/{collection}/aggregate` runs `count` / `sum` / `avg` /
`min` / `max` queries with optional grouping — dashboard numbers without
shipping rows to the client:

```bash
curl "$API/records/orders/aggregate?aggregate=count,sum:total&groupBy=status&sort=-sum_total"
# → { "items": [ { "status": "paid", "count": 41, "sum_total": 1290.5 }, ... ] }
```

```js
const { items } = await client.collection('orders').aggregate({
  aggregate: 'count,sum:total,avg:total',
  groupBy: 'status',
  filter: 'created_at > "2026-01-01"',
  sort: '-sum_total',
})
```

- `aggregate` — comma-separated: `count`, `sum:field`, `avg:field`, `min:field`,
  `max:field`. `sum`/`avg` require `number` fields. Result keys are `count` or
  `<fn>_<field>` (e.g. `sum_total`).
- `groupBy` — comma-separated group fields, returned alongside the aggregates.
- `filter` — the same filter syntax as listing.
- `sort` — aggregate aliases or group fields, `-` prefix for descending.
- `limit` — default 100, max 1000.

Aggregates are governed by the collection's **list rule** — the resolved rule is
compiled into the `WHERE` clause, so an aggregate never counts rows the
requester could not list.

---

## Relation expansion

`?expand=` resolves relations server-side, including back-relations and nested paths:

```bash
curl "$API/records/posts?expand=author"                  # forward relation
curl "$API/records/posts?expand=comments_via_post"       # back-relation
curl "$API/records/posts?expand=comments_via_post.user"  # nested, up to 6 levels
```

Back-relations use the `<collection>_via_<relationField>` form: records in
`comments` whose `post` relation points back at the listed post. Expanded
records are nested under each record's `expand` key.

Every level is rule-checked against the *target* collection: forward expansion
honors the target's **view rule**, back-relations honor the target's **list
rule**, and locked targets are silently skipped for non-superusers — expanding
can never leak records the caller could not fetch directly. Back-relations
fetch at most 1000 related records per request (newest first).

---

## Anonymous sign-in

Guest sessions without a sign-up form. Off by default: the auth collection must
opt in via the `allowAnonymous` option (a toggle in the dashboard schema
editor).

```bash
curl -X POST $API/collections/users/auth/auth-with-anonymous
# → { "token": "...", "refreshToken": "...", "record": { "id": "...", ... } }
```

```js
const { record } = await client.collection('users').authWithAnonymous()
```

Each call creates a fresh record in the collection and signs it in with an
`anonymous = true` token claim that **survives token refresh**. Rules can tell
guests apart with the `@request.auth.anonymous` macro:

```
owner = @request.auth.id && @request.auth.anonymous = false
```

Conversion path: set an identity (email) and password on the record later and
the user keeps their id and all their data.

---

## Passkeys (WebAuthn)

Phishing-resistant sign-in with Touch ID, Windows Hello, Android, or hardware
security keys — fully in-process, no external identity service. Off by
default: the auth collection must opt in via the `allowPasskeys` option (a
toggle in the dashboard schema editor, right next to anonymous sign-in).
Disabled collections answer `403` on every passkey endpoint.

```js
// While signed in (e.g. after authWithPassword): enroll a passkey.
await client.collection('users').registerPasskey('My laptop')

// Later: sign in with it. No email = discoverable (usernameless) login;
// with an email the server scopes the credential list to that account.
await client.collection('users').authWithPasskey()
await client.collection('users').authWithPasskey('ada@example.com')

// Manage your own passkeys (descriptors only — never key material).
const passkeys = await client.collection('users').listPasskeys()
await client.collection('users').deletePasskey(passkeys[0].id)
```

Endpoints (under `/api/v1/collections/{collection}/auth`): `POST
/passkey/register-begin`, `POST /passkey/register-finish` (both require a
record auth token for that collection), `POST /passkey/login-begin`, `POST
/passkey/login-finish` (public), plus `GET /passkeys` and `DELETE
/passkeys/{id}` for self-service management.

Details that matter:

- Credentials are created with `residentKey: preferred`, so modern
  authenticators produce **discoverable** credentials and users can sign in
  without typing an email.
- `login-begin` with an unknown email returns the same options shape (empty
  `allowCredentials`) as a known account without passkeys — no account
  enumeration.
- Ceremony state lives in the database (`_webauthn_sessions`, 5-minute TTL),
  so begin/finish round-trips work across multi-node deployments.
- A successful passkey login mints the exact same tokens and response shape
  as `auth-with-password`. No MFA second step is required: a passkey is
  already possession + on-device biometric/PIN.
- The relying party (RPID + origin) is derived from the Application URL in
  dashboard settings (falling back to the `domain` config) — the same source
  the mailer uses for absolute links.

---

## Vector search (pgvector)

Add a `vector` field to any collection to store embeddings and run semantic
search — the building block for AI and retrieval-augmented-generation features.
It costs **zero extra infrastructure**: pgvector is a PostgreSQL extension.

Define a vector field (dashboard schema editor, or via the API):

```json
{
  "name": "embedding",
  "type": "vector",
  "options": { "dimensions": 1536, "distance": "cosine", "index": "hnsw" }
}
```

- `dimensions` — the embedding size (e.g. 1536 for OpenAI `text-embedding-3-small`).
- `distance` — `cosine` (default), `l2`, or `inner`.
- `index` — `hnsw` (default, falls back to `ivfflat` on older pgvector), or `none`.

Store records with the embedding as a number array, then search:

```bash
curl -X POST $API/records/documents/search-vector \
  -d '{ "field": "embedding", "vector": [0.01, -0.2, ...], "limit": 10 }'
# → { "items": [ { "id": "...", "title": "...", "_distance": 0.0123 }, ... ] }
```

Results are ordered by similarity, annotated with `_distance`, and **respect the
collection's list rule** — a similarity search never returns rows the caller may
not see.

**Availability:** vector fields require the `vector` extension. Most managed
PostgreSQL providers ship it (Neon, RDS/Aurora with pgvector, the
`pgvector/pgvector` Docker image, and others). The zero-dependency embedded
PostgreSQL does **not**, so `GET /api/v1/features` reports `"vector": false`
there and the dashboard guides you to an external PostgreSQL. Creating a vector
collection on a server without pgvector returns a clear error instead of failing
mysteriously.

---

## Typed SDK generation

Get fully typed client ergonomics generated **at the app layer** — no extra
service to run. The server generates a TypeScript module from your live schema:

```bash
# From the dashboard: "Download SDK (.ts)"
# From the API (authenticated):
curl -H "Authorization: Bearer $TOKEN" $API/types.ts -o gresbase.ts
# From the CLI:
gresbase types > gresbase.ts
gresbase types --base-url https://api.example.com/api/v1 > gresbase.ts
```

You get one interface per collection (field types mapped to TS — `vector` →
`number[]`, `select` → a string union, etc.) plus a typed client with
`list/getOne/create/update/delete` per collection. Regenerate whenever your schema
changes.

The hand-maintained client (`sdk/typescript`, version 1.0.0 — build-ready,
registry publication pending) covers the full surface: record auth
(`authWithPassword`, `authWithAnonymous`, `authRefresh`,
password reset and email verification), `aggregate()`, realtime channels
(`subscribeToChannel`, `broadcast`, `presence`), and file URLs with transform
options (`files.getURL(..., { thumb, format, quality })`). The generated
`GET /api/v1/types.ts` module remains the source of schema-typed interfaces.

---

## Realtime

SSE (primary) and WebSocket (fallback) with Gresbase record topics:

```js
// subscribe to all records in a collection: "posts/*"
// subscribe to one record:                  "posts/<id>"
```

Realtime delivery is **rule-enforced per subscriber** — a client only receives
events for records its access rules allow (wildcard topics use the list rule,
single-record topics use the view rule), and it fails closed. Subscriptions
support per-subscription filter expressions and field picking. Client IDs are
cryptographically random (SSE subscription management is capability-gated), there's
a per-client subscription cap, and server event names are reserved so clients can't
spoof record events.

### WAL change capture (`realtime_wal_enabled`)

By default, record events are emitted by the API write handlers — rows changed
via the SQL console, `psql`, the generated RLS role, or any direct PostgreSQL
connection are invisible to subscribers. Setting `realtime_wal_enabled: true`
(env `REALTIME_WAL_ENABLED=1`) closes that gap: Gresbase opens an in-process
logical replication stream — no separate change-data-capture service — and
sources record events from the WAL itself. Every
committed INSERT/UPDATE/DELETE on a collection table reaches subscribers, no
matter who wrote it — and every event still passes the same per-subscriber rule
checks (locked collections stay locked; password fields are stripped).

While the WAL stream is healthy it is the single source of truth (API-emitted
events are suppressed, so API writes are not double-delivered). If the stream
drops, the API-emitted path resumes instantly and the capture reconnects with
backoff — `gresbase_realtime_wal_healthy` on `/metrics` tells you which mode
you're in.

Requirements & caveats:

- **`wal_level = logical`** on the server (`ALTER SYSTEM SET wal_level =
  logical;` + restart) and a role with `REPLICATION`. The embedded PostgreSQL
  is configured automatically; if the server can't do logical decoding,
  Gresbase logs the fix and keeps running on API-emitted events only.
- **Multi-node deployments need a UNIQUE replication slot per node** — set
  `realtime_wal_slot` differently on each node. Two nodes sharing a slot will
  each see only fragments of the stream.
- **An abandoned slot retains WAL.** PostgreSQL keeps WAL on disk for a slot
  until it is consumed. If you turn WAL capture off (or rename the slot), drop
  the old slot manually — `SELECT
  pg_drop_replication_slot('gresbase_realtime');` — or the server's disk will
  eventually fill.
- The publication (`realtime_wal_publication`, default `gresbase_realtime`)
  tracks collection record tables automatically: collection create/rename/
  delete updates it immediately, with a periodic reconciliation as backstop.
- DELETE events carry the record id only (PostgreSQL's default replica
  identity logs just the primary key), matching the API delete event shape.

---

## Broadcast channels & presence

Beyond record topics, authenticated clients can publish to arbitrary channels
and share presence state — typing indicators, cursors, who's-online lists:

```js
// Subscribe and announce your presence state on join
const unsub = client.realtime.subscribeToChannel('room:1', (msg) => {
  console.log(msg.event, msg.data)   // includes presence:join / presence:leave
}, { presence: { name: 'Ada' } })

// Publish to the channel
await client.realtime.broadcast('room:1', 'typing', { user: 'Ada' })

// Snapshot: subscriber count + declared presence states
const { clients, members } = await client.realtime.presence('room:1')
```

Or over REST:

```bash
curl -X POST $API/realtime/broadcast \
  -H "Authorization: Bearer $TOKEN" \
  -d '{ "channel": "room:1", "event": "typing", "data": { "user": "Ada" } }'
# → 204 No Content
```

Subscriptions that declare `options.presence` state trigger `presence:join` /
`presence:leave` events on the topic, and a `{"type": "presence"}` query over
the realtime connection returns `{topic, clients, members}`. Spoofing is
designed out: broadcasting requires authentication, reserved server event names
(`record:*`, `connection:*`, `subscription:*`, `presence*`) are rejected, and
channels that shadow a collection's record topics are rejected — a client
broadcast can never look like a server-generated record event. In multi-node
mode broadcasts cross nodes over LISTEN/NOTIFY; presence member lists are
node-local.

---

## Image transforms

File downloads accept on-the-fly thumbnail and format parameters:

```bash
curl "$API/files/posts/$RECORD_ID/cover.png?thumb=300x200&format=jpeg&quality=80"
```

```js
client.files.getURL('posts', recordId, 'cover.png', { thumb: '300x200', format: 'jpeg', quality: 80 })
```

- `thumb` — `WxH` (center crop), `WxHt` (top crop), `WxHf` (fit, no crop),
  `Wx` / `xH` (single axis); max 2048px per side.
- `format` — `jpeg` or `png` output (with `thumb`).
- `quality` — JPEG encode quality, 1–100.

Each variant is generated once, cached in storage, and served through the same
rule-checked download path as the original. WebP *sources* decode fine; WebP
*output* is not supported — the encoder would need cgo, and the binary stays
pure Go.

---

## Resumable uploads (TUS)

Large or flaky-connection uploads can use the [TUS protocol](https://tus.io)
at `POST /api/v1/files/tus/`, served in-process — no upload sidecar to run. An
upload targets an **existing record's file field** and attaches on completion:

```js
import * as tus from 'tus-js-client'

const upload = new tus.Upload(file, {
  endpoint: 'http://localhost:8080/api/v1/files/tus/',
  headers: { Authorization: `Bearer ${token}` },
  metadata: {
    collection: 'videos',
    recordId: record.id,
    field: 'media',
    filename: file.name,
  },
  onSuccess: () => console.log('attached'),
})
upload.start()
```

- **Rules enforced twice, fail-closed**: the collection's *update rule* is
  checked at upload creation (the authenticated identity is stamped server-side
  into the upload — clients can't inject it) and re-checked against the
  record's current state at completion. Locked collections (`null` rule) are
  superuser-only, as everywhere else.
- **Field constraints honored at completion**: actual bytes are MIME-sniffed
  against the field's `mimeTypes`, size against `max_size` (also enforced
  up-front via `Tus-Max-Size`; fields without a max get a 5 GiB cap), and
  multi-file fields respect `maxSelect`.
- On completion the file moves into the regular storage backend (local or S3),
  the record updates through the normal path — event hooks fire and realtime
  subscribers see the update — and the chunks are deleted.
- Extensions: `creation`, `creation-with-upload`, `termination` (DELETE, same
  identity required), `expiration`. Abandoned uploads are garbage-collected
  after 24h.
- **Multi-node caveat**: chunks are staged on the node's local disk
  (`{storage_local_path}/.tus_uploads`), so resumable uploads behind a load
  balancer need sticky routing to the same node for the duration of an upload.
- Record **creation** with files stays on the regular multipart endpoint;
  TUS is for attaching to records that already exist.

---

## Customizable email templates

Every transactional email — verification, OTP, magic link, password reset,
email change, sign-in alert, backup success/failure — can be customized from
**Dashboard → Settings → Email templates**. Subjects and HTML bodies are Go
templates with per-template placeholders (e.g. `{{.Link}}`, `{{.Code}}`,
`{{.AppName}}`) shown inline in the editor.

- Overrides live in instance settings; anything left empty uses the built-in
  default, and "Reset to default" clears an override.
- Overrides are **validated at save time** (parse + trial render — a broken
  template is rejected with a 400 naming the template). If a bad override ever
  reaches the mailer anyway, it logs a warning and falls back to the default:
  template editing can never break delivery.
- Programmatic access: the `email_templates` section of `GET/PUT
  /api/v1/settings`, plus `GET /api/v1/settings/email-templates` for defaults +
  placeholder metadata (superuser-only, like all settings).

---

## JS file hooks (gb_hooks)

File-based hooks: drop `*.js` files into `./gb_hooks` and they load
at boot and hot-reload on change (~2s poll — no filesystem-watcher dependency):

```bash
gresbase hooks init   # scaffolds gb_hooks/ with types.d.ts + example.js.tmpl
```

```js
// gb_hooks/example.js
/// <reference path="./types.d.ts" />
registerHook("onRecordCreate", (e) => {
  console.log("record created in", e.collectionName, "id:", e.recordId);
  $http.post("https://example.com/notify", { id: e.recordId });
});
```

Writing to the server's filesystem already implies full trust, so hooks run
**unsandboxed** in the server process — trusted code only. Configure with
`hooks_dir` / `hooks_watch`
(`HOOKS_DIR`, `HOOKS_WATCH`); set `HOOKS_DIR=""` to disable. A hook file that
fails to compile is skipped with a warning so one broken hook cannot take the
others down.

---

## Schema migrations (gb_migrations)

Migration files for collection schemas, so your data model
lives in git and deploys reproducibly. Each file is a declarative JSON
snapshot — applying it upserts every listed collection to exactly that
definition (the underlying Postgres table is ALTERed automatically) and
removes anything listed in `deleted`. Apply is idempotent by construction:

```json
{
  "formatVersion": 1,
  "name": "create articles",
  "createdAt": "2026-06-12T10:00:00Z",
  "collections": [ { "name": "articles", "type": "base", "schema": [ ... ],
                     "list_rule": null, "view_rule": "", "create_rule": "title != ''" } ],
  "deleted": []
}
```

Files live in `./gb_migrations/` (configure with `migrations_dir` /
`MIGRATIONS_DIR`), named `{unix_timestamp}_{slug}.json`, applied in filename
order on boot — after Gresbase's own internal migrations, before serving.
A malformed or failing file aborts startup with the filename in the error
(fail-closed: a half-applied schema is worse than not starting). Applied files
are tracked in the `_collection_migrations` table. Collection snapshots use
the same JSON shape as the export/import API, so files are hand-editable; the
tri-state access rules round-trip exactly (`null` = locked, `""` = public,
expression = filter).

In **dev mode**, automigrate is on: every collection create/update/delete made
through the dashboard or API writes a migration file automatically,
pre-marked as applied (your live database already has the state). Commit the
files; production replays them on boot.

```bash
gresbase migrations snapshot [name]  # baseline an existing instance: one file
                                     # with all non-system collections, marked applied
gresbase migrations list             # applied/pending status per file
gresbase migrations apply            # apply pending without serving (CI/deploy)
```

Honest caveat on branches: two files touching the same collection apply in
timestamp order — last snapshot wins. Merging branches that both changed a
collection does not field-merge; the newest file is the effective definition.
Re-run `migrations snapshot` after a messy merge to re-baseline. Bulk imports
through `POST /collections/import` do not emit automigrate files — take a
snapshot afterwards if you want them in history. System collections are never
written to or modified by migration files.

---

## Scaling out: multi-node realtime with LISTEN/NOTIFY

A single Gresbase node already scales reads via Postgres read replicas and a big
box: set `DATABASE_REPLICA_URL` and record lists, aggregations, and relation
expansion route to a read replica while writes stay on the primary (see
[Read Replicas](DEPLOYMENT.md#read-replicas) in the deployment guide). To run
**multiple app nodes** behind a load balancer with realtime that works across
all of them, enable cluster mode:

```yaml
realtime_multi_node: true   # or REALTIME_MULTI_NODE=true
```

When on, record events are published over a Postgres `LISTEN/NOTIFY` channel; every
node listens and fans out to its own local subscribers (with the same rule
enforcement). **No Redis, no NATS, no message broker** — the database you already
run is the bus. This turns the app tier horizontal using only PostgreSQL.

(Events larger than ~7KB are delivered without the record body on multi-node
deployments; subscribers still receive the change and can refetch.)

---

## Embedding Gresbase as a Go framework

Use Gresbase as a library and write hooks in **real Go** (not a sandboxed JS
runtime). Hooks registered before `Start()` bind to the live event bus. See
[`backend/examples/embed`](../backend/examples/embed) for a runnable example:

```go
gb := gresbase.New()

gb.OnRecordCreate().BindFunc(func(e events.Event) error {
    if re, ok := e.(*events.RecordEvent); ok {
        log.Printf("record created in %s: %s", re.CollectionName, re.RecordID)
    }
    return e.Next()
})

gb.OnServe().BindFunc(func(e events.Event) error {
    if se, ok := e.(*events.ServeEvent); ok {
        se.Router.Get("/api/v1/custom/ping", pingHandler)
    }
    return e.Next()
})

gb.Start()
```

Available hooks include `OnRecordCreate/Update/Delete`, `OnCollectionCreate/Update/
Delete`, `OnAuthLogin`, `OnBootstrap`, `OnServe`, `OnTerminate`, and more.

---

## Self-update

```bash
gresbase update           # upgrade the binary in place from the latest release
gresbase update --check   # just report whether an update exists
```

The updater compares the latest GitHub release tag against the running
version, downloads the platform archive
(`gresbase_{version}_{os}_{arch}.zip`), verifies its SHA-256 against the
release's `checksums.txt` (any mismatch aborts with nothing replaced), and
atomically swaps the executable — keeping the previous binary as
`gresbase.bak` for instant rollback. Restart the server to apply. Use
`--repo owner/name` or `GRESBASE_UPDATE_REPO` to update from a fork.

---

## Operational hardening

Small things that decide whether self-hosting is calm or terrifying:

- **Backup failure alerts** — when a scheduled (`backup_cron`) backup fails,
  every superuser is emailed the error, timestamp, and instance address
  (requires SMTP; throttled to one alert per hour; manual backups never
  alert — you're watching those).
- **Job panic recovery** — a panicking cron/job handler can't crash the
  server: the panic is caught, logged with a stack trace, and recorded as a
  failed run while the scheduler keeps going.
- **Realtime max connection age** — `realtime_max_connection_age` (default
  `30m`, `0` disables) cleanly closes SSE/WebSocket connections past the cap
  so zombie connections can't accumulate; clients reconnect automatically.
- **Embedded PostgreSQL 17 with a data-dir guard** — new embedded data
  directories run PostgreSQL 17; existing directories keep the major version
  recorded in their `PG_VERSION` file (never auto-upgraded across majors), and
  unrecognized clusters fail closed instead of risking data.

---

## TLS

Gresbase does not run its own certificate authority. Terminate TLS one of two
ways:

- **Reverse proxy (recommended for production)** — front Gresbase with Caddy,
  nginx, or Traefik and let it handle certificates (e.g. via Let's Encrypt).
  Gresbase listens on plain HTTP behind the proxy.
- **Operator-provided certificates** — set `ENABLE_TLS=true` and point
  `TLS_CERT_FILE` / `TLS_KEY_FILE` at your own certificate and key, and Gresbase
  serves HTTPS directly.

`GET /api/v1/features` reports which optional capabilities this deployment has.
