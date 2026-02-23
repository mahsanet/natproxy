package xray

import "encoding/json"

// proxySettings carries all protocol/transport configuration.
// It is JSON-serialized and passed between server.go, client.go, and api.go.
type proxySettings struct {
	Protocol  string `json:"protocol"`  // "socks" or "vless"
	Transport string `json:"transport"` // "kcp" or "xhttp"

	// SOCKS
	SocksAuth     string `json:"socksAuth"` // "noauth" or "password"
	SocksUsername string `json:"socksUsername"`
	SocksPassword string `json:"socksPassword"`
	SocksUDP      bool   `json:"socksUdp"`

	// KCP
	KcpMTU              int  `json:"kcpMtu"`
	KcpTTI              int  `json:"kcpTti"`
	KcpUplinkCapacity   int  `json:"kcpUplinkCapacity"`
	KcpDownlinkCapacity int  `json:"kcpDownlinkCapacity"`
	KcpCongestion       bool `json:"kcpCongestion"`
	KcpReadBufferSize   int  `json:"kcpReadBufferSize"`
	KcpWriteBufferSize  int  `json:"kcpWriteBufferSize"`

	// xHTTP
	XhttpPath string `json:"xhttpPath"`
	XhttpHost string `json:"xhttpHost"`
	XhttpMode string `json:"xhttpMode"` // "auto","packet-up","stream-up","stream-one"

	// FinalMask (UDP packet obfuscation for mKCP)
	FinalMaskType     string `json:"finalMaskType"`     // "none","header-srtp","header-dtls","header-wechat","header-utp","header-wireguard","header-dns","mkcp-original","mkcp-aes128gcm"
	FinalMaskPassword string `json:"finalMaskPassword"` // for mkcp-aes128gcm
	FinalMaskDomain   string `json:"finalMaskDomain"`   // for header-dns
}

func defaultProxySettings() proxySettings {
	return proxySettings{
		Protocol:            "socks",
		Transport:           "kcp",
		SocksAuth:           "noauth",
		SocksUDP:            true,
		KcpMTU:              1350,
		KcpTTI:              20,
		KcpUplinkCapacity:   12,
		KcpDownlinkCapacity: 100,
		KcpCongestion:       true,
		KcpReadBufferSize:   4,
		KcpWriteBufferSize:  4,
		XhttpPath:           "/",
		XhttpMode:           "auto",
		FinalMaskType:       "header-dtls",
		FinalMaskPassword:   "",
		FinalMaskDomain:     "",
	}
}

func parseProxySettings(settingsJSON string) proxySettings {
	ps := defaultProxySettings()
	if settingsJSON != "" {
		json.Unmarshal([]byte(settingsJSON), &ps)
	}
	return ps
}

// BuildServerConfig generates an xray-core JSON config for server mode.
// settingsJSON is a JSON-encoded proxySettings struct.
func BuildServerConfig(listenAddr string, listenPort int, uuid string, settingsJSON string) ([]byte, error) {
	ps := parseProxySettings(settingsJSON)

	var inbound map[string]interface{}

	switch ps.Protocol {
	case "socks":
		settings := map[string]interface{}{
			"auth": ps.SocksAuth,
			"udp":  ps.SocksUDP,
		}
		if ps.SocksAuth == "password" {
			settings["accounts"] = []map[string]string{
				{"user": ps.SocksUsername, "pass": ps.SocksPassword},
			}
		}
		inbound = map[string]interface{}{
			"tag":      "proxy-in",
			"port":     listenPort,
			"listen":   listenAddr,
			"protocol": "socks",
			"settings": settings,
		}
	default: // "vless"
		streamSettings := map[string]interface{}{
			"security": "none",
		}
		switch ps.Transport {
		case "xhttp":
			streamSettings["network"] = "xhttp"
			xhttpSettings := map[string]interface{}{
				"path": ps.XhttpPath,
				"mode": ps.XhttpMode,
			}
			if ps.XhttpHost != "" {
				xhttpSettings["host"] = ps.XhttpHost
			}
			streamSettings["xhttpSettings"] = xhttpSettings
		default: // "kcp"
			streamSettings["network"] = "kcp"
			streamSettings["kcpSettings"] = map[string]interface{}{
				"mtu":              ps.KcpMTU,
				"tti":              ps.KcpTTI,
				"uplinkCapacity":   ps.KcpUplinkCapacity,
				"downlinkCapacity": ps.KcpDownlinkCapacity,
				"congestion":       ps.KcpCongestion,
				"readBufferSize":   ps.KcpReadBufferSize,
				"writeBufferSize":  ps.KcpWriteBufferSize,
			}
			if ps.FinalMaskType != "" && ps.FinalMaskType != "none" {
				mask := map[string]interface{}{
					"type": ps.FinalMaskType,
				}
				switch ps.FinalMaskType {
				case "mkcp-aes128gcm":
					mask["settings"] = map[string]interface{}{
						"password": ps.FinalMaskPassword,
					}
				case "header-dns":
					mask["settings"] = map[string]interface{}{
						"domain": ps.FinalMaskDomain,
					}
				}
				streamSettings["finalmask"] = map[string]interface{}{
					"udp": []map[string]interface{}{mask},
				}
			}
		}
		inbound = map[string]interface{}{
			"tag":      "proxy-in",
			"port":     listenPort,
			"listen":   listenAddr,
			"protocol": "vless",
			"settings": map[string]interface{}{
				"clients": []map[string]interface{}{
					{"id": uuid},
				},
				"decryption": "none",
			},
			"streamSettings": streamSettings,
		}
	}

	config := map[string]interface{}{
		"log": map[string]interface{}{
			"loglevel": "info",
		},
		"dns": map[string]interface{}{
			"servers": []string{
				"https+local://1.1.1.1/dns-query",
				"https+local://8.8.8.8/dns-query",
			},
		},
		"inbounds": []map[string]interface{}{inbound},
		"outbounds": []map[string]interface{}{
			{
				"tag":      "direct",
				"protocol": "freedom",
				"settings": map[string]interface{}{},
			},
			{
				"tag":      "dns-out",
				"protocol": "dns",
			},
		},
		"routing": map[string]interface{}{
			"rules": []map[string]interface{}{
				{
					"type":        "field",
					"port":        53,
					"outboundTag": "dns-out",
				},
			},
		},
	}

	return json.Marshal(config)
}
