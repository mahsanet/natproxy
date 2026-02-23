package webrtc

// PeerConfig holds tunable settings for the WebRTC PeerConnection's
// SettingEngine (SCTP, DTLS, ICE, UDP buffers). These are independent
// per side — they don't need to match between server and client.
type PeerConfig struct {
	SCTPRecvBuffer     int   // KB, default 8192
	SCTPRTOMax         int   // ms, default 2500
	UDPReadBuffer      int   // KB, default 8192
	UDPWriteBuffer     int   // KB, default 8192
	ICEDisconnTimeout  int   // ms, default 15000
	ICEFailedTimeout   int   // ms, default 25000
	ICEKeepalive       int   // ms, default 2000
	DTLSRetransmit     int   // ms, default 100
	DTLSSkipVerify     *bool // default true
	SCTPZeroChecksum   *bool // default true
	DisableCloseByDTLS *bool // default true
}

func boolPtr(v bool) *bool { return &v }

// applyPeerDefaults fills zero-valued PeerConfig fields with defaults.
func applyPeerDefaults(cfg *PeerConfig) {
	if cfg.SCTPRecvBuffer == 0 {
		cfg.SCTPRecvBuffer = 8192
	}
	if cfg.SCTPRTOMax == 0 {
		cfg.SCTPRTOMax = 2500
	}
	if cfg.UDPReadBuffer == 0 {
		cfg.UDPReadBuffer = 8192
	}
	if cfg.UDPWriteBuffer == 0 {
		cfg.UDPWriteBuffer = 8192
	}
	if cfg.ICEDisconnTimeout == 0 {
		cfg.ICEDisconnTimeout = 15000
	}
	if cfg.ICEFailedTimeout == 0 {
		cfg.ICEFailedTimeout = 25000
	}
	if cfg.ICEKeepalive == 0 {
		cfg.ICEKeepalive = 2000
	}
	if cfg.DTLSRetransmit == 0 {
		cfg.DTLSRetransmit = 100
	}
	if cfg.DTLSSkipVerify == nil {
		cfg.DTLSSkipVerify = boolPtr(true)
	}
	if cfg.SCTPZeroChecksum == nil {
		cfg.SCTPZeroChecksum = boolPtr(true)
	}
	if cfg.DisableCloseByDTLS == nil {
		cfg.DisableCloseByDTLS = boolPtr(true)
	}
}

// applyServerDefaults fills zero-valued ServerOptions fields with defaults.
func applyServerDefaults(opts *ServerOptions) {
	if opts.NumChannels == 0 {
		opts.NumChannels = 6
	}
	if opts.SmuxStreamBuffer == 0 {
		opts.SmuxStreamBuffer = 2048
	}
	if opts.SmuxSessionBuffer == 0 {
		opts.SmuxSessionBuffer = 8192
	}
	if opts.SmuxFrameSize == 0 {
		opts.SmuxFrameSize = 32768
	}
	if opts.SmuxKeepAlive == 0 {
		opts.SmuxKeepAlive = 10
	}
	if opts.SmuxKeepAliveTimeout == 0 {
		opts.SmuxKeepAliveTimeout = 300
	}
	if opts.NumPeerConnections == 0 {
		opts.NumPeerConnections = 6
	}
	if opts.DCMaxBuffered == 0 {
		opts.DCMaxBuffered = 2048
	}
	if opts.DCLowMark == 0 {
		opts.DCLowMark = 512
	}
	// MaxTotalStreams 0 = unlimited (no server-wide semaphore)
	if opts.PaddingMax == 0 && opts.Padding {
		opts.PaddingMax = 256
	}
	applyPeerDefaults(&opts.PeerConfig)
}

// applyClientDefaults fills zero-valued ClientOptions fields with defaults.
func applyClientDefaults(opts *ClientOptions) {
	if opts.NumChannels == 0 {
		opts.NumChannels = 6
	}
	if opts.SmuxStreamBuffer == 0 {
		opts.SmuxStreamBuffer = 2048
	}
	if opts.SmuxSessionBuffer == 0 {
		opts.SmuxSessionBuffer = 8192
	}
	if opts.SmuxFrameSize == 0 {
		opts.SmuxFrameSize = 32768
	}
	if opts.SmuxKeepAlive == 0 {
		opts.SmuxKeepAlive = 10
	}
	if opts.SmuxKeepAliveTimeout == 0 {
		opts.SmuxKeepAliveTimeout = 300
	}
	if opts.NumPeerConnections == 0 {
		opts.NumPeerConnections = 6
	}
	if opts.DCMaxBuffered == 0 {
		opts.DCMaxBuffered = 2048
	}
	if opts.DCLowMark == 0 {
		opts.DCLowMark = 512
	}
	if opts.PaddingMax == 0 && opts.Padding {
		opts.PaddingMax = 256
	}
	applyPeerDefaults(&opts.PeerConfig)
}
