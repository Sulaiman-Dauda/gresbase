/// File service for uploading and downloading files.
library gresbase_sdk_file;

import 'dart:typed_data';
import 'http_client.dart';

/// Provides file upload and download operations.
class FileService {
  final HttpClient _http;

  FileService(this._http);

  /// Get a file download URL.
  String getURL(String collection, String recordId, String filename,
      {String? token}) {
    var url = '${_http.baseUrl}/api/v1/files/$collection/$recordId/$filename';
    if (token != null) {
      url += '?token=${Uri.encodeComponent(token)}';
    }
    return url;
  }

  /// Upload a file.
  ///
  /// Returns the file metadata including filename, url, and size.
  Future<Map<String, dynamic>> upload(
    Uint8List fileBytes,
    String filename,
  ) async {
    return _http.upload(
      '/api/v1/files/upload',
      fileBytes.toList(),
      filename,
    );
  }

  /// Delete a file.
  Future<void> delete(
      String collection, String recordId, String filename) async {
    await _http.request(
      'DELETE',
      '/api/v1/files/$collection/$recordId/$filename',
    );
  }

  /// Download file bytes.
  Future<Uint8List> download(String collection, String recordId,
      String filename) async {
    final url = getURL(collection, recordId, filename);
    final client = HttpClient();
    final uri = Uri.parse(url);
    final request = await client.getUrl(uri);

    final token = _http.getToken?.call();
    if (token != null && token.isNotEmpty) {
      request.headers.set('Authorization', 'Bearer $token');
    }

    final response = await request.close();

    if (response.statusCode >= 400) {
      throw HttpException('Download failed: HTTP ${response.statusCode}');
    }

    final bytes = <int>[];
    await for (final chunk in response) {
      bytes.addAll(chunk);
    }

    return Uint8List.fromList(bytes);
  }
}
