/// Main Gresbase client.
library;

import 'package:http/http.dart' as http;

import 'auth_service.dart';
import 'collection_service.dart';
import 'file_service.dart';
import 'http_client.dart';
import 'models.dart';
import 'realtime_service.dart';

/// Gresbase API client.
///
/// ```dart
/// final client = GresbaseClient(url: 'http://localhost:8080');
/// await client.auth.login('admin@example.com', 'password');
/// final posts = await client.collection('posts').getList(page: 1, perPage: 20);
/// ```
class GresbaseClient {
  /// Admin auth + shared token store.
  late final AuthService auth;

  /// Realtime subscriptions over SSE.
  late final RealtimeService realtime;

  /// File operations.
  late final FileService files;

  late final GresbaseHttpClient _http;

  /// Create a client.
  ///
  /// - [url] — base URL of the Gresbase server (e.g. `http://localhost:8080`).
  /// - [token] — optional initial auth token.
  /// - [httpClient] — optional `package:http` client (inject for testing).
  /// - [onAuthChange] — optional persistence callback invoked with a
  ///   JSON-serializable auth snapshot whenever the auth state changes;
  ///   restore it on startup with `client.auth.restore(snapshot)`.
  /// - [sseReconnectDelay] — base delay for SSE reconnection backoff.
  GresbaseClient({
    required String url,
    String? token,
    http.Client? httpClient,
    AuthPersistCallback? onAuthChange,
    Duration sseReconnectDelay = const Duration(seconds: 3),
  }) {
    _http = GresbaseHttpClient(
      baseUrl: url,
      client: httpClient,
      getToken: () => auth.token,
      getRefreshToken: () => auth.refreshToken,
      onRefresh: () => auth.refresh(),
    );

    auth = AuthService(_http, onChange: onAuthChange);
    if (token != null) {
      auth.setToken(token);
    }

    realtime = RealtimeService(_http, reconnectDelay: sseReconnectDelay);
    files = FileService(_http);
  }

  /// The normalized base URL.
  String get baseUrl => _http.baseUrl;

  /// Get a collection service for record operations.
  CollectionService collection(String name) =>
      CollectionService(name, _http, auth);

  // ---- Admin Management ----

  /// List all admin users.
  Future<List<AdminUser>> getAdmins() async {
    final result = await _http.send('GET', '/api/v1/admin/users');
    return (result as List<dynamic>)
        .map((e) => AdminUser.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// Create a new admin user.
  Future<AdminUser> createAdmin(String email, String password,
      {String role = 'admin'}) async {
    final result = await _http.send('POST', '/api/v1/admin/users',
        body: {'email': email, 'password': password, 'role': role});
    return AdminUser.fromJson(result as Map<String, dynamic>);
  }

  /// Update an admin user.
  Future<AdminUser> updateAdmin(String id, Map<String, dynamic> data) async {
    final result =
        await _http.send('PUT', '/api/v1/admin/users/$id', body: data);
    return AdminUser.fromJson(result as Map<String, dynamic>);
  }

  /// Delete an admin user.
  Future<void> deleteAdmin(String id) async {
    await _http.send('DELETE', '/api/v1/admin/users/$id');
  }

  // ---- Collections Management ----

  /// List all collections.
  Future<List<CollectionModel>> getCollections() async {
    final result = await _http.send('GET', '/api/v1/collections');
    return (result as List<dynamic>)
        .map((e) => CollectionModel.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// Get a single collection by ID or name.
  Future<CollectionModel> getCollection(String idOrName) async {
    final result = await _http.send('GET', '/api/v1/collections/$idOrName');
    return CollectionModel.fromJson(result as Map<String, dynamic>);
  }

  /// Create a collection.
  Future<CollectionModel> createCollection(Map<String, dynamic> data) async {
    final result = await _http.send('POST', '/api/v1/collections', body: data);
    return CollectionModel.fromJson(result as Map<String, dynamic>);
  }

  /// Update a collection.
  Future<CollectionModel> updateCollection(
      String id, Map<String, dynamic> data) async {
    final result =
        await _http.send('PUT', '/api/v1/collections/$id', body: data);
    return CollectionModel.fromJson(result as Map<String, dynamic>);
  }

  /// Delete a collection.
  Future<void> deleteCollection(String id) async {
    await _http.send('DELETE', '/api/v1/collections/$id');
  }

  /// Import collection schemas.
  Future<void> importCollections(List<Map<String, dynamic>> data) async {
    await _http.send('POST', '/api/v1/collections/import', body: data);
  }

  // ---- API Keys ----

  /// List API keys.
  Future<List<ApiKeyData>> getApiKeys() async {
    final result = await _http.send('GET', '/api/v1/api-keys');
    return (result as List<dynamic>)
        .map((e) => ApiKeyData.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// Create an API key (the full key is returned only once).
  Future<ApiKeyData> createApiKey(String name,
      {List<String> permissions = const []}) async {
    final result = await _http.send('POST', '/api/v1/api-keys',
        body: {'name': name, 'permissions': permissions});
    return ApiKeyData.fromJson(result as Map<String, dynamic>);
  }

  /// Delete an API key.
  Future<void> deleteApiKey(String id) async {
    await _http.send('DELETE', '/api/v1/api-keys/$id');
  }

  // ---- Certificates ----

  /// List TLS certificates.
  Future<List<Certificate>> getCertificates() async {
    final result = await _http.send('GET', '/api/v1/certificates');
    return (result as List<dynamic>)
        .map((e) => Certificate.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  /// Issue a TLS certificate.
  Future<Certificate> issueCertificate(String domain) async {
    final result = await _http
        .send('POST', '/api/v1/certificates/issue', body: {'domain': domain});
    return Certificate.fromJson(result as Map<String, dynamic>);
  }

  /// Revoke a certificate.
  Future<void> revokeCertificate(String id) async {
    await _http.send('DELETE', '/api/v1/certificates/$id');
  }

  // ---- Logs ----

  /// Get audit logs.
  Future<PaginatedResponse<RecordData>> getLogs(
      {int? page, int? perPage, String? filter}) async {
    final query = <String, String>{
      if (page != null) 'page': '$page',
      if (perPage != null) 'perPage': '$perPage',
      if (filter != null) 'filter': filter,
    };
    final result = await _http.send('GET', '/api/v1/logs',
        query: query.isEmpty ? null : query);
    return PaginatedResponse<RecordData>.fromJson(
        result as Map<String, dynamic>);
  }

  // ---- Settings ----

  /// Get server settings.
  Future<Map<String, dynamic>> getSettings() async {
    final result = await _http.send('GET', '/api/v1/settings');
    return result as Map<String, dynamic>;
  }

  /// Update server settings.
  Future<Map<String, dynamic>> updateSettings(Map<String, dynamic> data) async {
    final result = await _http.send('PUT', '/api/v1/settings', body: data);
    return result as Map<String, dynamic>;
  }

  // ---- Backups ----

  /// List backups.
  Future<List<Map<String, dynamic>>> getBackups() async {
    final result = await _http.send('GET', '/api/v1/backups');
    return (result as List<dynamic>)
        .map((e) => e as Map<String, dynamic>)
        .toList();
  }

  /// Create a backup.
  Future<void> createBackup({String? name}) async {
    await _http.send('POST', '/api/v1/backups', body: {'name': name});
  }

  /// Restore a backup.
  Future<void> restoreBackup(String name) async {
    await _http.send('POST', '/api/v1/backups/$name/restore');
  }

  /// Delete a backup.
  Future<void> deleteBackup(String name) async {
    await _http.send('DELETE', '/api/v1/backups/$name');
  }

  // ---- Health ----

  /// Health check.
  Future<Map<String, dynamic>> health() async {
    final result = await _http.send('GET', '/api/v1/health');
    return result as Map<String, dynamic>;
  }

  // ---- Generic Batch ----

  /// Execute a generic batch of API requests
  /// (`POST /api/v1/batch`, body `{requests: [{method, url, body}]}`).
  ///
  /// For transactional per-collection record batches, prefer
  /// `client.collection(name).batch(...)`.
  Future<List<dynamic>> batch(List<Map<String, dynamic>> requests) async {
    final result =
        await _http.send('POST', '/api/v1/batch', body: {'requests': requests});
    if (result is List<dynamic>) return result;
    if (result is Map<String, dynamic> && result['responses'] is List) {
      return result['responses'] as List<dynamic>;
    }
    return const [];
  }

  /// Close the realtime connection and the underlying HTTP client.
  void close() {
    realtime.dispose();
    auth.dispose();
    _http.close();
  }
}
