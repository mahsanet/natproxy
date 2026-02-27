package config

import "natproxy/cli/internal/types"

const (
	DefaultSignalingURL = "http://[IP]:5601"
	DefaultDiscoveryURL = "http://[IP]:5602"
	DefaultStunServer   = "stun.l.google.com:19302"
	DefaultStunServer2  = "stun1.l.google.com:19302"
	DefaultListenPort   = 10853
	DefaultSocksPort    = 10808
)

func DefaultServerConfig() types.ServerConfig {
	return types.ServerConfig{
		ListenPort:        DefaultListenPort,
		StunServer:        DefaultStunServer,
		SignalingURL:      DefaultSignalingURL,
		NatMethod:         "auto",
		Protocol:          "vless",
		Transport:         "xhttp",
		SocksAuth:         "noauth",
		SocksUDP:          true,
		KcpMTU:            1350,
		KcpTTI:            20,
		KcpUplinkCapacity: 12,
		KcpDownlinkCapacity: 100,
		KcpCongestion:       true,
		KcpReadBufferSize:   4,
		KcpWriteBufferSize:  4,
		XhttpPath:           "/",
		XhttpMode:           "auto",
		FinalMaskType:       "header-dtls",
		UpnpLeaseDuration:   3600,
		UpnpRetries:         3,
		SsdpTimeout:         3,
		DiscoveryEnabled:     true,
		Obfuscation:          true,
		PaddingEnabled:       false,
		PaddingMax:           256,
		NumPeerConnections:   6,
		NumChannels:          6,
		SmuxStreamBuffer:     2048,
		SmuxSessionBuffer:    8192,
		SmuxFrameSize:        32768,
		SmuxKeepAlive:        10,
		SmuxKeepAliveTimeout: 300,
		DCMaxBuffered:        2048,
		DCLowMark:            512,
		SCTPRecvBuffer:       8192,
		SCTPRTOMax:           2500,
		SCTPZeroChecksum:     true,
		DTLSRetransmit:       100,
		DTLSSkipVerify:       true,
		DisableCloseByDTLS:   true,
		ICEDisconnTimeout:    15000,
		ICEFailedTimeout:     25000,
		ICEKeepalive:         2000,
		UDPReadBuffer:        8192,
		UDPWriteBuffer:       8192,
		MaskIPs:              false,
	}
}

func DefaultClientConfig() types.ClientConfig {
	return types.ClientConfig{
		SocksPort:          DefaultSocksPort,
		StunServer:         DefaultStunServer,
		SignalingURL:       DefaultSignalingURL,
		SCTPRecvBuffer:     8192,
		SCTPRTOMax:         2500,
		SCTPZeroChecksum:   true,
		DTLSRetransmit:     100,
		DTLSSkipVerify:     true,
		DisableCloseByDTLS: true,
		ICEDisconnTimeout:  15000,
		ICEFailedTimeout:   25000,
		ICEKeepalive:       2000,
		UDPReadBuffer:      8192,
		UDPWriteBuffer:     8192,
		MaskIPs:            false,
	}
}
