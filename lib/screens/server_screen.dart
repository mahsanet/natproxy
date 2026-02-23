import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../models/settings_model.dart';
import '../services/platform_bridge.dart';
import '../services/settings_service.dart';
import '../models/log_entry.dart';
import '../widgets/status_card.dart';
import '../widgets/connection_code.dart';
import '../widgets/vless_link.dart';
import '../widgets/log_panel.dart';

const _kLogPollInterval = Duration(milliseconds: 500);
const _kStatusPollInterval = Duration(seconds: 2);

class ServerScreen extends StatefulWidget {
  const ServerScreen({super.key});

  @override
  State<ServerScreen> createState() => _ServerScreenState();
}

class _ServerScreenState extends State<ServerScreen>
    with WidgetsBindingObserver {
  bool _isRunning = false;
  bool _isStarting = false;
  String _connectionCode = '';
  String _vlessLink = '';
  String _publicIP = '';
  String _natMethod = '';
  String _protocol = '';
  String _transport = '';
  int _clientCount = 0;
  int _totalPeers = 0;
  int _bytesUp = 0;
  int _bytesDown = 0;
  double _rateUp = 0;
  double _rateDown = 0;
  double _uptimeSec = 0;
  int _goroutines = 0;
  double _heapMB = 0;
  int _dataChannels = 0;
  int _peerConnections = 0;
  int _smuxStreams = 0;
  List<int> _streamDist = [];
  String? _error;
  String _listingId = '';
  String _natType = '';
  bool _detectingNat = false;
  ServerSettings _settings = const ServerSettings();
  final List<LogEntry> _logEntries = [];
  int _logCursor = 0;
  StreamSubscription<Map<String, dynamic>>? _statusSub;

  // Manual signaling state
  int _manualStep = 0; // 0=off, 1=show offer, 2=enter answer, 3=connecting
  String _offerCode = '';
  final _answerController = TextEditingController();
  DateTime? _offerCreatedAt;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _statusSub = PlatformBridge.statusStream.listen(_onPlatformEvent);
    _loadSettings();
    _syncState();
    _detectNatType();
    PlatformBridge.requestBatteryOptimization();
  }

  @override
  void dispose() {
    _statusSub?.cancel();
    _answerController.dispose();
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  void _onPlatformEvent(Map<String, dynamic> event) {
    if (event['event'] == 'stopped' && event['source'] == 'server' && mounted) {
      setState(() {
        _isRunning = false;
        _connectionCode = '';
        _vlessLink = '';
        _publicIP = '';
        _natMethod = '';
        _protocol = '';
        _transport = '';
        _clientCount = 0;
        _totalPeers = 0;
        _bytesUp = 0;
        _bytesDown = 0;
        _rateUp = 0;
        _rateDown = 0;
        _uptimeSec = 0;
        _goroutines = 0;
        _heapMB = 0;
        _dataChannels = 0;
        _peerConnections = 0;
        _smuxStreams = 0;
        _streamDist = [];
        _listingId = '';
        _manualStep = 0;
        _offerCode = '';
      });
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed && !_isStarting) {
      _syncState();
    }
  }

  Future<void> _loadSettings() async {
    final settings = await SettingsService().loadServerSettings();
    if (mounted) setState(() => _settings = settings);
  }

  Future<void> _syncState() async {
    try {
      final statusJson = await PlatformBridge.getServerStatus();
      final status = jsonDecode(statusJson) as Map<String, dynamic>;
      if (mounted) {
        final running = status['running'] as bool? ?? false;
        final isManual = status['manualMode'] as bool? ?? false;
        setState(() {
          _isRunning = running;
          if (running) {
            _publicIP = status['publicIP'] as String? ?? '';
            _clientCount = status['clients'] as int? ?? 0;
            _natMethod = status['upnp'] == true ? 'UPnP' : 'Hole Punch';
            _protocol = status['protocol'] as String? ?? '';
            _transport = status['transport'] as String? ?? '';
            final code = status['connectionCode'] as String? ?? '';
            if (code.isNotEmpty) _connectionCode = code;
            _vlessLink = status['vlessLink'] as String? ?? '';
            _totalPeers = status['totalPeers'] as int? ?? 0;
            _bytesUp = status['bytesUp'] as int? ?? 0;
            _bytesDown = status['bytesDown'] as int? ?? 0;
            _rateUp = (status['rateUp'] as num?)?.toDouble() ?? 0;
            _rateDown = (status['rateDown'] as num?)?.toDouble() ?? 0;
            _uptimeSec = (status['uptimeSec'] as num?)?.toDouble() ?? 0;
            _goroutines = status['goroutines'] as int? ?? 0;
            _heapMB = (status['heapMB'] as num?)?.toDouble() ?? 0;
            _dataChannels = status['dataChannels'] as int? ?? 0;
            _peerConnections = status['peerConnections'] as int? ?? 0;
            _smuxStreams = status['smuxStreams'] as int? ?? 0;
            _streamDist = (status['streamDist'] as List<dynamic>?)
                    ?.map((e) => (e as num).toInt())
                    .toList() ??
                [];
            // Restore manual mode state on resume
            if (isManual && _manualStep == 0) {
              final offer = status['offerCode'] as String? ?? '';
              if (offer.isNotEmpty) {
                _offerCode = offer;
                _manualStep = 1;
              }
            }
          }
        });
        if (running) {
          _pollStatus();
          _pollLogs();
        }
      }
    } catch (e) {
      debugPrint('ServerScreen: syncState error: $e');
    }
  }

  Future<void> _detectNatType() async {
    setState(() => _detectingNat = true);
    try {
      final result = await PlatformBridge.detectNatType();
      if (mounted) {
        setState(() {
          _natType = result;
          _detectingNat = false;
        });
      }
    } catch (e) {
      debugPrint('ServerScreen: detectNatType error: $e');
      if (mounted) setState(() => _detectingNat = false);
    }
  }

  String _formatNatType(String raw) {
    switch (raw) {
      case 'full_cone':
        return 'Full Cone';
      case 'restricted_cone':
        return 'Restricted Cone';
      case 'port_restricted':
        return 'Port Restricted';
      case 'symmetric':
        return 'Symmetric';
      case 'cone':
        return 'Cone';
      case 'unknown':
        return 'Unknown';
      default:
        return raw;
    }
  }

  Future<void> _startServer() async {
    setState(() {
      _isStarting = true;
      _error = null;
    });
    _pollLogs();
    try {
      final settingsJson = jsonEncode(_settings.toJson());
      final result = await PlatformBridge.startServer(settingsJson);
      if (!mounted) return;
      // Parse result: JSON {"code":"...", "vless":"..."} or raw base64 string
      String code = result;
      String vless = '';
      if (result.startsWith('{')) {
        try {
          final parsed = jsonDecode(result) as Map<String, dynamic>;
          code = parsed['code'] as String? ?? result;
          vless = parsed['vless'] as String? ?? '';
        } catch (_) {
          // Fallback: treat as raw connection code
        }
      }
      setState(() {
        _connectionCode = code;
        _vlessLink = vless;
        _isRunning = true;
        _isStarting = false;
      });
      _pollStatus();
      // Register on discovery if enabled
      if (_settings.discoveryEnabled && _settings.displayName.isNotEmpty) {
        try {
          final discoverySettings = jsonEncode({
            'signalingUrl': _settings.signalingUrl,
            'discoveryUrl': _settings.discoveryUrl,
            'displayName': _settings.displayName,
            'room': _settings.room,
          });
          final id = await PlatformBridge.registerDiscovery(
            code,
            discoverySettings,
          );
          if (mounted) setState(() => _listingId = id);
        } catch (e) {
          debugPrint('ServerScreen: discovery register failed: $e');
          if (mounted) {
            setState(
              () => _error = 'Server started but discovery registration failed',
            );
          }
        }
      }
    } catch (e, stack) {
      debugPrint('ServerScreen: startServer error: $e\n$stack');
      if (!mounted) return;
      setState(() {
        _error = e.toString();
        _isStarting = false;
      });
    }
  }

  // Manual signaling

  Future<void> _startServerManual() async {
    setState(() {
      _isStarting = true;
      _error = null;
      _manualStep = 0;
      _offerCode = '';
    });
    _pollLogs();
    try {
      final settingsJson = jsonEncode(_settings.toJson());
      final offerCode = await PlatformBridge.startServerManual(settingsJson);
      if (!mounted) return;
      setState(() {
        _offerCode = offerCode;
        _manualStep = 1;
        _isRunning = true;
        _isStarting = false;
        _offerCreatedAt = DateTime.now();
      });
      _pollStatus();
    } catch (e, stack) {
      debugPrint('ServerScreen: startServerManual error: $e\n$stack');
      if (!mounted) return;
      setState(() {
        _error = e.toString();
        _isStarting = false;
      });
    }
  }

  Future<void> _acceptManualAnswer() async {
    final answerCode = _answerController.text.trim();
    if (answerCode.isEmpty) {
      setState(() => _error = 'Please paste the answer code');
      return;
    }

    setState(() {
      _manualStep = 3;
      _error = null;
    });

    try {
      await PlatformBridge.acceptManualAnswer(answerCode);
      if (!mounted) return;
      setState(() {
        _manualStep = 0;
        _offerCode = '';
        _answerController.clear();
      });
    } catch (e, stack) {
      debugPrint('ServerScreen: acceptManualAnswer error: $e\n$stack');
      if (!mounted) return;
      setState(() {
        _error = e.toString();
        _manualStep = 2; // allow retry
      });
    }
  }

  Future<void> _stopServer() async {
    try {
      if (_listingId.isNotEmpty) {
        try {
          await PlatformBridge.unregisterDiscovery();
        } catch (e) {
          debugPrint('ServerScreen: discovery unregister failed: $e');
        }
      }
      await PlatformBridge.stop();
      setState(() {
        _isRunning = false;
        _connectionCode = '';
        _vlessLink = '';
        _publicIP = '';
        _natMethod = '';
        _protocol = '';
        _transport = '';
        _clientCount = 0;
        _totalPeers = 0;
        _bytesUp = 0;
        _bytesDown = 0;
        _rateUp = 0;
        _rateDown = 0;
        _uptimeSec = 0;
        _goroutines = 0;
        _heapMB = 0;
        _dataChannels = 0;
        _peerConnections = 0;
        _smuxStreams = 0;
        _streamDist = [];
        _listingId = '';
        _manualStep = 0;
        _offerCode = '';
      });
    } catch (e, stack) {
      debugPrint('ServerScreen: stopServer error: $e\n$stack');
      setState(() {
        _error = e.toString();
      });
    }
  }

  Future<void> _pollLogs() async {
    while ((_isStarting || _isRunning) && mounted) {
      await _fetchLogs();
      await Future.delayed(_kLogPollInterval);
    }
  }

  Future<void> _pollStatus() async {
    while (_isRunning && mounted) {
      try {
        final statusJson = await PlatformBridge.getServerStatus();
        final status = jsonDecode(statusJson) as Map<String, dynamic>;
        if (mounted && _isRunning) {
          setState(() {
            _publicIP = status['publicIP'] as String? ?? '';
            _clientCount = status['clients'] as int? ?? 0;
            _natMethod = status['upnp'] == true ? 'UPnP' : 'Hole Punch';
            _protocol = status['protocol'] as String? ?? '';
            _transport = status['transport'] as String? ?? '';
            _vlessLink = status['vlessLink'] as String? ?? '';
            _totalPeers = status['totalPeers'] as int? ?? 0;
            _bytesUp = status['bytesUp'] as int? ?? 0;
            _bytesDown = status['bytesDown'] as int? ?? 0;
            _rateUp = (status['rateUp'] as num?)?.toDouble() ?? 0;
            _rateDown = (status['rateDown'] as num?)?.toDouble() ?? 0;
            _uptimeSec = (status['uptimeSec'] as num?)?.toDouble() ?? 0;
            _goroutines = status['goroutines'] as int? ?? 0;
            _heapMB = (status['heapMB'] as num?)?.toDouble() ?? 0;
            _dataChannels = status['dataChannels'] as int? ?? 0;
            _peerConnections = status['peerConnections'] as int? ?? 0;
            _smuxStreams = status['smuxStreams'] as int? ?? 0;
            _streamDist = (status['streamDist'] as List<dynamic>?)
                    ?.map((e) => (e as num).toInt())
                    .toList() ??
                [];
          });
        }
      } catch (e) {
        debugPrint('ServerScreen: pollStatus error: $e');
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
      debugPrint('ServerScreen: fetchLogs error: $e');
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
      debugPrint('ServerScreen: clearLogs error: $e');
    }
  }

  String _formatBytes(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    if (bytes < 1024 * 1024 * 1024) {
      return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
    }
    return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(1)} GB';
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

  Widget _buildManualWizard() {
    switch (_manualStep) {
      case 1:
        // Show offer code
        return Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(
                  children: [
                    const Icon(Icons.qr_code, size: 20),
                    const SizedBox(width: 8),
                    const Text(
                      'Step 1: Share Offer Code',
                      style: TextStyle(
                        fontWeight: FontWeight.bold,
                        fontSize: 16,
                      ),
                    ),
                    const Spacer(),
                    if (_offerCreatedAt != null)
                      StreamBuilder(
                        stream: Stream.periodic(const Duration(seconds: 1)),
                        builder: (context, _) {
                          final elapsed = DateTime.now()
                              .difference(_offerCreatedAt!)
                              .inSeconds;
                          return Text(
                            '${elapsed}s ago',
                            style: TextStyle(
                              fontSize: 12,
                              color: elapsed > 120
                                  ? Colors.red
                                  : Colors.grey,
                            ),
                          );
                        },
                      ),
                  ],
                ),
                const SizedBox(height: 8),
                const Text(
                  'Send this code to the client device. '
                  'Complete the exchange within 2-3 minutes.',
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
                    _offerCode,
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
                            ClipboardData(text: _offerCode),
                          );
                          ScaffoldMessenger.of(context).showSnackBar(
                            const SnackBar(
                              content: Text('Offer code copied'),
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
                        onPressed: () =>
                            setState(() => _manualStep = 2),
                        icon: const Icon(Icons.arrow_forward, size: 18),
                        label: const Text('Next'),
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
        );

      case 2:
        // Enter answer code
        return Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const Row(
                  children: [
                    Icon(Icons.input, size: 20),
                    SizedBox(width: 8),
                    Text(
                      'Step 2: Enter Answer Code',
                      style: TextStyle(
                        fontWeight: FontWeight.bold,
                        fontSize: 16,
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 8),
                const Text(
                  'Paste the answer code from the client device.',
                  style: TextStyle(fontSize: 13),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: _answerController,
                  maxLines: 3,
                  decoration: const InputDecoration(
                    hintText: 'Paste M1A:... code here',
                    border: OutlineInputBorder(),
                    isDense: true,
                  ),
                  style: const TextStyle(
                    fontFamily: 'monospace',
                    fontSize: 12,
                  ),
                ),
                const SizedBox(height: 12),
                Row(
                  children: [
                    Expanded(
                      child: OutlinedButton(
                        onPressed: () =>
                            setState(() => _manualStep = 1),
                        child: const Text('Back'),
                      ),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: FilledButton.icon(
                        onPressed: _acceptManualAnswer,
                        icon: const Icon(Icons.check, size: 18),
                        label: const Text('Accept'),
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
        );

      case 3:
        // Connecting
        return const Card(
          child: Padding(
            padding: EdgeInsets.all(24),
            child: Column(
              children: [
                CircularProgressIndicator(),
                SizedBox(height: 16),
                Text(
                  'Establishing connection...',
                  style: TextStyle(fontSize: 16),
                ),
                SizedBox(height: 4),
                Text(
                  'Waiting for ICE negotiation',
                  style: TextStyle(fontSize: 13, color: Colors.grey),
                ),
              ],
            ),
          ),
        );

      default:
        return const SizedBox.shrink();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Share Internet'),
        backgroundColor: Theme.of(context).colorScheme.inversePrimary,
        actions: [
          IconButton(
            icon: const Icon(Icons.settings),
            onPressed: () async {
              await Navigator.pushNamed(context, '/server-settings');
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
                    icon: _isRunning ? Icons.cloud_upload : Icons.cloud_off,
                    title: _isRunning ? 'Server Running' : 'Server Stopped',
                    color: _isRunning ? Colors.green : Colors.grey,
                    children: [
                      if (_detectingNat)
                        const Row(
                          children: [
                            SizedBox(
                              width: 14,
                              height: 14,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            ),
                            SizedBox(width: 8),
                            Text('Detecting NAT type...'),
                          ],
                        )
                      else if (_natType.isNotEmpty)
                        Text('NAT Type: ${_formatNatType(_natType)}'),
                      if (_publicIP.isNotEmpty) Text('Public IP: $_publicIP'),
                      if (_natMethod.isNotEmpty)
                        Text('NAT Method: $_natMethod'),
                      if (_protocol.isNotEmpty)
                        Text(
                          'Protocol: ${_protocol.toUpperCase()}'
                          '${_transport.isNotEmpty ? ' / ${_transport.toUpperCase()}' : ''}',
                        ),
                      if (_isRunning) ...[
                        Text(
                          'Peers: $_clientCount active'
                          '${_totalPeers > 0 ? ' / $_totalPeers total' : ''}',
                        ),
                        Text(
                          'Upload: ${_formatBytes(_bytesUp)}'
                          '${_rateUp > 0 ? ' (${_formatRate(_rateUp)})' : ''}',
                        ),
                        Text(
                          'Download: ${_formatBytes(_bytesDown)}'
                          '${_rateDown > 0 ? ' (${_formatRate(_rateDown)})' : ''}',
                        ),
                        Text('Uptime: ${_formatUptime(_uptimeSec)}'),
                        Theme(
                          data: Theme.of(context).copyWith(
                            dividerColor: Colors.transparent,
                          ),
                          child: ExpansionTile(
                            tilePadding: EdgeInsets.zero,
                            title: const Text(
                              'Resource Details',
                              style: TextStyle(fontSize: 13),
                            ),
                            children: [
                              Text('Goroutines: $_goroutines'),
                              Text(
                                'Heap: ${_heapMB.toStringAsFixed(1)} MB',
                              ),
                              Text('PeerConnections: $_peerConnections'),
                              Text('Data Channels: $_dataChannels'),
                              Text('Smux Streams: $_smuxStreams'),
                              if (_streamDist.isNotEmpty)
                                Text('Stream Dist: $_streamDist'),
                            ],
                          ),
                        ),
                      ],
                      if (_listingId.isNotEmpty && _isRunning)
                        const Text(
                          'Listed on discovery',
                          style: TextStyle(color: Colors.blue),
                        ),
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
                  if (_manualStep > 0) ...[
                    const SizedBox(height: 16),
                    _buildManualWizard(),
                  ],
                  if (_connectionCode.isNotEmpty && _manualStep == 0) ...[
                    const SizedBox(height: 16),
                    ConnectionCodeDisplay(code: _connectionCode),
                  ],
                  if (_vlessLink.isNotEmpty && _manualStep == 0) ...[
                    const SizedBox(height: 12),
                    VLESSLinkDisplay(link: _vlessLink),
                  ],
                  const SizedBox(height: 24),
                  if (!_isRunning && _manualStep == 0) ...[
                    SizedBox(
                      height: 56,
                      child: FilledButton.icon(
                        onPressed: _isStarting ? null : _startServer,
                        icon: _isStarting
                            ? const SizedBox(
                                width: 20,
                                height: 20,
                                child: CircularProgressIndicator(
                                  strokeWidth: 2,
                                  color: Colors.white,
                                ),
                              )
                            : const Icon(Icons.play_arrow),
                        label: Text(
                          _isStarting ? 'Starting...' : 'Start Sharing',
                        ),
                      ),
                    ),
                    const SizedBox(height: 12),
                    SizedBox(
                      height: 48,
                      child: OutlinedButton.icon(
                        onPressed: _isStarting ? null : _startServerManual,
                        icon: const Icon(Icons.swap_horiz, size: 20),
                        label: const Text('Manual Exchange'),
                      ),
                    ),
                  ] else if (_isRunning) ...[
                    SizedBox(
                      height: 56,
                      child: FilledButton.icon(
                        onPressed: _stopServer,
                        icon: const Icon(Icons.stop),
                        label: const Text('Stop Sharing'),
                      ),
                    ),
                  ],
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
