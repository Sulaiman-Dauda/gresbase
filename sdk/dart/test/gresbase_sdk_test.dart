import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:test/test.dart';

import 'package:gresbase_sdk/gresbase_sdk.dart';

const baseUrl = 'http://localhost:8080';

/// A captured request: method, uri, decoded JSON body (when JSON), headers.
class CapturedRequest {
  final String method;
  final Uri uri;
  final String rawBody;
  final Map<String, String> headers;

  CapturedRequest(this.method, this.uri, this.rawBody, this.headers);

  dynamic get jsonBody => rawBody.isEmpty ? null : jsonDecode(rawBody);
}

/// Builds a [GresbaseClient] whose HTTP layer is served by [handler].
/// All requests are recorded into [log].
GresbaseClient mockedClient(
  List<CapturedRequest> log,
  http.Response Function(CapturedRequest req) handler, {
  String? token,
  AuthPersistCallback? onAuthChange,
}) {
  final mock = MockClient((request) async {
    final captured = CapturedRequest(
        request.method, request.url, request.body, request.headers);
    log.add(captured);
    return handler(captured);
  });
  return GresbaseClient(
    url: baseUrl,
    httpClient: mock,
    token: token,
    onAuthChange: onAuthChange,
  );
}

http.Response jsonResponse(Object body, {int status = 200}) =>
    http.Response(jsonEncode(body), status,
        headers: {'content-type': 'application/json'});

void main() {
  group('auth token storage', () {
    test('login stores token, refreshToken, admin and fires persistence',
        () async {
      final log = <CapturedRequest>[];
      final snapshots = <Map<String, dynamic>>[];
      final client = mockedClient(
        log,
        (req) => jsonResponse({
          'token': 'tok1',
          'refreshToken': 'ref1',
          'admin': {'id': 'a1', 'email': 'admin@example.com', 'role': 'admin'},
        }),
        onAuthChange: snapshots.add,
      );

      final res = await client.auth.login('admin@example.com', 'secret');

      expect(log.single.method, 'POST');
      expect(log.single.uri.path, '/api/v1/auth/login');
      expect(log.single.jsonBody,
          {'email': 'admin@example.com', 'password': 'secret'});
      expect(res.token, 'tok1');
      expect(client.auth.token, 'tok1');
      expect(client.auth.refreshToken, 'ref1');
      expect(client.auth.admin?.email, 'admin@example.com');
      expect(client.auth.isAuthenticated, isTrue);
      expect(snapshots.last['token'], 'tok1');
      expect(snapshots.last['refreshToken'], 'ref1');
    });

    test('requests carry Authorization bearer header', () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(
        log,
        (req) => jsonResponse({'items': [], 'page': 1}),
        token: 'tokX',
      );

      await client.collection('posts').getList();
      expect(log.single.headers['Authorization'], 'Bearer tokX');
    });

    test('auto-refreshes once on 401 and retries the original request',
        () async {
      final log = <CapturedRequest>[];
      late GresbaseClient client;
      client = mockedClient(log, (req) {
        if (req.uri.path == '/api/v1/auth/refresh') {
          expect(req.jsonBody, {'refreshToken': 'ref-old'});
          return jsonResponse({'token': 'tok-new', 'refreshToken': 'ref-new'});
        }
        if (req.uri.path == '/api/v1/admin/me') {
          if (req.headers['Authorization'] == 'Bearer tok-new') {
            return jsonResponse({'id': 'a1', 'email': 'me@example.com'});
          }
          return jsonResponse({'message': 'token expired'}, status: 401);
        }
        return jsonResponse({'message': 'not found'}, status: 404);
      });
      client.auth.setToken('tok-old', 'ref-old');

      final me = await client.auth.getMe();

      expect(me.email, 'me@example.com');
      expect(client.auth.token, 'tok-new');
      expect(client.auth.refreshToken, 'ref-new');
      // me (401) -> refresh -> me (200)
      final paths = log.map((r) => r.uri.path).toList();
      expect(paths,
          ['/api/v1/admin/me', '/api/v1/auth/refresh', '/api/v1/admin/me']);
    });

    test('failed refresh clears auth state and surfaces the 401', () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(log, (req) {
        if (req.uri.path == '/api/v1/auth/refresh') {
          return jsonResponse({'message': 'invalid refresh token'},
              status: 401);
        }
        return jsonResponse({'message': 'unauthorized'}, status: 401);
      });
      client.auth.setToken('tok-old', 'ref-old');

      await expectLater(
        client.auth.getMe(),
        throwsA(isA<GresbaseException>()
            .having((e) => e.statusCode, 'statusCode', 401)),
      );
      expect(client.auth.token, isNull);
      expect(client.auth.refreshToken, isNull);
    });

    test('logout clears state and calls the server', () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(log, (req) => jsonResponse({}));
      client.auth.setToken('tok', 'ref');

      await client.auth.logout();

      expect(log.single.uri.path, '/api/v1/auth/logout');
      expect(client.auth.token, isNull);
      expect(client.auth.isAuthenticated, isFalse);
    });
  });

  group('record auth', () {
    final authResult = {
      'token': 'rec-tok',
      'refreshToken': 'rec-ref',
      'record': {'id': 'u1', 'email': 'user@example.com'},
    };

    test('authWithPassword posts identity/password and stores tokens',
        () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(log, (req) => jsonResponse(authResult));

      final res = await client
          .collection('users')
          .authWithPassword('user@example.com', 'pw123');

      expect(log.single.method, 'POST');
      expect(log.single.uri.path,
          '/api/v1/collections/users/auth/auth-with-password');
      expect(log.single.jsonBody,
          {'identity': 'user@example.com', 'password': 'pw123'});
      expect(res.token, 'rec-tok');
      expect(res.record['id'], 'u1');
      expect(client.auth.token, 'rec-tok');
      expect(client.auth.refreshToken, 'rec-ref');
      expect(client.auth.record?['id'], 'u1');
    });

    test('authWithAnonymous posts to the anonymous route', () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(log, (req) => jsonResponse(authResult));

      final res = await client.collection('users').authWithAnonymous();

      expect(log.single.method, 'POST');
      expect(log.single.uri.path,
          '/api/v1/collections/users/auth/auth-with-anonymous');
      expect(res.token, 'rec-tok');
      expect(client.auth.token, 'rec-tok');
    });

    test('authRefresh sends the stored refresh token', () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(log, (req) => jsonResponse(authResult));
      client.auth.setToken('old', 'stored-ref');

      await client.collection('users').authRefresh();

      expect(
          log.single.uri.path, '/api/v1/collections/users/auth/auth-refresh');
      expect(log.single.jsonBody, {'refreshToken': 'stored-ref'});
      expect(client.auth.token, 'rec-tok');
    });

    test('authRefresh without a refresh token throws', () async {
      final client = mockedClient([], (req) => jsonResponse(authResult));
      expect(() => client.collection('users').authRefresh(),
          throwsA(isA<GresbaseException>()));
    });

    test('password reset and verification routes', () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(log, (req) => jsonResponse({}));
      final users = client.collection('users');

      await users.requestPasswordReset('a@b.c');
      await users.confirmPasswordReset('tok123', 'newpw');
      await users.requestVerification('a@b.c');
      await users.confirmVerification('vtok');

      expect(log[0].uri.path,
          '/api/v1/collections/users/auth/request-password-reset');
      expect(log[0].jsonBody, {'email': 'a@b.c'});
      expect(log[1].uri.path,
          '/api/v1/collections/users/auth/confirm-password-reset');
      expect(log[1].jsonBody, {'token': 'tok123', 'password': 'newpw'});
      expect(log[2].uri.path,
          '/api/v1/collections/users/auth/request-verification');
      expect(log[2].jsonBody, {'email': 'a@b.c'});
      expect(log[3].uri.path,
          '/api/v1/collections/users/auth/confirm-verification');
      expect(log[3].jsonBody, {'token': 'vtok'});
    });
  });

  group('records CRUD', () {
    test('getList encodes pagination and query params', () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(
        log,
        (req) => jsonResponse({
          'items': [
            {'id': 'r1'}
          ],
          'page': 2,
          'perPage': 50,
          'totalItems': 51,
          'totalPages': 2,
        }),
      );

      final res = await client.collection('posts').getList(
            page: 2,
            perPage: 50,
            filter: 'status = "active" && age >= 18',
            sort: '-created',
            expand: 'author',
            fields: 'id,title',
          );

      final req = log.single;
      expect(req.method, 'GET');
      expect(req.uri.path, '/api/v1/records/posts');
      expect(req.uri.queryParameters, {
        'page': '2',
        'perPage': '50',
        'filter': 'status = "active" && age >= 18',
        'sort': '-created',
        'expand': 'author',
        'fields': 'id,title',
      });
      expect(res.items.single['id'], 'r1');
      expect(res.page, 2);
      expect(res.perPage, 50);
      expect(res.totalItems, 51);
      expect(res.totalPages, 2);
    });

    test('getFullList auto-paginates', () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(log, (req) {
        final page = int.parse(req.uri.queryParameters['page']!);
        return jsonResponse({
          'items': [
            {'id': 'r$page'}
          ],
          'page': page,
          'perPage': 1,
          'totalItems': 3,
          'totalPages': 3,
        });
      });

      final items = await client.collection('posts').getFullList(perPage: 1);
      expect(items.map((r) => r['id']), ['r1', 'r2', 'r3']);
      expect(log, hasLength(3));
    });

    test('getOne, create, update, delete hit the right routes', () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(log, (req) => jsonResponse({'id': 'r1'}));
      final posts = client.collection('posts');

      await posts.getOne('r1', expand: 'author');
      await posts.create({'title': 'hello'});
      await posts.update('r1', {'title': 'updated'});
      await posts.delete('r1');

      expect(log[0].method, 'GET');
      expect(log[0].uri.path, '/api/v1/records/posts/r1');
      expect(log[0].uri.queryParameters['expand'], 'author');
      expect(log[1].method, 'POST');
      expect(log[1].uri.path, '/api/v1/records/posts');
      expect(log[1].jsonBody, {'title': 'hello'});
      expect(log[2].method, 'PUT');
      expect(log[2].uri.path, '/api/v1/records/posts/r1');
      expect(log[2].jsonBody, {'title': 'updated'});
      expect(log[3].method, 'DELETE');
      expect(log[3].uri.path, '/api/v1/records/posts/r1');
    });

    test('getFirstListItem returns first match and throws on empty', () async {
      final log = <CapturedRequest>[];
      var empty = false;
      final client = mockedClient(
        log,
        (req) => jsonResponse({
          'items': empty
              ? []
              : [
                  {'id': 'first'}
                ],
          'page': 1,
          'perPage': 1,
          'totalItems': empty ? 0 : 1,
          'totalPages': empty ? 0 : 1,
        }),
      );

      final rec =
          await client.collection('posts').getFirstListItem('slug = "x"');
      expect(rec['id'], 'first');
      expect(log.single.uri.queryParameters['filter'], 'slug = "x"');
      expect(log.single.uri.queryParameters['perPage'], '1');

      empty = true;
      expect(() => client.collection('posts').getFirstListItem('slug = "y"'),
          throwsA(isA<GresbaseException>()));
    });
  });

  group('batch', () {
    test('collection batch sends creates/updates/deletes body', () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(
        log,
        (req) => jsonResponse({
          'created': [
            {'id': 'n1', 'title': 'a'}
          ],
          'updated': 1,
          'deleted': 2,
        }),
      );

      final res = await client.collection('posts').batch(
        creates: [
          {'title': 'a'}
        ],
        updates: {
          'r1': {'title': 'b'}
        },
        deletes: ['r2', 'r3'],
      );

      final req = log.single;
      expect(req.method, 'POST');
      expect(req.uri.path, '/api/v1/batch/posts');
      expect(req.jsonBody, {
        'creates': [
          {'title': 'a'}
        ],
        'updates': {
          'r1': {'title': 'b'}
        },
        'deletes': ['r2', 'r3'],
      });
      expect(res.created.single['id'], 'n1');
      expect(res.updated, 1);
      expect(res.deleted, 2);
    });

    test('batchCreate/batchUpdate/batchDelete convenience wrappers', () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(log,
          (req) => jsonResponse({'created': [], 'updated': 0, 'deleted': 0}));
      final posts = client.collection('posts');

      await posts.batchCreate([
        {'title': 'x'}
      ]);
      await posts.batchUpdate({
        'r1': {'title': 'y'}
      });
      await posts.batchDelete(['r1']);

      expect(log[0].jsonBody, {
        'creates': [
          {'title': 'x'}
        ]
      });
      expect(log[1].jsonBody, {
        'updates': {
          'r1': {'title': 'y'}
        }
      });
      expect(log[2].jsonBody, {
        'deletes': ['r1']
      });
    });
  });

  group('aggregate', () {
    test('builds the aggregate URL with all params', () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(
        log,
        (req) => jsonResponse({
          'items': [
            {'status': 'paid', 'count': 12, 'sum_total': 423.5}
          ]
        }),
      );

      final res = await client.collection('orders').aggregate(
            aggregate: 'count,sum:total,avg:total',
            groupBy: 'status',
            filter: 'created >= "2026-01-01"',
            sort: '-count',
            limit: 10,
          );

      final req = log.single;
      expect(req.method, 'GET');
      expect(req.uri.path, '/api/v1/records/orders/aggregate');
      expect(req.uri.queryParameters, {
        'aggregate': 'count,sum:total,avg:total',
        'groupBy': 'status',
        'filter': 'created >= "2026-01-01"',
        'sort': '-count',
        'limit': '10',
      });
      expect(res.items.single['count'], 12);
      expect(res.items.single['sum_total'], 423.5);
    });
  });

  group('files', () {
    test('getUrl builds plain and decorated URLs', () {
      final client = mockedClient([], (req) => jsonResponse({}));

      expect(
        client.files.getUrl('posts', 'r1', 'cover.png'),
        '$baseUrl/api/v1/files/posts/r1/cover.png',
      );

      final url = client.files.getUrl('posts', 'r1', 'cover.png',
          token: 'ft', thumb: '300x200', format: 'jpeg', quality: 80);
      final uri = Uri.parse(url);
      expect(uri.path, '/api/v1/files/posts/r1/cover.png');
      expect(uri.queryParameters, {
        'token': 'ft',
        'thumb': '300x200',
        'format': 'jpeg',
        'quality': '80',
      });
    });

    test('upload sends multipart form data with auth header', () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(
        log,
        (req) =>
            jsonResponse({'filename': 'a.txt', 'url': '/f/a.txt', 'size': 5}),
        token: 'tokU',
      );

      final res = await client.files.upload(utf8.encode('hello'), 'a.txt');

      final req = log.single;
      expect(req.method, 'POST');
      expect(req.uri.path, '/api/v1/files/upload');
      expect(req.headers['Authorization'], 'Bearer tokU');
      expect(req.headers['content-type'], contains('multipart/form-data'));
      expect(req.rawBody, contains('filename="a.txt"'));
      expect(req.rawBody, contains('hello'));
      expect(res.filename, 'a.txt');
      expect(res.size, 5);
    });
  });

  group('realtime', () {
    /// Builds a client whose mock HTTP layer serves an SSE stream on
    /// /api/v1/sse (driven by [sse]) and records JSON posts into [log].
    GresbaseClient sseClient(
      StreamController<List<int>> sse,
      List<CapturedRequest> log, {
      http.Response Function(CapturedRequest req)? handler,
      void Function(CapturedRequest req)? onRealtimePost,
    }) {
      final mock = MockClient.streaming((request, bodyStream) async {
        if (request.url.path == '/api/v1/sse') {
          return http.StreamedResponse(sse.stream, 200,
              headers: {'content-type': 'text/event-stream'});
        }
        final body = await bodyStream.bytesToString();
        final captured =
            CapturedRequest(request.method, request.url, body, request.headers);
        log.add(captured);
        if (request.url.path == '/api/v1/realtime') {
          onRealtimePost?.call(captured);
        }
        final response =
            handler?.call(captured) ?? jsonResponse(const <String, dynamic>{});
        return http.StreamedResponse(
            Stream.value(utf8.encode(response.body)), response.statusCode,
            headers: response.headers);
      });
      return GresbaseClient(url: baseUrl, httpClient: mock);
    }

    void emitSse(StreamController<List<int>> sse, Map<String, dynamic> msg) {
      sse.add(utf8.encode('data: ${jsonEncode(msg)}\n\n'));
    }

    test(
        'subscribe sends envelope after connection:established and delivers '
        'record events', () async {
      final sse = StreamController<List<int>>();
      final log = <CapturedRequest>[];
      final envelope = Completer<CapturedRequest>();
      final client = sseClient(sse, log, onRealtimePost: (req) {
        if (!envelope.isCompleted) envelope.complete(req);
      });

      final events = <RecordSubscriptionEvent>[];
      client.realtime.subscribe('posts', events.add,
          filter: 'published = true', fields: 'id,title');

      // Server assigns the client ID; pending subscriptions are flushed.
      emitSse(sse, {
        'event': 'connection:established',
        'client_id': 'cid-1',
        'timestamp': 1,
      });

      final req = await envelope.future.timeout(const Duration(seconds: 5));
      expect(req.jsonBody, {
        'type': 'subscribe',
        'clientId': 'cid-1',
        'subscriptions': ['posts/*'],
        'query': {
          'filter': 'published = true',
          'fields': 'id,title',
          'expand': '',
        },
      });
      expect(client.realtime.clientId, 'cid-1');
      expect(client.realtime.isConnected, isTrue);

      // Record event delivery.
      emitSse(sse, {
        'event': 'record:create',
        'topic': 'posts/r9',
        'data': {
          'record': {'id': 'r9', 'title': 'hi'}
        },
        'timestamp': 2,
      });
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(events.single.action, 'create');
      expect(events.single.record['id'], 'r9');

      client.realtime.disconnect();
      await sse.close();
    });

    test('subscribeToChannel includes presence options in the envelope',
        () async {
      final sse = StreamController<List<int>>();
      final log = <CapturedRequest>[];
      final envelope = Completer<CapturedRequest>();
      final client = sseClient(sse, log, onRealtimePost: (req) {
        if (!envelope.isCompleted) envelope.complete(req);
      });

      final messages = <ChannelMessage>[];
      client.realtime.subscribeToChannel('room:1', messages.add,
          presence: {'name': 'Ada'});

      emitSse(sse, {
        'event': 'connection:established',
        'client_id': 'cid-2',
        'timestamp': 1,
      });

      final req = await envelope.future.timeout(const Duration(seconds: 5));
      expect(req.jsonBody, {
        'type': 'subscribe',
        'clientId': 'cid-2',
        'subscriptions': ['room:1'],
        'options': {
          'presence': {'name': 'Ada'}
        },
      });

      // Channel message routing (only matching channel is delivered).
      emitSse(sse, {
        'event': 'chat',
        'channel': 'room:1',
        'data': {'text': 'hello'},
        'client_id': 'cid-9',
        'timestamp': 2,
      });
      emitSse(sse, {
        'event': 'chat',
        'channel': 'room:2',
        'data': {'text': 'other'},
        'timestamp': 3,
      });
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(messages, hasLength(1));
      expect(messages.single.event, 'chat');
      expect(messages.single.data, {'text': 'hello'});
      expect(messages.single.clientId, 'cid-9');

      client.realtime.disconnect();
      await sse.close();
    });

    test('unsubscribe sends an unsubscribe envelope', () async {
      final sse = StreamController<List<int>>();
      final log = <CapturedRequest>[];
      final posts = <CapturedRequest>[];
      final first = Completer<void>();
      final client = sseClient(sse, log, onRealtimePost: (req) {
        posts.add(req);
        if (!first.isCompleted) first.complete();
      });

      final unsub = client.realtime.subscribeToChannel('room:1', (_) {});
      emitSse(sse, {
        'event': 'connection:established',
        'client_id': 'cid-3',
        'timestamp': 1,
      });
      await first.future.timeout(const Duration(seconds: 5));

      await unsub();
      expect(posts.last.jsonBody, {
        'type': 'unsubscribe',
        'clientId': 'cid-3',
        'subscriptions': ['room:1'],
      });

      client.realtime.disconnect();
      await sse.close();
    });

    test('broadcast posts the channel/event/data payload', () async {
      final log = <CapturedRequest>[];
      final client =
          mockedClient(log, (req) => jsonResponse({}), token: 'tokB');

      await client.realtime
          .broadcast('room:1', 'chat', {'text': 'hello world'});

      final req = log.single;
      expect(req.method, 'POST');
      expect(req.uri.path, '/api/v1/realtime/broadcast');
      expect(req.headers['Authorization'], 'Bearer tokB');
      expect(req.jsonBody, {
        'channel': 'room:1',
        'event': 'chat',
        'data': {'text': 'hello world'},
      });
    });

    test('presence posts a presence envelope and parses the snapshot',
        () async {
      final log = <CapturedRequest>[];
      final client = mockedClient(
        log,
        (req) => jsonResponse({
          'topic': 'room:1',
          'clients': 2,
          'members': [
            {
              'client_id': 'c1',
              'state': {'name': 'Ada'}
            },
            {
              'client_id': 'c2',
              'state': {'name': 'Bob'}
            },
          ],
        }),
      );

      final info = await client.realtime.presence('room:1');

      final req = log.single;
      expect(req.uri.path, '/api/v1/realtime');
      expect(req.jsonBody, {'type': 'presence', 'channel': 'room:1'});
      expect(info.topic, 'room:1');
      expect(info.clients, 2);
      expect(info.members, hasLength(2));
      expect(info.members.first.clientId, 'c1');
      expect(info.members.first.state, {'name': 'Ada'});
    });

    test('presence unwraps an enveloped data payload', () async {
      final client = mockedClient(
        [],
        (req) => jsonResponse({
          'event': 'presence',
          'data': jsonEncode({
            'topic': 'room:1',
            'clients': 1,
            'members': [
              {'client_id': 'c1', 'state': null}
            ],
          }),
          'timestamp': 1,
        }),
      );

      final info = await client.realtime.presence('room:1');
      expect(info.clients, 1);
      expect(info.members.single.clientId, 'c1');
    });
  });

  group('errors', () {
    test('non-2xx responses raise GresbaseException with server message',
        () async {
      final client = mockedClient(
        [],
        (req) => jsonResponse({'message': 'Access denied'}, status: 403),
      );

      await expectLater(
        client.collection('posts').getOne('r1'),
        throwsA(isA<GresbaseException>()
            .having((e) => e.statusCode, 'statusCode', 403)
            .having((e) => e.message, 'message', 'Access denied')),
      );
    });
  });
}
