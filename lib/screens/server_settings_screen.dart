import 'dart:math';
import 'package:flutter/material.dart';
import '../models/settings_model.dart';
import '../services/settings_service.dart';
import '../services/name_generator.dart';

class ServerSettingsScreen extends StatefulWidget {
  const ServerSettingsScreen({super.key});

  @override
  State<ServerSettingsScreen> createState() => _ServerSettingsScreenState();
}

class _ServerSettingsScreenState extends State<ServerSettingsScreen> {
  final _formKey = GlobalKey<FormState>();
  final _settingsService = SettingsService();

  late TextEditingController _portController;
  late TextEditingController _stunController;
  late TextEditingController _signalingController;
  late TextEditingController _discoveryUrlController;
  late TextEditingController _displayNameController;
  late TextEditingController _roomController;
  late TextEditingController _socksUsernameController;
  late TextEditingController _socksPasswordController;
  late TextEditingController _kcpMtuController;
  late TextEditingController _kcpTtiController;
  late TextEditingController _kcpUplinkController;
  late TextEditingController _kcpDownlinkController;
  late TextEditingController _kcpReadBufferController;
  late TextEditingController _kcpWriteBufferController;
  late TextEditingController _xhttpPathController;
  late TextEditingController _xhttpHostController;
  late TextEditingController _finalMaskPasswordController;
  late TextEditingController _finalMaskDomainController;
  late TextEditingController _upnpLeaseDurationController;
  late TextEditingController _upnpRetriesController;
  late TextEditingController _ssdpTimeoutController;
  late TextEditingController _rateLimitUpController;
  late TextEditingController _rateLimitDownController;
  late TextEditingController _numPeerConnectionsController;
  late TextEditingController _numChannelsController;
  late TextEditingController _smuxStreamBufferController;
  late TextEditingController _smuxSessionBufferController;
  late TextEditingController _smuxFrameSizeController;
  late TextEditingController _smuxKeepAliveController;
  late TextEditingController _smuxKeepAliveTimeoutController;
  late TextEditingController _dcMaxBufferedController;
  late TextEditingController _dcLowMarkController;
  late TextEditingController _paddingMaxController;
  late TextEditingController _sctpRecvBufferController;
  late TextEditingController _sctpRTOMaxController;
  late TextEditingController _udpReadBufferController;
  late TextEditingController _udpWriteBufferController;
  late TextEditingController _iceDisconnTimeoutController;
  late TextEditingController _iceFailedTimeoutController;
  late TextEditingController _iceKeepaliveController;
  late TextEditingController _dtlsRetransmitController;
  late TextEditingController _uuidController;

  String _natMethod = ServerSettings.defaultNatMethod;
  String _uuidMode = 'random'; // 'random' or 'custom'
  String _protocol = ServerSettings.defaultProtocol;
  String _transport = ServerSettings.defaultTransport;
  String _socksAuth = ServerSettings.defaultSocksAuth;
  bool _socksUdp = ServerSettings.defaultSocksUdp;
  bool _kcpCongestion = ServerSettings.defaultKcpCongestion;
  String _xhttpMode = ServerSettings.defaultXhttpMode;
  String _finalMaskType = ServerSettings.defaultFinalMaskType;
  bool _useRelay = ServerSettings.defaultUseRelay;
  String _transportMode = ServerSettings.defaultTransportMode;
  bool _disableIPv6 = ServerSettings.defaultDisableIPv6;
  bool _paddingEnabled = ServerSettings.defaultPaddingEnabled;
  bool _dtlsSkipVerify = ServerSettings.defaultDtlsSkipVerify;
  bool _sctpZeroChecksum = ServerSettings.defaultSctpZeroChecksum;
  bool _disableCloseByDTLS = ServerSettings.defaultDisableCloseByDTLS;
  bool _maskIPs = ServerSettings.defaultMaskIPs;
  bool _discoveryEnabled = true;
  bool _isLoading = true;

  @override
  void initState() {
    super.initState();
    _portController = TextEditingController();
    _stunController = TextEditingController();
    _signalingController = TextEditingController();
    _discoveryUrlController = TextEditingController();
    _displayNameController = TextEditingController();
    _roomController = TextEditingController();
    _socksUsernameController = TextEditingController();
    _socksPasswordController = TextEditingController();
    _kcpMtuController = TextEditingController();
    _kcpTtiController = TextEditingController();
    _kcpUplinkController = TextEditingController();
    _kcpDownlinkController = TextEditingController();
    _kcpReadBufferController = TextEditingController();
    _kcpWriteBufferController = TextEditingController();
    _xhttpPathController = TextEditingController();
    _xhttpHostController = TextEditingController();
    _finalMaskPasswordController = TextEditingController();
    _finalMaskDomainController = TextEditingController();
    _upnpLeaseDurationController = TextEditingController();
    _upnpRetriesController = TextEditingController();
    _ssdpTimeoutController = TextEditingController();

    _rateLimitUpController = TextEditingController();
    _rateLimitDownController = TextEditingController();
    _numPeerConnectionsController = TextEditingController();
    _numChannelsController = TextEditingController();
    _smuxStreamBufferController = TextEditingController();
    _smuxSessionBufferController = TextEditingController();
    _smuxFrameSizeController = TextEditingController();
    _smuxKeepAliveController = TextEditingController();
    _smuxKeepAliveTimeoutController = TextEditingController();
    _dcMaxBufferedController = TextEditingController();
    _dcLowMarkController = TextEditingController();
    _paddingMaxController = TextEditingController();
    _sctpRecvBufferController = TextEditingController();
    _sctpRTOMaxController = TextEditingController();
    _udpReadBufferController = TextEditingController();
    _udpWriteBufferController = TextEditingController();
    _iceDisconnTimeoutController = TextEditingController();
    _iceFailedTimeoutController = TextEditingController();
    _iceKeepaliveController = TextEditingController();
    _dtlsRetransmitController = TextEditingController();
    _uuidController = TextEditingController();
    _loadSettings();
  }

  Future<void> _loadSettings() async {
    try {
      final settings = await _settingsService.loadServerSettings();
      if (!mounted) return;
      _applySettings(settings);
      setState(() => _isLoading = false);
    } catch (e) {
      debugPrint('ServerSettingsScreen: loadSettings error: $e');
      if (mounted) setState(() => _isLoading = false);
    }
  }

  Future<void> _saveSettings() async {
    if (!_formKey.currentState!.validate()) return;

    // Generate random display name if empty and discovery is enabled
    String displayName = _displayNameController.text.trim();
    if (displayName.isEmpty && _discoveryEnabled) {
      displayName = NameGenerator.generate();
      _displayNameController.text = displayName;
    }

    final settings = ServerSettings(
      listenPort: int.parse(_portController.text.trim()),
      stunServer: _stunController.text.trim(),
      signalingUrl: _signalingController.text.trim(),
      discoveryUrl: _discoveryUrlController.text.trim(),
      natMethod: _natMethod,
      discoveryEnabled: _discoveryEnabled,
      displayName: displayName,
      room: _roomController.text.trim(),
      protocol: _protocol,
      transport: _transport,
      socksAuth: _socksAuth,
      socksUsername: _socksUsernameController.text.trim(),
      socksPassword: _socksPasswordController.text.trim(),
      socksUdp: _socksUdp,
      kcpMtu:
          int.tryParse(_kcpMtuController.text.trim()) ??
          ServerSettings.defaultKcpMtu,
      kcpTti:
          int.tryParse(_kcpTtiController.text.trim()) ??
          ServerSettings.defaultKcpTti,
      kcpUplinkCapacity:
          int.tryParse(_kcpUplinkController.text.trim()) ??
          ServerSettings.defaultKcpUplinkCapacity,
      kcpDownlinkCapacity:
          int.tryParse(_kcpDownlinkController.text.trim()) ??
          ServerSettings.defaultKcpDownlinkCapacity,
      kcpCongestion: _kcpCongestion,
      kcpReadBufferSize:
          int.tryParse(_kcpReadBufferController.text.trim()) ??
          ServerSettings.defaultKcpReadBufferSize,
      kcpWriteBufferSize:
          int.tryParse(_kcpWriteBufferController.text.trim()) ??
          ServerSettings.defaultKcpWriteBufferSize,
      xhttpPath: _xhttpPathController.text.trim(),
      xhttpHost: _xhttpHostController.text.trim(),
      xhttpMode: _xhttpMode,
      finalMaskType: _finalMaskType,
      finalMaskPassword: _finalMaskPasswordController.text.trim(),
      finalMaskDomain: _finalMaskDomainController.text.trim(),
      upnpLeaseDuration:
          int.tryParse(_upnpLeaseDurationController.text.trim()) ??
          ServerSettings.defaultUpnpLeaseDuration,
      upnpRetries:
          int.tryParse(_upnpRetriesController.text.trim()) ??
          ServerSettings.defaultUpnpRetries,
      ssdpTimeout:
          int.tryParse(_ssdpTimeoutController.text.trim()) ??
          ServerSettings.defaultSsdpTimeout,
      useRelay: _useRelay,
      transportMode: _transportMode,
      disableIPv6: _disableIPv6,
      rateLimitUp:
          (int.tryParse(_rateLimitUpController.text.trim()) ?? 0) * 1024,
      rateLimitDown:
          (int.tryParse(_rateLimitDownController.text.trim()) ?? 0) * 1024,
      numPeerConnections:
          int.tryParse(_numPeerConnectionsController.text.trim()) ??
          ServerSettings.defaultNumPeerConnections,
      numChannels:
          int.tryParse(_numChannelsController.text.trim()) ??
          ServerSettings.defaultNumChannels,
      smuxStreamBuffer:
          int.tryParse(_smuxStreamBufferController.text.trim()) ??
          ServerSettings.defaultSmuxStreamBuffer,
      smuxSessionBuffer:
          int.tryParse(_smuxSessionBufferController.text.trim()) ??
          ServerSettings.defaultSmuxSessionBuffer,
      smuxFrameSize:
          int.tryParse(_smuxFrameSizeController.text.trim()) ??
          ServerSettings.defaultSmuxFrameSize,
      smuxKeepAlive:
          int.tryParse(_smuxKeepAliveController.text.trim()) ??
          ServerSettings.defaultSmuxKeepAlive,
      smuxKeepAliveTimeout:
          int.tryParse(_smuxKeepAliveTimeoutController.text.trim()) ??
          ServerSettings.defaultSmuxKeepAliveTimeout,
      dcMaxBuffered:
          int.tryParse(_dcMaxBufferedController.text.trim()) ??
          ServerSettings.defaultDcMaxBuffered,
      dcLowMark:
          int.tryParse(_dcLowMarkController.text.trim()) ??
          ServerSettings.defaultDcLowMark,
      paddingEnabled: _paddingEnabled,
      paddingMax:
          int.tryParse(_paddingMaxController.text.trim()) ??
          ServerSettings.defaultPaddingMax,
      sctpRecvBuffer:
          int.tryParse(_sctpRecvBufferController.text.trim()) ??
          ServerSettings.defaultSctpRecvBuffer,
      sctpRTOMax:
          int.tryParse(_sctpRTOMaxController.text.trim()) ??
          ServerSettings.defaultSctpRTOMax,
      udpReadBuffer:
          int.tryParse(_udpReadBufferController.text.trim()) ??
          ServerSettings.defaultUdpReadBuffer,
      udpWriteBuffer:
          int.tryParse(_udpWriteBufferController.text.trim()) ??
          ServerSettings.defaultUdpWriteBuffer,
      iceDisconnTimeout:
          int.tryParse(_iceDisconnTimeoutController.text.trim()) ??
          ServerSettings.defaultIceDisconnTimeout,
      iceFailedTimeout:
          int.tryParse(_iceFailedTimeoutController.text.trim()) ??
          ServerSettings.defaultIceFailedTimeout,
      iceKeepalive:
          int.tryParse(_iceKeepaliveController.text.trim()) ??
          ServerSettings.defaultIceKeepalive,
      dtlsRetransmit:
          int.tryParse(_dtlsRetransmitController.text.trim()) ??
          ServerSettings.defaultDtlsRetransmit,
      dtlsSkipVerify: _dtlsSkipVerify,
      sctpZeroChecksum: _sctpZeroChecksum,
      disableCloseByDTLS: _disableCloseByDTLS,
      maskIPs: _maskIPs,
      uuid: _uuidMode == 'custom' ? _uuidController.text.trim() : '',
    );

    await _settingsService.saveServerSettings(settings);
    if (mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('Server settings saved')));
      Navigator.of(context).pop();
    }
  }

  void _applySettings(ServerSettings settings) {
    setState(() {
      _portController.text = settings.listenPort.toString();
      _stunController.text = settings.stunServer;
      _signalingController.text = settings.signalingUrl;
      _discoveryUrlController.text = settings.discoveryUrl;
      _natMethod = settings.natMethod;
      _discoveryEnabled = settings.discoveryEnabled;
      _displayNameController.text = settings.displayName;
      _roomController.text = settings.room;
      _protocol = settings.protocol;
      _transport = settings.transport;
      _socksAuth = settings.socksAuth;
      _socksUsernameController.text = settings.socksUsername;
      _socksPasswordController.text = settings.socksPassword;
      _socksUdp = settings.socksUdp;
      _kcpMtuController.text = settings.kcpMtu.toString();
      _kcpTtiController.text = settings.kcpTti.toString();
      _kcpUplinkController.text = settings.kcpUplinkCapacity.toString();
      _kcpDownlinkController.text = settings.kcpDownlinkCapacity.toString();
      _kcpCongestion = settings.kcpCongestion;
      _kcpReadBufferController.text = settings.kcpReadBufferSize.toString();
      _kcpWriteBufferController.text = settings.kcpWriteBufferSize.toString();
      _xhttpPathController.text = settings.xhttpPath;
      _xhttpHostController.text = settings.xhttpHost;
      _xhttpMode = settings.xhttpMode;
      _finalMaskType = settings.finalMaskType;
      _finalMaskPasswordController.text = settings.finalMaskPassword;
      _finalMaskDomainController.text = settings.finalMaskDomain;
      _upnpLeaseDurationController.text = settings.upnpLeaseDuration.toString();
      _upnpRetriesController.text = settings.upnpRetries.toString();
      _ssdpTimeoutController.text = settings.ssdpTimeout.toString();

      _useRelay = settings.useRelay;
      _transportMode = settings.transportMode;
      _disableIPv6 = settings.disableIPv6;
      _rateLimitUpController.text = settings.rateLimitUp > 0
          ? (settings.rateLimitUp ~/ 1024).toString()
          : '';
      _rateLimitDownController.text = settings.rateLimitDown > 0
          ? (settings.rateLimitDown ~/ 1024).toString()
          : '';
      _numPeerConnectionsController.text = settings.numPeerConnections.toString();
      _numChannelsController.text = settings.numChannels.toString();
      _smuxStreamBufferController.text = settings.smuxStreamBuffer.toString();
      _smuxSessionBufferController.text = settings.smuxSessionBuffer.toString();
      _smuxFrameSizeController.text = settings.smuxFrameSize.toString();
      _smuxKeepAliveController.text = settings.smuxKeepAlive.toString();
      _smuxKeepAliveTimeoutController.text = settings.smuxKeepAliveTimeout.toString();
      _dcMaxBufferedController.text = settings.dcMaxBuffered.toString();
      _dcLowMarkController.text = settings.dcLowMark.toString();
      _paddingEnabled = settings.paddingEnabled;
      _paddingMaxController.text = settings.paddingMax.toString();
      _sctpRecvBufferController.text = settings.sctpRecvBuffer.toString();
      _sctpRTOMaxController.text = settings.sctpRTOMax.toString();
      _udpReadBufferController.text = settings.udpReadBuffer.toString();
      _udpWriteBufferController.text = settings.udpWriteBuffer.toString();
      _iceDisconnTimeoutController.text = settings.iceDisconnTimeout.toString();
      _iceFailedTimeoutController.text = settings.iceFailedTimeout.toString();
      _iceKeepaliveController.text = settings.iceKeepalive.toString();
      _dtlsRetransmitController.text = settings.dtlsRetransmit.toString();
      _dtlsSkipVerify = settings.dtlsSkipVerify;
      _sctpZeroChecksum = settings.sctpZeroChecksum;
      _disableCloseByDTLS = settings.disableCloseByDTLS;
      _maskIPs = settings.maskIPs;
      _uuidController.text = settings.uuid;
      _uuidMode = settings.uuid.isEmpty ? 'random' : 'custom';
    });
  }

  Future<void> _resetToDefaults() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Reset to Defaults'),
        content: const Text('All server settings will be reverted to their default values. This does not save automatically.'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('Reset'),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      _applySettings(const ServerSettings());
    }
  }

  void _onProtocolChanged(String? value) {
    if (value == null) return;
    setState(() {
      _protocol = value;
    });
  }

  void _onTransportChanged(String? value) {
    if (value == null) return;
    setState(() {
      _transport = value;
    });
  }

  bool _isHolepunchSelected() {
    return _natMethod == 'holepunch';
  }

  String? _validatePort(String? value) {
    if (value == null || value.trim().isEmpty) return 'Required';
    final port = int.tryParse(value.trim());
    if (port == null || port < 1024 || port > 65535) {
      return 'Port must be 1024-65535';
    }
    return null;
  }

  String? _validateHostPort(String? value) {
    if (value == null || value.trim().isEmpty) return 'Required';
    final parts = value.trim().split(':');
    if (parts.length != 2 ||
        parts[0].isEmpty ||
        int.tryParse(parts[1]) == null) {
      return 'Format: host:port';
    }
    return null;
  }

  String? _validateUrl(String? value) {
    if (value == null || value.trim().isEmpty) return 'Required';
    final uri = Uri.tryParse(value.trim());
    if (uri == null || !uri.hasScheme || !uri.hasAuthority) {
      return 'Enter a valid URL';
    }
    return null;
  }

  String? _validateKcpMtu(String? value) {
    if (value == null || value.trim().isEmpty) return 'Required';
    final v = int.tryParse(value.trim());
    if (v == null || v < 576 || v > 1460) return '576-1460';
    return null;
  }

  String? _validateKcpTti(String? value) {
    if (value == null || value.trim().isEmpty) return 'Required';
    final v = int.tryParse(value.trim());
    if (v == null || v < 10 || v > 100) return '10-100';
    return null;
  }

  String? _validatePositiveInt(String? value) {
    if (value == null || value.trim().isEmpty) return 'Required';
    final v = int.tryParse(value.trim());
    if (v == null || v <= 0) return 'Must be > 0';
    return null;
  }

  static final _uuidRegex = RegExp(
    r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
  );

  String? _validateUuid(String? value) {
    if (value == null || value.trim().isEmpty) return 'Required';
    if (!_uuidRegex.hasMatch(value.trim())) {
      return 'Invalid UUID format';
    }
    return null;
  }

  String _generateUuid() {
    final rng = Random.secure();
    final bytes = List<int>.generate(16, (_) => rng.nextInt(256));
    bytes[6] = (bytes[6] & 0x0f) | 0x40; // version 4
    bytes[8] = (bytes[8] & 0x3f) | 0x80; // variant 2
    String hex(List<int> b) => b.map((e) => e.toRadixString(16).padLeft(2, '0')).join();
    return '${hex(bytes.sublist(0, 4))}-${hex(bytes.sublist(4, 6))}-'
        '${hex(bytes.sublist(6, 8))}-${hex(bytes.sublist(8, 10))}-'
        '${hex(bytes.sublist(10, 16))}';
  }

  @override
  void dispose() {
    _portController.dispose();
    _stunController.dispose();
    _signalingController.dispose();
    _discoveryUrlController.dispose();
    _displayNameController.dispose();
    _roomController.dispose();
    _socksUsernameController.dispose();
    _socksPasswordController.dispose();
    _kcpMtuController.dispose();
    _kcpTtiController.dispose();
    _kcpUplinkController.dispose();
    _kcpDownlinkController.dispose();
    _kcpReadBufferController.dispose();
    _kcpWriteBufferController.dispose();
    _xhttpPathController.dispose();
    _xhttpHostController.dispose();
    _finalMaskPasswordController.dispose();
    _finalMaskDomainController.dispose();
    _upnpLeaseDurationController.dispose();
    _upnpRetriesController.dispose();
    _ssdpTimeoutController.dispose();

    _rateLimitUpController.dispose();
    _rateLimitDownController.dispose();
    _numPeerConnectionsController.dispose();
    _numChannelsController.dispose();
    _smuxStreamBufferController.dispose();
    _smuxSessionBufferController.dispose();
    _smuxFrameSizeController.dispose();
    _smuxKeepAliveController.dispose();
    _smuxKeepAliveTimeoutController.dispose();
    _dcMaxBufferedController.dispose();
    _dcLowMarkController.dispose();
    _paddingMaxController.dispose();
    _sctpRecvBufferController.dispose();
    _sctpRTOMaxController.dispose();
    _udpReadBufferController.dispose();
    _udpWriteBufferController.dispose();
    _iceDisconnTimeoutController.dispose();
    _iceFailedTimeoutController.dispose();
    _iceKeepaliveController.dispose();
    _dtlsRetransmitController.dispose();
    _uuidController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Server Settings'),
        backgroundColor: Theme.of(context).colorScheme.inversePrimary,
        actions: [
          IconButton(
            icon: const Icon(Icons.restore),
            tooltip: 'Reset to Defaults',
            onPressed: _isLoading ? null : _resetToDefaults,
          ),
          IconButton(
            icon: const Icon(Icons.check),
            onPressed: _isLoading ? null : _saveSettings,
          ),
        ],
      ),
      body: _isLoading
          ? const Center(child: CircularProgressIndicator())
          : SingleChildScrollView(
              padding: const EdgeInsets.all(16),
              child: Form(
                key: _formKey,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    // === Network ===
                    Text(
                      'Network',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _portController,
                      decoration: const InputDecoration(
                        labelText: 'Listen Port',
                        hintText: '10853',
                        border: OutlineInputBorder(),
                      ),
                      keyboardType: TextInputType.number,
                      validator: _validatePort,
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _stunController,
                      decoration: const InputDecoration(
                        labelText: 'STUN Server',
                        hintText: 'stun.l.google.com:19302',
                        border: OutlineInputBorder(),
                      ),
                      validator: _validateHostPort,
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _signalingController,
                      decoration: const InputDecoration(
                        labelText: 'Signaling Server URL',
                        hintText: 'http://[IP]:5601',
                        border: OutlineInputBorder(),
                      ),
                      keyboardType: TextInputType.url,
                      validator: _validateUrl,
                    ),
                    const SizedBox(height: 24),

                    // === NAT Traversal ===
                    Text(
                      'NAT Traversal',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: 12),
                    DropdownButtonFormField<String>(
                      initialValue: _natMethod,
                      decoration: const InputDecoration(
                        labelText: 'NAT Method',
                        border: OutlineInputBorder(),
                      ),
                      items: [
                        const DropdownMenuItem(
                          value: 'auto',
                          child: Text('Auto'),
                        ),
                        const DropdownMenuItem(
                          value: 'upnp',
                          child: Text('UPnP Only'),
                        ),
                        const DropdownMenuItem(
                          value: 'holepunch',
                          child: Text('Hole Punch Only (WebRTC)'),
                        ),
                      ],
                      onChanged: (value) {
                        if (value != null) setState(() => _natMethod = value);
                      },
                    ),

                    // UPnP Settings (visible when natMethod != 'holepunch')
                    if (_natMethod != 'holepunch') ...[
                      const SizedBox(height: 16),
                      Text(
                        'UPnP Settings',
                        style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                          fontWeight: FontWeight.w500,
                        ),
                      ),
                      const SizedBox(height: 8),
                      TextFormField(
                        controller: _upnpLeaseDurationController,
                        decoration: const InputDecoration(
                          labelText: 'Lease Duration (seconds)',
                          hintText: '3600',
                          helperText: '0-86400 (0 = indefinite)',
                          border: OutlineInputBorder(),
                        ),
                        keyboardType: TextInputType.number,
                        validator: (value) {
                          if (value == null || value.trim().isEmpty) return 'Required';
                          final v = int.tryParse(value.trim());
                          if (v == null || v < 0 || v > 86400) return '0-86400';
                          return null;
                        },
                      ),
                      const SizedBox(height: 12),
                      TextFormField(
                        controller: _upnpRetriesController,
                        decoration: const InputDecoration(
                          labelText: 'Mapping Retries',
                          hintText: '3',
                          helperText: '1-10',
                          border: OutlineInputBorder(),
                        ),
                        keyboardType: TextInputType.number,
                        validator: (value) {
                          if (value == null || value.trim().isEmpty) return 'Required';
                          final v = int.tryParse(value.trim());
                          if (v == null || v < 1 || v > 10) return '1-10';
                          return null;
                        },
                      ),
                      const SizedBox(height: 12),
                      TextFormField(
                        controller: _ssdpTimeoutController,
                        decoration: const InputDecoration(
                          labelText: 'SSDP Timeout (seconds)',
                          hintText: '3',
                          helperText: '1-30',
                          border: OutlineInputBorder(),
                        ),
                        keyboardType: TextInputType.number,
                        validator: (value) {
                          if (value == null || value.trim().isEmpty) return 'Required';
                          final v = int.tryParse(value.trim());
                          if (v == null || v < 1 || v > 30) return '1-30';
                          return null;
                        },
                      ),
                    ],

                    const SizedBox(height: 24),

                    // === Protocol & Transport (hidden when holepunch uses WebRTC) ===
                    if (!_isHolepunchSelected()) ...[
                      Text(
                        'Protocol & Transport',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      const SizedBox(height: 12),
                      DropdownButtonFormField<String>(
                        initialValue: _protocol,
                        decoration: const InputDecoration(
                          labelText: 'Protocol',
                          border: OutlineInputBorder(),
                        ),
                        items: const [
                          DropdownMenuItem(value: 'vless', child: Text('VLESS')),
                          DropdownMenuItem(value: 'socks', child: Text('SOCKS5')),
                        ],
                        onChanged: _onProtocolChanged,
                      ),
                      if (_protocol == 'vless') ...[
                        const SizedBox(height: 12),
                        DropdownButtonFormField<String>(
                          initialValue: _transport,
                          decoration: const InputDecoration(
                            labelText: 'Transport',
                            border: OutlineInputBorder(),
                          ),
                          items: const [
                            DropdownMenuItem(
                              value: 'kcp',
                              child: Text('KCP (UDP)'),
                            ),
                            DropdownMenuItem(
                              value: 'xhttp',
                              child: Text('xHTTP (TCP)'),
                            ),
                          ],
                          onChanged: _onTransportChanged,
                        ),
                      ],
                      if (_protocol == 'vless') ...[
                        const SizedBox(height: 16),
                        Row(
                          children: [
                            Text(
                              'UUID',
                              style: Theme.of(context).textTheme.titleSmall,
                            ),
                            const Spacer(),
                            SegmentedButton<String>(
                              segments: const [
                                ButtonSegment(
                                  value: 'random',
                                  label: Text('Random'),
                                ),
                                ButtonSegment(
                                  value: 'custom',
                                  label: Text('Custom'),
                                ),
                              ],
                              selected: {_uuidMode},
                              onSelectionChanged: (value) {
                                setState(() => _uuidMode = value.first);
                              },
                            ),
                          ],
                        ),
                        if (_uuidMode == 'random')
                          Padding(
                            padding: const EdgeInsets.only(top: 8),
                            child: Text(
                              'A new random UUID will be generated each time the server starts',
                              style: Theme.of(context).textTheme.bodySmall,
                            ),
                          ),
                        if (_uuidMode == 'custom') ...[
                          const SizedBox(height: 8),
                          TextFormField(
                            controller: _uuidController,
                            decoration: InputDecoration(
                              labelText: 'UUID',
                              hintText: '00000000-0000-0000-0000-000000000000',
                              border: const OutlineInputBorder(),
                              suffixIcon: IconButton(
                                icon: const Icon(Icons.refresh),
                                tooltip: 'Generate UUID',
                                onPressed: () {
                                  _uuidController.text = _generateUuid();
                                },
                              ),
                            ),
                            style: const TextStyle(fontFamily: 'monospace', fontSize: 14),
                            validator: _validateUuid,
                          ),
                          const SizedBox(height: 4),
                          Text(
                            'This UUID will be reused across server restarts',
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                        ],
                      ],
                      const SizedBox(height: 4),
                      Text(
                        _protocol == 'socks'
                            ? 'SOCKS5 uses TCP via UPnP'
                            : _transport == 'kcp'
                            ? 'VLESS+KCP uses UDP (supports hole punch)'
                            : 'VLESS+xHTTP uses TCP via UPnP',
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                      const SizedBox(height: 24),
                    ] else ...[
                      const SizedBox(height: 4),
                      Text(
                        'Hole Punch uses WebRTC (ICE/DTLS/SCTP) for NAT traversal',
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                      const SizedBox(height: 8),
                      SwitchListTile(
                        title: const Text('UDP Relay Fallback'),
                        subtitle: const Text(
                          'Route through signaling server when direct connection fails (symmetric NAT)',
                        ),
                        value: _useRelay,
                        onChanged: (value) =>
                            setState(() => _useRelay = value),
                        contentPadding: EdgeInsets.zero,
                      ),
                      const SizedBox(height: 12),
                      DropdownButtonFormField<String>(
                        initialValue: _transportMode,
                        decoration: const InputDecoration(
                          labelText: 'Transport Mode',
                          border: OutlineInputBorder(),
                        ),
                        items: const [
                          DropdownMenuItem(
                            value: 'datachannel',
                            child: Text('Data Channel (default)'),
                          ),
                          DropdownMenuItem(
                            value: 'media',
                            child: Text('Media Stream (video call)'),
                          ),
                        ],
                        onChanged: (value) {
                          if (value != null) {
                            setState(() => _transportMode = value);
                          }
                        },
                      ),
                      const SizedBox(height: 4),
                      Text(
                        _transportMode == 'media'
                            ? 'Disguises traffic as a video call (harder to detect)'
                            : 'Standard WebRTC data channel transport',
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                      SwitchListTile(
                        title: const Text('Disable IPv6'),
                        subtitle: const Text(
                          'Skip IPv6 ICE candidates',
                        ),
                        value: _disableIPv6,
                        onChanged: (value) =>
                            setState(() => _disableIPv6 = value),
                        contentPadding: EdgeInsets.zero,
                      ),
                      const SizedBox(height: 12),
                      TextFormField(
                        controller: _rateLimitUpController,
                        decoration: const InputDecoration(
                          labelText: 'Upload Rate Limit (KB/s)',
                          hintText: '0 = unlimited',
                          helperText: 'Per-client upload limit',
                          border: OutlineInputBorder(),
                        ),
                        keyboardType: TextInputType.number,
                        validator: (value) {
                          if (value != null && value.trim().isNotEmpty) {
                            final v = int.tryParse(value.trim());
                            if (v == null || v < 0) return 'Must be >= 0';
                          }
                          return null;
                        },
                      ),
                      const SizedBox(height: 12),
                      TextFormField(
                        controller: _rateLimitDownController,
                        decoration: const InputDecoration(
                          labelText: 'Download Rate Limit (KB/s)',
                          hintText: '0 = unlimited',
                          helperText: 'Per-client download limit',
                          border: OutlineInputBorder(),
                        ),
                        keyboardType: TextInputType.number,
                        validator: (value) {
                          if (value != null && value.trim().isNotEmpty) {
                            final v = int.tryParse(value.trim());
                            if (v == null || v < 0) return 'Must be >= 0';
                          }
                          return null;
                        },
                      ),
                      const SizedBox(height: 24),

                      // === Traffic Padding ===
                      Text(
                        'Traffic Padding',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      SwitchListTile(
                        title: const Text('Enable Padding'),
                        subtitle: const Text('Add random padding to WebRTC writes'),
                        value: _paddingEnabled,
                        onChanged: (value) =>
                            setState(() => _paddingEnabled = value),
                        contentPadding: EdgeInsets.zero,
                      ),
                      if (_paddingEnabled) ...[
                        const SizedBox(height: 8),
                        TextFormField(
                          controller: _paddingMaxController,
                          decoration: const InputDecoration(
                            labelText: 'Max Padding Bytes',
                            hintText: '256',
                            border: OutlineInputBorder(),
                          ),
                          keyboardType: TextInputType.number,
                          validator: _validatePositiveInt,
                        ),
                      ],
                      const SizedBox(height: 24),

                      // === Advanced WebRTC Tuning ===
                      ExpansionTile(
                        title: const Text('Advanced WebRTC Tuning'),
                        tilePadding: EdgeInsets.zero,
                        childrenPadding: const EdgeInsets.only(bottom: 16),
                        children: [
                          // Channels & Multiplexing
                          Text(
                            'Channels & Multiplexing',
                            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                              fontWeight: FontWeight.w500,
                            ),
                          ),
                          const SizedBox(height: 8),
                          TextFormField(
                            controller: _numPeerConnectionsController,
                            decoration: const InputDecoration(
                              labelText: 'Peer Connections',
                              hintText: '1',
                              helperText: '1-8 (each gets independent bandwidth)',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: (value) {
                              if (value == null || value.trim().isEmpty) return 'Required';
                              final v = int.tryParse(value.trim());
                              if (v == null || v < 1 || v > 8) return '1-8';
                              return null;
                            },
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _numChannelsController,
                            decoration: const InputDecoration(
                              labelText: 'Data Channels',
                              hintText: '6',
                              helperText: '1-32 (distributed across peer connections)',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: (value) {
                              if (value == null || value.trim().isEmpty) return 'Required';
                              final v = int.tryParse(value.trim());
                              if (v == null || v < 1 || v > 32) return '1-32';
                              return null;
                            },
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _smuxStreamBufferController,
                            decoration: const InputDecoration(
                              labelText: 'Smux Stream Buffer (KB)',
                              hintText: '2048',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _smuxSessionBufferController,
                            decoration: const InputDecoration(
                              labelText: 'Smux Session Buffer (KB)',
                              hintText: '8192',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _smuxFrameSizeController,
                            decoration: const InputDecoration(
                              labelText: 'Smux Frame Size (bytes)',
                              hintText: '32768',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _smuxKeepAliveController,
                            decoration: const InputDecoration(
                              labelText: 'Smux Keepalive (sec)',
                              hintText: '10',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _smuxKeepAliveTimeoutController,
                            decoration: const InputDecoration(
                              labelText: 'Smux Keepalive Timeout (sec)',
                              hintText: '300',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _dcMaxBufferedController,
                            decoration: const InputDecoration(
                              labelText: 'DC Max Buffered (KB)',
                              hintText: '512',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _dcLowMarkController,
                            decoration: const InputDecoration(
                              labelText: 'DC Low Mark (KB)',
                              hintText: '128',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          const SizedBox(height: 16),

                          // SCTP / DTLS / ICE
                          Text(
                            'SCTP / DTLS / ICE',
                            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                              fontWeight: FontWeight.w500,
                            ),
                          ),
                          const SizedBox(height: 8),
                          TextFormField(
                            controller: _sctpRecvBufferController,
                            decoration: const InputDecoration(
                              labelText: 'SCTP Recv Buffer (KB)',
                              hintText: '8192',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _sctpRTOMaxController,
                            decoration: const InputDecoration(
                              labelText: 'SCTP RTO Max (ms)',
                              hintText: '2500',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _udpReadBufferController,
                            decoration: const InputDecoration(
                              labelText: 'UDP Read Buffer (KB)',
                              hintText: '8192',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _udpWriteBufferController,
                            decoration: const InputDecoration(
                              labelText: 'UDP Write Buffer (KB)',
                              hintText: '8192',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _iceDisconnTimeoutController,
                            decoration: const InputDecoration(
                              labelText: 'ICE Disconnected Timeout (ms)',
                              hintText: '15000',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _iceFailedTimeoutController,
                            decoration: const InputDecoration(
                              labelText: 'ICE Failed Timeout (ms)',
                              hintText: '25000',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _iceKeepaliveController,
                            decoration: const InputDecoration(
                              labelText: 'ICE Keepalive (ms)',
                              hintText: '2000',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          const SizedBox(height: 12),
                          TextFormField(
                            controller: _dtlsRetransmitController,
                            decoration: const InputDecoration(
                              labelText: 'DTLS Retransmit (ms)',
                              hintText: '100',
                              border: OutlineInputBorder(),
                            ),
                            keyboardType: TextInputType.number,
                            validator: _validatePositiveInt,
                          ),
                          SwitchListTile(
                            title: const Text('Skip DTLS HelloVerify'),
                            value: _dtlsSkipVerify,
                            onChanged: (value) =>
                                setState(() => _dtlsSkipVerify = value),
                            contentPadding: EdgeInsets.zero,
                          ),
                          SwitchListTile(
                            title: const Text('SCTP Zero Checksum'),
                            value: _sctpZeroChecksum,
                            onChanged: (value) =>
                                setState(() => _sctpZeroChecksum = value),
                            contentPadding: EdgeInsets.zero,
                          ),
                          SwitchListTile(
                            title: const Text('Disable Close by DTLS'),
                            value: _disableCloseByDTLS,
                            onChanged: (value) =>
                                setState(() => _disableCloseByDTLS = value),
                            contentPadding: EdgeInsets.zero,
                          ),
                        ],
                      ),
                      const SizedBox(height: 24),
                    ],

                    // === SOCKS Settings (hidden when holepunch) ===
                    if (!_isHolepunchSelected() && _protocol == 'socks') ...[
                      Text(
                        'SOCKS Settings',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      const SizedBox(height: 12),
                      DropdownButtonFormField<String>(
                        initialValue: _socksAuth,
                        decoration: const InputDecoration(
                          labelText: 'Authentication',
                          border: OutlineInputBorder(),
                        ),
                        items: const [
                          DropdownMenuItem(
                            value: 'noauth',
                            child: Text('No Auth'),
                          ),
                          DropdownMenuItem(
                            value: 'password',
                            child: Text('Username + Password'),
                          ),
                        ],
                        onChanged: (value) {
                          if (value != null) {
                            setState(() => _socksAuth = value);
                          }
                        },
                      ),
                      if (_socksAuth == 'password') ...[
                        const SizedBox(height: 12),
                        TextFormField(
                          controller: _socksUsernameController,
                          decoration: const InputDecoration(
                            labelText: 'Username',
                            border: OutlineInputBorder(),
                          ),
                          validator: (value) {
                            if (value == null || value.trim().isEmpty) {
                              return 'Required';
                            }
                            return null;
                          },
                        ),
                        const SizedBox(height: 12),
                        TextFormField(
                          controller: _socksPasswordController,
                          decoration: const InputDecoration(
                            labelText: 'Password',
                            border: OutlineInputBorder(),
                          ),
                          obscureText: true,
                          validator: (value) {
                            if (value == null || value.trim().isEmpty) {
                              return 'Required';
                            }
                            return null;
                          },
                        ),
                      ],
                      SwitchListTile(
                        title: const Text('UDP Support'),
                        value: _socksUdp,
                        onChanged: (value) => setState(() => _socksUdp = value),
                        contentPadding: EdgeInsets.zero,
                      ),
                      const SizedBox(height: 24),
                    ],

                    // === KCP Settings (hidden when holepunch) ===
                    if (!_isHolepunchSelected() && _protocol == 'vless' && _transport == 'kcp') ...[
                      Text(
                        'KCP Settings',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      const SizedBox(height: 12),
                      TextFormField(
                        controller: _kcpMtuController,
                        decoration: const InputDecoration(
                          labelText: 'MTU',
                          hintText: '1350',
                          helperText: '576-1460',
                          border: OutlineInputBorder(),
                        ),
                        keyboardType: TextInputType.number,
                        validator: _validateKcpMtu,
                      ),
                      const SizedBox(height: 12),
                      TextFormField(
                        controller: _kcpTtiController,
                        decoration: const InputDecoration(
                          labelText: 'TTI (ms)',
                          hintText: '50',
                          helperText: '10-100',
                          border: OutlineInputBorder(),
                        ),
                        keyboardType: TextInputType.number,
                        validator: _validateKcpTti,
                      ),
                      const SizedBox(height: 12),
                      TextFormField(
                        controller: _kcpUplinkController,
                        decoration: const InputDecoration(
                          labelText: 'Uplink Capacity (MB/s)',
                          hintText: '5',
                          border: OutlineInputBorder(),
                        ),
                        keyboardType: TextInputType.number,
                        validator: _validatePositiveInt,
                      ),
                      const SizedBox(height: 12),
                      TextFormField(
                        controller: _kcpDownlinkController,
                        decoration: const InputDecoration(
                          labelText: 'Downlink Capacity (MB/s)',
                          hintText: '20',
                          border: OutlineInputBorder(),
                        ),
                        keyboardType: TextInputType.number,
                        validator: _validatePositiveInt,
                      ),
                      SwitchListTile(
                        title: const Text('Congestion Control'),
                        value: _kcpCongestion,
                        onChanged: (value) =>
                            setState(() => _kcpCongestion = value),
                        contentPadding: EdgeInsets.zero,
                      ),
                      const SizedBox(height: 12),
                      TextFormField(
                        controller: _kcpReadBufferController,
                        decoration: const InputDecoration(
                          labelText: 'Read Buffer Size (MB)',
                          hintText: '2',
                          border: OutlineInputBorder(),
                        ),
                        keyboardType: TextInputType.number,
                        validator: _validatePositiveInt,
                      ),
                      const SizedBox(height: 12),
                      TextFormField(
                        controller: _kcpWriteBufferController,
                        decoration: const InputDecoration(
                          labelText: 'Write Buffer Size (MB)',
                          hintText: '2',
                          border: OutlineInputBorder(),
                        ),
                        keyboardType: TextInputType.number,
                        validator: _validatePositiveInt,
                      ),
                      const SizedBox(height: 24),

                      // === FinalMask Settings ===
                      Text(
                        'FinalMask (Packet Obfuscation)',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      const SizedBox(height: 12),
                      DropdownButtonFormField<String>(
                        initialValue: _finalMaskType,
                        decoration: const InputDecoration(
                          labelText: 'Mask Type',
                          border: OutlineInputBorder(),
                        ),
                        items: const [
                          DropdownMenuItem(
                            value: 'none',
                            child: Text('None'),
                          ),
                          DropdownMenuItem(
                            value: 'header-srtp',
                            child: Text('SRTP (Video Call)'),
                          ),
                          DropdownMenuItem(
                            value: 'header-dtls',
                            child: Text('DTLS 1.2'),
                          ),
                          DropdownMenuItem(
                            value: 'header-wechat',
                            child: Text('WeChat Video'),
                          ),
                          DropdownMenuItem(
                            value: 'header-utp',
                            child: Text('uTP (BitTorrent)'),
                          ),
                          DropdownMenuItem(
                            value: 'header-wireguard',
                            child: Text('WireGuard'),
                          ),
                          DropdownMenuItem(
                            value: 'header-dns',
                            child: Text('DNS Query'),
                          ),
                          DropdownMenuItem(
                            value: 'mkcp-original',
                            child: Text('mKCP Original'),
                          ),
                          DropdownMenuItem(
                            value: 'mkcp-aes128gcm',
                            child: Text('mKCP AES-128-GCM'),
                          ),
                        ],
                        onChanged: (value) {
                          if (value != null) {
                            setState(() => _finalMaskType = value);
                          }
                        },
                      ),
                      if (_finalMaskType == 'mkcp-aes128gcm') ...[
                        const SizedBox(height: 12),
                        TextFormField(
                          controller: _finalMaskPasswordController,
                          decoration: const InputDecoration(
                            labelText: 'Password',
                            border: OutlineInputBorder(),
                          ),
                          obscureText: true,
                          validator: (value) {
                            if (_finalMaskType == 'mkcp-aes128gcm' &&
                                (value == null || value.trim().isEmpty)) {
                              return 'Required for AES-128-GCM';
                            }
                            return null;
                          },
                        ),
                      ],
                      if (_finalMaskType == 'header-dns') ...[
                        const SizedBox(height: 12),
                        TextFormField(
                          controller: _finalMaskDomainController,
                          decoration: const InputDecoration(
                            labelText: 'Domain',
                            hintText: 'example.com',
                            border: OutlineInputBorder(),
                          ),
                          validator: (value) {
                            if (_finalMaskType == 'header-dns' &&
                                (value == null || value.trim().isEmpty)) {
                              return 'Required for DNS mask';
                            }
                            return null;
                          },
                        ),
                      ],
                      const SizedBox(height: 24),
                    ],

                    // === xHTTP Settings (hidden when holepunch) ===
                    if (!_isHolepunchSelected() && _protocol == 'vless' && _transport == 'xhttp') ...[
                      Text(
                        'xHTTP Settings',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      const SizedBox(height: 12),
                      TextFormField(
                        controller: _xhttpPathController,
                        decoration: const InputDecoration(
                          labelText: 'Path',
                          hintText: '/',
                          border: OutlineInputBorder(),
                        ),
                      ),
                      const SizedBox(height: 12),
                      TextFormField(
                        controller: _xhttpHostController,
                        decoration: const InputDecoration(
                          labelText: 'Host (optional)',
                          border: OutlineInputBorder(),
                        ),
                      ),
                      const SizedBox(height: 12),
                      DropdownButtonFormField<String>(
                        initialValue: _xhttpMode,
                        decoration: const InputDecoration(
                          labelText: 'Mode',
                          border: OutlineInputBorder(),
                        ),
                        items: const [
                          DropdownMenuItem(value: 'auto', child: Text('Auto')),
                          DropdownMenuItem(
                            value: 'packet-up',
                            child: Text('Packet Up'),
                          ),
                          DropdownMenuItem(
                            value: 'stream-up',
                            child: Text('Stream Up'),
                          ),
                          DropdownMenuItem(
                            value: 'stream-one',
                            child: Text('Stream One'),
                          ),
                        ],
                        onChanged: (value) {
                          if (value != null) {
                            setState(() => _xhttpMode = value);
                          }
                        },
                      ),
                      const SizedBox(height: 24),
                    ],

                    // === Server Discovery ===
                    Text(
                      'Server Discovery',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _discoveryUrlController,
                      decoration: const InputDecoration(
                        labelText: 'Discovery Server URL',
                        hintText: 'http://host:5602',
                        border: OutlineInputBorder(),
                      ),
                      keyboardType: TextInputType.url,
                      validator: _validateUrl,
                    ),
                    const SizedBox(height: 12),
                    SwitchListTile(
                      title: const Text('List on discovery'),
                      subtitle: const Text('Let clients find this server'),
                      value: _discoveryEnabled,
                      onChanged: (value) =>
                          setState(() => _discoveryEnabled = value),
                      contentPadding: EdgeInsets.zero,
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _displayNameController,
                      decoration: const InputDecoration(
                        labelText: 'Display Name',
                        hintText: 'Leave empty for random name',
                        border: OutlineInputBorder(),
                      ),
                      enabled: _discoveryEnabled,
                      maxLength: 50,
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _roomController,
                      decoration: const InputDecoration(
                        labelText: 'Room',
                        hintText: 'Leave empty for public',
                        border: OutlineInputBorder(),
                      ),
                      enabled: _discoveryEnabled,
                    ),
                    const SizedBox(height: 24),

                    // === Logging ===
                    Text(
                      'Logging',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    SwitchListTile(
                      title: const Text('Mask IP Addresses in Logs'),
                      subtitle: const Text('Replace last octet with *'),
                      value: _maskIPs,
                      onChanged: (value) =>
                          setState(() => _maskIPs = value),
                      contentPadding: EdgeInsets.zero,
                    ),
                    const SizedBox(height: 24),
                    SizedBox(
                      width: double.infinity,
                      height: 48,
                      child: FilledButton(
                        onPressed: _saveSettings,
                        child: const Text('Save'),
                      ),
                    ),
                  ],
                ),
              ),
            ),
    );
  }
}
