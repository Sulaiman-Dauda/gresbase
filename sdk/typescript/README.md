# gresbase-sdk

Isomorphic TypeScript SDK for [Gresbase](https://github.com/gresbase/gresbase) — a PostgreSQL-backed backend platform with authentication, record CRUD, realtime subscriptions, and file storage.

Works in the browser and in Node.js (>= 18). Zero runtime dependencies, ships both ESM and CommonJS builds with full type declarations.

## Install

```sh
npm install gresbase-sdk
```

## Quickstart

```ts
import { GresbaseClient } from 'gresbase-sdk'

const client = new GresbaseClient({ url: 'http://localhost:8080' })

// --- Auth (end users) ---
await client.collection('users').authWithPassword('ada@example.com', 'hunter2')
// Tokens are stored on the client; all subsequent requests are authenticated.

// --- CRUD ---
const posts = client.collection('posts')

const page = await posts.getList(1, 20, {
  filter: 'published = true && views > 100',
  sort: '-created',
  expand: 'author',
})

const post = await posts.create({ title: 'Hello', content: '...' })
await posts.update(post.id, { title: 'Hello again' })
await posts.delete(post.id)

// --- Realtime ---
client.realtime.connect()
const unsubscribe = client.realtime.subscribe('posts', ({ action, record }) => {
  console.log(action, record) // 'create' | 'update' | 'delete'
}, { filter: 'published = true' })

// --- Files ---
const { filename } = await client.files.upload(file, {
  onProgress: (loaded, total) => console.log(`${loaded}/${total}`),
})
const url = client.files.getURL('posts', post.id, filename, {
  thumb: '300x200',
  format: 'jpeg', // 'jpeg' | 'png'
  quality: 80,    // 1-100
})
```

Admin authentication uses the dedicated auth service:

```ts
await client.auth.login('admin@example.com', 'password')   // admin login
await client.auth.logout()
```

## Typed collections

Every collection accessor accepts a generic for end-to-end type safety:

```ts
interface Post {
  id: string
  title: string
  content: string
  published: boolean
}

const posts = client.collection<Post>('posts')
const list = await posts.getList()  // PaginatedResponse<Post>
const one = await posts.getOne(id)  // Post
```

You don't have to write these interfaces by hand — Gresbase generates them from your live schema:

```sh
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/types.ts > src/gresbase-types.ts
```

## Record auth

Record (end-user) auth lives on the collection service and shares the client's single token store with admin auth — whichever you use last wins, and realtime connections pick up the same token.

```ts
const users = client.collection('users')

// Email/username + password
const { token, record } = await users.authWithPassword('ada@example.com', 'hunter2')

// Anonymous sessions (when the collection allows it)
await users.authWithAnonymous()

// Refresh the session with the stored refresh token
await users.authRefresh()

// Password reset + email verification flows
await users.requestPasswordReset('ada@example.com')
await users.confirmPasswordReset(tokenFromEmail, 'new-password')
await users.requestVerification('ada@example.com')
await users.confirmVerification(tokenFromEmail)

client.auth.getRecord() // the authenticated record
client.auth.on('login', (res) => console.log('authenticated', res))
```

## Aggregations

Run `count` / `sum` / `avg` / `min` / `max` over a collection, optionally grouped and filtered:

```ts
const { items } = await client.collection('orders').aggregate({
  aggregate: 'count,sum:total,avg:total', // count | sum:field | avg:field | min:field | max:field
  groupBy: 'status',                      // optional, comma-separated
  filter: 'created >= "2026-01-01"',      // optional, same syntax as getList
  sort: '-count',                         // optional
  limit: 100,                             // optional (default 100, max 1000)
})
// items: [{ status: 'paid', count: 12, sum_total: 423.5, avg_total: 35.29 }, ...]
```

Result keys are the groupBy fields plus `count`, `sum_<field>`, `avg_<field>`, `min_<field>`, `max_<field>`.

## Channels: broadcast & presence

Beyond record subscriptions, you can publish and subscribe on arbitrary channels — useful for chat, cursors, "who's online", etc.

```ts
client.realtime.connect()

// Subscribe to a channel and announce presence state
const unsub = client.realtime.subscribeToChannel('room:42', (msg) => {
  // msg: { event, data, client_id? }
  if (msg.event === 'presence:join') console.log('joined:', msg.data)   // { client_id, state }
  if (msg.event === 'presence:leave') console.log('left:', msg.data)
  if (msg.event === 'cursor') console.log('cursor move:', msg.data)
}, { presence: { name: 'Ada', color: '#f0f' } })

// Publish to the channel (REST, requires an auth token)
await client.realtime.broadcast('room:42', 'cursor', { x: 120, y: 80 })

// Snapshot of who's connected
const { clients, members } = await client.realtime.presence('room:42')
// members: [{ client_id, state }]

unsub()
```

## More

- **Batch**: `client.batch([...])`, `client.collection('x').batchCreate([...])`
- **Search**: `client.collection('x').search('full text query')`
- **Admin management**: collections, API keys, settings, backups, logs, TLS certificates — see the typed methods on `GresbaseClient`
- **Auto token refresh**: 401 responses transparently retry after refreshing the access token

## License

MIT
