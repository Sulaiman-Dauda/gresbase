/// Internal HTTP layer with auth token injection and auto-refresh on 401.
library;

import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;

import 'models.dart';

/// Internal HTTP client used by all SDK services.
///
/// Wraps a `package:http` [http.Client] (injectable for testing), attaches
/// the bearer token from the shared token store, and transparently refreshes
/// the access token once on a 401 response before retrying the request.
class GresbaseHttpClient {
  /// Base URL of the Gresbase server, without trailing slash.
  final String baseUrl;

  final http.Client _client;

  /// Resolves the current access token (read lazily per request).
  String? Function() getToken;

  /// Resolves the current refresh token.
  String? Function() getRefreshToken;

  /// Performs a token refresh; returns the new access token or null.
  Future<String?> Function()? onRefresh;

  GresbaseHttpClient({
    required String baseUrl,
    http.Client? client,
    String? Function()? getToken,
    String? Function()? getRefreshToken,
    this.onRefresh,
  })  : baseUrl = baseUrl.endsWith('/')
            ? baseUrl.substring(0, baseUrl.length - 1)
            : baseUrl,
        _client = client ?? http.Client(),
        getToken = getToken ?? (() => null),
        getRefreshToken = getRefreshToken ?? (() => null);

  /// The underlying `package:http` client (also used for SSE streaming).
  http.Client get rawClient => _client;

  Uri buildUri(String path, [Map<String, String>? query]) {
    final uri = Uri.parse('$baseUrl$path');
    if (query == null || query.isEmpty) return uri;
    return uri.replace(queryParameters: {...uri.queryParameters, ...query});
  }

  /// Send a JSON request and decode the JSON response.
  ///
  /// Returns the decoded body (`Map`, `List`, ...) or `null` for empty/204
  /// responses. Throws [GresbaseException] on non-2xx status codes.
  ///
  /// Set [retryOn401] to false to bypass the auto-refresh interceptor
  /// (used by the token refresh request itself to avoid recursion).
  Future<dynamic> send(
    String method,
    String path, {
    Object? body,
    Map<String, String>? query,
    Map<String, String>? headers,
    bool retryOn401 = true,
  }) async {
    var response = await _attempt(method, path,
        body: body, query: query, headers: headers);

    // Auto-refresh once on 401, then retry the original request.
    if (retryOn401 &&
        response.statusCode == 401 &&
        getRefreshToken() != null &&
        onRefresh != null) {
      final newToken = await onRefresh!();
      if (newToken != null) {
        response = await _attempt(method, path,
            body: body, query: query, headers: headers);
      }
    }

    return _decode(response);
  }

  /// Upload a file as `multipart/form-data`.
  Future<dynamic> multipart(
    String path, {
    required List<int> fileBytes,
    required String filename,
    String fileField = 'file',
    Map<String, String>? fields,
  }) async {
    var response = await _attemptMultipart(path,
        fileBytes: fileBytes,
        filename: filename,
        fileField: fileField,
        fields: fields);

    if (response.statusCode == 401 &&
        getRefreshToken() != null &&
        onRefresh != null) {
      final newToken = await onRefresh!();
      if (newToken != null) {
        response = await _attemptMultipart(path,
            fileBytes: fileBytes,
            filename: filename,
            fileField: fileField,
            fields: fields);
      }
    }

    return _decode(response);
  }

  Future<http.Response> _attempt(
    String method,
    String path, {
    Object? body,
    Map<String, String>? query,
    Map<String, String>? headers,
  }) async {
    final request = http.Request(method, buildUri(path, query));
    request.headers['Accept'] = 'application/json';
    if (headers != null) request.headers.addAll(headers);

    final token = getToken();
    if (token != null && token.isNotEmpty) {
      request.headers['Authorization'] = 'Bearer $token';
    }

    if (body != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(body);
    }

    final streamed = await _client.send(request);
    return http.Response.fromStream(streamed);
  }

  Future<http.Response> _attemptMultipart(
    String path, {
    required List<int> fileBytes,
    required String filename,
    required String fileField,
    Map<String, String>? fields,
  }) async {
    final request = http.MultipartRequest('POST', buildUri(path));
    request.files.add(
        http.MultipartFile.fromBytes(fileField, fileBytes, filename: filename));
    if (fields != null) request.fields.addAll(fields);

    final token = getToken();
    if (token != null && token.isNotEmpty) {
      request.headers['Authorization'] = 'Bearer $token';
    }

    final streamed = await _client.send(request);
    return http.Response.fromStream(streamed);
  }

  dynamic _decode(http.Response response) {
    if (response.statusCode >= 400) {
      var message = 'HTTP ${response.statusCode}';
      dynamic decoded;
      try {
        decoded = jsonDecode(utf8.decode(response.bodyBytes));
        if (decoded is Map<String, dynamic>) {
          final err = decoded['error'];
          message = decoded['message'] as String? ??
              (err is Map<String, dynamic>
                  ? err['message'] as String?
                  : null) ??
              (err is String ? err : null) ??
              message;
        }
      } catch (_) {}
      throw GresbaseException(response.statusCode, message, decoded);
    }

    if (response.statusCode == 204 || response.bodyBytes.isEmpty) {
      return null;
    }

    return jsonDecode(utf8.decode(response.bodyBytes));
  }

  /// Close the underlying HTTP client.
  void close() => _client.close();
}
