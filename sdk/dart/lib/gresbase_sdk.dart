/// Gresbase Dart SDK
///
/// Production-grade client SDK for the Gresbase backend platform.
/// Matches PocketBase Dart SDK capabilities:
/// - Full CRUD with filtering, sorting, pagination
/// - Auth (email/password, OAuth, OTP, magic link, API keys)
/// - Realtime subscriptions via SSE
/// - File upload/download
/// - Batch operations
/// - Auto-refresh token management
///
/// ```dart
/// final client = GresbaseClient(url: 'http://localhost:8080');
/// await client.auth.login('admin@example.com', 'password');
/// final records = await client.collection('posts').getList();
/// ```
library gresbase_sdk;

export 'src/client.dart';
export 'src/auth_service.dart';
export 'src/collection_service.dart';
export 'src/realtime_service.dart';
export 'src/file_service.dart';
export 'src/models.dart';
export 'src/http_client.dart';
