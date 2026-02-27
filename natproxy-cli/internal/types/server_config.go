package types

import "encoding/json"

type ServerConfig struct {
	ListenPort   int    `json:"listenPort"`
	StunServer   string `json:"stunServer"`
	SignalingURL  string `json:"signalingUrl"`
	DiscoveryURL string `json:"discoveryUrl"`
	NatMethod    string `json:"natMethod"`
	UseRelay     bool   `json:"useRelay"`

	Protocol  string `json:"protocol"`
	Transport string `json:"transport"`

	SocksAuth     string `json:"socksAuth"`
	SocksUsername string `json:"socksUsername"`
	SocksPassword string `json:"socksPassword"`
	SocksUDP      bool   `json:"socksUdp"`

	KcpMTU              int  `json:"kcpMtu"`
	KcpTTI              int  `json:"kcpTti"`
	KcpUplinkCapacity   int  `json:"kcpUplinkCapacity"`
	KcpDownlinkCapacity int  `json:"kcpDownlinkCapacity"`
	KcpCongestion       bool `json:"kcpCongestion"`
	KcpReadBufferSize   int  `json:"kcpReadBufferSize"`
	KcpWriteBufferSize  int  `json:"kcpWriteBufferSize"`

	XhttpPath string `json:"xhttpPath"`
	XhttpHost string `json:"xhttpHost"`
	XhttpMode string `json:"xhttpMode"`

	FinalMaskType     string `json:"finalMaskType"`
	FinalMaskPassword string `json:"finalMaskPassword"`
	FinalMaskDomain   string `json:"finalMaskDomain"`

	UpnpLeaseDuration int `json:"upnpLeaseDuration"`
	UpnpRetries       int `json:"upnpRetries"`
	SsdpTimeout       int `json:"ssdpTimeout"`

	DiscoveryEnabled bool   `json:"discoveryEnabled"`
	DiscoveryName    string `json:"discoveryName"`
	DiscoveryRoom    string `json:"discoveryRoom"`

	// WebRTC transport
	TransportMode string `json:"transportMode"` // "datachannel" or "media"
	Obfuscation   bool   `json:"obfuscation"`   // UDP obfuscation (default true)
	DisableIPv6   bool   `json:"disableIPv6"`
	RateLimitUp   int64  `json:"rateLimitUp"`
	RateLimitDown int64  `json:"rateLimitDown"`

	// Padding
	PaddingEnabled bool `json:"paddingEnabled"`
	PaddingMax     int  `json:"paddingMax"`

	// WebRTC channels & smux
	NumPeerConnections   int `json:"numPeerConnections"`
	NumChannels          int `json:"numChannels"`
	SmuxStreamBuffer     int `json:"smuxStreamBuffer"`
	SmuxSessionBuffer    int `json:"smuxSessionBuffer"`
	SmuxFrameSize        int `json:"smuxFrameSize"`
	SmuxKeepAlive        int `json:"smuxKeepAlive"`
	SmuxKeepAliveTimeout int `json:"smuxKeepAliveTimeout"`

	// Data channel backpressure
	DCMaxBuffered int `json:"dcMaxBuffered"`
	DCLowMark     int `json:"dcLowMark"`

	// SCTP
	SCTPRecvBuffer   int  `json:"sctpRecvBuffer"`
	SCTPRTOMax       int  `json:"sctpRTOMax"`
	SCTPZeroChecksum bool `json:"sctpZeroChecksum"`

	// DTLS
	DTLSRetransmit     int  `json:"dtlsRetransmit"`
	DTLSSkipVerify     bool `json:"dtlsSkipVerify"`
	DisableCloseByDTLS bool `json:"disableCloseByDTLS"`

	// ICE
	ICEDisconnTimeout int `json:"iceDisconnTimeout"`
	ICEFailedTimeout  int `json:"iceFailedTimeout"`
	ICEKeepalive      int `json:"iceKeepalive"`

	// UDP buffers
	UDPReadBuffer  int `json:"udpReadBuffer"`
	UDPWriteBuffer int `json:"udpWriteBuffer"`

	// Logging
	MaskIPs bool `json:"maskIPs"`

	// UUID (empty = random)
	UUID string `json:"uuid"`
}

// BuildProxySettingsJSON constructs the proxySettings JSON from server config fields.
func (cfg *ServerConfig) BuildProxySettingsJSON() string {
	ps := map[string]interface{}{
		"protocol":            cfg.Protocol,
		"transport":           cfg.Transport,
		"socksAuth":           cfg.SocksAuth,
		"socksUsername":       cfg.SocksUsername,
		"socksPassword":       cfg.SocksPassword,
		"socksUdp":            cfg.SocksUDP,
		"kcpMtu":              cfg.KcpMTU,
		"kcpTti":              cfg.KcpTTI,
		"kcpUplinkCapacity":   cfg.KcpUplinkCapacity,
		"kcpDownlinkCapacity": cfg.KcpDownlinkCapacity,
		"kcpCongestion":       cfg.KcpCongestion,
		"kcpReadBufferSize":   cfg.KcpReadBufferSize,
		"kcpWriteBufferSize":  cfg.KcpWriteBufferSize,
		"xhttpPath":           cfg.XhttpPath,
		"xhttpHost":           cfg.XhttpHost,
		"xhttpMode":           cfg.XhttpMode,
		"finalMaskType":       cfg.FinalMaskType,
		"finalMaskPassword":   cfg.FinalMaskPassword,
		"finalMaskDomain":     cfg.FinalMaskDomain,
	}
	data, _ := json.Marshal(ps)
	return string(data)
}
