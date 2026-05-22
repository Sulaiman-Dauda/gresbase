/// Main Gresbase Dart SDK client.
///
/// ```dart
/// final client = GresbaseClient(url: 'http://localhost:8080');
/// await client.auth.login('admin@example.com', 'password');
///
/// // CRUD operations
/// final posts = client.collection('posts');
/// final list = await posts.getList(page: 1, perPage: 20);
///
/// // Realtime subscriptions
/// client.realtime.subscribe('posts', (event) {
///   print('${event.action}: ${event.record}');
/// });
/// client.realtime.connect();
/// ```
library gresbase_sdk_client;

import 'models.dart';
import 'http_client.dart';
import 'auth_service.dart';
import 'collection_service.dart';
import 'realtime_service.dart';
import 'file_service.dart';

/// The main Gresbase client SDK entry point.
class GresbaseClient {
  late final HttpClient _http;
  late final AuthService _auth;
  late final RealtimeService _realtime;
  late final FileService _files;

  GresbaseClient({
    required String url,
    String? token,
    int sseReconnectDelay = 3000,
  }) {
    final baseUrl = url.endsWith('/') ? url.substring(0, url.length - 1) : url;

    _http = HttpClient(
      baseUrl: baseUrl,
      getToken: () => _auth.token,
      onRefresh: () => _auth.refresh(),
    );

    _auth = AuthService(_http);
    _realtime = RealtimeService(
      baseUrl,
      () => _auth.token,
      reconnectDelay: sseReconnectDelay,
    );
    _files = FileService(_http);
  }

  /// Authentication service.
  AuthService get auth => _auth;

  /// Realtime service (SSE-based).
  RealtimeService get realtime => _realtime;

  /// File upload/download service.
  FileService get files => _files;

  /// Get a collection service for typed record CRUD.
  ///
  /// ```dart
  /// final posts = client.collection('posts');
  /// final list = await posts.getList();
  /// ```
  CollectionService<RecordData> collection(String name) {
    return CollectionService<RecordData>(name, _http);
  }

  /// Get a typed collection service with custom fromJson.
  CollectionService<T> typedCollection<T extends RecordData>(
    String name,
    T Function(Map<String, dynamic>) fromJson,
  ) {
    return CollectionService<T>(name, _http, fromJson: fromJson);
  }

  // ---- Admin Management ----

  /// List all admin users.
  Future<List<AdminUser>> getAdmins() async {
    final result = await _http.requestList('GET', '/api/v1/admin/users');
    return result
        .map((e) => AdminUser.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// Create a new admin user.
  Future<AdminUser> createAdmin(
      String email, String password, String role) async {
    final result = await _http.request('POST', '/api/v1/admin/users',
        body: {'email': email, 'password': password, 'role': role});
    return AdminUser.fromJson(result);
  }

  /// Update an admin user.
  Future<AdminUser> updateAdmin(String id, Map<String, dynamic> data) async {
    final result =
        await _http.request('PUT', '/api/v1/admin/users/$id', body: data);
    return AdminUser.fromJson(result);
  }

  /// Delete an admin user.
  Future<void> deleteAdmin(String id) async {
    await _http.request('DELETE', '/api/v1/admin/users/$id');
  }

  // ---- Collections Management ----

  /// List all collections.
  Future<List<CollectionModel>> getCollections() async {
    final result = await _http.requestList('GET', '/api/v1/collections');
    return result
        .map((e) => CollectionModel.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// Get a single collection.
  Future<CollectionModel> getCollection(String idOrName) async {
    final result =
        await _http.request('GET', '/api/v1/collections/$idOrName');
    return CollectionModel.fromJson(result);
  }

  /// Create a collection.
  Future<CollectionModel> createCollection(Map<String, dynamic> data) async {
    final result =
        await _http.request('POST', '/api/v1/collections', body: data);
    return CollectionModel.fromJson(result);
  }

  /// Update a collection.
  Future<CollectionModel> updateCollection(
      String id, Map<String, dynamic> data) async {
    final result =
        await _http.request('PUT', '/api/v1/collections/$id', body: data);
    return CollectionModel.fromJson(result);
  }

  /// Delete a collection.
  Future<void> deleteCollection(String id) async {
    await _http.request('DELETE', '/api/v1/collections/$id');
  }

  // ---- API Keys ----

  /// List API keys.
  Future<List<ApiKeyData>> getApiKeys() async {
    final result = await _http.requestList('GET', '/api/v1/api-keys');
    return result
        .map((e) => ApiKeyData.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// Create an API key (returns the full key only once).
  Future<ApiKeyData> createApiKey(String name) async {
    final result =
        await _http.request('POST', '/api/v1/api-keys', body: {'name': name});
    return ApiKeyData.fromJson(result);
  }

  /// Delete an API key.
  Future<void> deleteApiKey(String id) async {
    await _http.request('DELETE', '/api/v1/api-keys/$id');
  }

  // ---- Certificates ----

  /// List certificates.
  Future<List<Certificate>> getCertificates() async {
    final result = await _http.requestList('GET', '/api/v1/certificates');
    return result
        .map((e) => Certificate.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// Issue a TLS certificate.
  Future<Certificate> issueCertificate(String domain) async {
    final result = await _http.request('POST', '/api/v1/certificates/issue',
        body: {'domain': domain});
    return Certificate.fromJson(result);
  }

  // ---- Backups ----

  /// List backups.
  Future<List<Map<String, dynamic>>> getBackups() async {
    final result = await _http.requestList('GET', '/api/v1/backups');
    return result.cast<Map<String, dynamic>>();
  }

  /// Create a backup.
  Future<void> createBackup({String? name}) async {
    await _http.request('POST', '/api/v1/backups', body: {'name': name});
  }

  /// Restore a backup.
  Future<void> restoreBackup(String name) async {
    await _http.request('POST', '/api/v1/backups/$name/restore');
  }

  /// Delete a backup.
  Future<void> deleteBackup(String name) async {
    await _http.request('DELETE', '/api/v1/backups/$name');
  }

  // ---- Settings ----

  /// Get server settings.
  Future<Map<String, dynamic>> getSettings() async {
    return _http.request('GET', '/api/v1/settings');
  }

  /// Update server settings.
  Future<Map<String, dynamic>> updateSettings(Map<String, dynamic> data) async {
    return _http.request('PUT', '/api/v1/settings', body: data);
  }

  // ---- Health ----

  /// Health check.
  Future<Map<String, dynamic>> health() async {
    return _http.request('GET', '/api/v1/health');
  }
}
