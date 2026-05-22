/// Realtime service with SSE support for Gresbase Dart SDK.
library gresbase_sdk_realtime;

import 'dart:async';
import 'dart:convert';
import 'models.dart';

/// Callback for realtime subscription events.
typedef RealtimeCallback = void Function(RealtimeEvent event);

/// Realtime event data.
class RealtimeEvent {
  final String action;
  final Map<String, dynamic>? record;
  final RealtimeMessage? raw;

  RealtimeEvent({required this.action, this.record, this.raw});
}

/// Unsubscribe function type.
typedef UnsubscribeFn = void Function();

/// Realtime service for subscribing to collection changes via SSE.
class RealtimeService {
  final String _baseUrl;
  final String Function()? _getToken;
  HttpClient? _sseClient;
  StreamSubscription? _sseSubscription;
  String? _clientId;
  Timer? _reconnectTimer;
  bool _connected = false;
  final int _reconnectDelay;

  final Map<String, _Subscription> _subscriptions = {};

  final StreamController<RealtimeEvent> _eventController =
      StreamController<RealtimeEvent>.broadcast();
  final StreamController<RealtimeMessage> _messageController =
      StreamController<RealtimeMessage>.broadcast();
  final StreamController<bool> _connectionController =
      StreamController<bool>.broadcast();

  RealtimeService(
    this._baseUrl,
    this._getToken, {
    int reconnectDelay = 3000,
  }) : _reconnectDelay = reconnectDelay;

  /// Whether the realtime client is connected.
  bool get isConnected => _connected;

  /// Current client identifier assigned by the server.
  String? get clientId => _clientId;

  /// Stream of record events (create, update, delete).
  Stream<RealtimeEvent> get events => _eventController.stream;

  /// Stream of raw realtime messages.
  Stream<RealtimeMessage> get messages => _messageController.stream;

  /// Stream of connection state changes.
  Stream<bool> get connectionState => _connectionController.stream;

  /// Connect to the realtime server via SSE.
  Future<void> connect() async {
    if (_connected) return;
    await _connectSSE();
  }

  Future<void> _connectSSE() async {
    try {
      final client = HttpClient();
      final uri = Uri.parse('$_baseUrl/api/v1/sse');
      final request = await client.getUrl(uri);
      request.headers.set('Accept', 'text/event-stream');
      request.headers.set('Cache-Control', 'no-store');

      final response = await request.close();

      if (response.statusCode != 200) {
        throw Exception('SSE connection failed: HTTP ${response.statusCode}');
      }

      _connected = true;
      _connectionController.add(true);

      _sseSubscription = response
          .transform(utf8.decoder)
          .transform(const LineSplitter())
          .listen(
        (line) {
          if (line.startsWith('data: ')) {
            final jsonStr = line.substring(6);
            try {
              final msg =
                  RealtimeMessage.fromJson(jsonDecode(jsonStr) as Map<String, dynamic>);
              _handleMessage(msg);
            } catch (_) {}
          }
        },
        onError: (error) {
          _connected = false;
          _connectionController.add(false);
          _scheduleReconnect();
        },
        onDone: () {
          _connected = false;
          _connectionController.add(false);
          _scheduleReconnect();
        },
        cancelOnError: false,
      );
    } catch (_) {
      _connected = false;
      _scheduleReconnect();
    }
  }

  /// Disconnect from the realtime server.
  void disconnect() {
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _sseSubscription?.cancel();
    _sseSubscription = null;
    _connected = false;
    _clientId = null;
    _connectionController.add(false);
  }

  /// Subscribe to record changes in a collection.
  ///
  /// Returns an unsubscribe function.
  UnsubscribeFn subscribe(
    String collection,
    RealtimeCallback callback, {
    String? filter,
    String? fields,
    String? expand,
  }) {
    final subId = '$collection-${DateTime.now().millisecondsSinceEpoch}';
    final topic = '$collection/*';

    _subscriptions[subId] = _Subscription(
      topic: topic,
      callback: callback,
      filter: filter,
      fields: fields,
      expand: expand,
    );

    _sendSubscription('subscribe', [topic], filter: filter, fields: fields, expand: expand);

    return () {
      _subscriptions.remove(subId);
      _sendSubscription('unsubscribe', [topic]);
    };
  }

  /// Subscribe to a specific record.
  UnsubscribeFn subscribeToRecord(
    String collection,
    String recordId,
    RealtimeCallback callback, {
    String? fields,
    String? expand,
  }) {
    final subId = '$collection/$recordId-${DateTime.now().millisecondsSinceEpoch}';
    final topic = '$collection/$recordId';

    _subscriptions[subId] = _Subscription(
      topic: topic,
      callback: callback,
      fields: fields,
      expand: expand,
    );

    _sendSubscription('subscribe', [topic], fields: fields, expand: expand);

    return () {
      _subscriptions.remove(subId);
      _sendSubscription('unsubscribe', [topic]);
    };
  }

  Future<void> _sendSubscription(
    String type,
    List<String> topics, {
    String? filter,
    String? fields,
    String? expand,
  }) async {
    if (_clientId == null) return;

    try {
      final client = HttpClient();
      final uri = Uri.parse('$_baseUrl/api/v1/realtime');
      final request = await client.postUrl(uri);
      request.headers.set('Content-Type', 'application/json');

      final query = <String, String>{};
      if (filter != null) query['filter'] = filter;
      if (fields != null) query['fields'] = fields;
      if (expand != null) query['expand'] = expand;

      final body = jsonEncode({
        'type': type,
        'clientId': _clientId,
        'subscriptions': topics,
        'query': query,
      });

      request.write(body);
      await request.close();
    } catch (_) {}
  }

  void _handleMessage(RealtimeMessage msg) {
    _messageController.add(msg);

    if (msg.event == 'connection:established' && msg.clientId != null) {
      _clientId = msg.clientId;
      _resubscribeAll();
      return;
    }

    if (msg.event?.startsWith('record:') == true) {
      final action = msg.event!.replaceFirst('record:', '');
      dynamic recordData = msg.data;
      if (recordData is String) {
        try {
          recordData = jsonDecode(recordData);
        } catch (_) {}
      }

      final event = RealtimeEvent(
        action: action,
        record: recordData is Map<String, dynamic>
            ? (recordData['record'] as Map<String, dynamic>? ?? recordData)
            : null,
        raw: msg,
      );

      _eventController.add(event);

      // Notify matching subscriptions
      for (final sub in _subscriptions.values) {
        sub.callback(event);
      }
    }
  }

  void _resubscribeAll() {
    for (final sub in _subscriptions.values) {
      _sendSubscription(
        'subscribe',
        [sub.topic],
        filter: sub.filter,
        fields: sub.fields,
        expand: sub.expand,
      );
    }
  }

  void _scheduleReconnect() {
    _reconnectTimer?.cancel();
    _reconnectTimer = Timer(Duration(milliseconds: _reconnectDelay), () {
      connect();
    });
  }

  /// Dispose of the service.
  void dispose() {
    disconnect();
    _eventController.close();
    _messageController.close();
    _connectionController.close();
  }
}

class _Subscription {
  final String topic;
  final RealtimeCallback callback;
  final String? filter;
  final String? fields;
  final String? expand;

  _Subscription({
    required this.topic,
    required this.callback,
    this.filter,
    this.fields,
    this.expand,
  });
}
