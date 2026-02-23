import 'package:natproxy/config/env_config.dart';

class ServerSettings {
  static const defaultListenPort = EnvConfig.SERVER_LISTEN_PORT;
  static const defaultStunServer = EnvConfig.STUN_SERVER;
  static const defaultSignalingUrl = EnvConfig.SIGNALING_URL;
  static const defaultDiscoveryUrl = EnvConfig.DISCOVERY_URL;
  static const defaultNatMethod = EnvConfig.SERVER_NAT_METHOD;
  static const defaultProtocol = EnvConfig.SERVER_PROTOCOL;
  static const defaultTransport = EnvConfig.SERVER_TRANSPORT;
  static const defaultSocksAuth = 'noauth';
  static const defaultSocksUdp = true;
  static const defaultKcpMtu = 1350;
  static const defaultKcpTti = 20;
  static const defaultKcpUplinkCapacity = 12;
  static const defaultKcpDownlinkCapacity = 100;
  static const defaultKcpCongestion = true;
  static const defaultKcpReadBufferSize = 4;
  static const defaultKcpWriteBufferSize = 4;
  static const defaultXhttpPath = '/';
  static const defaultXhttpHost = '';
  static const defaultXhttpMode = 'auto';
  static const defaultFinalMaskType = 'header-dtls';
  static const defaultFinalMaskPassword = '';
  static const defaultFinalMaskDomain = '';

  // NAT Traversal - UPnP
  static const defaultUpnpLeaseDuration = 3600;
  static const defaultUpnpRetries = 3;
  static const defaultSsdpTimeout = 3;

  // NAT Traversal - UDP Relay
  static const defaultUseRelay = EnvConfig.SERVER_USE_RELAY;

  // WebRTC Transport
  static const defaultTransportMode = 'datachannel';
  static const defaultDisableIPv6 = false;
  static const defaultRateLimitUp = 0;
  static const defaultRateLimitDown = 0;

  // WebRTC Tuning
  static const defaultNumPeerConnections = 6;
  static const defaultNumChannels = 6;
  static const defaultSmuxStreamBuffer = 2048;    // KB
  static const defaultSmuxSessionBuffer = 8192;   // KB
  static const defaultSmuxFrameSize = 32768;      // bytes
  static const defaultSmuxKeepAlive = 10;         // seconds
  static const defaultSmuxKeepAliveTimeout = 300; // seconds
  static const defaultDcMaxBuffered = 2048;       // KB
  static const defaultDcLowMark = 512;            // KB

  // Padding
  static const defaultPaddingEnabled = false;
  static const defaultPaddingMax = 256;

  // SCTP/DTLS/ICE
  static const defaultSctpRecvBuffer = 8192;      // KB
  static const defaultSctpRTOMax = 2500;          // ms
  static const defaultUdpReadBuffer = 8192;       // KB
  static const defaultUdpWriteBuffer = 8192;      // KB
  static const defaultIceDisconnTimeout = 15000;  // ms
  static const defaultIceFailedTimeout = 25000;   // ms
  static const defaultIceKeepalive = 2000;        // ms
  static const defaultDtlsRetransmit = 100;       // ms
  static const defaultDtlsSkipVerify = true;
  static const defaultSctpZeroChecksum = true;
  static const defaultDisableCloseByDTLS = true;

  // Logging
  static const defaultMaskIPs = false;

  // UUID (empty = random on each start)
  static const defaultUuid = '';

  final int listenPort;
  final String stunServer;
  final String signalingUrl;
  final String discoveryUrl;
  final String natMethod; // auto, upnp, holepunch
  final bool discoveryEnabled;
  final String displayName;
  final String room;

  // Protocol & Transport
  final String protocol; // 'socks', 'vless'
  final String transport; // 'kcp', 'xhttp'

  // SOCKS
  final String socksAuth; // 'noauth', 'password'
  final String socksUsername;
  final String socksPassword;
  final bool socksUdp;

  // KCP
  final int kcpMtu; // 576-1460
  final int kcpTti; // 10-100
  final int kcpUplinkCapacity;
  final int kcpDownlinkCapacity;
  final bool kcpCongestion;
  final int kcpReadBufferSize;
  final int kcpWriteBufferSize;

  // xHTTP
  final String xhttpPath;
  final String xhttpHost;
  final String xhttpMode; // 'auto', 'packet-up', 'stream-up', 'stream-one'

  // FinalMask (UDP packet obfuscation for mKCP)
  final String finalMaskType; // 'none','header-srtp','header-dtls','header-wechat','header-utp','header-wireguard','header-dns','mkcp-original','mkcp-aes128gcm'
  final String finalMaskPassword; // for mkcp-aes128gcm
  final String finalMaskDomain; // for header-dns

  // NAT Traversal - UPnP
  final int upnpLeaseDuration;
  final int upnpRetries;
  final int ssdpTimeout;

  // NAT Traversal - UDP Relay
  final bool useRelay;

  // WebRTC Transport
  final String transportMode; // 'datachannel' or 'media'
  final bool disableIPv6;
  final int rateLimitUp; // bytes/sec, 0 = unlimited
  final int rateLimitDown; // bytes/sec, 0 = unlimited

  // WebRTC Tuning
  final int numPeerConnections;
  final int numChannels;
  final int smuxStreamBuffer;     // KB
  final int smuxSessionBuffer;    // KB
  final int smuxFrameSize;        // bytes
  final int smuxKeepAlive;        // seconds
  final int smuxKeepAliveTimeout; // seconds
  final int dcMaxBuffered;        // KB
  final int dcLowMark;            // KB

  // Padding
  final bool paddingEnabled;
  final int paddingMax;

  // SCTP/DTLS/ICE
  final int sctpRecvBuffer;      // KB
  final int sctpRTOMax;          // ms
  final int udpReadBuffer;       // KB
  final int udpWriteBuffer;      // KB
  final int iceDisconnTimeout;   // ms
  final int iceFailedTimeout;    // ms
  final int iceKeepalive;        // ms
  final int dtlsRetransmit;      // ms
  final bool dtlsSkipVerify;
  final bool sctpZeroChecksum;
  final bool disableCloseByDTLS;

  // Logging
  final bool maskIPs;

  // UUID
  final String uuid;

  const ServerSettings({
    this.listenPort = defaultListenPort,
    this.stunServer = defaultStunServer,
    this.signalingUrl = defaultSignalingUrl,
    this.discoveryUrl = defaultDiscoveryUrl,
    this.natMethod = defaultNatMethod,
    this.discoveryEnabled = true,
    this.displayName = '',
    this.room = '',
    this.protocol = defaultProtocol,
    this.transport = defaultTransport,
    this.socksAuth = defaultSocksAuth,
    this.socksUsername = '',
    this.socksPassword = '',
    this.socksUdp = defaultSocksUdp,
    this.kcpMtu = defaultKcpMtu,
    this.kcpTti = defaultKcpTti,
    this.kcpUplinkCapacity = defaultKcpUplinkCapacity,
    this.kcpDownlinkCapacity = defaultKcpDownlinkCapacity,
    this.kcpCongestion = defaultKcpCongestion,
    this.kcpReadBufferSize = defaultKcpReadBufferSize,
    this.kcpWriteBufferSize = defaultKcpWriteBufferSize,
    this.xhttpPath = defaultXhttpPath,
    this.xhttpHost = defaultXhttpHost,
    this.xhttpMode = defaultXhttpMode,
    this.finalMaskType = defaultFinalMaskType,
    this.finalMaskPassword = defaultFinalMaskPassword,
    this.finalMaskDomain = defaultFinalMaskDomain,
    this.upnpLeaseDuration = defaultUpnpLeaseDuration,
    this.upnpRetries = defaultUpnpRetries,
    this.ssdpTimeout = defaultSsdpTimeout,
    this.useRelay = defaultUseRelay,
    this.transportMode = defaultTransportMode,
    this.disableIPv6 = defaultDisableIPv6,
    this.rateLimitUp = defaultRateLimitUp,
    this.rateLimitDown = defaultRateLimitDown,
    this.numPeerConnections = defaultNumPeerConnections,
    this.numChannels = defaultNumChannels,
    this.smuxStreamBuffer = defaultSmuxStreamBuffer,
    this.smuxSessionBuffer = defaultSmuxSessionBuffer,
    this.smuxFrameSize = defaultSmuxFrameSize,
    this.smuxKeepAlive = defaultSmuxKeepAlive,
    this.smuxKeepAliveTimeout = defaultSmuxKeepAliveTimeout,
    this.dcMaxBuffered = defaultDcMaxBuffered,
    this.dcLowMark = defaultDcLowMark,
    this.paddingEnabled = defaultPaddingEnabled,
    this.paddingMax = defaultPaddingMax,
    this.sctpRecvBuffer = defaultSctpRecvBuffer,
    this.sctpRTOMax = defaultSctpRTOMax,
    this.udpReadBuffer = defaultUdpReadBuffer,
    this.udpWriteBuffer = defaultUdpWriteBuffer,
    this.iceDisconnTimeout = defaultIceDisconnTimeout,
    this.iceFailedTimeout = defaultIceFailedTimeout,
    this.iceKeepalive = defaultIceKeepalive,
    this.dtlsRetransmit = defaultDtlsRetransmit,
    this.dtlsSkipVerify = defaultDtlsSkipVerify,
    this.sctpZeroChecksum = defaultSctpZeroChecksum,
    this.disableCloseByDTLS = defaultDisableCloseByDTLS,
    this.maskIPs = defaultMaskIPs,
    this.uuid = defaultUuid,
  });

  ServerSettings copyWith({
    int? listenPort,
    String? stunServer,
    String? signalingUrl,
    String? discoveryUrl,
    String? natMethod,
    bool? discoveryEnabled,
    String? displayName,
    String? room,
    String? protocol,
    String? transport,
    String? socksAuth,
    String? socksUsername,
    String? socksPassword,
    bool? socksUdp,
    int? kcpMtu,
    int? kcpTti,
    int? kcpUplinkCapacity,
    int? kcpDownlinkCapacity,
    bool? kcpCongestion,
    int? kcpReadBufferSize,
    int? kcpWriteBufferSize,
    String? xhttpPath,
    String? xhttpHost,
    String? xhttpMode,
    String? finalMaskType,
    String? finalMaskPassword,
    String? finalMaskDomain,
    int? upnpLeaseDuration,
    int? upnpRetries,
    int? ssdpTimeout,
    bool? useRelay,
    String? transportMode,
    bool? disableIPv6,
    int? rateLimitUp,
    int? rateLimitDown,
    int? numPeerConnections,
    int? numChannels,
    int? smuxStreamBuffer,
    int? smuxSessionBuffer,
    int? smuxFrameSize,
    int? smuxKeepAlive,
    int? smuxKeepAliveTimeout,
    int? dcMaxBuffered,
    int? dcLowMark,
    bool? paddingEnabled,
    int? paddingMax,
    int? sctpRecvBuffer,
    int? sctpRTOMax,
    int? udpReadBuffer,
    int? udpWriteBuffer,
    int? iceDisconnTimeout,
    int? iceFailedTimeout,
    int? iceKeepalive,
    int? dtlsRetransmit,
    bool? dtlsSkipVerify,
    bool? sctpZeroChecksum,
    bool? disableCloseByDTLS,
    bool? maskIPs,
    String? uuid,
  }) {
    return ServerSettings(
      listenPort: listenPort ?? this.listenPort,
      stunServer: stunServer ?? this.stunServer,
      signalingUrl: signalingUrl ?? this.signalingUrl,
      discoveryUrl: discoveryUrl ?? this.discoveryUrl,
      natMethod: natMethod ?? this.natMethod,
      discoveryEnabled: discoveryEnabled ?? this.discoveryEnabled,
      displayName: displayName ?? this.displayName,
      room: room ?? this.room,
      protocol: protocol ?? this.protocol,
      transport: transport ?? this.transport,
      socksAuth: socksAuth ?? this.socksAuth,
      socksUsername: socksUsername ?? this.socksUsername,
      socksPassword: socksPassword ?? this.socksPassword,
      socksUdp: socksUdp ?? this.socksUdp,
      kcpMtu: kcpMtu ?? this.kcpMtu,
      kcpTti: kcpTti ?? this.kcpTti,
      kcpUplinkCapacity: kcpUplinkCapacity ?? this.kcpUplinkCapacity,
      kcpDownlinkCapacity: kcpDownlinkCapacity ?? this.kcpDownlinkCapacity,
      kcpCongestion: kcpCongestion ?? this.kcpCongestion,
      kcpReadBufferSize: kcpReadBufferSize ?? this.kcpReadBufferSize,
      kcpWriteBufferSize: kcpWriteBufferSize ?? this.kcpWriteBufferSize,
      xhttpPath: xhttpPath ?? this.xhttpPath,
      xhttpHost: xhttpHost ?? this.xhttpHost,
      xhttpMode: xhttpMode ?? this.xhttpMode,
      finalMaskType: finalMaskType ?? this.finalMaskType,
      finalMaskPassword: finalMaskPassword ?? this.finalMaskPassword,
      finalMaskDomain: finalMaskDomain ?? this.finalMaskDomain,
      upnpLeaseDuration: upnpLeaseDuration ?? this.upnpLeaseDuration,
      upnpRetries: upnpRetries ?? this.upnpRetries,
      ssdpTimeout: ssdpTimeout ?? this.ssdpTimeout,
      useRelay: useRelay ?? this.useRelay,
      transportMode: transportMode ?? this.transportMode,
      disableIPv6: disableIPv6 ?? this.disableIPv6,
      rateLimitUp: rateLimitUp ?? this.rateLimitUp,
      rateLimitDown: rateLimitDown ?? this.rateLimitDown,
      numPeerConnections: numPeerConnections ?? this.numPeerConnections,
      numChannels: numChannels ?? this.numChannels,
      smuxStreamBuffer: smuxStreamBuffer ?? this.smuxStreamBuffer,
      smuxSessionBuffer: smuxSessionBuffer ?? this.smuxSessionBuffer,
      smuxFrameSize: smuxFrameSize ?? this.smuxFrameSize,
      smuxKeepAlive: smuxKeepAlive ?? this.smuxKeepAlive,
      smuxKeepAliveTimeout: smuxKeepAliveTimeout ?? this.smuxKeepAliveTimeout,
      dcMaxBuffered: dcMaxBuffered ?? this.dcMaxBuffered,
      dcLowMark: dcLowMark ?? this.dcLowMark,
      paddingEnabled: paddingEnabled ?? this.paddingEnabled,
      paddingMax: paddingMax ?? this.paddingMax,
      sctpRecvBuffer: sctpRecvBuffer ?? this.sctpRecvBuffer,
      sctpRTOMax: sctpRTOMax ?? this.sctpRTOMax,
      udpReadBuffer: udpReadBuffer ?? this.udpReadBuffer,
      udpWriteBuffer: udpWriteBuffer ?? this.udpWriteBuffer,
      iceDisconnTimeout: iceDisconnTimeout ?? this.iceDisconnTimeout,
      iceFailedTimeout: iceFailedTimeout ?? this.iceFailedTimeout,
      iceKeepalive: iceKeepalive ?? this.iceKeepalive,
      dtlsRetransmit: dtlsRetransmit ?? this.dtlsRetransmit,
      dtlsSkipVerify: dtlsSkipVerify ?? this.dtlsSkipVerify,
      sctpZeroChecksum: sctpZeroChecksum ?? this.sctpZeroChecksum,
      disableCloseByDTLS: disableCloseByDTLS ?? this.disableCloseByDTLS,
      maskIPs: maskIPs ?? this.maskIPs,
      uuid: uuid ?? this.uuid,
    );
  }

  Map<String, dynamic> toJson() => {
    'listenPort': listenPort,
    'stunServer': stunServer,
    'signalingUrl': signalingUrl,
    'discoveryUrl': discoveryUrl,
    'natMethod': natMethod,
    'protocol': protocol,
    'transport': transport,
    'socksAuth': socksAuth,
    'socksUsername': socksUsername,
    'socksPassword': socksPassword,
    'socksUdp': socksUdp,
    'kcpMtu': kcpMtu,
    'kcpTti': kcpTti,
    'kcpUplinkCapacity': kcpUplinkCapacity,
    'kcpDownlinkCapacity': kcpDownlinkCapacity,
    'kcpCongestion': kcpCongestion,
    'kcpReadBufferSize': kcpReadBufferSize,
    'kcpWriteBufferSize': kcpWriteBufferSize,
    'xhttpPath': xhttpPath,
    'xhttpHost': xhttpHost,
    'xhttpMode': xhttpMode,
    'finalMaskType': finalMaskType,
    'finalMaskPassword': finalMaskPassword,
    'finalMaskDomain': finalMaskDomain,
    'upnpLeaseDuration': upnpLeaseDuration,
    'upnpRetries': upnpRetries,
    'ssdpTimeout': ssdpTimeout,
    'useRelay': useRelay,
    'transportMode': transportMode,
    'disableIPv6': disableIPv6,
    'rateLimitUp': rateLimitUp,
    'rateLimitDown': rateLimitDown,
    'numPeerConnections': numPeerConnections,
    'numChannels': numChannels,
    'smuxStreamBuffer': smuxStreamBuffer,
    'smuxSessionBuffer': smuxSessionBuffer,
    'smuxFrameSize': smuxFrameSize,
    'smuxKeepAlive': smuxKeepAlive,
    'smuxKeepAliveTimeout': smuxKeepAliveTimeout,
    'dcMaxBuffered': dcMaxBuffered,
    'dcLowMark': dcLowMark,
    'paddingEnabled': paddingEnabled,
    'paddingMax': paddingMax,
    'sctpRecvBuffer': sctpRecvBuffer,
    'sctpRTOMax': sctpRTOMax,
    'udpReadBuffer': udpReadBuffer,
    'udpWriteBuffer': udpWriteBuffer,
    'iceDisconnTimeout': iceDisconnTimeout,
    'iceFailedTimeout': iceFailedTimeout,
    'iceKeepalive': iceKeepalive,
    'dtlsRetransmit': dtlsRetransmit,
    'dtlsSkipVerify': dtlsSkipVerify,
    'sctpZeroChecksum': sctpZeroChecksum,
    'disableCloseByDTLS': disableCloseByDTLS,
    'maskIPs': maskIPs,
    'uuid': uuid,
  };

  factory ServerSettings.fromJson(Map<String, dynamic> json) {
    return ServerSettings(
      listenPort: json['listenPort'] as int? ?? defaultListenPort,
      stunServer: json['stunServer'] as String? ?? defaultStunServer,
      signalingUrl: json['signalingUrl'] as String? ?? defaultSignalingUrl,
      discoveryUrl: json['discoveryUrl'] as String? ?? defaultDiscoveryUrl,
      natMethod: json['natMethod'] as String? ?? defaultNatMethod,
      discoveryEnabled: json['discoveryEnabled'] as bool? ?? true,
      displayName: json['displayName'] as String? ?? '',
      room: json['room'] as String? ?? '',
      protocol: json['protocol'] as String? ?? defaultProtocol,
      transport: json['transport'] as String? ?? defaultTransport,
      socksAuth: json['socksAuth'] as String? ?? defaultSocksAuth,
      socksUsername: json['socksUsername'] as String? ?? '',
      socksPassword: json['socksPassword'] as String? ?? '',
      socksUdp: json['socksUdp'] as bool? ?? defaultSocksUdp,
      kcpMtu: json['kcpMtu'] as int? ?? defaultKcpMtu,
      kcpTti: json['kcpTti'] as int? ?? defaultKcpTti,
      kcpUplinkCapacity:
          json['kcpUplinkCapacity'] as int? ?? defaultKcpUplinkCapacity,
      kcpDownlinkCapacity:
          json['kcpDownlinkCapacity'] as int? ?? defaultKcpDownlinkCapacity,
      kcpCongestion: json['kcpCongestion'] as bool? ?? defaultKcpCongestion,
      kcpReadBufferSize:
          json['kcpReadBufferSize'] as int? ?? defaultKcpReadBufferSize,
      kcpWriteBufferSize:
          json['kcpWriteBufferSize'] as int? ?? defaultKcpWriteBufferSize,
      xhttpPath: json['xhttpPath'] as String? ?? defaultXhttpPath,
      xhttpHost: json['xhttpHost'] as String? ?? defaultXhttpHost,
      xhttpMode: json['xhttpMode'] as String? ?? defaultXhttpMode,
      finalMaskType:
          json['finalMaskType'] as String? ?? defaultFinalMaskType,
      finalMaskPassword:
          json['finalMaskPassword'] as String? ?? defaultFinalMaskPassword,
      finalMaskDomain:
          json['finalMaskDomain'] as String? ?? defaultFinalMaskDomain,
      upnpLeaseDuration:
          json['upnpLeaseDuration'] as int? ?? defaultUpnpLeaseDuration,
      upnpRetries: json['upnpRetries'] as int? ?? defaultUpnpRetries,
      ssdpTimeout: json['ssdpTimeout'] as int? ?? defaultSsdpTimeout,
      useRelay: json['useRelay'] as bool? ?? defaultUseRelay,
      transportMode:
          json['transportMode'] as String? ?? defaultTransportMode,
      disableIPv6: json['disableIPv6'] as bool? ?? defaultDisableIPv6,
      rateLimitUp: json['rateLimitUp'] as int? ?? defaultRateLimitUp,
      rateLimitDown: json['rateLimitDown'] as int? ?? defaultRateLimitDown,
      numPeerConnections: json['numPeerConnections'] as int? ?? defaultNumPeerConnections,
      numChannels: json['numChannels'] as int? ?? defaultNumChannels,
      smuxStreamBuffer: json['smuxStreamBuffer'] as int? ?? defaultSmuxStreamBuffer,
      smuxSessionBuffer: json['smuxSessionBuffer'] as int? ?? defaultSmuxSessionBuffer,
      smuxFrameSize: json['smuxFrameSize'] as int? ?? defaultSmuxFrameSize,
      smuxKeepAlive: json['smuxKeepAlive'] as int? ?? defaultSmuxKeepAlive,
      smuxKeepAliveTimeout: json['smuxKeepAliveTimeout'] as int? ?? defaultSmuxKeepAliveTimeout,
      dcMaxBuffered: json['dcMaxBuffered'] as int? ?? defaultDcMaxBuffered,
      dcLowMark: json['dcLowMark'] as int? ?? defaultDcLowMark,
      paddingEnabled: json['paddingEnabled'] as bool? ?? defaultPaddingEnabled,
      paddingMax: json['paddingMax'] as int? ?? defaultPaddingMax,
      sctpRecvBuffer: json['sctpRecvBuffer'] as int? ?? defaultSctpRecvBuffer,
      sctpRTOMax: json['sctpRTOMax'] as int? ?? defaultSctpRTOMax,
      udpReadBuffer: json['udpReadBuffer'] as int? ?? defaultUdpReadBuffer,
      udpWriteBuffer: json['udpWriteBuffer'] as int? ?? defaultUdpWriteBuffer,
      iceDisconnTimeout: json['iceDisconnTimeout'] as int? ?? defaultIceDisconnTimeout,
      iceFailedTimeout: json['iceFailedTimeout'] as int? ?? defaultIceFailedTimeout,
      iceKeepalive: json['iceKeepalive'] as int? ?? defaultIceKeepalive,
      dtlsRetransmit: json['dtlsRetransmit'] as int? ?? defaultDtlsRetransmit,
      dtlsSkipVerify: json['dtlsSkipVerify'] as bool? ?? defaultDtlsSkipVerify,
      sctpZeroChecksum: json['sctpZeroChecksum'] as bool? ?? defaultSctpZeroChecksum,
      disableCloseByDTLS: json['disableCloseByDTLS'] as bool? ?? defaultDisableCloseByDTLS,
      maskIPs: json['maskIPs'] as bool? ?? defaultMaskIPs,
      uuid: json['uuid'] as String? ?? defaultUuid,
    );
  }
}

class ClientSettings {
  static const defaultSocksPort = EnvConfig.CLIENT_SOCKS_PORT;
  static const defaultTunAddress = EnvConfig.CLIENT_TUN_ADDRESS;
  static const defaultMtu = EnvConfig.CLIENT_MTU;
  static const defaultDns1 = EnvConfig.CLIENT_DNS1;
  static const defaultDns2 = EnvConfig.CLIENT_DNS2;
  static const defaultStunServer = EnvConfig.STUN_SERVER;
  static const defaultSignalingUrl = EnvConfig.SIGNALING_URL;
  static const defaultDiscoveryUrl = EnvConfig.DISCOVERY_URL;
  // SCTP/DTLS/ICE
  static const defaultSctpRecvBuffer = 8192;      // KB
  static const defaultSctpRTOMax = 2500;          // ms
  static const defaultUdpReadBuffer = 8192;       // KB
  static const defaultUdpWriteBuffer = 8192;      // KB
  static const defaultIceDisconnTimeout = 15000;  // ms
  static const defaultIceFailedTimeout = 25000;   // ms
  static const defaultIceKeepalive = 2000;        // ms
  static const defaultDtlsRetransmit = 100;       // ms
  static const defaultDtlsSkipVerify = true;
  static const defaultSctpZeroChecksum = true;
  static const defaultDisableCloseByDTLS = true;

  // DNS privacy
  static const defaultAllowDirectDNS = EnvConfig.CLIENT_ALLOW_DIRECT_DNS;

  // Logging
  static const defaultMaskIPs = false;

  final int socksPort;
  final String tunAddress;
  final int mtu;
  final String dns1;
  final String dns2;
  final String stunServer;
  final String signalingUrl;
  final String discoveryUrl;
  final bool discoveryEnabled;
  final String roomFilter;

  // DNS privacy
  final bool allowDirectDNS;

  // SCTP/DTLS/ICE
  final int sctpRecvBuffer;      // KB
  final int sctpRTOMax;          // ms
  final int udpReadBuffer;       // KB
  final int udpWriteBuffer;      // KB
  final int iceDisconnTimeout;   // ms
  final int iceFailedTimeout;    // ms
  final int iceKeepalive;        // ms
  final int dtlsRetransmit;      // ms
  final bool dtlsSkipVerify;
  final bool sctpZeroChecksum;
  final bool disableCloseByDTLS;

  // Logging
  final bool maskIPs;

  const ClientSettings({
    this.socksPort = defaultSocksPort,
    this.tunAddress = defaultTunAddress,
    this.mtu = defaultMtu,
    this.dns1 = defaultDns1,
    this.dns2 = defaultDns2,
    this.stunServer = defaultStunServer,
    this.signalingUrl = defaultSignalingUrl,
    this.discoveryUrl = defaultDiscoveryUrl,
    this.discoveryEnabled = true,
    this.roomFilter = '',
    this.allowDirectDNS = defaultAllowDirectDNS,
    this.sctpRecvBuffer = defaultSctpRecvBuffer,
    this.sctpRTOMax = defaultSctpRTOMax,
    this.udpReadBuffer = defaultUdpReadBuffer,
    this.udpWriteBuffer = defaultUdpWriteBuffer,
    this.iceDisconnTimeout = defaultIceDisconnTimeout,
    this.iceFailedTimeout = defaultIceFailedTimeout,
    this.iceKeepalive = defaultIceKeepalive,
    this.dtlsRetransmit = defaultDtlsRetransmit,
    this.dtlsSkipVerify = defaultDtlsSkipVerify,
    this.sctpZeroChecksum = defaultSctpZeroChecksum,
    this.disableCloseByDTLS = defaultDisableCloseByDTLS,
    this.maskIPs = defaultMaskIPs,
  });

  ClientSettings copyWith({
    int? socksPort,
    String? tunAddress,
    int? mtu,
    String? dns1,
    String? dns2,
    String? stunServer,
    String? signalingUrl,
    String? discoveryUrl,
    bool? discoveryEnabled,
    String? roomFilter,
    bool? allowDirectDNS,
    int? sctpRecvBuffer,
    int? sctpRTOMax,
    int? udpReadBuffer,
    int? udpWriteBuffer,
    int? iceDisconnTimeout,
    int? iceFailedTimeout,
    int? iceKeepalive,
    int? dtlsRetransmit,
    bool? dtlsSkipVerify,
    bool? sctpZeroChecksum,
    bool? disableCloseByDTLS,
    bool? maskIPs,
  }) {
    return ClientSettings(
      socksPort: socksPort ?? this.socksPort,
      tunAddress: tunAddress ?? this.tunAddress,
      mtu: mtu ?? this.mtu,
      dns1: dns1 ?? this.dns1,
      dns2: dns2 ?? this.dns2,
      stunServer: stunServer ?? this.stunServer,
      signalingUrl: signalingUrl ?? this.signalingUrl,
      discoveryUrl: discoveryUrl ?? this.discoveryUrl,
      discoveryEnabled: discoveryEnabled ?? this.discoveryEnabled,
      roomFilter: roomFilter ?? this.roomFilter,
      allowDirectDNS: allowDirectDNS ?? this.allowDirectDNS,
      sctpRecvBuffer: sctpRecvBuffer ?? this.sctpRecvBuffer,
      sctpRTOMax: sctpRTOMax ?? this.sctpRTOMax,
      udpReadBuffer: udpReadBuffer ?? this.udpReadBuffer,
      udpWriteBuffer: udpWriteBuffer ?? this.udpWriteBuffer,
      iceDisconnTimeout: iceDisconnTimeout ?? this.iceDisconnTimeout,
      iceFailedTimeout: iceFailedTimeout ?? this.iceFailedTimeout,
      iceKeepalive: iceKeepalive ?? this.iceKeepalive,
      dtlsRetransmit: dtlsRetransmit ?? this.dtlsRetransmit,
      dtlsSkipVerify: dtlsSkipVerify ?? this.dtlsSkipVerify,
      sctpZeroChecksum: sctpZeroChecksum ?? this.sctpZeroChecksum,
      disableCloseByDTLS: disableCloseByDTLS ?? this.disableCloseByDTLS,
      maskIPs: maskIPs ?? this.maskIPs,
    );
  }

  Map<String, dynamic> toJson() => {
    'socksPort': socksPort,
    'tunAddress': tunAddress,
    'mtu': mtu,
    'dns1': dns1,
    'dns2': dns2,
    'stunServer': stunServer,
    'signalingUrl': signalingUrl,
    'discoveryUrl': discoveryUrl,
    'discoveryEnabled': discoveryEnabled,
    'roomFilter': roomFilter,
    'allowDirectDNS': allowDirectDNS,
    'vpnSessionName': EnvConfig.VPN_SESSION_NAME,
    'sctpRecvBuffer': sctpRecvBuffer,
    'sctpRTOMax': sctpRTOMax,
    'udpReadBuffer': udpReadBuffer,
    'udpWriteBuffer': udpWriteBuffer,
    'iceDisconnTimeout': iceDisconnTimeout,
    'iceFailedTimeout': iceFailedTimeout,
    'iceKeepalive': iceKeepalive,
    'dtlsRetransmit': dtlsRetransmit,
    'dtlsSkipVerify': dtlsSkipVerify,
    'sctpZeroChecksum': sctpZeroChecksum,
    'disableCloseByDTLS': disableCloseByDTLS,
    'maskIPs': maskIPs,
  };

  factory ClientSettings.fromJson(Map<String, dynamic> json) {
    return ClientSettings(
      socksPort: json['socksPort'] as int? ?? defaultSocksPort,
      tunAddress: json['tunAddress'] as String? ?? defaultTunAddress,
      mtu: json['mtu'] as int? ?? defaultMtu,
      dns1: json['dns1'] as String? ?? defaultDns1,
      dns2: json['dns2'] as String? ?? defaultDns2,
      stunServer: json['stunServer'] as String? ?? defaultStunServer,
      signalingUrl: json['signalingUrl'] as String? ?? defaultSignalingUrl,
      discoveryUrl: json['discoveryUrl'] as String? ?? defaultDiscoveryUrl,
      discoveryEnabled: json['discoveryEnabled'] as bool? ?? true,
      roomFilter: json['roomFilter'] as String? ?? '',
      allowDirectDNS: json['allowDirectDNS'] as bool? ?? defaultAllowDirectDNS,
      sctpRecvBuffer: json['sctpRecvBuffer'] as int? ?? defaultSctpRecvBuffer,
      sctpRTOMax: json['sctpRTOMax'] as int? ?? defaultSctpRTOMax,
      udpReadBuffer: json['udpReadBuffer'] as int? ?? defaultUdpReadBuffer,
      udpWriteBuffer: json['udpWriteBuffer'] as int? ?? defaultUdpWriteBuffer,
      iceDisconnTimeout: json['iceDisconnTimeout'] as int? ?? defaultIceDisconnTimeout,
      iceFailedTimeout: json['iceFailedTimeout'] as int? ?? defaultIceFailedTimeout,
      iceKeepalive: json['iceKeepalive'] as int? ?? defaultIceKeepalive,
      dtlsRetransmit: json['dtlsRetransmit'] as int? ?? defaultDtlsRetransmit,
      dtlsSkipVerify: json['dtlsSkipVerify'] as bool? ?? defaultDtlsSkipVerify,
      sctpZeroChecksum: json['sctpZeroChecksum'] as bool? ?? defaultSctpZeroChecksum,
      disableCloseByDTLS: json['disableCloseByDTLS'] as bool? ?? defaultDisableCloseByDTLS,
      maskIPs: json['maskIPs'] as bool? ?? defaultMaskIPs,
    );
  }
}
