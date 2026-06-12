# Gresbase API Reference

Base URL: `http://localhost:8080/api/v1`

## Authentication

All admin endpoints require a JWT token sent as a Bearer header:
```
Authorization: Bearer <token>
```

API keys can also be used with the same header format:
```
Authorization: Bearer gb_<api-key>
```

---

## Health

### `GET /health`
Health check endpoint.

**Response** `200 OK`
```json
{
  "status": "healthy",
  "version": "1.0.0",
  "uptime": "2h34m15s",
  "uptime_ms": 9255000,
  "components": {
    "api": { "status": "healthy" },
    "database": { "status": "healthy" },
    "realtime": { "status": "healthy" }
  },
  "system": {
    "go_version": "go1.25.0",
    "num_cpu": 4,
    "num_goroutines": 25,
    "alloc_mb": 12.5
  },
  "database": {
    "status": "healthy",
    "mode": "embedded",
    "open_connections": 2,
    "idle_connections": 1,
    "max_connections": 25
  }
}
```

---

## Authentication

### `POST /auth/login`
Authenticate as an admin user.

**Request Body**
```json
{
  "email": "admin@example.com",
  "password": "your-password"
}
```

**Response** `200 OK`
```json
{
  "token": "eyJhbGci...",
  "refreshToken": "eyJhbGci...",
  "admin": {
    "id": "abc123",
    "email": "admin@example.com",
    "role": "admin",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

### `POST /auth/refresh`
Refresh an expiring access token.

**Request Body**
```json
{
  "refreshToken": "eyJhbGci..."
}
```

**Response** `200 OK`
```json
{
  "token": "eyJhbGci...",
  "refreshToken": "eyJhbGci..."
}
```

### `POST /auth/register`
Register a new admin (requires super admin).

**Request Body**
```json
{
  "email": "new-admin@example.com",
  "password": "secure-password",
  "role": "admin"
}
```

### `POST /auth/logout`
Invalidate the current session.

---

## Admin Users

### `GET /admin/users`
List all admin users.

**Query Parameters**
| Param | Type | Default | Description |
|-------|------|---------|-------------|
| page | int | 1 | Page number |
| perPage | int | 30 | Items per page |

**Response** `200 OK`
```json
{
  "items": [
    {
      "id": "abc123",
      "email": "admin@example.com",
      "role": "admin",
      "created_at": "2024-01-01T00:00:00Z"
    }
  ],
  "page": 1,
  "perPage": 30,
  "totalItems": 1,
  "totalPages": 1
}
```

### `POST /admin/users`
Create a new admin user.

### `GET /admin/me`
Get the currently authenticated admin.

### `DELETE /admin/users/{id}`
Delete an admin user.

---

## Collections

### `GET /collections`
List all collections.

**Response** `200 OK`
```json
[
  {
    "id": "col_abc",
    "name": "posts",
    "type": "base",
    "schema": [
      { "name": "title", "type": "text", "required": true },
      { "name": "content", "type": "editor" }
    ],
    "list_rule": null,
    "create_rule": "@request.auth.role = 'admin'",
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

### `POST /collections`
Create a new collection.

**Request Body**
```json
{
  "name": "posts",
  "type": "base",
  "schema": [
    { "name": "title", "type": "text", "required": true },
    { "name": "content", "type": "editor" },
    { "name": "published", "type": "bool" }
  ],
  "list_rule": "published = true",
  "create_rule": "@request.auth.role = 'admin'"
}
```

### `PUT /collections/{id}`
Update a collection schema.

### `DELETE /collections/{id}`
Delete a collection.

---

## Typed SDK

### `GET /types.ts`
Generate a TypeScript module from the live schema (requires authentication): one interface per collection plus a typed client. See also the hand-maintained `gresbase-sdk` package on npm.

```bash
curl -H "Authorization: Bearer $TOKEN" $API/types.ts -o gresbase.ts
```

---

## Records

### `GET /records/{collection}`
List records in a collection.

**Query Parameters**
| Param | Type | Default | Description |
|-------|------|---------|-------------|
| page | int | 1 | Page number |
| perPage | int | 30 | Records per page (max 200) |
| filter | string | — | Filter expression |
| sort | string | `-created_at` | Sort field(s), prefix with `-` for desc |
| expand | string | — | Comma-separated relation paths to expand (see [Relation Expansion](#relation-expansion)) |
| fields | string | `*` | Comma-separated fields to return |
| skipTotal | bool | false | Skip total count for performance |

**Example**
```
GET /api/v1/records/posts?page=1&perPage=10&filter=published=true&sort=-created_at
```

**Response** `200 OK`
```json
{
  "items": [
    {
      "id": "rec_abc",
      "title": "Hello World",
      "content": "Post content...",
      "published": true,
      "created_at": "2024-01-15T10:00:00Z",
      "updated_at": "2024-01-15T10:00:00Z"
    }
  ],
  "page": 1,
  "perPage": 10,
  "totalItems": 42,
  "totalPages": 5
}
```

### `POST /records/{collection}`
Create a new record.

**Request Body**
```json
{
  "title": "New Post",
  "content": "Some content",
  "published": true
}
```

### `GET /records/{collection}/{id}`
Get a single record by ID.

### `PUT /records/{collection}/{id}`
Update a record.

### `PATCH /records/{collection}/{id}`
Partially update a record.

### `DELETE /records/{collection}/{id}`
Delete a record.

### `GET /records/{collection}/aggregate`
Run aggregate queries over a collection. Read access is governed by the collection's **list rule**, exactly like listing — the resolved rule is compiled into the `WHERE` clause, so aggregates never count rows the requester could not list.

**Query Parameters**
| Param | Type | Default | Description |
|-------|------|---------|-------------|
| aggregate | string | — | **Required.** Comma-separated functions: `count`, `sum:field`, `avg:field`, `min:field`, `max:field` (max 10) |
| groupBy | string | — | Comma-separated fields to group by (max 5) |
| filter | string | — | Filter expression, combined with the list rule |
| sort | string | — | Aggregate aliases or `groupBy` fields, prefix with `-` for desc |
| limit | int | 100 | Maximum result rows (max 1000) |

`sum` and `avg` require `number` fields; `min`/`max` accept any field. Result keys are named after the alias: `count` for `count`, `<fn>_<field>` otherwise (e.g. `sum_total`).

**Example**
```
GET /api/v1/records/orders/aggregate?aggregate=count,sum:total&groupBy=status&sort=-sum_total
```

**Response** `200 OK`
```json
{
  "items": [
    { "status": "paid", "count": 41, "sum_total": 1290.5 },
    { "status": "pending", "count": 7, "sum_total": 310 }
  ]
}
```

### Relation Expansion

The `expand` parameter on record list/get endpoints resolves relations server-side and nests the related records under each record's `expand` key:

| Form | Example | Description |
|------|---------|-------------|
| Forward | `?expand=author` | Expands a `relation` field on the listed records |
| Back-relation | `?expand=comments_via_post` | `<collection>_via_<relationField>` — records in `comments` whose `post` relation points back at this record |
| Nested | `?expand=comments_via_post.user` | Dot-separated path, expanded level by level, up to 6 levels deep |

**Example**
```
GET /api/v1/records/posts?expand=author,comments_via_post.user
```

**Response** `200 OK`
```json
{
  "items": [
    {
      "id": "rec_abc",
      "title": "Hello World",
      "author": "rec_user1",
      "expand": {
        "author": { "id": "rec_user1", "name": "John Doe" },
        "comments_via_post": [
          {
            "id": "rec_c1",
            "post": "rec_abc",
            "text": "Nice post",
            "expand": { "user": { "id": "rec_user2", "name": "Jane" } }
          }
        ]
      }
    }
  ]
}
```

Each level is rule-checked against the **target** collection: forward expansion honors the target's view rule, back-relations honor the target's list rule, and locked targets are silently skipped for non-superusers. Back-relations return at most 1000 related records per request (newest first).

---

## Record Authentication (Auth Collections)

For collections with an `"auth"` type, the following endpoints enable end-user authentication. Replace `{collection}` with the auth collection name (e.g., `users`).

### `POST /collections/{collection}/auth/auth-with-password`
Authenticate with email/username and password.

**Request Body**
```json
{
  "identity": "user@example.com",
  "password": "their-password"
}
```

**Response** `200 OK`
```json
{
  "token": "eyJhbGci...",
  "refreshToken": "eyJhbGci...",
  "record": {
    "id": "rec_user1",
    "email": "user@example.com",
    "name": "John Doe"
  }
}
```

### `POST /collections/{collection}/auth/auth-with-anonymous`
Create and sign in a throwaway anonymous record. No request body.

Disabled by default — the auth collection must opt in via the `allowAnonymous` collection option (a toggle in the dashboard schema editor). Returns `403` otherwise.

**Response** `200 OK` — same shape as `auth-with-password`. The issued token carries an `anonymous` claim that survives `auth-refresh`. Rules can gate anonymous users with the `@request.auth.anonymous` macro (e.g. `@request.auth.anonymous = false`). To convert an anonymous user into a real account, set an identity (email) and password on the record later — the user keeps their id and data.

### `POST /collections/{collection}/auth/auth-refresh`
Refresh a record auth token with `{ "refreshToken": "..." }`.

### `POST /collections/{collection}/auth/auth-otp-request`
Request a one-time password.

### `POST /collections/{collection}/auth/auth-with-otp`
Verify OTP and authenticate.

### `POST /collections/{collection}/auth/request-password-reset`
Request password reset email.

### `POST /collections/{collection}/auth/confirm-password-reset`
Confirm password reset with token.

### `POST /collections/{collection}/auth/request-verification`
Request email verification.

### `POST /collections/{collection}/auth/confirm-verification`
Confirm email verification.

### `GET /collections/{collection}/auth/oauth2/{provider}`
Initiate OAuth2 flow. Redirects to the provider.

### Passkeys (WebAuthn)

Requires `allowPasskeys: true` in the collection's options (off by default —
endpoints return 403 otherwise). A passkey is a full possession+verification
factor; login mints the same token response as `auth-with-password`.

### `POST /collections/{collection}/auth/passkey/register-begin`
Start passkey registration for the **authenticated** record (record token
required). Returns WebAuthn `CredentialCreation` options; the challenge is
valid for 5 minutes.

### `POST /collections/{collection}/auth/passkey/register-finish`
Complete registration with the authenticator's response. Optional `"name"` in
the body labels the passkey. Returns the stored passkey descriptor.

### `POST /collections/{collection}/auth/passkey/login-begin`
Start passkey login (no auth, rate limited). With no body, returns options for
**discoverable** credentials; with `{"email": "..."}`, scopes
`allowCredentials` to that account — unknown emails get the same empty-list
response as accounts without passkeys (no enumeration).

### `POST /collections/{collection}/auth/passkey/login-finish`
Verify the assertion and authenticate. Response shape matches
`auth-with-password` (token + record).

### `GET /collections/{collection}/auth/passkeys`
List the authenticated record's passkeys (`id`, `name`, `created`,
`last_used_at` — never credential material).

### `DELETE /collections/{collection}/auth/passkeys/{id}`
Delete one of the authenticated record's own passkeys.

---

## Files

### `GET /files/{collection}/{recordId}/{filename}`
Download a file.

**Query Parameters**
| Param | Type | Description |
|-------|------|-------------|
| thumb | string | Thumbnail size: `100x100` (center crop), `100x100t` (top crop), `100x100f` (fit, no crop), `100x` / `x100` (single axis). Max 2048px per side |
| format | string | Convert the image output: `jpeg` or `png` (requires `thumb`) |
| quality | int | JPEG encode quality, `1`–`100` (requires `thumb`) |

**Example**
```
GET /api/v1/files/posts/rec_abc/cover.png?thumb=300x200&format=jpeg&quality=80
```

Each variant is generated once and cached; cached variants are served through the same rule-checked download path as the original. Invalid `format`/`quality` values return `400`; a `thumb` request on a non-image file falls back to the original. WebP sources are decoded, but WebP **output** is not supported.

### File Upload
Files are uploaded as part of record creation/update using `multipart/form-data`:

```
POST /api/v1/records/{collection}
Content-Type: multipart/form-data

--boundary
Content-Disposition: form-data; name="title"
Hello
--boundary
Content-Disposition: form-data; name="file"; filename="photo.jpg"
Content-Type: image/jpeg
<binary data>
--boundary--
```

### Resumable Uploads (TUS)

`/api/v1/files/tus/` implements the [TUS 1.0 protocol](https://tus.io)
(`creation`, `creation-with-upload`, `termination`, `expiration`) for
attaching large files to **existing** records. Required `Upload-Metadata`
keys (base64-encoded per the TUS spec): `collection`, `recordId`, `field`,
`filename`.

```
POST /api/v1/files/tus/            # create upload (auth + update rule checked)
PATCH /api/v1/files/tus/{id}       # send chunks (resumable)
HEAD /api/v1/files/tus/{id}        # retrieve current offset
DELETE /api/v1/files/tus/{id}      # terminate (same identity only)
```

The collection's update rule is enforced at creation and re-checked at
completion; field `mimeTypes`/`max_size`/`maxSelect` constraints are validated
against the actual bytes before the file is attached and the record update is
broadcast to realtime subscribers. Unfinished uploads expire after 24 hours.
Works with any TUS client, e.g. `tus-js-client` pointed at the endpoint with
an `Authorization` header.

---

## Realtime

### WebSocket `GET /realtime`
Upgrade to WebSocket for real-time events.

**Subscribe Message**
```json
{
  "type": "subscribe",
  "clientId": "client_abc123",
  "subscriptions": ["posts/*"],
  "options": { "presence": { "name": "Ada" } }
}
```

`options.presence` is optional, arbitrary client state — when set, the subscription opts into presence (see [Presence](#presence)).

**Event Messages (received)**
```json
{
  "event": "record:create",
  "channel": "posts",
  "data": {
    "id": "rec_new",
    "title": "New Post"
  },
  "timestamp": 1700000000000
}
```

### SSE `GET /sse`
Server-Sent Events for real-time updates.
Same message format as WebSocket.

### `POST /realtime/broadcast`
Publish a message to a custom realtime channel. Requires authentication (admin or record auth).

**Request Body**
```json
{
  "channel": "room:1",
  "event": "typing",
  "data": { "user": "Ada" }
}
```

**Response** `204 No Content`

`event` defaults to `message`. Reserved server event names (`record:*`, `connection:*`, `subscription:*`, `presence*`) and channels that shadow a collection's record topics are rejected with `400`, so client broadcasts can never spoof server events. On multi-node deployments broadcasts reach subscribers on every node via PostgreSQL LISTEN/NOTIFY.

### Presence
A subscription with `options.presence` set announces the client on the topic: other subscribers receive `presence:join` / `presence:leave` events with `{ "client_id": "...", "state": { ... } }`. Query the current members with a `presence` message:

```json
{ "type": "presence", "clientId": "client_abc123", "channel": "room:1" }
```

**Response message**
```json
{
  "client_id": "client_abc123",
  "event": "presence",
  "topic": "room:1",
  "data": {
    "topic": "room:1",
    "clients": 2,
    "members": [
      { "client_id": "client_abc123", "state": { "name": "Ada" } }
    ]
  },
  "timestamp": 1700000000000
}
```

`clients` counts all subscribers of the topic; `members` lists only those that declared presence state. On multi-node deployments presence member lists are node-local.

---

## API Keys

### `GET /api-keys`
List all API keys.

### `POST /api-keys`
Create a new API key.

**Request Body**
```json
{
  "name": "My App Key",
  "permissions": ["read", "write"]
}
```

**Response** `201 Created`
```json
{
  "key": "gb_abc123xyz...",
  "apiKey": {
    "id": "key_abc",
    "name": "My App Key",
    "prefix": "gb_abc...",
    "permissions": ["read", "write"],
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

### `DELETE /api-keys/{id}`
Revoke an API key.

---

## Settings

### `GET /settings`
Get all application settings. Includes the `email_templates` section: a map of
template id → `{subject, body}` overrides (empty/missing = built-in default).

### `PUT /settings`
Update application settings. Email template overrides are validated at save
time (parse + trial render); invalid templates are rejected with a 400 keyed
`email_templates.<id>`. Saving an entry with empty subject and body resets that
template to its default.

### `GET /settings/email-templates`
List all email templates with metadata for editors:
`[{id, name, description, placeholders[], defaultSubject, defaultBody, customSubject?, customBody?}]`.
Template ids: `verification`, `otp`, `magic_link`, `password_reset`,
`email_change`, `auth_alert`, `backup`, `backup_failed`.

---

## Logs

### `GET /logs`
View audit logs.

**Query Parameters**
| Param | Type | Description |
|-------|------|-------------|
| page | int | Page number |
| perPage | int | Items per page |
| filter | string | Filter expression |

---

## Backups

### `POST /backups/create`
Create a new backup.

### `GET /backups`
List available backups.

---

## Search

### `GET /search`
Full-text search across collections.

**Query Parameters**
| Param | Type | Description |
|-------|------|-------------|
| q | string | Search query |
| collection | string | Limit to collection |
| page | int | Page number |

---

## Error Responses

All errors follow this format:

```json
{
  "code": 400,
  "message": "Validation error",
  "errors": {
    "title": "Title is required",
    "email": "Invalid email format"
  }
}
```

**HTTP Status Codes**
| Code | Description |
|------|-------------|
| 200 | Success |
| 201 | Created |
| 400 | Bad request / validation error |
| 401 | Unauthorized (invalid/expired token) |
| 403 | Forbidden (insufficient permissions) |
| 404 | Not found |
| 409 | Conflict (duplicate entry) |
| 429 | Too many requests (rate limited) |
| 500 | Internal server error |

---

## Rate Limiting

- **Login**: 10 requests per minute
- **Registration**: 5 requests per minute
- **OTP requests**: 3 requests per minute
- **General API**: 100 requests per minute (configurable)

Rate limit headers are included in responses:
```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1700000000
```

---

## Filter Syntax

Filters use a SQL-like syntax with field names, operators, and values:

```
field operator value
```

**Operators**
| Operator | Description | Example |
|----------|-------------|---------|
| `=` | Equal | `status = 'active'` |
| `!=` | Not equal | `role != 'banned'` |
| `>` | Greater than | `views > 100` |
| `>=` | Greater or equal | `age >= 18` |
| `<` | Less than | `price < 50` |
| `<=` | Less or equal | `stock <= 10` |
| `~` | Contains (text) | `title ~ 'hello'` |
| `!~` | Not contains | `email !~ 'spam'` |
| `&&` | AND | `status = 'active' && role = 'admin'` |
| `||` | OR | `role = 'admin' || role = 'editor'` |

**Special Variables**
| Variable | Description |
|----------|-------------|
| `@request.auth.id` | Authenticated user ID |
| `@request.auth.role` | Authenticated user role |
| `@request.auth.collection` | Auth collection name |
| `@request.auth.anonymous` | `true` for anonymous sign-ins |
| `@now` | Current timestamp |

---

## Sort Syntax

Sort by one or more fields, comma-separated. Prefix with `-` for descending:

```
GET /api/v1/records/posts?sort=-created_at,title
```

---

## Field Types

| Type | PostgreSQL Type | Description |
|------|----------------|-------------|
| `text` | TEXT | Single-line text |
| `number` | DOUBLE PRECISION | Floating-point number |
| `bool` | BOOLEAN | True/false |
| `email` | TEXT | Email address (validated) |
| `url` | TEXT | URL (validated) |
| `date` | TIMESTAMPTZ | Date/time |
| `select` | TEXT | Single/multi select |
| `json` | JSONB | Arbitrary JSON data |
| `file` | TEXT | File reference |
| `relation` | TEXT | Reference to another collection's record |
| `password` | TEXT | Hashed password (auth collections only) |
| `editor` | TEXT | Rich text / HTML content |
| `geo_point` | TEXT | Geographic coordinates |
| `autodate` | TIMESTAMPTZ | Auto-managed date |
