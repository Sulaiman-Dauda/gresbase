/// Collection service for typed record CRUD operations.
library gresbase_sdk_collection;

import 'http_client.dart';
import 'models.dart';

/// Provides CRUD operations on a Gresbase collection.
class CollectionService<T extends RecordData> {
  final String collectionName;
  final HttpClient _http;
  final T Function(Map<String, dynamic>)? _fromJson;

  CollectionService(
    this.collectionName,
    this._http, {
    T Function(Map<String, dynamic>)? fromJson,
  }) : _fromJson = fromJson;

  /// Get a paginated list of records.
  Future<PaginatedResponse<T>> getList({
    int page = 1,
    int perPage = 30,
    String? sort,
    String? filter,
    String? fields,
    String? expand,
  }) async {
    final params = <String, String>{
      'page': page.toString(),
      'perPage': perPage.toString(),
    };
    if (sort != null) params['sort'] = sort;
    if (filter != null) params['filter'] = filter;
    if (fields != null) params['fields'] = fields;
    if (expand != null) params['expand'] = expand;

    final result = await _http.request(
      'GET',
      '/api/v1/records/$collectionName',
      queryParams: params,
    );

    return PaginatedResponse.fromJson(result, (json) {
      if (_fromJson != null) return _fromJson!(json);
      return json as T;
    });
  }

  /// Get all records by auto-paginating.
  Future<List<T>> getFullList({
    int perPage = 100,
    String? sort,
    String? filter,
    String? fields,
    String? expand,
  }) async {
    final all = <T>[];
    int page = 1;
    bool hasMore = true;

    while (hasMore) {
      final result = await getList(
        page: page,
        perPage: perPage,
        sort: sort,
        filter: filter,
        fields: fields,
        expand: expand,
      );
      all.addAll(result.items);
      hasMore = page < result.totalPages;
      page++;
    }

    return all;
  }

  /// Get a single record by ID.
  Future<T> getOne(String id, {String? fields, String? expand}) async {
    final params = <String, String>{};
    if (fields != null) params['fields'] = fields;
    if (expand != null) params['expand'] = expand;

    final result = await _http.request(
      'GET',
      '/api/v1/records/$collectionName/$id',
      queryParams: params.isNotEmpty ? params : null,
    );
    if (_fromJson != null) return _fromJson!(result);
    return result as T;
  }

  /// Get the first record matching a filter.
  Future<T> getFirstListItem(String filter,
      {String? sort, String? fields, String? expand}) async {
    final result = await getList(
      page: 1,
      perPage: 1,
      filter: filter,
      sort: sort,
      fields: fields,
      expand: expand,
    );
    if (result.items.isEmpty) {
      throw Exception('No record found matching the filter');
    }
    return result.items.first;
  }

  /// Create a new record.
  Future<T> create(Map<String, dynamic> data) async {
    final result = await _http.request(
      'POST',
      '/api/v1/records/$collectionName',
      body: data,
    );
    if (_fromJson != null) return _fromJson!(result);
    return result as T;
  }

  /// Update an existing record.
  Future<T> update(String id, Map<String, dynamic> data) async {
    final result = await _http.request(
      'PUT',
      '/api/v1/records/$collectionName/$id',
      body: data,
    );
    if (_fromJson != null) return _fromJson!(result);
    return result as T;
  }

  /// Delete a record.
  Future<void> delete(String id) async {
    await _http.request(
      'DELETE',
      '/api/v1/records/$collectionName/$id',
    );
  }

  /// Batch create records (up to 500 per request).
  Future<List<T>> batchCreate(List<Map<String, dynamic>> records) async {
    final result = await _http.requestList(
      'POST',
      '/api/v1/batch/$collectionName',
      body: {'action': 'create', 'records': records},
    );
    return result.map((e) {
      if (_fromJson != null) return _fromJson!(e as Map<String, dynamic>);
      return e as T;
    }).toList();
  }
}
