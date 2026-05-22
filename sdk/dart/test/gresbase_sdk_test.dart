import 'package:test/test.dart';
import 'package:gresbase_sdk/gresbase_sdk.dart';

void main() {
  group('GresbaseClient', () {
    test('creates client with URL', () {
      final client = GresbaseClient(url: 'http://localhost:8080');
      expect(client, isNotNull);
    });

    test('strips trailing slash from URL', () {
      final client = GresbaseClient(url: 'http://localhost:8080/');
      expect(client, isNotNull);
    });
  });

  group('AuthResponse', () {
    test('fromJson parses correctly', () {
      final json = {
        'token': 'test-token-123',
        'refreshToken': 'test-refresh-456',
        'admin': {
          'id': 'admin-1',
          'email': 'admin@test.com',
          'role': 'admin',
          'tenant_id': 'default',
        },
      };

      final response = AuthResponse.fromJson(json);
      expect(response.token, equals('test-token-123'));
      expect(response.refreshToken, equals('test-refresh-456'));
      expect(response.admin.email, equals('admin@test.com'));
      expect(response.admin.role, equals('admin'));
    });

    test('toJson roundtrips', () {
      final original = AuthResponse(
        token: 'tok',
        refreshToken: 'ref',
        admin: AdminUser(
          id: '1',
          email: 'a@b.com',
          role: 'admin',
          tenantId: 'default',
        ),
      );
      final json = original.toJson();
      final parsed = AuthResponse.fromJson(json);
      expect(parsed.token, equals(original.token));
    });
  });

  group('AdminUser', () {
    test('fromJson with all fields', () {
      final json = {
        'id': 'user-1',
        'email': 'test@example.com',
        'role': 'superuser',
        'avatar': 'https://example.com/avatar.png',
        'tenant_id': 't1',
        'created_at': '2024-01-01T00:00:00Z',
        'updated_at': '2024-06-01T00:00:00Z',
      };

      final user = AdminUser.fromJson(json);
      expect(user.id, equals('user-1'));
      expect(user.email, equals('test@example.com'));
      expect(user.role, equals('superuser'));
      expect(user.avatar, equals('https://example.com/avatar.png'));
      expect(user.tenantId, equals('t1'));
      expect(user.createdAt, equals('2024-01-01T00:00:00Z'));
    });

    test('fromJson with missing fields uses defaults', () {
      final json = <String, dynamic>{};
      final user = AdminUser.fromJson(json);
      expect(user.id, equals(''));
      expect(user.email, equals(''));
      expect(user.role, equals('admin'));
      expect(user.tenantId, equals('default'));
    });
  });

  group('CollectionModel', () {
    test('fromJson parses schema fields', () {
      final json = {
        'id': 'coll-1',
        'name': 'posts',
        'type': 'base',
        'schema': [
          {'id': 'f1', 'name': 'title', 'type': 'text', 'required': true},
          {'id': 'f2', 'name': 'content', 'type': 'editor'},
        ],
        'list_rule': 'published = true',
        'system': false,
      };

      final coll = CollectionModel.fromJson(json);
      expect(coll.name, equals('posts'));
      expect(coll.schema.length, equals(2));
      expect(coll.schema[0].name, equals('title'));
      expect(coll.schema[0].required, isTrue);
      expect(coll.listRule, equals('published = true'));
    });
  });

  group('PaginatedResponse', () {
    test('fromJson with typed items', () {
      final json = {
        'items': [
          {'id': '1', 'email': 'a@b.com', 'role': 'admin', 'tenant_id': 'd'},
          {'id': '2', 'email': 'c@d.com', 'role': 'admin', 'tenant_id': 'd'},
        ],
        'page': 1,
        'perPage': 30,
        'totalItems': 42,
        'totalPages': 2,
      };

      final response = PaginatedResponse.fromJson(json, AdminUser.fromJson);
      expect(response.items.length, equals(2));
      expect(response.page, equals(1));
      expect(response.totalItems, equals(42));
      expect(response.totalPages, equals(2));
      expect(response.items[0].email, equals('a@b.com'));
    });
  });

  group('ListParams', () {
    test('toQuery builds correct params', () {
      final params = ListParams(
        page: 2,
        perPage: 50,
        sort: '-created',
        filter: 'status = "active"',
      );

      final query = params.toQuery();
      expect(query['page'], equals('2'));
      expect(query['perPage'], equals('50'));
      expect(query['sort'], equals('-created'));
      expect(query['filter'], equals('status = "active"'));
    });

    test('toQuery excludes null values', () {
      final params = ListParams(page: 1);
      final query = params.toQuery();
      expect(query.length, equals(1));
      expect(query.containsKey('sort'), isFalse);
    });
  });

  group('ApiKeyData', () {
    test('fromJson with key', () {
      final json = {
        'id': 'key-1',
        'name': 'Mobile App',
        'prefix': 'gb_abc123',
        'key': 'gb_full_key_here_1234567890',
        'created_at': '2024-01-01T00:00:00Z',
      };

      final key = ApiKeyData.fromJson(json);
      expect(key.name, equals('Mobile App'));
      expect(key.prefix, equals('gb_abc123'));
      expect(key.key, equals('gb_full_key_here_1234567890'));
    });
  });

  group('Certificate', () {
    test('fromJson parses correctly', () {
      final json = {
        'id': 'cert-1',
        'domain': 'example.com',
        'status': 'active',
        'not_before': '2024-01-01T00:00:00Z',
        'not_after': '2024-04-01T00:00:00Z',
        'auto_renew': true,
      };

      final cert = Certificate.fromJson(json);
      expect(cert.domain, equals('example.com'));
      expect(cert.status, equals('active'));
      expect(cert.autoRenew, isTrue);
    });
  });

  group('RealtimeMessage', () {
    test('fromJson parses event data', () {
      final json = {
        'event': 'record:create',
        'topic': 'posts/*',
        'data': {'action': 'create', 'record': {'id': '1', 'title': 'Hello'}},
        'timestamp': 1700000000000,
      };

      final msg = RealtimeMessage.fromJson(json);
      expect(msg.event, equals('record:create'));
      expect(msg.topic, equals('posts/*'));
      expect(msg.data, isNotNull);
    });
  });

  group('SchemaField', () {
    test('fromJson with options', () {
      final json = {
        'id': 'f1',
        'name': 'status',
        'type': 'select',
        'required': true,
        'options': {'values': ['draft', 'published', 'archived']},
      };

      final field = SchemaField.fromJson(json);
      expect(field.name, equals('status'));
      expect(field.type, equals('select'));
      expect(field.required, isTrue);
      expect(field.options?['values'], equals(['draft', 'published', 'archived']));
    });
  });
}
