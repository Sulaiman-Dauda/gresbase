/// Gresbase Dart SDK.
///
/// Client SDK for the Gresbase backend platform:
/// - Full CRUD with filtering, sorting, pagination, expansion
/// - Admin auth (email/password, OTP, magic link, OAuth) and
///   record auth (password, anonymous) with auto-refresh tokens
/// - Realtime subscriptions over SSE (collections, records, channels,
///   presence, broadcast) with auto-reconnect
/// - File upload/download with thumbnail/format/quality options
/// - Aggregations and transactional batch operations
///
/// ```dart
/// final client = GresbaseClient(url: 'http://localhost:8080');
/// await client.auth.login('admin@example.com', 'password');
/// final records = await client.collection('posts').getList(page: 1, perPage: 20);
/// ```
library;

export 'src/auth_service.dart' show AuthService, AuthPersistCallback;
export 'src/client.dart' show GresbaseClient;
export 'src/collection_service.dart' show CollectionService;
export 'src/file_service.dart' show FileService, FileUploadResult;
export 'src/http_client.dart' show GresbaseHttpClient;
export 'src/models.dart';
export 'src/realtime_service.dart' show RealtimeService, UnsubscribeFunc;
