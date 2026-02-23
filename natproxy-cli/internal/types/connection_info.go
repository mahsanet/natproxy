package types

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type ConnectionInfo struct {
	PublicIP      string `json:"ip"`
	Port          int    `json:"port"`
	UUID          string `json:"uuid"`
	Transport     string `json:"transport"`
	Method        string `json:"method"`
	StunPort      int    `json:"stun_port,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	Protocol      string `json:"protocol"`
	ProxySettings string `json:"proxy_settings,omitempty"`
	ObfsKey       string `json:"obfs_key,omitempty"`
	RelayAddr     string `json:"relay_addr,omitempty"`
	Padding       bool   `json:"padding,omitempty"`
	Version       int    `json:"v,omitempty"`
	SigV          int    `json:"sig_v,omitempty"`
	TransportV    int    `json:"transport_v,omitempty"`

	NumPeerConns      int `json:"npc,omitempty"`
	NumChannels       int `json:"nc,omitempty"`
	SmuxStreamBuffer  int `json:"ssb,omitempty"`
	SmuxSessionBuffer int `json:"srb,omitempty"`
	SmuxFrameSize     int `json:"sfr,omitempty"`
	DCMaxBuffered        int `json:"dcb,omitempty"`
	DCLowMark            int `json:"dcl,omitempty"`
	PaddingMax           int `json:"pm,omitempty"`
	SmuxKeepAlive        int `json:"ska,omitempty"`
	SmuxKeepAliveTimeout int `json:"skt,omitempty"`
}

func (ci *ConnectionInfo) Encode() string {
	data, _ := json.Marshal(ci)
	return base64.StdEncoding.EncodeToString(data)
}

func DecodeConnectionInfo(code string) (*ConnectionInfo, error) {
	data, err := base64.StdEncoding.DecodeString(code)
	if err != nil {
		return nil, fmt.Errorf("invalid connection code: %w", err)
	}
	var info ConnectionInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("parse connection info: %w", err)
	}
	return &info, nil
}

func GenerateUUID() string {
	uuid := make([]byte, 16)
	rand.Read(uuid)
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}
