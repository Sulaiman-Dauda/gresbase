/// HTTP client for Gresbase Dart SDK with auto-refresh token handling.
library gresbase_sdk_http;

import 'dart:async';
import 'dart:convert';
import 'dart:io';

/// HTTP client wrapper with auth token management.
class HttpClient {
  final String baseUrl;
  final String Function()? getToken;
  final Future<String?> Function()? onRefresh;
  final HttpClient Function()? _clientFactory;

  HttpClient._({
    required this.baseUrl,
    this.getToken,
    this.onRefresh,
    HttpClient Function()? clientFactory,
  }) : _clientFactory = clientFactory;

  factory HttpClient({
    required String baseUrl,
    String Function()? getToken,
    Future<String?> Function()? onRefresh,
  }) {
    return HttpClient._(
      baseUrl: baseUrl.endsWith('/') ? baseUrl.substring(0, baseUrl.length - 1) : baseUrl,
      getToken: getToken,
      onRefresh: onRefresh,
    );
  }

  Future<HttpClientRequest> _createRequest(String method, String path) async {
    final client = _clientFactory?.call() ?? HttpClient();
    final uri = Uri.parse('$baseUrl$path');
    final request = await client.openUrl(method, uri);
    request.headers.set('Content-Type', 'application/json');
    request.headers.set('Accept', 'application/json');

    final token = getToken?.call();
    if (token != null && token.isNotEmpty) {
      request.headers.set('Authorization', 'Bearer $token');
    }

    return request;
  }

  Future<Map<String, dynamic>> request(
    String method,
    String path, {
    Map<String, dynamic>? body,
    Map<String, String>? queryParams,
  }) async {
    final fullPath = queryParams != null && queryParams.isNotEmpty
        ? '$path?${Uri(queryParameters: queryParams).query}'
        : path;

    var response = await _send(method, fullPath, body: body);

    // Auto-refresh on 401
    if (response.statusCode == 401 && onRefresh != null) {
      final newToken = await onRefresh!();
      if (newToken != null) {
        response = await _send(method, fullPath, body: body);
      }
    }

    if (response.statusCode >= 400) {
      String message = 'HTTP ${response.statusCode}';
      try {
        final errorBody = await response.transform(utf8.decoder).join();
        final errorJson = jsonDecode(errorBody) as Map<String, dynamic>;
        message = errorJson['message'] as String? ??
            errorJson['error']?['message'] as String? ??
            message;
      } catch (_) {}
      throw HttpException(message);
    }

    if (response.statusCode == 204) {
      return {};
    }

    final bodyStr = await response.transform(utf8.decoder).join();
    if (bodyStr.isEmpty) return {};
    return jsonDecode(bodyStr) as Map<String, dynamic>;
  }

  Future<List<dynamic>> requestList(
    String method,
    String path, {
    Map<String, dynamic>? body,
    Map<String, String>? queryParams,
  }) async {
    final fullPath = queryParams != null && queryParams.isNotEmpty
        ? '$path?${Uri(queryParameters: queryParams).query}'
        : path;

    var response = await _send(method, fullPath, body: body);

    if (response.statusCode == 401 && onRefresh != null) {
      final newToken = await onRefresh!();
      if (newToken != null) {
        response = await _send(method, fullPath, body: body);
      }
    }

    if (response.statusCode >= 400) {
      String message = 'HTTP ${response.statusCode}';
      try {
        final errorBody = await response.transform(utf8.decoder).join();
        final errorJson = jsonDecode(errorBody) as Map<String, dynamic>;
        message = errorJson['message'] as String? ?? message;
      } catch (_) {}
      throw HttpException(message);
    }

    final bodyStr = await response.transform(utf8.decoder).join();
    if (bodyStr.isEmpty) return [];
    final decoded = jsonDecode(bodyStr);
    return decoded is List ? decoded : [];
  }

  Future<HttpClientResponse> _send(
    String method,
    String path, {
    Map<String, dynamic>? body,
  }) async {
    final request = await _createRequest(method, path);

    if (body != null) {
      final bodyStr = jsonEncode(body);
      request.headers.set('Content-Length', bodyStr.length.toString());
      request.write(bodyStr);
    }

    return request.close();
  }

  /// Upload a file using multipart form data.
  Future<Map<String, dynamic>> upload(
    String path,
    List<int> fileBytes,
    String filename, {
    String fieldName = 'file',
  }) async {
    final client = HttpClient();
    final uri = Uri.parse('$baseUrl$path');
    final request = await client.postUrl(uri);

    final boundary = 'gresbase-upload-${DateTime.now().millisecondsSinceEpoch}';
    request.headers.set('Content-Type', 'multipart/form-data; boundary=$boundary');

    final token = getToken?.call();
    if (token != null && token.isNotEmpty) {
      request.headers.set('Authorization', 'Bearer $token');
    }

    // Build multipart body
    final body = <int>[];
    body.addAll(utf8.encode('--$boundary\r\n'));
    body.addAll(utf8.encode(
        'Content-Disposition: form-data; name="$fieldName"; filename="$filename"\r\n'));
    body.addAll(utf8.encode('Content-Type: application/octet-stream\r\n\r\n'));
    body.addAll(fileBytes);
    body.addAll(utf8.encode('\r\n--$boundary--\r\n'));

    request.headers.set('Content-Length', body.length.toString());
    request.add(body);

    final response = await request.close();

    if (response.statusCode >= 400) {
      throw HttpException('Upload failed: HTTP ${response.statusCode}');
    }

    final bodyStr = await response.transform(utf8.decoder).join();
    return jsonDecode(bodyStr) as Map<String, dynamic>;
  }
}

/// HTTP exception with message.
class HttpException implements Exception {
  final String message;
  HttpException(this.message);

  @override
  String toString() => 'HttpException: $message';
}
