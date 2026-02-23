import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter/foundation.dart';
import '../models/server_listing.dart';

enum DiscoveryStreamState { disconnected, connecting, connected, error }

class DiscoveryStreamService {
  static const _maxBackoff = Duration(seconds: 30);
  static const _initialBackoff = Duration(seconds: 1);

  final _serversController = StreamController<List<ServerListing>>.broadcast();
  final _stateController = StreamController<DiscoveryStreamState>.broadcast();

  Stream<List<ServerListing>> get servers => _serversController.stream;
  Stream<DiscoveryStreamState> get state => _stateController.stream;
  DiscoveryStreamState get currentState => _currentState;

  DiscoveryStreamState _currentState = DiscoveryStreamState.disconnected;
  HttpClient? _httpClient;
  StreamSubscription<List<int>>? _streamSub;
  Timer? _reconnectTimer;
  Duration _backoff = _initialBackoff;
  bool _permanentlyClosed = false;
  String _signalingUrl = '';
  String _room = '';

  void connect(String signalingUrl, {String room = ''}) {
    _signalingUrl = signalingUrl;
    _room = room;
    _permanentlyClosed = false;
    _backoff = _initialBackoff;
    _doConnect();
  }

  void _doConnect() {
    if (_permanentlyClosed) return;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;

    _setState(DiscoveryStreamState.connecting);

    _httpClient?.close(force: true);
    _httpClient = HttpClient();
    _httpClient!.connectionTimeout = const Duration(seconds: 10);

    final uri = _buildUri();
    _httpClient!
        .getUrl(uri)
        .then((request) {
          request.headers.set('Accept', 'text/event-stream');
          return request.close();
        })
        .then((response) {
          if (_permanentlyClosed) {
            response.drain<void>();
            return;
          }
          if (response.statusCode != 200) {
            debugPrint('DiscoveryStream: HTTP ${response.statusCode}');
            response.drain<void>();
            _setState(DiscoveryStreamState.error);
            _scheduleReconnect();
            return;
          }

          _setState(DiscoveryStreamState.connected);
          _backoff = _initialBackoff;

          final buffer = StringBuffer();
          _streamSub = response.listen(
            (bytes) {
              buffer.write(utf8.decode(bytes));
              _processBuffer(buffer);
            },
            onError: (Object error) {
              debugPrint('DiscoveryStream: stream error: $error');
              _setState(DiscoveryStreamState.error);
              _scheduleReconnect();
            },
            onDone: () {
              if (!_permanentlyClosed) {
                debugPrint('DiscoveryStream: stream closed by server');
                _setState(DiscoveryStreamState.disconnected);
                _scheduleReconnect();
              }
            },
            cancelOnError: true,
          );
        })
        .catchError((Object error) {
          debugPrint('DiscoveryStream: connect error: $error');
          _setState(DiscoveryStreamState.error);
          _scheduleReconnect();
        });
  }

  Uri _buildUri() {
    var base = _signalingUrl.trimRight();
    if (base.endsWith('/')) base = base.substring(0, base.length - 1);
    final path = '$base/discovery/stream';
    final uri = Uri.parse(path);
    if (_room.isNotEmpty) {
      return uri.replace(queryParameters: {'room': _room});
    }
    return uri;
  }

  void _processBuffer(StringBuffer buffer) {
    final content = buffer.toString();
    // SSE events are delimited by double newline.
    final parts = content.split('\n\n');
    if (parts.length < 2) return; // No complete event yet.

    // Process all complete events, keep the incomplete tail.
    for (var i = 0; i < parts.length - 1; i++) {
      _parseSSEEvent(parts[i]);
    }
    buffer.clear();
    buffer.write(parts.last);
  }

  void _parseSSEEvent(String raw) {
    String? eventType;
    final dataLines = <String>[];

    for (final line in raw.split('\n')) {
      if (line.startsWith(':')) continue; // SSE comment (keepalive)
      if (line.startsWith('event:')) {
        eventType = line.substring(6).trim();
      } else if (line.startsWith('data:')) {
        dataLines.add(line.substring(5).trim());
      }
    }

    if (eventType == 'servers' && dataLines.isNotEmpty) {
      final jsonStr = dataLines.join('\n');
      try {
        final list = (jsonDecode(jsonStr) as List<dynamic>)
            .map((e) => ServerListing.fromJson(e as Map<String, dynamic>))
            .toList();
        _serversController.add(list);
      } catch (e) {
        debugPrint('DiscoveryStream: parse error: $e');
      }
    }
  }

  void _scheduleReconnect() {
    if (_permanentlyClosed) return;
    _streamSub?.cancel();
    _streamSub = null;

    debugPrint('DiscoveryStream: reconnecting in ${_backoff.inSeconds}s');
    _reconnectTimer = Timer(_backoff, _doConnect);
    _backoff = Duration(
      milliseconds: min(
        _backoff.inMilliseconds * 2,
        _maxBackoff.inMilliseconds,
      ),
    );
  }

  void _setState(DiscoveryStreamState s) {
    if (_currentState == s) return;
    _currentState = s;
    _stateController.add(s);
  }

  void disconnect({bool permanent = false}) {
    _permanentlyClosed = permanent;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _streamSub?.cancel();
    _streamSub = null;
    _httpClient?.close(force: true);
    _httpClient = null;
    _setState(DiscoveryStreamState.disconnected);
  }

  void dispose() {
    disconnect(permanent: true);
    _serversController.close();
    _stateController.close();
  }
}
