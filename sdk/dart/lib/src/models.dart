/// Data models for the Gresbase Dart SDK.
library;

/// A generic record from a collection.
typedef RecordData = Map<String, dynamic>;

/// Exception thrown for non-2xx API responses and SDK errors.
class GresbaseException implements Exception {
  /// HTTP status code (0 for client-side errors).
  final int statusCode;

  /// Human-readable error message.
  final String message;

  /// Raw decoded response body, when available.
  final dynamic response;

  GresbaseException(this.statusCode, this.message, [this.response]);

  @override
  String toString() => 'GresbaseException($statusCode): $message';
}

/// Admin authentication response from login/register/refresh.
class AuthResponse {
  final String token;
  final String refreshToken;
  final AdminUser admin;

  AuthResponse({
    required this.token,
    required this.refreshToken,
    required this.admin,
  });

  factory AuthResponse.fromJson(Map<String, dynamic> json) => AuthResponse(
        token: json['token'] as String? ?? '',
        refreshToken: json['refreshToken'] as String? ?? '',
        admin: AdminUser.fromJson(
            json['admin'] as Map<String, dynamic>? ?? const {}),
      );

  Map<String, dynamic> toJson() => {
        'token': token,
        'refreshToken': refreshToken,
        'admin': admin.toJson(),
      };
}

/// Record auth response (for auth collections).
class RecordAuthResponse {
  final String token;
  final String refreshToken;
  final RecordData record;

  RecordAuthResponse({
    required this.token,
    required this.refreshToken,
    required this.record,
  });

  factory RecordAuthResponse.fromJson(Map<String, dynamic> json) =>
      RecordAuthResponse(
        token: json['token'] as String? ?? '',
        refreshToken: json['refreshToken'] as String? ?? '',
        record: (json['record'] as Map<String, dynamic>?) ?? const {},
      );

  Map<String, dynamic> toJson() => {
        'token': token,
        'refreshToken': refreshToken,
        'record': record,
      };
}

/// Admin user model.
class AdminUser {
  final String id;
  final String email;
  final String role;
  final String? avatar;
  final String tenantId;
  final String? createdAt;
  final String? updatedAt;

  AdminUser({
    required this.id,
    required this.email,
    required this.role,
    this.avatar,
    required this.tenantId,
    this.createdAt,
    this.updatedAt,
  });

  factory AdminUser.fromJson(Map<String, dynamic> json) => AdminUser(
        id: json['id'] as String? ?? '',
        email: json['email'] as String? ?? '',
        role: json['role'] as String? ?? 'admin',
        avatar: json['avatar'] as String?,
        tenantId: json['tenant_id'] as String? ?? 'default',
        createdAt: json['created_at'] as String?,
        updatedAt: json['updated_at'] as String?,
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'email': email,
        'role': role,
        'avatar': avatar,
        'tenant_id': tenantId,
        'created_at': createdAt,
        'updated_at': updatedAt,
      };
}

/// Collection schema field definition.
class SchemaField {
  final String id;
  final String name;
  final String type;
  final bool system;
  final bool required;
  final bool unique;
  final Map<String, dynamic>? options;

  SchemaField({
    required this.id,
    required this.name,
    required this.type,
    this.system = false,
    this.required = false,
    this.unique = false,
    this.options,
  });

  factory SchemaField.fromJson(Map<String, dynamic> json) => SchemaField(
        id: json['id'] as String? ?? '',
        name: json['name'] as String? ?? '',
        type: json['type'] as String? ?? 'text',
        system: json['system'] as bool? ?? false,
        required: json['required'] as bool? ?? false,
        unique: json['unique'] as bool? ?? false,
        options: json['options'] as Map<String, dynamic>?,
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'type': type,
        'system': system,
        'required': required,
        'unique': unique,
        'options': options,
      };
}

/// Collection model.
///
/// Access rules are tri-state: `null` = locked (superusers only),
/// `''` = public, expression = filtered.
class CollectionModel {
  final String id;
  final String name;
  final String type;
  final List<SchemaField> schema;
  final String? listRule;
  final String? viewRule;
  final String? createRule;
  final String? updateRule;
  final String? deleteRule;
  final List<String>? indexes;
  final bool system;
  final Map<String, dynamic>? options;
  final String? createdAt;
  final String? updatedAt;

  CollectionModel({
    required this.id,
    required this.name,
    required this.type,
    required this.schema,
    this.listRule,
    this.viewRule,
    this.createRule,
    this.updateRule,
    this.deleteRule,
    this.indexes,
    this.system = false,
    this.options,
    this.createdAt,
    this.updatedAt,
  });

  factory CollectionModel.fromJson(Map<String, dynamic> json) =>
      CollectionModel(
        id: json['id'] as String? ?? '',
        name: json['name'] as String? ?? '',
        type: json['type'] as String? ?? 'base',
        schema: (json['schema'] as List<dynamic>?)
                ?.map((e) => SchemaField.fromJson(e as Map<String, dynamic>))
                .toList() ??
            [],
        listRule: json['list_rule'] as String?,
        viewRule: json['view_rule'] as String?,
        createRule: json['create_rule'] as String?,
        updateRule: json['update_rule'] as String?,
        deleteRule: json['delete_rule'] as String?,
        indexes: (json['indexes'] as List<dynamic>?)?.cast<String>(),
        system: json['system'] as bool? ?? false,
        options: json['options'] as Map<String, dynamic>?,
        createdAt: json['created_at'] as String?,
        updatedAt: json['updated_at'] as String?,
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'type': type,
        'schema': schema.map((e) => e.toJson()).toList(),
        'list_rule': listRule,
        'view_rule': viewRule,
        'create_rule': createRule,
        'update_rule': updateRule,
        'delete_rule': deleteRule,
        'indexes': indexes,
        'system': system,
        'options': options,
        'created_at': createdAt,
        'updated_at': updatedAt,
      };
}

/// Paginated list response.
class PaginatedResponse<T> {
  final List<T> items;
  final int page;
  final int perPage;
  final int totalItems;
  final int totalPages;

  PaginatedResponse({
    required this.items,
    required this.page,
    required this.perPage,
    required this.totalItems,
    required this.totalPages,
  });

  /// Parse from JSON. When [fromJsonT] is omitted, items are returned as
  /// raw `Map<String, dynamic>` records (only valid when `T` is RecordData).
  factory PaginatedResponse.fromJson(
    Map<String, dynamic> json, [
    T Function(Map<String, dynamic>)? fromJsonT,
  ]) =>
      PaginatedResponse(
        items: (json['items'] as List<dynamic>?)
                ?.map((e) => fromJsonT != null
                    ? fromJsonT(e as Map<String, dynamic>)
                    : (e as Map<String, dynamic>) as T)
                .toList() ??
            [],
        page: (json['page'] as num?)?.toInt() ?? 1,
        perPage: (json['perPage'] as num?)?.toInt() ?? 30,
        totalItems: (json['totalItems'] as num?)?.toInt() ?? 0,
        totalPages: (json['totalPages'] as num?)?.toInt() ?? 0,
      );
}

/// Result of an aggregate query.
///
/// Each item contains the groupBy fields plus `count`, `sum_<field>`,
/// `avg_<field>`, `min_<field>`, `max_<field>` keys.
class AggregateResponse {
  final List<Map<String, dynamic>> items;

  AggregateResponse({required this.items});

  factory AggregateResponse.fromJson(Map<String, dynamic> json) =>
      AggregateResponse(
        items: (json['items'] as List<dynamic>?)
                ?.map((e) => e as Map<String, dynamic>)
                .toList() ??
            [],
      );
}

/// Result of a pgvector similarity search
/// (`POST /api/v1/records/{collection}/search-vector`).
class VectorSearchResponse {
  /// Matching records, nearest first. Each map carries a numeric `_distance`
  /// (smaller = closer to the query embedding).
  final List<RecordData> items;

  /// Number of records returned.
  final int totalItems;

  VectorSearchResponse({required this.items, required this.totalItems});

  factory VectorSearchResponse.fromJson(Map<String, dynamic> json) =>
      VectorSearchResponse(
        items: (json['items'] as List<dynamic>?)
                ?.map((e) => e as Map<String, dynamic>)
                .toList() ??
            [],
        totalItems: (json['totalItems'] as num?)?.toInt() ?? 0,
      );
}

/// Result of a per-collection batch operation
/// (`POST /api/v1/batch/{collection}`).
class BatchResult {
  /// Records created in this batch (full record payloads).
  final List<RecordData> created;

  /// Number of records updated.
  final int updated;

  /// Number of records deleted.
  final int deleted;

  BatchResult({
    required this.created,
    required this.updated,
    required this.deleted,
  });

  factory BatchResult.fromJson(Map<String, dynamic> json) => BatchResult(
        created: (json['created'] as List<dynamic>?)
                ?.map((e) => e as Map<String, dynamic>)
                .toList() ??
            [],
        updated: (json['updated'] as num?)?.toInt() ?? 0,
        deleted: (json['deleted'] as num?)?.toInt() ?? 0,
      );
}

/// A realtime event for a record subscription.
class RecordSubscriptionEvent {
  /// One of `create`, `update`, `delete`.
  final String action;

  /// The affected record.
  final RecordData record;

  RecordSubscriptionEvent({required this.action, required this.record});
}

/// A message published on a realtime channel
/// (broadcasts, `presence:join` / `presence:leave`, custom events).
class ChannelMessage {
  final String event;
  final dynamic data;
  final String? clientId;

  ChannelMessage({required this.event, this.data, this.clientId});
}

/// A member present on a channel.
class PresenceMember {
  final String clientId;
  final dynamic state;

  PresenceMember({required this.clientId, this.state});

  factory PresenceMember.fromJson(Map<String, dynamic> json) => PresenceMember(
        clientId: json['client_id'] as String? ?? '',
        state: json['state'],
      );
}

/// Presence snapshot for a channel.
class PresenceInfo {
  final String? topic;
  final int clients;
  final List<PresenceMember> members;

  PresenceInfo({this.topic, required this.clients, required this.members});

  factory PresenceInfo.fromJson(Map<String, dynamic> json) => PresenceInfo(
        topic: json['topic'] as String?,
        clients: (json['clients'] as num?)?.toInt() ?? 0,
        members: (json['members'] as List<dynamic>?)
                ?.map((e) => PresenceMember.fromJson(e as Map<String, dynamic>))
                .toList() ??
            [],
      );
}

/// Raw realtime message envelope received over SSE.
class RealtimeMessage {
  final String? clientId;
  final String event;
  final String? channel;
  final String? topic;
  final dynamic data;
  final int? timestamp;

  RealtimeMessage({
    this.clientId,
    required this.event,
    this.channel,
    this.topic,
    this.data,
    this.timestamp,
  });

  factory RealtimeMessage.fromJson(Map<String, dynamic> json) =>
      RealtimeMessage(
        clientId: json['client_id'] as String?,
        event: json['event'] as String? ?? '',
        channel: json['channel'] as String?,
        topic: json['topic'] as String?,
        data: json['data'],
        timestamp: (json['timestamp'] as num?)?.toInt(),
      );
}

/// API key data.
class ApiKeyData {
  final String id;
  final String name;
  final String prefix;
  final List<String>? permissions;
  final String? lastUsedAt;
  final String? expiresAt;
  final String? createdAt;

  /// Only present on creation.
  final String? key;

  ApiKeyData({
    required this.id,
    required this.name,
    required this.prefix,
    this.permissions,
    this.lastUsedAt,
    this.expiresAt,
    this.createdAt,
    this.key,
  });

  factory ApiKeyData.fromJson(Map<String, dynamic> json) => ApiKeyData(
        id: json['id'] as String? ?? '',
        name: json['name'] as String? ?? '',
        prefix: json['prefix'] as String? ?? '',
        permissions: (json['permissions'] as List<dynamic>?)?.cast<String>(),
        lastUsedAt: json['last_used_at'] as String?,
        expiresAt: json['expires_at'] as String?,
        createdAt: json['created_at'] as String?,
        key: json['key'] as String?,
      );
}

/// TLS certificate data.
class Certificate {
  final String id;
  final String domain;
  final String status;
  final String? notBefore;
  final String? notAfter;
  final bool autoRenew;

  Certificate({
    required this.id,
    required this.domain,
    required this.status,
    this.notBefore,
    this.notAfter,
    this.autoRenew = true,
  });

  factory Certificate.fromJson(Map<String, dynamic> json) => Certificate(
        id: json['id'] as String? ?? '',
        domain: json['domain'] as String? ?? '',
        status: json['status'] as String? ?? 'active',
        notBefore: json['not_before'] as String?,
        notAfter: json['not_after'] as String?,
        autoRenew: json['auto_renew'] as bool? ?? true,
      );
}
