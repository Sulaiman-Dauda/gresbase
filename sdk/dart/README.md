# Gresbase Dart SDK

Official Dart client for [Gresbase](https://github.com/gresbase/gresbase) — auth, CRUD, realtime, files, aggregations, and batch operations. Mirrors the TypeScript SDK (v0.4.0) API surface in idiomatic Dart.

Works in Dart server/CLI apps and Flutter. The only runtime dependency is `package:http`.

## Install

```yaml
# pubspec.yaml
dependencies:
  gresbase_sdk:
    path: ../path/to/gresbase/sdk/dart   # or hosted, once published
```

```bash
dart pub get
```

## Quickstart

```dart
import 'package:gresbase_sdk/gresbase_sdk.dart';

void main() async {
  final client = GresbaseClient(url: 'http://localhost:8080');

  await client.auth.login('admin@example.com', 'password');

  final posts = await client.collection('posts').getList(page: 1, perPage: 20);
  for (final post in posts.items) {
    print(post['title']);
  }

  client.close();
}
```

## Auth

### Admin auth

```dart
// Email/password login — tokens are stored on the client and attached
// to every subsequent request. Expired access tokens are refreshed
// automatically (one refresh + retry on 401).
await client.auth.login('admin@example.com', 'password');

final me = await client.auth.getMe();
await client.auth.refresh();   // manual refresh (usually not needed)
await client.auth.logout();
```

### Record auth (auth collections)

```dart
final users = client.collection('users');

// Identity (email/username) + password
final auth = await users.authWithPassword('user@example.com', 'secret');
print(auth.record['email']);
print(auth.token); // also stored on the client automatically

// Anonymous auth (when enabled on the collection)
await users.authWithAnonymous();

// Session refresh using the stored refresh token
await users.authRefresh();

// Password reset & email verification flows
await users.requestPasswordReset('user@example.com');
await users.confirmPasswordReset(emailedToken, 'newPassword');
await users.requestVerification('user@example.com');
await users.confirmVerification(emailedToken);
```

### Persisting sessions

The token store is in-memory. Pass `onAuthChange` to mirror auth state to
durable storage (e.g. `shared_preferences`), and restore it on startup:

```dart
final client = GresbaseClient(
  url: 'http://localhost:8080',
  onAuthChange: (snapshot) => prefs.setString('gresbase_auth', jsonEncode(snapshot)),
);

// On startup:
final saved = prefs.getString('gresbase_auth');
if (saved != null) {
  client.auth.restore(jsonDecode(saved) as Map<String, dynamic>);
}
```

## Records (CRUD)

```dart
final posts = client.collection('posts');

// Paginated list with filtering, sorting, relation expansion
final page = await posts.getList(
  page: 1,
  perPage: 30,
  filter: 'status = "active" && likes > 10',
  sort: '-created',
  expand: 'author',
  fields: 'id,title,author',
);
print('${page.totalItems} records over ${page.totalPages} pages');

// All records (auto-paginates)
final all = await posts.getFullList(filter: 'published = true');

// Single record
final post = await posts.getOne('RECORD_ID', expand: 'author');

// First match
final first = await posts.getFirstListItem('slug = "hello-world"');

// Create / update / delete
final created = await posts.create({'title': 'Hello', 'status': 'active'});
await posts.update(created['id'] as String, {'title': 'Hello again'});
await posts.delete(created['id'] as String);
```

### Batch operations

Transactional create/update/delete in one request:

```dart
final result = await posts.batch(
  creates: [
    {'title': 'A'},
    {'title': 'B'},
  ],
  updates: {
    'RECORD_ID': {'status': 'archived'},
  },
  deletes: ['OTHER_RECORD_ID'],
);
print('created ${result.created.length}, '
    'updated ${result.updated}, deleted ${result.deleted}');

// Or the convenience wrappers:
await posts.batchCreate([{'title': 'C'}]);
await posts.batchUpdate({'id1': {'title': 'D'}});
await posts.batchDelete(['id2', 'id3']);
```

## Aggregations

```dart
final res = await client.collection('orders').aggregate(
  aggregate: 'count,sum:total,avg:total',  // count | sum:f | avg:f | min:f | max:f
  groupBy: 'status',
  filter: 'created >= "2026-01-01"',
  sort: '-count',
  limit: 100,
);
// res.items: [{status: 'paid', count: 12, sum_total: 423.5, avg_total: 35.29}, ...]
```

## Realtime

Realtime uses Server-Sent Events (`GET /api/v1/sse`) with automatic
reconnection (exponential backoff) and resubscription. Subscribing connects
lazily — no explicit `connect()` needed.

```dart
// Collection events (create/update/delete)
final unsub = client.realtime.subscribe('posts', (e) {
  print('${e.action}: ${e.record['title']}');
}, filter: 'published = true');

// A single record
final unsubRec = client.realtime.subscribeToRecord('posts', 'RECORD_ID', (e) {
  print('record ${e.action}');
});

// Channels (broadcast + presence)
final unsubRoom = client.realtime.subscribeToChannel('room:1', (msg) {
  print('${msg.event}: ${msg.data}');   // includes presence:join / presence:leave
}, presence: {'name': 'Ada'});

// Publish to a channel (requires auth)
await client.realtime.broadcast('room:1', 'chat', {'text': 'hello'});

// Presence snapshot
final info = await client.realtime.presence('room:1');
print('${info.clients} clients online');
for (final member in info.members) {
  print('${member.clientId}: ${member.state}');
}

// Cleanup
await unsub();
await unsubRec();
await unsubRoom();
client.realtime.disconnect();
```

## Files

```dart
// Upload (multipart/form-data)
final bytes = await File('cover.png').readAsBytes();
final uploaded = await client.files.upload(bytes, 'cover.png');
print(uploaded.url);

// Download / display URL with optional thumbnail transforms
final url = client.files.getUrl(
  'posts', 'RECORD_ID', 'cover.png',
  thumb: '300x200',
  format: 'jpeg',   // 'jpeg' | 'png'
  quality: 80,      // 1-100
  token: 'FILE_TOKEN', // for protected files
);

// Delete
await client.files.delete('posts', 'RECORD_ID', 'cover.png');
```

## Admin & server management

The full management surface from the TypeScript SDK is available too:
`getAdmins` / `createAdmin` / `updateAdmin` / `deleteAdmin`,
`getCollections` / `getCollection` / `createCollection` / `updateCollection` /
`deleteCollection` / `importCollections`, `getApiKeys` / `createApiKey` /
`deleteApiKey`, `getCertificates` / `issueCertificate` / `revokeCertificate`,
`getLogs`, `getSettings` / `updateSettings`, `getBackups` / `createBackup` /
`restoreBackup` / `deleteBackup`, and `health()`.

## Error handling

All non-2xx responses throw `GresbaseException` with the HTTP status code and
the server's error message:

```dart
try {
  await client.collection('posts').getOne('missing');
} on GresbaseException catch (e) {
  print('${e.statusCode}: ${e.message}');
}
```

## Testing

The HTTP layer is injectable — pass any `package:http` `Client`
(e.g. `MockClient` from `package:http/testing.dart`):

```dart
final client = GresbaseClient(url: 'http://test', httpClient: myMockClient);
```

Run the SDK's own tests:

```bash
dart pub get
dart analyze
dart test
```
