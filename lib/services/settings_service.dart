import 'package:shared_preferences/shared_preferences.dart';
import '../models/settings_model.dart';

class SettingsService {
  static final SettingsService _instance = SettingsService._();
  factory SettingsService() => _instance;
  SettingsService._();

  SharedPreferences? _prefs;

  Future<SharedPreferences> get _preferences async {
    _prefs ??= await SharedPreferences.getInstance();
    return _prefs!;
  }

  // Server settings

  Future<ServerSettings> loadServerSettings() async {
    final prefs = await _preferences;
    return ServerSettings(
      listenPort:
          prefs.getInt('server_listenPort') ?? ServerSettings.defaultListenPort,
      stunServer:
          prefs.getString('server_stunServer') ??
          ServerSettings.defaultStunServer,
      signalingUrl:
          prefs.getString('server_signalingUrl') ??
          ServerSettings.defaultSignalingUrl,
      discoveryUrl:
          prefs.getString('server_discoveryUrl') ??
          ServerSettings.defaultDiscoveryUrl,
      natMethod:
          prefs.getString('server_natMethod') ??
          ServerSettings.defaultNatMethod,
      discoveryEnabled: prefs.getBool('server_discoveryEnabled') ?? true,
      displayName: prefs.getString('server_displayName') ?? '',
      room: prefs.getString('server_room') ?? '',
      protocol:
          prefs.getString('server_protocol') ?? ServerSettings.defaultProtocol,
      transport:
          prefs.getString('server_transport') ??
          ServerSettings.defaultTransport,
      socksAuth:
          prefs.getString('server_socksAuth') ??
          ServerSettings.defaultSocksAuth,
      socksUsername: prefs.getString('server_socksUsername') ?? '',
      socksPassword: prefs.getString('server_socksPassword') ?? '',
      socksUdp:
          prefs.getBool('server_socksUdp') ?? ServerSettings.defaultSocksUdp,
      kcpMtu: prefs.getInt('server_kcpMtu') ?? ServerSettings.defaultKcpMtu,
      kcpTti: prefs.getInt('server_kcpTti') ?? ServerSettings.defaultKcpTti,
      kcpUplinkCapacity:
          prefs.getInt('server_kcpUplinkCapacity') ??
          ServerSettings.defaultKcpUplinkCapacity,
      kcpDownlinkCapacity:
          prefs.getInt('server_kcpDownlinkCapacity') ??
          ServerSettings.defaultKcpDownlinkCapacity,
      kcpCongestion:
          prefs.getBool('server_kcpCongestion') ??
          ServerSettings.defaultKcpCongestion,
      kcpReadBufferSize:
          prefs.getInt('server_kcpReadBufferSize') ??
          ServerSettings.defaultKcpReadBufferSize,
      kcpWriteBufferSize:
          prefs.getInt('server_kcpWriteBufferSize') ??
          ServerSettings.defaultKcpWriteBufferSize,
      xhttpPath:
          prefs.getString('server_xhttpPath') ??
          ServerSettings.defaultXhttpPath,
      xhttpHost:
          prefs.getString('server_xhttpHost') ??
          ServerSettings.defaultXhttpHost,
      xhttpMode:
          prefs.getString('server_xhttpMode') ??
          ServerSettings.defaultXhttpMode,
      finalMaskType:
          prefs.getString('server_finalMaskType') ??
          ServerSettings.defaultFinalMaskType,
      finalMaskPassword:
          prefs.getString('server_finalMaskPassword') ??
          ServerSettings.defaultFinalMaskPassword,
      finalMaskDomain:
          prefs.getString('server_finalMaskDomain') ??
          ServerSettings.defaultFinalMaskDomain,
      upnpLeaseDuration:
          prefs.getInt('server_upnpLeaseDuration') ??
          ServerSettings.defaultUpnpLeaseDuration,
      upnpRetries:
          prefs.getInt('server_upnpRetries') ??
          ServerSettings.defaultUpnpRetries,
      ssdpTimeout:
          prefs.getInt('server_ssdpTimeout') ??
          ServerSettings.defaultSsdpTimeout,
      useRelay:
          prefs.getBool('server_useRelay') ?? ServerSettings.defaultUseRelay,
      transportMode:
          prefs.getString('server_transportMode') ??
          ServerSettings.defaultTransportMode,
      disableIPv6:
          prefs.getBool('server_disableIPv6') ??
          ServerSettings.defaultDisableIPv6,
      rateLimitUp:
          prefs.getInt('server_rateLimitUp') ??
          ServerSettings.defaultRateLimitUp,
      rateLimitDown:
          prefs.getInt('server_rateLimitDown') ??
          ServerSettings.defaultRateLimitDown,
      numPeerConnections:
          prefs.getInt('server_numPeerConnections') ??
          ServerSettings.defaultNumPeerConnections,
      numChannels:
          prefs.getInt('server_numChannels') ??
          ServerSettings.defaultNumChannels,
      smuxStreamBuffer:
          prefs.getInt('server_smuxStreamBuffer') ??
          ServerSettings.defaultSmuxStreamBuffer,
      smuxSessionBuffer:
          prefs.getInt('server_smuxSessionBuffer') ??
          ServerSettings.defaultSmuxSessionBuffer,
      smuxFrameSize:
          prefs.getInt('server_smuxFrameSize') ??
          ServerSettings.defaultSmuxFrameSize,
      smuxKeepAlive:
          prefs.getInt('server_smuxKeepAlive') ??
          ServerSettings.defaultSmuxKeepAlive,
      smuxKeepAliveTimeout:
          prefs.getInt('server_smuxKeepAliveTimeout') ??
          ServerSettings.defaultSmuxKeepAliveTimeout,
      dcMaxBuffered:
          prefs.getInt('server_dcMaxBuffered') ??
          ServerSettings.defaultDcMaxBuffered,
      dcLowMark:
          prefs.getInt('server_dcLowMark') ??
          ServerSettings.defaultDcLowMark,
      paddingEnabled:
          prefs.getBool('server_paddingEnabled') ??
          ServerSettings.defaultPaddingEnabled,
      paddingMax:
          prefs.getInt('server_paddingMax') ??
          ServerSettings.defaultPaddingMax,
      sctpRecvBuffer:
          prefs.getInt('server_sctpRecvBuffer') ??
          ServerSettings.defaultSctpRecvBuffer,
      sctpRTOMax:
          prefs.getInt('server_sctpRTOMax') ??
          ServerSettings.defaultSctpRTOMax,
      udpReadBuffer:
          prefs.getInt('server_udpReadBuffer') ??
          ServerSettings.defaultUdpReadBuffer,
      udpWriteBuffer:
          prefs.getInt('server_udpWriteBuffer') ??
          ServerSettings.defaultUdpWriteBuffer,
      iceDisconnTimeout:
          prefs.getInt('server_iceDisconnTimeout') ??
          ServerSettings.defaultIceDisconnTimeout,
      iceFailedTimeout:
          prefs.getInt('server_iceFailedTimeout') ??
          ServerSettings.defaultIceFailedTimeout,
      iceKeepalive:
          prefs.getInt('server_iceKeepalive') ??
          ServerSettings.defaultIceKeepalive,
      dtlsRetransmit:
          prefs.getInt('server_dtlsRetransmit') ??
          ServerSettings.defaultDtlsRetransmit,
      dtlsSkipVerify:
          prefs.getBool('server_dtlsSkipVerify') ??
          ServerSettings.defaultDtlsSkipVerify,
      sctpZeroChecksum:
          prefs.getBool('server_sctpZeroChecksum') ??
          ServerSettings.defaultSctpZeroChecksum,
      disableCloseByDTLS:
          prefs.getBool('server_disableCloseByDTLS') ??
          ServerSettings.defaultDisableCloseByDTLS,
      maskIPs:
          prefs.getBool('server_maskIPs') ??
          ServerSettings.defaultMaskIPs,
      uuid:
          prefs.getString('server_uuid') ??
          ServerSettings.defaultUuid,
    );
  }

  Future<void> saveServerSettings(ServerSettings settings) async {
    final prefs = await _preferences;
    await prefs.setInt('server_listenPort', settings.listenPort);
    await prefs.setString('server_stunServer', settings.stunServer);
    await prefs.setString('server_signalingUrl', settings.signalingUrl);
    await prefs.setString('server_discoveryUrl', settings.discoveryUrl);
    await prefs.setString('server_natMethod', settings.natMethod);
    await prefs.setBool('server_discoveryEnabled', settings.discoveryEnabled);
    await prefs.setString('server_displayName', settings.displayName);
    await prefs.setString('server_room', settings.room);
    await prefs.setString('server_protocol', settings.protocol);
    await prefs.setString('server_transport', settings.transport);
    await prefs.setString('server_socksAuth', settings.socksAuth);
    await prefs.setString('server_socksUsername', settings.socksUsername);
    await prefs.setString('server_socksPassword', settings.socksPassword);
    await prefs.setBool('server_socksUdp', settings.socksUdp);
    await prefs.setInt('server_kcpMtu', settings.kcpMtu);
    await prefs.setInt('server_kcpTti', settings.kcpTti);
    await prefs.setInt('server_kcpUplinkCapacity', settings.kcpUplinkCapacity);
    await prefs.setInt(
      'server_kcpDownlinkCapacity',
      settings.kcpDownlinkCapacity,
    );
    await prefs.setBool('server_kcpCongestion', settings.kcpCongestion);
    await prefs.setInt('server_kcpReadBufferSize', settings.kcpReadBufferSize);
    await prefs.setInt(
      'server_kcpWriteBufferSize',
      settings.kcpWriteBufferSize,
    );
    await prefs.setString('server_xhttpPath', settings.xhttpPath);
    await prefs.setString('server_xhttpHost', settings.xhttpHost);
    await prefs.setString('server_xhttpMode', settings.xhttpMode);
    await prefs.setString('server_finalMaskType', settings.finalMaskType);
    await prefs.setString(
      'server_finalMaskPassword',
      settings.finalMaskPassword,
    );
    await prefs.setString('server_finalMaskDomain', settings.finalMaskDomain);
    await prefs.setInt('server_upnpLeaseDuration', settings.upnpLeaseDuration);
    await prefs.setInt('server_upnpRetries', settings.upnpRetries);
    await prefs.setInt('server_ssdpTimeout', settings.ssdpTimeout);
    await prefs.setBool('server_useRelay', settings.useRelay);
    await prefs.setString('server_transportMode', settings.transportMode);
    await prefs.setBool('server_disableIPv6', settings.disableIPv6);
    await prefs.setInt('server_rateLimitUp', settings.rateLimitUp);
    await prefs.setInt('server_rateLimitDown', settings.rateLimitDown);
    await prefs.setInt('server_numPeerConnections', settings.numPeerConnections);
    await prefs.setInt('server_numChannels', settings.numChannels);
    await prefs.setInt('server_smuxStreamBuffer', settings.smuxStreamBuffer);
    await prefs.setInt('server_smuxSessionBuffer', settings.smuxSessionBuffer);
    await prefs.setInt('server_smuxFrameSize', settings.smuxFrameSize);
    await prefs.setInt('server_smuxKeepAlive', settings.smuxKeepAlive);
    await prefs.setInt('server_smuxKeepAliveTimeout', settings.smuxKeepAliveTimeout);
    await prefs.setInt('server_dcMaxBuffered', settings.dcMaxBuffered);
    await prefs.setInt('server_dcLowMark', settings.dcLowMark);
    await prefs.setBool('server_paddingEnabled', settings.paddingEnabled);
    await prefs.setInt('server_paddingMax', settings.paddingMax);
    await prefs.setInt('server_sctpRecvBuffer', settings.sctpRecvBuffer);
    await prefs.setInt('server_sctpRTOMax', settings.sctpRTOMax);
    await prefs.setInt('server_udpReadBuffer', settings.udpReadBuffer);
    await prefs.setInt('server_udpWriteBuffer', settings.udpWriteBuffer);
    await prefs.setInt('server_iceDisconnTimeout', settings.iceDisconnTimeout);
    await prefs.setInt('server_iceFailedTimeout', settings.iceFailedTimeout);
    await prefs.setInt('server_iceKeepalive', settings.iceKeepalive);
    await prefs.setInt('server_dtlsRetransmit', settings.dtlsRetransmit);
    await prefs.setBool('server_dtlsSkipVerify', settings.dtlsSkipVerify);
    await prefs.setBool('server_sctpZeroChecksum', settings.sctpZeroChecksum);
    await prefs.setBool('server_disableCloseByDTLS', settings.disableCloseByDTLS);
    await prefs.setBool('server_maskIPs', settings.maskIPs);
    await prefs.setString('server_uuid', settings.uuid);
  }

  // Client settings

  Future<ClientSettings> loadClientSettings() async {
    final prefs = await _preferences;
    return ClientSettings(
      socksPort:
          prefs.getInt('client_socksPort') ?? ClientSettings.defaultSocksPort,
      tunAddress:
          prefs.getString('client_tunAddress') ??
          ClientSettings.defaultTunAddress,
      mtu: prefs.getInt('client_mtu') ?? ClientSettings.defaultMtu,
      dns1: prefs.getString('client_dns1') ?? ClientSettings.defaultDns1,
      dns2: prefs.getString('client_dns2') ?? ClientSettings.defaultDns2,
      stunServer:
          prefs.getString('client_stunServer') ??
          ClientSettings.defaultStunServer,
      signalingUrl:
          prefs.getString('client_signalingUrl') ??
          ClientSettings.defaultSignalingUrl,
      discoveryUrl:
          prefs.getString('client_discoveryUrl') ??
          ClientSettings.defaultDiscoveryUrl,
      discoveryEnabled: prefs.getBool('client_discoveryEnabled') ?? true,
      roomFilter: prefs.getString('client_roomFilter') ?? '',
      allowDirectDNS:
          prefs.getBool('client_allowDirectDNS') ??
          ClientSettings.defaultAllowDirectDNS,
      sctpRecvBuffer:
          prefs.getInt('client_sctpRecvBuffer') ??
          ClientSettings.defaultSctpRecvBuffer,
      sctpRTOMax:
          prefs.getInt('client_sctpRTOMax') ??
          ClientSettings.defaultSctpRTOMax,
      udpReadBuffer:
          prefs.getInt('client_udpReadBuffer') ??
          ClientSettings.defaultUdpReadBuffer,
      udpWriteBuffer:
          prefs.getInt('client_udpWriteBuffer') ??
          ClientSettings.defaultUdpWriteBuffer,
      iceDisconnTimeout:
          prefs.getInt('client_iceDisconnTimeout') ??
          ClientSettings.defaultIceDisconnTimeout,
      iceFailedTimeout:
          prefs.getInt('client_iceFailedTimeout') ??
          ClientSettings.defaultIceFailedTimeout,
      iceKeepalive:
          prefs.getInt('client_iceKeepalive') ??
          ClientSettings.defaultIceKeepalive,
      dtlsRetransmit:
          prefs.getInt('client_dtlsRetransmit') ??
          ClientSettings.defaultDtlsRetransmit,
      dtlsSkipVerify:
          prefs.getBool('client_dtlsSkipVerify') ??
          ClientSettings.defaultDtlsSkipVerify,
      sctpZeroChecksum:
          prefs.getBool('client_sctpZeroChecksum') ??
          ClientSettings.defaultSctpZeroChecksum,
      disableCloseByDTLS:
          prefs.getBool('client_disableCloseByDTLS') ??
          ClientSettings.defaultDisableCloseByDTLS,
      maskIPs:
          prefs.getBool('client_maskIPs') ??
          ClientSettings.defaultMaskIPs,
    );
  }

  Future<void> saveClientSettings(ClientSettings settings) async {
    final prefs = await _preferences;
    await prefs.setInt('client_socksPort', settings.socksPort);
    await prefs.setString('client_tunAddress', settings.tunAddress);
    await prefs.setInt('client_mtu', settings.mtu);
    await prefs.setString('client_dns1', settings.dns1);
    await prefs.setString('client_dns2', settings.dns2);
    await prefs.setString('client_stunServer', settings.stunServer);
    await prefs.setString('client_signalingUrl', settings.signalingUrl);
    await prefs.setString('client_discoveryUrl', settings.discoveryUrl);
    await prefs.setBool('client_discoveryEnabled', settings.discoveryEnabled);
    await prefs.setString('client_roomFilter', settings.roomFilter);
    await prefs.setBool('client_allowDirectDNS', settings.allowDirectDNS);
    await prefs.setInt('client_sctpRecvBuffer', settings.sctpRecvBuffer);
    await prefs.setInt('client_sctpRTOMax', settings.sctpRTOMax);
    await prefs.setInt('client_udpReadBuffer', settings.udpReadBuffer);
    await prefs.setInt('client_udpWriteBuffer', settings.udpWriteBuffer);
    await prefs.setInt('client_iceDisconnTimeout', settings.iceDisconnTimeout);
    await prefs.setInt('client_iceFailedTimeout', settings.iceFailedTimeout);
    await prefs.setInt('client_iceKeepalive', settings.iceKeepalive);
    await prefs.setInt('client_dtlsRetransmit', settings.dtlsRetransmit);
    await prefs.setBool('client_dtlsSkipVerify', settings.dtlsSkipVerify);
    await prefs.setBool('client_sctpZeroChecksum', settings.sctpZeroChecksum);
    await prefs.setBool('client_disableCloseByDTLS', settings.disableCloseByDTLS);
    await prefs.setBool('client_maskIPs', settings.maskIPs);
  }
}
