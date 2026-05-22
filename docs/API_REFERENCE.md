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
  "version": "0.3.0",
  "uptime": "2h34m15s",
  "uptime_ms": 9255000,
  "components": {
    "api": { "status": "healthy" },
    "database": { "status": "healthy" },
    "realtime": { "status": "healthy" }
  },
  "system": {
    "go_version": "go1.23.0",
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
| expand | string | — | Comma-separated relation fields to expand |
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
  "refresh_token": "eyJhbGci...",
  "record": {
    "id": "rec_user1",
    "email": "user@example.com",
    "name": "John Doe"
  }
}
```

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

---

## Files

### `GET /files/{collection}/{recordId}/{filename}`
Download a file.

**Query Parameters**
| Param | Type | Description |
|-------|------|-------------|
| thumb | string | Thumbnail size (e.g., `100x100`) |

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

---

## Realtime

### WebSocket `GET /realtime`
Upgrade to WebSocket for real-time events.

**Subscribe Message**
```json
{
  "type": "subscribe",
  "clientId": "client_abc123",
  "channel": "posts",
  "subscriptions": ["posts"]
}
```

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

---

## ACME CA / Certificates

### `GET /acme/directory`
ACME directory endpoint. Returns URLs for ACME operations.

### `POST /acme/new-account`
Create an ACME account.

### `POST /acme/new-order`
Place a certificate order.

### `GET /certificates`
List managed certificates.

### `POST /certificates/issue`
Issue a new certificate.

**Request Body**
```json
{
  "domain": "example.com",
  "challenge_type": "http-01"
}
```

### `DELETE /certificates/{id}`
Revoke a certificate.

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
Get all application settings.

### `PUT /settings`
Update application settings.

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
