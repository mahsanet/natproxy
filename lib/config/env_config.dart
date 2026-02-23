// GENERATED FILE — DO NOT EDIT MANUALLY
// Source: .env
// Regenerate: dart scripts/apply_env.dart
//
// This file is committed with default values so the app
// builds without a local .env file (e.g. CI, new contributors).

// ignore_for_file: constant_identifier_names
abstract final class EnvConfig {
  static const String SIGNALING_URL = 'http://[IP]:5601';
  static const String DISCOVERY_URL = 'http://[IP]:5602';
  static const String STUN_SERVER = 'stun.l.google.com:19302';
  static const int SERVER_LISTEN_PORT = 10853;
  static const String SERVER_NAT_METHOD = 'auto';
  static const String SERVER_PROTOCOL = 'vless';
  static const String SERVER_TRANSPORT = 'xhttp';
  static const bool SERVER_DISCOVERY_ENABLED = true;
  static const bool SERVER_USE_RELAY = false;
  static const int CLIENT_SOCKS_PORT = 10808;
  static const String CLIENT_TUN_ADDRESS = '10.0.0.2';
  static const int CLIENT_MTU = 1500;
  static const String CLIENT_DNS1 = '8.8.8.8';
  static const String CLIENT_DNS2 = '1.1.1.1';
  static const bool CLIENT_ALLOW_DIRECT_DNS = false;
  static const bool CLIENT_DISCOVERY_ENABLED = true;
  static const String VPN_SESSION_NAME = 'NATProxy';
}
