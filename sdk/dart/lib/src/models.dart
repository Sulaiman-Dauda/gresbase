/// Data models for the Gresbase Dart SDK.
library gresbase_sdk_models;

/// Authentication response from login/register/refresh.
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
        token: json['token'] as String,
        refreshToken: json['refreshToken'] as String? ?? '',
        admin: AdminUser.fromJson(json['admin'] as Map<String, dynamic>),
      );

  Map<String, dynamic> toJson() => {
        'token': token,
        'refreshToken': refreshToken,
        'admin': admin.toJson(),
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

  factory CollectionModel.fromJson(Map<String, dynamic> json) => CollectionModel(
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

/// A generic record from a collection.
typedef RecordData = Map<String, dynamic>;

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

  factory PaginatedResponse.fromJson(
    Map<String, dynamic> json,
    T Function(Map<String, dynamic>) fromJsonT,
  ) =>
      PaginatedResponse(
        items: (json['items'] as List<dynamic>?)
                ?.map((e) => fromJsonT(e as Map<String, dynamic>))
                .toList() ??
            [],
        page: json['page'] as int? ?? 1,
        perPage: json['perPage'] as int? ?? 30,
        totalItems: json['totalItems'] as int? ?? 0,
        totalPages: json['totalPages'] as int? ?? 0,
      );
}

/// List parameters for querying records.
class ListParams {
  final int? page;
  final int? perPage;
  final String? sort;
  final String? filter;
  final String? fields;
  final String? expand;

  ListParams({
    this.page,
    this.perPage,
    this.sort,
    this.filter,
    this.fields,
    this.expand,
  });

  Map<String, String> toQuery() {
    final map = <String, String>{};
    if (page != null) map['page'] = page.toString();
    if (perPage != null) map['perPage'] = perPage.toString();
    if (sort != null) map['sort'] = sort!;
    if (filter != null) map['filter'] = filter!;
    if (fields != null) map['fields'] = fields!;
    if (expand != null) map['expand'] = expand!;
    return map;
  }
}

/// API key data.
class ApiKeyData {
  final String id;
  final String name;
  final String prefix;
  final String? key;
  final String? createdAt;

  ApiKeyData({
    required this.id,
    required this.name,
    required this.prefix,
    this.key,
    this.createdAt,
  });

  factory ApiKeyData.fromJson(Map<String, dynamic> json) => ApiKeyData(
        id: json['id'] as String? ?? '',
        name: json['name'] as String? ?? '',
        prefix: json['prefix'] as String? ?? '',
        key: json['key'] as String?,
        createdAt: json['created_at'] as String?,
      );
}

/// Certificate data.
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

/// Realtime message event.
class RealtimeMessage {
  final String? clientId;
  final String? event;
  final String? topic;
  final dynamic data;
  final int? timestamp;

  RealtimeMessage({
    this.clientId,
    this.event,
    this.topic,
    this.data,
    this.timestamp,
  });

  factory RealtimeMessage.fromJson(Map<String, dynamic> json) => RealtimeMessage(
        clientId: json['client_id'] as String?,
        event: json['event'] as String?,
        topic: json['topic'] as String?,
        data: json['data'],
        timestamp: json['timestamp'] as int?,
      );
}
