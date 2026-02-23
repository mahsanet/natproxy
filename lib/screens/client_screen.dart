import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../models/settings_model.dart';
import '../models/server_listing.dart';
import '../services/platform_bridge.dart';
import '../services/settings_service.dart';
import '../services/discovery_stream_service.dart';
import '../models/log_entry.dart';
import '../widgets/status_card.dart';
import '../widgets/connection_code.dart';
import '../widgets/log_panel.dart';
import '../widgets/server_list.dart';

const _kLogPollInterval = Duration(milliseconds: 500);
const _kStatusPollInterval = Duration(seconds: 1);
const _kLatencyGoodMs = 100;
const _kLatencyWarningMs = 300;

class ClientScreen extends StatefulWidget {
  const ClientScreen({super.key});

  @override
  State<ClientScreen> createState() => _ClientScreenState();
}

class _ClientScreenState extends State<ClientScreen>
    with WidgetsBindingObserver {
  final _codeController = TextEditingController();
  bool _isConnected = false;
  bool _isConnecting = false;
  int _bytesUp = 0;
  int _bytesDown = 0;
  double _rateUp = 0;
  double _rateDown = 0;
  double _uptimeSec = 0;
  int _smuxStreams = 0;
  int _dataChannels = 0;
  int _peerConnections = 0;
  String? _error;
  ClientSettings _settings = const ClientSettings();
  final List<LogEntry> _logEntries = [];
  int _logCursor = 0;
  int? _latencyMs;
  bool _isTesting = false;
  String? _latencyError;
  List<ServerListing> _availableServers = [];
  bool _isLoadingServers = false;
  String? _discoveryError;
  DiscoveryStreamService? _discoveryStream;
  StreamSubscription<List<ServerListing>>? _discoveryServersSub;
  StreamSubscription<DiscoveryStreamState>? _discoveryStateSub;
  DiscoveryStreamState _discoveryState = DiscoveryStreamState.disconnected;
  StreamSubscription<Map<String, dynamic>>? _statusSub;

  // Manual signaling state
  bool _isManualMode = false;
  String _answerCode = '';
  int _manualClientStep = 0; // 0=none, 1=show answer, 2=waiting for server

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _statusSub = PlatformBridge.statusStream.listen(_onPlatformEvent);
    _loadSettings();
    _syncState();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed && !_isConnecting) {
      _syncState();
    }
  }

  void _onPlatformEvent(Map<String, dynamic> event) {
    if (event['event'] == 'stopped' && event['source'] == 'client' && mounted) {
      setState(() {
        _isConnected = false;
        _bytesUp = 0;
        _bytesDown = 0;
        _rateUp = 0;
        _rateDown = 0;
        _uptimeSec = 0;
        _smuxStreams = 0;
        _dataChannels = 0;
        _peerConnections = 0;
        _latencyMs = null;
        _latencyError = null;
        _isTesting = false;
      });
      if (_settings.discoveryEnabled) {
        _connectDiscoveryStream();
      }
    }
  }

  Future<void> _loadSettings() async {
    final settings = await SettingsService().loadClientSettings();
    if (mounted) {
      setState(() => _settings = settings);
      if (settings.discoveryEnabled) {
        _connectDiscoveryStream();
      } else {
        _disconnectDiscoveryStream();
      }
    }
  }

  void _connectDiscoveryStream() {
    _disconnectDiscoveryStream();
    final service = DiscoveryStreamService();
    _discoveryStream = service;
    _discoveryServersSub = service.servers.listen((list) {
      if (mounted) {
        setState(() {
          _availableServers = list;
          _isLoadingServers = false;
          _discoveryError = null;
        });
      }
    });
    _discoveryStateSub = service.state.listen((state) {
      if (mounted) {
        setState(() {
          _discoveryState = state;
          if (state == DiscoveryStreamState.connecting) {
            _isLoadingServers = true;
          } else if (state == DiscoveryStreamState.error) {
            _isLoadingServers = false;
            _discoveryError = 'Connection lost, retrying...';
          }
        });
      }
    });
    service.connect(_settings.discoveryUrl, room: _settings.roomFilter);
  }

  void _disconnectDiscoveryStream() {
    _discoveryServersSub?.cancel();
    _discoveryServersSub = null;
    _discoveryStateSub?.cancel();
    _discoveryStateSub = null;
    _discoveryStream?.dispose();
    _discoveryStream = null;
    _discoveryState = DiscoveryStreamState.disconnected;
  }

  Future<void> _syncState() async {
    try {
      final statusJson = await PlatformBridge.getClientStatus();
      final status = jsonDecode(statusJson) as Map<String, dynamic>;
      // Discard stale result if user started/stopped connecting since we queried
      if (mounted && !_isConnecting) {
        final connected = status['connected'] as bool? ?? false;
        setState(() {
          _isConnected = connected;
          if (connected) {
            _bytesUp = status['bytesUp'] as int? ?? 0;
            _bytesDown = status['bytesDown'] as int? ?? 0;
            _rateUp = (status['rateUp'] as num?)?.toDouble() ?? 0;
            _rateDown = (status['rateDown'] as num?)?.toDouble() ?? 0;
            _uptimeSec = (status['uptimeSec'] as num?)?.toDouble() ?? 0;
            _smuxStreams = status['smuxStreams'] as int? ?? 0;
            _dataChannels = status['dataChannels'] as int? ?? 0;
            _peerConnections = status['peerConnections'] as int? ?? 0;
          }
        });
        if (connected) {
          _pollStatus();
          _pollLogs();
        }
      }
    } catch (e) {
      debugPrint('ClientScreen: syncState error: $e');
    }
  }

  Future<void> _toggleConnection() async {
    if (_isConnected) {
      await _disconnect();
    } else {
      await _connect();
    }
  }

  Future<void> _connect() async {
    final code = _codeController.text.trim();
    if (code.isEmpty) {
      debugPrint('ClientScreen: empty connection code');
      setState(() {
        _error = 'Please enter a connection code';
      });
      return;
    }

    // Auto-detect manual offer codes
    if (code.startsWith('M1:')) {
      return _connectManual(code);
    }

    setState(() {
      _isConnecting = true;
      _error = null;
    });
    // Stop SSE stream early so it cannot fire setState during connection
    // (the VPN starting can break the SSE socket and trigger error/reconnect).
    _disconnectDiscoveryStream();
    _pollLogs();
    try {
      // Request VPN permission upfront (needed for phase 2)
      final hasPermission = await PlatformBridge.requestVpnPermission();
      if (!mounted) return;
      if (!hasPermission) {
        debugPrint('ClientScreen: VPN permission denied');
        setState(() {
          _error = 'VPN permission required';
          _isConnecting = false;
        });
        if (_settings.discoveryEnabled) _connectDiscoveryStream();
        return;
      }

      final settingsJson = jsonEncode(_settings.toJson());

      // Detect connection method from the code to choose the right flow.
      final method = _connectionMethod(code);

      if (method == 'holepunch') {
        // Two-phase: connect WebRTC before the TUN comes up.
        await PlatformBridge.connectWebRTC(code, settingsJson);
        if (!mounted || !_isConnecting) return;

        // Then start VPN service to protect sockets and create TUN.
        await PlatformBridge.startVpn(settingsJson);
        if (!mounted || !_isConnecting) return;
      } else {
        // UPnP / direct: VPN service handles the full connection.
        await PlatformBridge.startClient(code, settingsJson);
        if (!mounted || !_isConnecting) return;
      }

      setState(() {
        _isConnected = true;
      });
      // Delay clearing _isConnecting so that the lifecycle resumed
      // callback (which fires after the VPN permission dialog closes)
      // still sees _isConnecting == true and skips _syncState().
      Future.delayed(const Duration(milliseconds: 500), () {
        if (mounted) setState(() => _isConnecting = false);
      });
      _pollStatus();
    } catch (e, stack) {
      debugPrint('ClientScreen: connect error: $e\n$stack');
      if (!mounted) return;
      setState(() {
        _error = e.toString();
        _isConnecting = false;
      });
      if (_settings.discoveryEnabled) _connectDiscoveryStream();
    }
  }

  /// Decode the connection code and return the method field ("holepunch", "upnp", etc.).
  /// Falls back to "holepunch" if the code cannot be decoded.
  String _connectionMethod(String code) {
    try {
      final json = jsonDecode(utf8.decode(base64Decode(code)));
      return (json['method'] as String?) ?? 'holepunch';
    } catch (_) {
      return 'holepunch';
    }
  }

  Future<void> _connectManual(String offerCode) async {
    setState(() {
      _isConnecting = true;
      _isManualMode = true;
      _manualClientStep = 0;
      _answerCode = '';
      _error = null;
    });
    _disconnectDiscoveryStream();
    _pollLogs();

    try {
      // Request VPN permission upfront
      final hasPermission = await PlatformBridge.requestVpnPermission();
      if (!mounted) return;
      if (!hasPermission) {
        setState(() {
          _error = 'VPN permission required';
          _isConnecting = false;
          _isManualMode = false;
        });
        if (_settings.discoveryEnabled) _connectDiscoveryStream();
        return;
      }

      final settingsJson = jsonEncode(_settings.toJson());

      // Process offer and get answer code
      final answer = await PlatformBridge.processManualOffer(
        offerCode,
        settingsJson,
      );
      if (!mounted || !_isConnecting) return;

      setState(() {
        _answerCode = answer;
        _manualClientStep = 1;
      });
    } catch (e, stack) {
      debugPrint('ClientScreen: connectManual error: $e\n$stack');
      if (!mounted) return;
      setState(() {
        _error = e.toString();
        _isConnecting = false;
        _isManualMode = false;
      });
      if (_settings.discoveryEnabled) _connectDiscoveryStream();
    }
  }

  Future<void> _continueManualConnection() async {
    setState(() {
      _manualClientStep = 2;
      _error = null;
    });

    try {
      // Wait for server to accept our answer (blocks until ICE connects)
      await PlatformBridge.waitManualConnection(120);
      if (!mounted || !_isConnecting) return;

      final settingsJson = jsonEncode(_settings.toJson());

      // Start VPN tunnel
      await PlatformBridge.startVpn(settingsJson);
      if (!mounted || !_isConnecting) return;

      setState(() {
        _isConnected = true;
        _isManualMode = false;
        _manualClientStep = 0;
        _answerCode = '';
      });
      Future.delayed(const Duration(milliseconds: 500), () {
        if (mounted) setState(() => _isConnecting = false);
      });
      _pollStatus();
    } catch (e, stack) {
      debugPrint('ClientScreen: continueManual error: $e\n$stack');
      if (!mounted) return;
      setState(() {
        _error = e.toString();
        _isConnecting = false;
        _isManualMode = false;
        _manualClientStep = 0;
        _answerCode = '';
      });
      // Clean up pending connection
      try {
        await PlatformBridge.stop();
      } catch (_) {}
      if (mounted && _settings.discoveryEnabled) _connectDiscoveryStream();
    }
  }

  Future<void> _cancelConnect() async {
    setState(() {
      _isConnecting = false;
      _isManualMode = false;
      _manualClientStep = 0;
      _answerCode = '';
      _error = null;
    });
    try {
      await PlatformBridge.stop();
    } catch (_) {}
    if (mounted && _settings.discoveryEnabled) {
      _connectDiscoveryStream();
    }
  }

  Future<void> _disconnect() async {
    try {
      await PlatformBridge.stop();
      setState(() {
        _isConnected = false;
        _bytesUp = 0;
        _bytesDown = 0;
        _latencyMs = null;
        _latencyError = null;
        _isTesting = false;
      });
      if (_settings.discoveryEnabled) {
        _connectDiscoveryStream();
      }
    } catch (e, stack) {
      debugPrint('ClientScreen: disconnect error: $e\n$stack');
      setState(() {
        _error = e.toString();
      });
    }
  }

  Future<void> _pollLogs() async {
    while ((_isConnecting || _isConnected) && mounted) {
      await _fetchLogs();
      await Future.delayed(_kLogPollInterval);
    }
  }

  Future<void> _pollStatus() async {
    while (_isConnected && mounted) {
      try {
        final statusJson = await PlatformBridge.getClientStatus();
        final status = jsonDecode(statusJson) as Map<String, dynamic>;
        if (mounted && _isConnected) {
          setState(() {
            _bytesUp = status['bytesUp'] as int? ?? 0;
            _bytesDown = status['bytesDown'] as int? ?? 0;
            _rateUp = (status['rateUp'] as num?)?.toDouble() ?? 0;
            _rateDown = (status['rateDown'] as num?)?.toDouble() ?? 0;
            _uptimeSec = (status['uptimeSec'] as num?)?.toDouble() ?? 0;
            _smuxStreams = status['smuxStreams'] as int? ?? 0;
            _dataChannels = status['dataChannels'] as int? ?? 0;
            _peerConnections = status['peerConnections'] as int? ?? 0;
          });
        }
      } catch (e) {
        debugPrint('ClientScreen: pollStatus error: $e');
      }
      await Future.delayed(_kStatusPollInterval);
    }
  }

  Future<void> _fetchLogs() async {
    try {
      final logsJson = await PlatformBridge.getLogs(_logCursor);
      final data = jsonDecode(logsJson) as Map<String, dynamic>;
      final entries =
          (data['e'] as List<dynamic>?)
              ?.map((e) => LogEntry.fromJson(e as Map<String, dynamic>))
              .toList() ??
          [];
      if (entries.isNotEmpty && mounted) {
        setState(() {
          _logEntries.addAll(entries);
          _logCursor = data['c'] as int? ?? _logCursor;
        });
      }
    } catch (e) {
      debugPrint('ClientScreen: fetchLogs error: $e');
    }
  }

  Future<void> _clearLogs() async {
    try {
      await PlatformBridge.clearLogs();
      setState(() {
        _logEntries.clear();
        _logCursor = 0;
      });
    } catch (e) {
      debugPrint('ClientScreen: clearLogs error: $e');
    }
  }

  Future<void> _testLatency() async {
    setState(() {
      _isTesting = true;
      _latencyError = null;
    });
    try {
      final resultJson = await PlatformBridge.testLatency();
      final data = jsonDecode(resultJson) as Map<String, dynamic>;
      if (mounted) {
        if (data.containsKey('error')) {
          setState(() {
            _latencyMs = null;
            _latencyError = data['error'] as String;
            _isTesting = false;
          });
        } else {
          setState(() {
            _latencyMs = (data['latency_ms'] as num?)?.toInt();
            _latencyError = null;
            _isTesting = false;
          });
        }
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _latencyMs = null;
          _latencyError = e.toString();
          _isTesting = false;
        });
      }
    }
  }

  Color _latencyColor(int ms) {
    if (ms < _kLatencyGoodMs) return Colors.green;
    if (ms <= _kLatencyWarningMs) return Colors.orange;
    return Colors.red;
  }

  String _formatRate(double bytesPerSec) {
    if (bytesPerSec < 1024) return '${bytesPerSec.toStringAsFixed(0)} B/s';
    if (bytesPerSec < 1024 * 1024) {
      return '${(bytesPerSec / 1024).toStringAsFixed(1)} KB/s';
    }
    return '${(bytesPerSec / (1024 * 1024)).toStringAsFixed(1)} MB/s';
  }

  String _formatUptime(double seconds) {
    final dur = Duration(seconds: seconds.toInt());
    if (dur.inHours > 0) {
      return '${dur.inHours}h ${dur.inMinutes.remainder(60)}m';
    }
    if (dur.inMinutes > 0) {
      return '${dur.inMinutes}m ${dur.inSeconds.remainder(60)}s';
    }
    return '${dur.inSeconds}s';
  }

  String _formatBytes(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    if (bytes < 1024 * 1024 * 1024) {
      return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
    }
    return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(1)} GB';
  }

  @override
  void dispose() {
    _disconnectDiscoveryStream();
    _statusSub?.cancel();
    WidgetsBinding.instance.removeObserver(this);
    _codeController.dispose();
    super.dispose();
  }

  Widget _buildManualClientUI() {
    if (_manualClientStep == 1) {
      // Show answer code for user to share back to server
      return Card(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Row(
                children: [
                  Icon(Icons.reply, size: 20),
                  SizedBox(width: 8),
                  Text(
                    'Share Answer Code',
                    style: TextStyle(
                      fontWeight: FontWeight.bold,
                      fontSize: 16,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 8),
              const Text(
                'Send this answer code back to the server device, '
                'then tap Continue.',
                style: TextStyle(fontSize: 13),
              ),
              const SizedBox(height: 12),
              Container(
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: Theme.of(context)
                      .colorScheme
                      .surfaceContainerHighest,
                  borderRadius: BorderRadius.circular(8),
                ),
                child: SelectableText(
                  _answerCode,
                  style: const TextStyle(
                    fontFamily: 'monospace',
                    fontSize: 11,
                  ),
                ),
              ),
              const SizedBox(height: 12),
              Row(
                children: [
                  Expanded(
                    child: OutlinedButton.icon(
                      onPressed: () {
                        Clipboard.setData(
                          ClipboardData(text: _answerCode),
                        );
                        ScaffoldMessenger.of(context).showSnackBar(
                          const SnackBar(
                            content: Text('Answer code copied'),
                            duration: Duration(seconds: 1),
                          ),
                        );
                      },
                      icon: const Icon(Icons.copy, size: 18),
                      label: const Text('Copy'),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: FilledButton.icon(
                      onPressed: _continueManualConnection,
                      icon: const Icon(Icons.arrow_forward, size: 18),
                      label: const Text('Continue'),
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
      );
    } else if (_manualClientStep == 2) {
      // Waiting for server to accept
      return const Card(
        child: Padding(
          padding: EdgeInsets.all(24),
          child: Column(
            children: [
              CircularProgressIndicator(),
              SizedBox(height: 16),
              Text(
                'Waiting for server...',
                style: TextStyle(fontSize: 16),
              ),
              SizedBox(height: 4),
              Text(
                'The server needs to enter your answer code',
                style: TextStyle(fontSize: 13, color: Colors.grey),
              ),
            ],
          ),
        ),
      );
    }
    return const SizedBox.shrink();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Connect'),
        backgroundColor: Theme.of(context).colorScheme.inversePrimary,
        actions: [
          IconButton(
            icon: const Icon(Icons.settings),
            onPressed: () async {
              await Navigator.pushNamed(context, '/client-settings');
              _loadSettings();
            },
          ),
        ],
      ),
      body: Column(
        children: [
          Expanded(
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  StatusCard(
                    icon: _isConnected ? Icons.vpn_key : Icons.vpn_key_off,
                    title: _isConnected ? 'Connected' : 'Disconnected',
                    color: _isConnected ? Colors.green : Colors.grey,
                    children: [
                      if (_isConnected) ...[
                        Text(
                          'Upload: ${_formatBytes(_bytesUp)}'
                          '${_rateUp > 0 ? ' (${_formatRate(_rateUp)})' : ''}',
                        ),
                        Text(
                          'Download: ${_formatBytes(_bytesDown)}'
                          '${_rateDown > 0 ? ' (${_formatRate(_rateDown)})' : ''}',
                        ),
                        Text('Uptime: ${_formatUptime(_uptimeSec)}'),
                        if (_peerConnections > 0 || _dataChannels > 0 || _smuxStreams > 0)
                          Text(
                            'PCs: $_peerConnections, Channels: $_dataChannels, Streams: $_smuxStreams',
                          ),
                        const SizedBox(height: 4),
                        Row(
                          children: [
                            if (_isTesting)
                              const SizedBox(
                                width: 16,
                                height: 16,
                                child: CircularProgressIndicator(
                                  strokeWidth: 2,
                                ),
                              )
                            else if (_latencyMs != null)
                              Text(
                                'Delay: $_latencyMs ms',
                                style: TextStyle(
                                  color: _latencyColor(_latencyMs!),
                                  fontWeight: FontWeight.w600,
                                ),
                              )
                            else if (_latencyError != null)
                              Flexible(
                                child: Text(
                                  'Delay: $_latencyError',
                                  style: const TextStyle(color: Colors.red),
                                  overflow: TextOverflow.ellipsis,
                                ),
                              )
                            else
                              const Text('Delay: --'),
                            const Spacer(),
                            SizedBox(
                              width: 32,
                              height: 32,
                              child: IconButton(
                                padding: EdgeInsets.zero,
                                iconSize: 20,
                                onPressed: _isTesting ? null : _testLatency,
                                icon: const Icon(Icons.speed),
                                tooltip: 'Test delay',
                              ),
                            ),
                          ],
                        ),
                      ],
                    ],
                  ),
                  if (_error != null) ...[
                    const SizedBox(height: 16),
                    Card(
                      color: Theme.of(context).colorScheme.errorContainer,
                      child: Padding(
                        padding: const EdgeInsets.all(16),
                        child: Text(
                          _error!,
                          style: TextStyle(
                            color: Theme.of(
                              context,
                            ).colorScheme.onErrorContainer,
                          ),
                        ),
                      ),
                    ),
                  ],
                  if (_isManualMode && _manualClientStep > 0) ...[
                    const SizedBox(height: 16),
                    _buildManualClientUI(),
                  ],
                  if (!_isConnected && !_isManualMode) ...[
                    if (_settings.discoveryEnabled) ...[
                      const SizedBox(height: 16),
                      ServerListCard(
                        servers: _availableServers,
                        isLoading: _isLoadingServers,
                        error: _discoveryError,
                        isLive:
                            _discoveryState == DiscoveryStreamState.connected,
                        onRefresh: _connectDiscoveryStream,
                        onServerTap: (server) {
                          _codeController.text = server.code;
                          _connect();
                        },
                      ),
                      const SizedBox(height: 16),
                      ExpansionTile(
                        title: const Text('Or enter code manually'),
                        tilePadding: EdgeInsets.zero,
                        children: [
                          ConnectionCodeInput(controller: _codeController),
                        ],
                      ),
                    ] else ...[
                      const SizedBox(height: 16),
                      ConnectionCodeInput(controller: _codeController),
                    ],
                  ],
                  const SizedBox(height: 24),
                  SizedBox(
                    height: 56,
                    child: FilledButton.icon(
                      onPressed:
                          _isConnecting
                              ? _cancelConnect
                              : _toggleConnection,
                      icon: _isConnecting
                          ? const SizedBox(
                              width: 20,
                              height: 20,
                              child: CircularProgressIndicator(
                                strokeWidth: 2,
                                color: Colors.white,
                              ),
                            )
                          : Icon(_isConnected ? Icons.link_off : Icons.link),
                      label: Text(
                        _isConnecting
                            ? 'Cancel'
                            : _isConnected
                            ? 'Disconnect'
                            : 'Connect',
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
          LogPanel(entries: _logEntries, onClear: _clearLogs),
        ],
      ),
    );
  }
}
