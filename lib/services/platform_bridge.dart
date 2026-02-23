import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

class PlatformBridge {
  static const _channel = MethodChannel('com.p2pshare/vpn');
  static const _statusChannel = EventChannel('com.p2pshare/status');

  static Future<bool> requestVpnPermission() async {
    return await _channel.invokeMethod<bool>('requestVpnPermission') ?? false;
  }

  static Future<bool> requestBatteryOptimization() async {
    return await _channel.invokeMethod<bool>('requestBatteryOptimization') ??
        false;
  }

  static Future<String> startServer(String settingsJson) async {
    final result = await _channel.invokeMethod<String>('startServer', {
      'settings': settingsJson,
    });
    if (result == null) {
      debugPrint('PlatformBridge: startServer returned null');
    }
    return result ?? '';
  }

  // Legacy mode: starts VPN service which handles the full connection
  // (used for UPnP / direct xray-core connections).
  static Future<void> startClient(
    String connectionCode,
    String settingsJson,
  ) async {
    await _channel.invokeMethod('startClient', {
      'code': connectionCode,
      'settings': settingsJson,
    });
  }

  // Phase 1: connect WebRTC before the TUN is up. Throws on failure.
  static Future<void> connectWebRTC(
    String connectionCode,
    String settingsJson,
  ) async {
    await _channel.invokeMethod('connectWebRTC', {
      'code': connectionCode,
      'settings': settingsJson,
    });
  }

  // Phase 2: WebRTC is up, now bring the TUN online. Call after connectWebRTC.
  static Future<void> startVpn(String settingsJson) async {
    await _channel.invokeMethod('startVpn', {
      'settings': settingsJson,
    });
  }

  static Future<void> stop() async {
    await _channel.invokeMethod('stop');
  }

  static Future<String> getServerStatus() async {
    final result = await _channel.invokeMethod<String>('getServerStatus');
    if (result == null) {
      debugPrint('PlatformBridge: getServerStatus returned null');
    }
    return result ?? '{}';
  }

  static Future<String> getClientStatus() async {
    final result = await _channel.invokeMethod<String>('getClientStatus');
    if (result == null) {
      debugPrint('PlatformBridge: getClientStatus returned null');
    }
    return result ?? '{}';
  }

  static Future<String> detectNatType() async {
    final result = await _channel.invokeMethod<String>('detectNatType');
    if (result == null) {
      debugPrint('PlatformBridge: detectNatType returned null');
    }
    return result ?? 'unknown';
  }

  static Future<String> getLogs(int cursor) async {
    final result = await _channel.invokeMethod<String>('getLogs', {
      'cursor': cursor,
    });
    return result ?? '{"c":0,"e":[]}';
  }

  static Future<void> clearLogs() async {
    await _channel.invokeMethod('clearLogs');
  }

  static Future<String> testLatency() async {
    final result = await _channel.invokeMethod<String>('testLatency');
    if (result == null) {
      debugPrint('PlatformBridge: testLatency returned null');
    }
    return result ?? '{"error": "no response"}';
  }

  static Future<String> listServers(
    String discoveryUrl,
    String room,
  ) async {
    final result = await _channel.invokeMethod<String>('listServers', {
      'discoveryUrl': discoveryUrl,
      'room': room,
    });
    if (result == null) {
      debugPrint('PlatformBridge: listServers returned null');
    }
    return result ?? '[]';
  }

  static Future<String> registerDiscovery(
    String code,
    String settingsJson,
  ) async {
    final result = await _channel.invokeMethod<String>('registerDiscovery', {
      'code': code,
      'settings': settingsJson,
    });
    if (result == null) {
      debugPrint('PlatformBridge: registerDiscovery returned null');
    }
    return result ?? '';
  }

  static Future<void> unregisterDiscovery() async {
    await _channel.invokeMethod('unregisterDiscovery');
  }

  // Manual signaling

  static Future<String> startServerManual(String settingsJson) async {
    final result = await _channel.invokeMethod<String>('startServerManual', {
      'settings': settingsJson,
    });
    return result ?? '';
  }

  static Future<void> acceptManualAnswer(String answerCode) async {
    await _channel.invokeMethod('acceptManualAnswer', {
      'answerCode': answerCode,
    });
  }

  static Future<String> processManualOffer(
    String offerCode,
    String settingsJson,
  ) async {
    final result = await _channel.invokeMethod<String>('processManualOffer', {
      'offerCode': offerCode,
      'settings': settingsJson,
    });
    return result ?? '';
  }

  static Future<void> waitManualConnection(int timeoutSec) async {
    await _channel.invokeMethod('waitManualConnection', {
      'timeout': timeoutSec,
    });
  }

  static Stream<Map<String, dynamic>> get statusStream {
    return _statusChannel.receiveBroadcastStream().map(
      (e) => Map<String, dynamic>.from(e as Map),
    );
  }
}
