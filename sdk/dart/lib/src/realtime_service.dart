/// Realtime subscriptions over Server-Sent Events.
library;

import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;

import 'http_client.dart';
import 'models.dart';

/// Function returned by subscribe calls; invoke to unsubscribe.
typedef UnsubscribeFunc = Future<void> Function();

class _Subscription {
  final String topic;
  final void Function(RealtimeMessage msg) deliver;
  final Map<String, String>? query;
  final Map<String, dynamic>? options;

  /// Channel subscriptions receive raw `{event, data, client_id}` messages.
  final bool isChannel;

  _Subscription({
    required this.topic,
    required this.deliver,
    this.query,
    this.options,
    this.isChannel = false,
  });
}

/// Realtime service: SSE transport (`GET /api/v1/sse`) with subscription
/// management via `POST /api/v1/realtime`.
///
/// Automatically reconnects with exponential backoff and replays all active
/// subscriptions after the server assigns a new client ID.
class RealtimeService {
  final GresbaseHttpClient _http;
  final Duration _baseReconnectDelay;
  static const _maxReconnectDelay = Duration(seconds: 30);

  String? _clientId;
  bool _connected = false;
  bool _connecting = false;
  bool _manuallyDisconnected = false;
  int _reconnectAttempts = 0;
  Timer? _reconnectTimer;
  StreamSubscription<String>? _sseSubscription;

  int _subSeq = 0;
  final Map<String, _Subscription> _subscriptions = {};

  final _messages = StreamController<RealtimeMessage>.broadcast();
  final _connectionEvents = StreamController<bool>.broadcast();

  RealtimeService(this._http,
      {Duration reconnectDelay = const Duration(seconds: 3)})
      : _baseReconnectDelay = reconnectDelay;

  /// Whether the SSE connection is currently open.
  bool get isConnected => _connected;

  /// The server-assigned realtime client ID (null until connected).
  String? get clientId => _clientId;

  /// Every realtime message received over the connection.
  Stream<RealtimeMessage> get messages => _messages.stream;

  /// Connection state changes (`true` = connected, `false` = disconnected).
  Stream<bool> get connectionEvents => _connectionEvents.stream;

  /// Open the SSE connection. Idempotent; subscribe calls connect lazily.
  Future<void> connect() async {
    if (_connected || _connecting) return;
    _connecting = true;
    _manuallyDisconnected = false;

    try {
      final request = http.Request('GET', _http.buildUri('/api/v1/sse'));
      request.headers['Accept'] = 'text/event-stream';
      request.headers['Cache-Control'] = 'no-cache';
      final token = _http.getToken();
      if (token != null && token.isNotEmpty) {
        request.headers['Authorization'] = 'Bearer $token';
      }

      final response = await _http.rawClient.send(request);
      if (response.statusCode != 200) {
        throw GresbaseException(response.statusCode, 'SSE connection failed');
      }

      _connected = true;
      _connecting = false;
      _reconnectAttempts = 0;
      if (!_connectionEvents.isClosed) _connectionEvents.add(true);

      var dataBuffer = StringBuffer();
      _sseSubscription = response.stream
          .transform(utf8.decoder)
          .transform(const LineSplitter())
          .listen(
        (line) {
          if (line.isEmpty) {
            // Dispatch accumulated event.
            final payload = dataBuffer.toString();
            dataBuffer = StringBuffer();
            if (payload.isNotEmpty) _dispatch(payload);
            return;
          }
          if (line.startsWith('data:')) {
            final chunk = line.startsWith('data: ')
                ? line.substring(6)
                : line.substring(5);
            if (dataBuffer.isNotEmpty) dataBuffer.write('\n');
            dataBuffer.write(chunk);
          }
          // `event:`, `id:`, `retry:` and comment lines are ignored — the
          // server encodes everything in the JSON data payload.
        },
        onDone: _handleDisconnect,
        onError: (Object _) => _handleDisconnect(),
        cancelOnError: true,
      );
    } catch (_) {
      _connecting = false;
      _handleDisconnect();
    }
  }

  /// Close the connection and stop reconnecting.
  void disconnect() {
    _manuallyDisconnected = true;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _sseSubscription?.cancel();
    _sseSubscription = null;
    final wasConnected = _connected;
    _connected = false;
    _clientId = null;
    if (wasConnected && !_connectionEvents.isClosed) {
      _connectionEvents.add(false);
    }
  }

  void _handleDisconnect() {
    final wasConnected = _connected;
    _connected = false;
    _sseSubscription?.cancel();
    _sseSubscription = null;
    if (wasConnected && !_connectionEvents.isClosed) {
      _connectionEvents.add(false);
    }
    if (!_manuallyDisconnected) _scheduleReconnect();
  }

  void _scheduleReconnect() {
    if (_reconnectTimer != null) return;
    final multiplier = 1 << (_reconnectAttempts > 5 ? 5 : _reconnectAttempts);
    var delay = _baseReconnectDelay * multiplier;
    if (delay > _maxReconnectDelay) delay = _maxReconnectDelay;
    _reconnectAttempts++;

    _reconnectTimer = Timer(delay, () {
      _reconnectTimer = null;
      connect();
    });
  }

  void _dispatch(String payload) {
    RealtimeMessage msg;
    try {
      msg =
          RealtimeMessage.fromJson(jsonDecode(payload) as Map<String, dynamic>);
    } catch (_) {
      return;
    }
    _handleMessage(msg);
  }

  void _handleMessage(RealtimeMessage msg) {
    // The server assigns the client ID on connect.
    if (msg.event == 'connection:established' && msg.clientId != null) {
      _clientId = msg.clientId;
      // Flush any subscriptions registered before the server assigned an ID.
      _resubscribeAll();
      if (!_messages.isClosed) _messages.add(msg);
      return;
    }

    for (final sub in _subscriptions.values.toList()) {
      if (msg.event.startsWith('record:')) {
        if (!sub.isChannel) sub.deliver(msg);
      } else {
        final target = msg.channel ?? msg.topic;
        if (sub.isChannel && target == sub.topic) sub.deliver(msg);
      }
    }

    if (!_messages.isClosed) _messages.add(msg);
  }

  static dynamic _parseData(dynamic data) {
    if (data is String) {
      try {
        return jsonDecode(data);
      } catch (_) {
        return data;
      }
    }
    return data;
  }

  /// Subscribe to realtime events for a collection.
  ///
  /// The callback receives a [RecordSubscriptionEvent] with the action
  /// (`create`, `update`, `delete`) and the affected record.
  ///
  /// ```dart
  /// final unsub = client.realtime.subscribe('posts', (e) {
  ///   print('${e.action}: ${e.record}');
  /// }, filter: 'published = true');
  /// ```
  UnsubscribeFunc subscribe(
    String collection,
    void Function(RecordSubscriptionEvent event) callback, {
    String? filter,
    String? fields,
    String? expand,
  }) {
    final topic = '$collection/*';
    final query = <String, String>{
      'filter': filter ?? '',
      'fields': fields ?? '',
      'expand': expand ?? '',
    };
    return _addRecordSubscription(topic, query, callback);
  }

  /// Subscribe to changes for a specific record.
  UnsubscribeFunc subscribeToRecord(
    String collection,
    String recordId,
    void Function(RecordSubscriptionEvent event) callback, {
    String? fields,
    String? expand,
  }) {
    final topic = '$collection/$recordId';
    final query = <String, String>{
      'fields': fields ?? '',
      'expand': expand ?? '',
    };
    return _addRecordSubscription(topic, query, callback);
  }

  UnsubscribeFunc _addRecordSubscription(
    String topic,
    Map<String, String> query,
    void Function(RecordSubscriptionEvent event) callback,
  ) {
    final subId = '$topic-${_subSeq++}';

    _subscriptions[subId] = _Subscription(
      topic: topic,
      query: query,
      deliver: (msg) {
        final action = msg.event.replaceFirst('record:', '');
        final data = _parseData(msg.data);
        final record = data is Map<String, dynamic>
            ? (data['record'] is Map<String, dynamic>
                ? data['record'] as Map<String, dynamic>
                : data)
            : <String, dynamic>{};
        callback(RecordSubscriptionEvent(action: action, record: record));
      },
    );

    unawaited(connect());
    _sendSubscription('subscribe', [topic], query: query);

    return () async {
      _subscriptions.remove(subId);
      await _sendSubscription('unsubscribe', [topic]);
    };
  }

  /// Subscribe to an arbitrary realtime channel (broadcast/presence
  /// messaging).
  ///
  /// The callback receives every message published on the channel, including
  /// `presence:join` / `presence:leave` events (data: `{client_id, state}`).
  /// Pass [presence] to announce your own presence state on join.
  ///
  /// ```dart
  /// final unsub = client.realtime.subscribeToChannel('room:1', (msg) {
  ///   print('${msg.event}: ${msg.data}');
  /// }, presence: {'name': 'Ada'});
  /// ```
  UnsubscribeFunc subscribeToChannel(
    String channel,
    void Function(ChannelMessage msg) callback, {
    Map<String, dynamic>? presence,
  }) {
    final subId = 'channel:$channel-${_subSeq++}';
    final options =
        presence != null ? <String, dynamic>{'presence': presence} : null;

    _subscriptions[subId] = _Subscription(
      topic: channel,
      options: options,
      isChannel: true,
      deliver: (msg) {
        callback(ChannelMessage(
          event: msg.event,
          data: _parseData(msg.data),
          clientId: msg.clientId,
        ));
      },
    );

    unawaited(connect());
    _sendSubscription('subscribe', [channel], options: options);

    return () async {
      _subscriptions.remove(subId);
      await _sendSubscription('unsubscribe', [channel]);
    };
  }

  /// Publish a message to a channel via the REST broadcast endpoint.
  /// Requires an auth token.
  Future<void> broadcast(String channel, String event, [dynamic data]) async {
    await _http.send('POST', '/api/v1/realtime/broadcast',
        body: {'channel': channel, 'event': event, 'data': data});
  }

  /// Fetch a presence snapshot for a channel: connected client count and the
  /// presence state of each member.
  Future<PresenceInfo> presence(String channel) async {
    final payload = <String, dynamic>{
      'type': 'presence',
      'channel': channel,
      if (_clientId != null) 'clientId': _clientId,
    };

    final res = await _http.send('POST', '/api/v1/realtime', body: payload);

    // The response may be the presence object itself, or a realtime message
    // envelope with the presence object in `data`.
    dynamic info = res;
    if (info is Map<String, dynamic> &&
        info['data'] != null &&
        info['clients'] == null) {
      info = _parseData(info['data']);
    }

    return PresenceInfo.fromJson(
        info is Map<String, dynamic> ? info : const {});
  }

  Future<void> _sendSubscription(
    String type,
    List<String> topics, {
    Map<String, String>? query,
    Map<String, dynamic>? options,
  }) async {
    if (!_connected || _clientId == null) return;

    final payload = <String, dynamic>{
      'type': type,
      'clientId': _clientId,
      'subscriptions': topics,
      if (query != null) 'query': query,
      if (options != null) 'options': options,
    };

    try {
      await _http.send('POST', '/api/v1/realtime', body: payload);
    } catch (_) {
      // Fire-and-forget: subscription failures are retried on reconnect.
    }
  }

  void _resubscribeAll() {
    for (final sub in _subscriptions.values.toList()) {
      _sendSubscription('subscribe', [sub.topic],
          query: sub.query, options: sub.options);
    }
  }

  /// Release all resources.
  void dispose() {
    disconnect();
    _subscriptions.clear();
    _messages.close();
    _connectionEvents.close();
  }
}
