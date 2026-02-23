package types

type ClientConfig struct {
	SocksPort    int    `json:"socksPort"`
	StunServer   string `json:"stunServer"`
	SignalingURL string `json:"signalingUrl"`

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
}
