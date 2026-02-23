import 'package:flutter/material.dart';
import '../models/settings_model.dart';
import '../services/settings_service.dart';

class ClientSettingsScreen extends StatefulWidget {
  const ClientSettingsScreen({super.key});

  @override
  State<ClientSettingsScreen> createState() => _ClientSettingsScreenState();
}

class _ClientSettingsScreenState extends State<ClientSettingsScreen> {
  final _formKey = GlobalKey<FormState>();
  final _settingsService = SettingsService();

  late TextEditingController _socksPortController;
  late TextEditingController _tunAddressController;
  late TextEditingController _mtuController;
  late TextEditingController _dns1Controller;
  late TextEditingController _dns2Controller;
  late TextEditingController _stunController;
  late TextEditingController _signalingController;
  late TextEditingController _discoveryUrlController;
  late TextEditingController _roomFilterController;
  late TextEditingController _sctpRecvBufferController;
  late TextEditingController _sctpRTOMaxController;
  late TextEditingController _udpReadBufferController;
  late TextEditingController _udpWriteBufferController;
  late TextEditingController _iceDisconnTimeoutController;
  late TextEditingController _iceFailedTimeoutController;
  late TextEditingController _iceKeepaliveController;
  late TextEditingController _dtlsRetransmitController;
  bool _dtlsSkipVerify = ClientSettings.defaultDtlsSkipVerify;
  bool _sctpZeroChecksum = ClientSettings.defaultSctpZeroChecksum;
  bool _disableCloseByDTLS = ClientSettings.defaultDisableCloseByDTLS;
  bool _allowDirectDNS = ClientSettings.defaultAllowDirectDNS;
  bool _maskIPs = ClientSettings.defaultMaskIPs;
  bool _discoveryEnabled = true;
  bool _isLoading = true;

  @override
  void initState() {
    super.initState();
    _socksPortController = TextEditingController();
    _tunAddressController = TextEditingController();
    _mtuController = TextEditingController();
    _dns1Controller = TextEditingController();
    _dns2Controller = TextEditingController();
    _stunController = TextEditingController();
    _signalingController = TextEditingController();
    _discoveryUrlController = TextEditingController();

    _roomFilterController = TextEditingController();
    _sctpRecvBufferController = TextEditingController();
    _sctpRTOMaxController = TextEditingController();
    _udpReadBufferController = TextEditingController();
    _udpWriteBufferController = TextEditingController();
    _iceDisconnTimeoutController = TextEditingController();
    _iceFailedTimeoutController = TextEditingController();
    _iceKeepaliveController = TextEditingController();
    _dtlsRetransmitController = TextEditingController();
    _loadSettings();
  }

  void _applySettings(ClientSettings settings) {
    setState(() {
      _socksPortController.text = settings.socksPort.toString();
      _tunAddressController.text = settings.tunAddress;
      _mtuController.text = settings.mtu.toString();
      _dns1Controller.text = settings.dns1;
      _dns2Controller.text = settings.dns2;
      _stunController.text = settings.stunServer;
      _signalingController.text = settings.signalingUrl;
      _discoveryUrlController.text = settings.discoveryUrl;

      _discoveryEnabled = settings.discoveryEnabled;
      _roomFilterController.text = settings.roomFilter;
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
      _allowDirectDNS = settings.allowDirectDNS;
      _maskIPs = settings.maskIPs;
    });
  }

  Future<void> _resetToDefaults() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Reset to Defaults'),
        content: const Text('All client settings will be reverted to their default values. This does not save automatically.'),
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
      _applySettings(const ClientSettings());
    }
  }

  Future<void> _loadSettings() async {
    try {
      final settings = await _settingsService.loadClientSettings();
      if (!mounted) return;
      _applySettings(settings);
      setState(() => _isLoading = false);
    } catch (e) {
      debugPrint('ClientSettingsScreen: loadSettings error: $e');
      if (mounted) setState(() => _isLoading = false);
    }
  }

  Future<void> _saveSettings() async {
    if (!_formKey.currentState!.validate()) return;

    final settings = ClientSettings(
      socksPort: int.parse(_socksPortController.text.trim()),
      tunAddress: _tunAddressController.text.trim(),
      mtu: int.parse(_mtuController.text.trim()),
      dns1: _dns1Controller.text.trim(),
      dns2: _dns2Controller.text.trim(),
      stunServer: _stunController.text.trim(),
      signalingUrl: _signalingController.text.trim(),
      discoveryUrl: _discoveryUrlController.text.trim(),
      discoveryEnabled: _discoveryEnabled,
      roomFilter: _roomFilterController.text.trim(),
      sctpRecvBuffer:
          int.tryParse(_sctpRecvBufferController.text.trim()) ??
          ClientSettings.defaultSctpRecvBuffer,
      sctpRTOMax:
          int.tryParse(_sctpRTOMaxController.text.trim()) ??
          ClientSettings.defaultSctpRTOMax,
      udpReadBuffer:
          int.tryParse(_udpReadBufferController.text.trim()) ??
          ClientSettings.defaultUdpReadBuffer,
      udpWriteBuffer:
          int.tryParse(_udpWriteBufferController.text.trim()) ??
          ClientSettings.defaultUdpWriteBuffer,
      iceDisconnTimeout:
          int.tryParse(_iceDisconnTimeoutController.text.trim()) ??
          ClientSettings.defaultIceDisconnTimeout,
      iceFailedTimeout:
          int.tryParse(_iceFailedTimeoutController.text.trim()) ??
          ClientSettings.defaultIceFailedTimeout,
      iceKeepalive:
          int.tryParse(_iceKeepaliveController.text.trim()) ??
          ClientSettings.defaultIceKeepalive,
      dtlsRetransmit:
          int.tryParse(_dtlsRetransmitController.text.trim()) ??
          ClientSettings.defaultDtlsRetransmit,
      dtlsSkipVerify: _dtlsSkipVerify,
      sctpZeroChecksum: _sctpZeroChecksum,
      disableCloseByDTLS: _disableCloseByDTLS,
      allowDirectDNS: _allowDirectDNS,
      maskIPs: _maskIPs,
    );

    await _settingsService.saveClientSettings(settings);
    if (mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('Client settings saved')));
      Navigator.of(context).pop();
    }
  }

  String? _validatePort(String? value) {
    if (value == null || value.trim().isEmpty) return 'Required';
    final port = int.tryParse(value.trim());
    if (port == null || port < 1024 || port > 65535) {
      return 'Port must be 1024-65535';
    }
    return null;
  }

  String? _validateIpv4(String? value) {
    if (value == null || value.trim().isEmpty) return 'Required';
    final parts = value.trim().split('.');
    if (parts.length != 4) return 'Invalid IPv4 address';
    for (final part in parts) {
      final n = int.tryParse(part);
      if (n == null || n < 0 || n > 255) return 'Invalid IPv4 address';
    }
    return null;
  }

  String? _validateMtu(String? value) {
    if (value == null || value.trim().isEmpty) return 'Required';
    final mtu = int.tryParse(value.trim());
    if (mtu == null || mtu < 1280 || mtu > 9000) {
      return 'MTU must be 1280-9000';
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

  String? _validatePositiveInt(String? value) {
    if (value == null || value.trim().isEmpty) return 'Required';
    final v = int.tryParse(value.trim());
    if (v == null || v <= 0) return 'Must be > 0';
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

  @override
  void dispose() {
    _socksPortController.dispose();
    _tunAddressController.dispose();
    _mtuController.dispose();
    _dns1Controller.dispose();
    _dns2Controller.dispose();
    _stunController.dispose();
    _signalingController.dispose();
    _discoveryUrlController.dispose();

    _roomFilterController.dispose();
    _sctpRecvBufferController.dispose();
    _sctpRTOMaxController.dispose();
    _udpReadBufferController.dispose();
    _udpWriteBufferController.dispose();
    _iceDisconnTimeoutController.dispose();
    _iceFailedTimeoutController.dispose();
    _iceKeepaliveController.dispose();
    _dtlsRetransmitController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Client Settings'),
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
                    Text(
                      'Proxy',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _socksPortController,
                      decoration: const InputDecoration(
                        labelText: 'SOCKS Proxy Port',
                        hintText: '10808',
                        border: OutlineInputBorder(),
                      ),
                      keyboardType: TextInputType.number,
                      validator: _validatePort,
                    ),
                    const SizedBox(height: 24),
                    Text('VPN', style: Theme.of(context).textTheme.titleMedium),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _tunAddressController,
                      decoration: const InputDecoration(
                        labelText: 'TUN Address',
                        hintText: '10.0.0.2',
                        border: OutlineInputBorder(),
                      ),
                      keyboardType: TextInputType.number,
                      validator: _validateIpv4,
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _mtuController,
                      decoration: const InputDecoration(
                        labelText: 'MTU',
                        hintText: '1500',
                        border: OutlineInputBorder(),
                      ),
                      keyboardType: TextInputType.number,
                      validator: _validateMtu,
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _dns1Controller,
                      decoration: const InputDecoration(
                        labelText: 'DNS Server 1',
                        hintText: '8.8.8.8',
                        border: OutlineInputBorder(),
                      ),
                      keyboardType: TextInputType.number,
                      validator: _validateIpv4,
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _dns2Controller,
                      decoration: const InputDecoration(
                        labelText: 'DNS Server 2',
                        hintText: '1.1.1.1',
                        border: OutlineInputBorder(),
                      ),
                      keyboardType: TextInputType.number,
                      validator: _validateIpv4,
                    ),
                    SwitchListTile(
                      title: const Text('Allow Direct DNS Fallback'),
                      subtitle: const Text(
                        'Falls back to ISP DNS if tunnel DNS fails (privacy leak)',
                      ),
                      value: _allowDirectDNS,
                      onChanged: (value) =>
                          setState(() => _allowDirectDNS = value),
                      contentPadding: EdgeInsets.zero,
                    ),
                    const SizedBox(height: 24),
                    Text(
                      'Network',
                      style: Theme.of(context).textTheme.titleMedium,
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

                    // === Advanced WebRTC Tuning ===
                    ExpansionTile(
                      title: const Text('Advanced WebRTC Tuning'),
                      tilePadding: EdgeInsets.zero,
                      childrenPadding: const EdgeInsets.only(bottom: 16),
                      children: [
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
                      title: const Text('Browse available servers'),
                      subtitle: const Text('Find servers automatically'),
                      value: _discoveryEnabled,
                      onChanged: (value) =>
                          setState(() => _discoveryEnabled = value),
                      contentPadding: EdgeInsets.zero,
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _roomFilterController,
                      decoration: const InputDecoration(
                        labelText: 'Room filter',
                        hintText: 'Leave empty for all',
                        border: OutlineInputBorder(),
                      ),
                      enabled: _discoveryEnabled,
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
