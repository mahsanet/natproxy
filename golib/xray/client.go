package xray

import "encoding/json"

// BuildClientConfig generates an xray-core JSON config for client mode.
// settingsJSON is a JSON-encoded proxySettings struct matching the server.
func BuildClientConfig(serverIP string, serverPort int, uuid string, socksPort int, settingsJSON string) ([]byte, error) {
	ps := parseProxySettings(settingsJSON)

	var outbound map[string]interface{}

	switch ps.Protocol {
	case "socks":
		server := map[string]interface{}{
			"address": serverIP,
			"port":    serverPort,
		}
		if ps.SocksAuth == "password" {
			server["users"] = []map[string]string{
				{"user": ps.SocksUsername, "pass": ps.SocksPassword},
			}
		}
		outbound = map[string]interface{}{
			"tag":      "proxy-out",
			"protocol": "socks",
			"settings": map[string]interface{}{
				"servers": []map[string]interface{}{server},
			},
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
		outbound = map[string]interface{}{
			"tag":      "proxy-out",
			"protocol": "vless",
			"settings": map[string]interface{}{
				"vnext": []map[string]interface{}{
					{
						"address": serverIP,
						"port":    serverPort,
						"users": []map[string]interface{}{
							{
								"id":         uuid,
								"encryption": "none",
							},
						},
					},
				},
			},
			"streamSettings": streamSettings,
		}
	}

	config := map[string]interface{}{
		"log": map[string]interface{}{
			"loglevel": "info",
		},
		"inbounds": []map[string]interface{}{
			{
				"tag":      "socks-in",
				"port":     socksPort,
				"listen":   "127.0.0.1",
				"protocol": "socks",
				"settings": map[string]interface{}{
					"udp": true,
				},
			},
		},
		"outbounds": []map[string]interface{}{outbound},
	}

	return json.Marshal(config)
}
