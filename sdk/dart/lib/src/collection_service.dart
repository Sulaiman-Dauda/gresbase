/// Record CRUD, aggregation, batch operations, and record auth for a collection.
library;

import 'auth_service.dart';
import 'http_client.dart';
import 'models.dart';

/// Service for record operations on a single collection.
///
/// Obtain via `client.collection('posts')`.
class CollectionService {
  final String collectionName;
  final GresbaseHttpClient _http;
  final AuthService _auth;

  CollectionService(this.collectionName, this._http, this._auth);

  String get _basePath => '/api/v1/records/$collectionName';
  String get _authBasePath => '/api/v1/collections/$collectionName/auth';

  // ---- CRUD ----

  /// Get a paginated list of records.
  Future<PaginatedResponse<RecordData>> getList({
    int page = 1,
    int perPage = 30,
    String? filter,
    String? sort,
    String? expand,
    String? fields,
  }) async {
    final query = <String, String>{
      'page': '$page',
      'perPage': '$perPage',
      if (sort != null) 'sort': sort,
      if (filter != null) 'filter': filter,
      if (fields != null) 'fields': fields,
      if (expand != null) 'expand': expand,
    };
    final result = await _http.send('GET', _basePath, query: query);
    return PaginatedResponse<RecordData>.fromJson(
        result as Map<String, dynamic>);
  }

  /// Get all records, auto-paginating until exhausted.
  Future<List<RecordData>> getFullList({
    int perPage = 100,
    String? filter,
    String? sort,
    String? expand,
    String? fields,
  }) async {
    final allItems = <RecordData>[];
    var page = 1;
    var hasMore = true;

    while (hasMore) {
      final result = await getList(
        page: page,
        perPage: perPage,
        filter: filter,
        sort: sort,
        expand: expand,
        fields: fields,
      );
      allItems.addAll(result.items);
      hasMore = page < result.totalPages;
      page++;
    }

    return allItems;
  }

  /// Get a single record by ID.
  Future<RecordData> getOne(String id, {String? expand, String? fields}) async {
    final query = <String, String>{
      if (fields != null) 'fields': fields,
      if (expand != null) 'expand': expand,
    };
    final result = await _http.send('GET', '$_basePath/$id',
        query: query.isEmpty ? null : query);
    return result as Map<String, dynamic>;
  }

  /// Get the first record matching a filter.
  ///
  /// Throws [GresbaseException] (404) when no record matches.
  Future<RecordData> getFirstListItem(
    String filter, {
    String? sort,
    String? expand,
    String? fields,
  }) async {
    final result = await getList(
      page: 1,
      perPage: 1,
      filter: filter,
      sort: sort,
      expand: expand,
      fields: fields,
    );
    if (result.items.isEmpty) {
      throw GresbaseException(404, 'No record found matching the filter');
    }
    return result.items.first;
  }

  /// Create a new record.
  Future<RecordData> create(Map<String, dynamic> data) async {
    final result = await _http.send('POST', _basePath, body: data);
    return result as Map<String, dynamic>;
  }

  /// Update an existing record.
  Future<RecordData> update(String id, Map<String, dynamic> data) async {
    final result = await _http.send('PUT', '$_basePath/$id', body: data);
    return result as Map<String, dynamic>;
  }

  /// Delete a record.
  Future<void> delete(String id) async {
    await _http.send('DELETE', '$_basePath/$id');
  }

  // ---- Batch ----

  /// Execute a transactional batch of creates, updates, and deletes.
  ///
  /// `POST /api/v1/batch/{collection}` with body
  /// `{creates: [...], updates: {id: patch}, deletes: [ids]}`.
  Future<BatchResult> batch({
    List<Map<String, dynamic>>? creates,
    Map<String, Map<String, dynamic>>? updates,
    List<String>? deletes,
  }) async {
    final result = await _http.send(
      'POST',
      '/api/v1/batch/$collectionName',
      body: {
        if (creates != null && creates.isNotEmpty) 'creates': creates,
        if (updates != null && updates.isNotEmpty) 'updates': updates,
        if (deletes != null && deletes.isNotEmpty) 'deletes': deletes,
      },
    );
    return BatchResult.fromJson(result as Map<String, dynamic>);
  }

  /// Batch create records. Returns the created records.
  Future<List<RecordData>> batchCreate(
      List<Map<String, dynamic>> records) async {
    final result = await batch(creates: records);
    return result.created;
  }

  /// Batch update records (map of record ID to patch).
  Future<void> batchUpdate(Map<String, Map<String, dynamic>> updates) async {
    await batch(updates: updates);
  }

  /// Batch delete records by ID.
  Future<void> batchDelete(List<String> ids) async {
    await batch(deletes: ids);
  }

  // ---- Search & Aggregate ----

  /// Search records using full-text search.
  Future<PaginatedResponse<RecordData>> search(
    String query, {
    int? page,
    int? perPage,
    String? filter,
    String? sort,
  }) async {
    final result = await _http.send(
      'POST',
      '/api/v1/search/$collectionName',
      body: {
        'query': query,
        if (page != null) 'page': page,
        if (perPage != null) 'perPage': perPage,
        if (filter != null) 'filter': filter,
        if (sort != null) 'sort': sort,
      },
    );
    return PaginatedResponse<RecordData>.fromJson(
        result as Map<String, dynamic>);
  }

  /// Run aggregations over the collection.
  ///
  /// [aggregate] is a comma-separated list: `count`, `sum:field`,
  /// `avg:field`, `min:field`, `max:field`.
  ///
  /// ```dart
  /// final res = await client.collection('orders').aggregate(
  ///   aggregate: 'count,sum:total,avg:total',
  ///   groupBy: 'status',
  ///   filter: 'created >= "2026-01-01"',
  /// );
  /// // res.items: [{status: 'paid', count: 12, sum_total: 423.5, ...}, ...]
  /// ```
  Future<AggregateResponse> aggregate({
    required String aggregate,
    String? groupBy,
    String? filter,
    String? sort,
    int? limit,
  }) async {
    final query = <String, String>{
      'aggregate': aggregate,
      if (groupBy != null) 'groupBy': groupBy,
      if (filter != null) 'filter': filter,
      if (sort != null) 'sort': sort,
      if (limit != null) 'limit': '$limit',
    };
    final result =
        await _http.send('GET', '$_basePath/aggregate', query: query);
    return AggregateResponse.fromJson(result as Map<String, dynamic>);
  }

  // ---- Record Auth (for auth collections) ----

  /// Authenticate a collection record with identity (email/username) and
  /// password.
  ///
  /// On success the token, refresh token and record are stored on the client,
  /// so subsequent requests and realtime connections are authenticated.
  Future<RecordAuthResponse> authWithPassword(
      String identity, String password) async {
    final result = await _http.send(
      'POST',
      '$_authBasePath/auth-with-password',
      body: {'identity': identity, 'password': password},
    );
    final response =
        RecordAuthResponse.fromJson(result as Map<String, dynamic>);
    _auth.setRecordAuth(response);
    return response;
  }

  /// Authenticate anonymously (creates an anonymous user record).
  ///
  /// Only works when the collection allows anonymous auth.
  Future<RecordAuthResponse> authWithAnonymous() async {
    final result =
        await _http.send('POST', '$_authBasePath/auth-with-anonymous');
    final response =
        RecordAuthResponse.fromJson(result as Map<String, dynamic>);
    _auth.setRecordAuth(response);
    return response;
  }

  /// Refresh the record auth session using the stored refresh token.
  Future<RecordAuthResponse> authRefresh() async {
    final refreshToken = _auth.refreshToken;
    if (refreshToken == null || refreshToken.isEmpty) {
      throw GresbaseException(
          0, 'No refresh token available — authenticate first');
    }
    final result = await _http.send(
      'POST',
      '$_authBasePath/auth-refresh',
      body: {'refreshToken': refreshToken},
    );
    final response =
        RecordAuthResponse.fromJson(result as Map<String, dynamic>);
    _auth.setRecordAuth(response);
    return response;
  }

  /// Request a password reset email for a record.
  Future<void> requestPasswordReset(String email) async {
    await _http.send('POST', '$_authBasePath/request-password-reset',
        body: {'email': email});
  }

  /// Confirm a password reset with the emailed token.
  Future<void> confirmPasswordReset(String token, String newPassword) async {
    await _http.send('POST', '$_authBasePath/confirm-password-reset',
        body: {'token': token, 'password': newPassword});
  }

  /// Request an email verification message for a record.
  Future<void> requestVerification(String email) async {
    await _http.send('POST', '$_authBasePath/request-verification',
        body: {'email': email});
  }

  /// Confirm email verification with the emailed token.
  Future<void> confirmVerification(String token) async {
    await _http.send('POST', '$_authBasePath/confirm-verification',
        body: {'token': token});
  }
}
