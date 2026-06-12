/// File URL construction, multipart upload, and deletion.
library;

import 'http_client.dart';

/// Result of a file upload.
class FileUploadResult {
  final String filename;
  final String url;
  final int size;

  FileUploadResult({
    required this.filename,
    required this.url,
    required this.size,
  });

  factory FileUploadResult.fromJson(Map<String, dynamic> json) =>
      FileUploadResult(
        filename: json['filename'] as String? ?? '',
        url: json['url'] as String? ?? '',
        size: (json['size'] as num?)?.toInt() ?? 0,
      );
}

/// File operations: URL construction, upload, delete.
class FileService {
  final GresbaseHttpClient _http;

  FileService(this._http);

  /// Build a file download URL.
  ///
  /// Options:
  /// - [token] — access token for protected files
  /// - [thumb] — thumbnail size (e.g. `'100x100'`)
  /// - [format] — convert the image (`'jpeg'` or `'png'`)
  /// - [quality] — quality 1-100 for lossy formats
  ///
  /// ```dart
  /// client.files.getUrl('posts', recordId, 'cover.png',
  ///     thumb: '300x200', format: 'jpeg', quality: 80);
  /// ```
  String getUrl(
    String collection,
    String recordId,
    String filename, {
    String? token,
    String? thumb,
    String? format,
    int? quality,
  }) {
    var url = '${_http.baseUrl}/api/v1/files/$collection/$recordId/$filename';
    final query = <String, String>{
      if (token != null) 'token': token,
      if (thumb != null) 'thumb': thumb,
      if (format != null) 'format': format,
      if (quality != null) 'quality': '$quality',
    };
    if (query.isNotEmpty) {
      url += '?${Uri(queryParameters: query).query}';
    }
    return url;
  }

  /// Upload a file via `multipart/form-data`.
  Future<FileUploadResult> upload(List<int> bytes, String filename) async {
    final result = await _http.multipart(
      '/api/v1/files/upload',
      fileBytes: bytes,
      filename: filename,
    );
    return FileUploadResult.fromJson(result as Map<String, dynamic>);
  }

  /// Delete a file.
  Future<void> delete(
      String collection, String recordId, String filename) async {
    await _http.send('DELETE', '/api/v1/files/$collection/$recordId/$filename');
  }
}
