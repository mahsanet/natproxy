package xray

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// VLESSLink holds all components of a VLESS URL.
type VLESSLink struct {
	UUID       string
	Address    string
	Port       int
	Encryption string // always "none"
	Security   string // "none" for direct connections
	Transport  string // "kcp" or "xhttp"
	HeaderType string // KCP header obfuscation type
	Seed       string // KCP seed (FinalMask password)
	Path       string // xHTTP path
	Host       string // xHTTP host
	Mode       string // xHTTP mode
	Remark     string // link name/label (fragment)
}

// finalMaskToHeaderType maps natproxy FinalMask types to standard xray headerType values.
var finalMaskToHeaderType = map[string]string{
	"header-srtp":      "srtp",
	"header-dtls":      "dtls",
	"header-wechat":    "wechat-video",
	"header-utp":       "utp",
	"header-wireguard": "wireguard",
	"header-dns":       "dns",
	"mkcp-original":    "none",
	"mkcp-aes128gcm":   "none",
}

// headerTypeToFinalMask maps standard xray headerType values back to natproxy FinalMask types.
var headerTypeToFinalMask = map[string]string{
	"srtp":         "header-srtp",
	"dtls":         "header-dtls",
	"wechat-video": "header-wechat",
	"utp":          "header-utp",
	"wireguard":    "header-wireguard",
	"dns":          "header-dns",
	"none":         "mkcp-original",
}

// MapFinalMaskToHeaderType converts a natproxy FinalMask type to a standard xray headerType.
func MapFinalMaskToHeaderType(fmType string) string {
	if ht, ok := finalMaskToHeaderType[fmType]; ok {
		return ht
	}
	return "none"
}

// MapHeaderTypeToFinalMask converts a standard xray headerType back to a natproxy FinalMask type.
func MapHeaderTypeToFinalMask(headerType string) string {
	if fm, ok := headerTypeToFinalMask[headerType]; ok {
		return fm
	}
	return "none"
}

// GenerateVLESSLink builds a vless:// URL from connection parameters.
// Returns "" if protocol is not "vless".
func GenerateVLESSLink(uuid, address string, port int, proxySettingsJSON, remark string) string {
	ps := parseProxySettings(proxySettingsJSON)
	if ps.Protocol != "vless" {
		return ""
	}

	// Wrap IPv6 addresses in brackets
	host := address
	if net.ParseIP(address) != nil && strings.Contains(address, ":") {
		host = "[" + address + "]"
	}

	u := &url.URL{
		Scheme:   "vless",
		User:     url.User(uuid),
		Host:     net.JoinHostPort(host, strconv.Itoa(port)),
		Fragment: remark,
	}

	q := url.Values{}
	q.Set("encryption", "none")
	q.Set("security", "none")

	switch ps.Transport {
	case "xhttp":
		q.Set("type", "xhttp")
		if ps.XhttpPath != "" {
			q.Set("path", ps.XhttpPath)
		}
		if ps.XhttpHost != "" {
			q.Set("host", ps.XhttpHost)
		}
		if ps.XhttpMode != "" {
			q.Set("mode", ps.XhttpMode)
		}
	default: // "kcp"
		q.Set("type", "kcp")
		headerType := MapFinalMaskToHeaderType(ps.FinalMaskType)
		q.Set("headerType", headerType)
		// For mkcp-aes128gcm or any FinalMask with a password, include seed
		if ps.FinalMaskPassword != "" {
			q.Set("seed", ps.FinalMaskPassword)
		}
	}

	u.RawQuery = q.Encode()
	return u.String()
}

// ParseVLESSLink parses a vless:// URL into a VLESSLink struct.
func ParseVLESSLink(link string) (*VLESSLink, error) {
	if !strings.HasPrefix(link, "vless://") {
		return nil, fmt.Errorf("not a vless:// link")
	}

	u, err := url.Parse(link)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	uuid := u.User.Username()
	if uuid == "" {
		return nil, fmt.Errorf("missing UUID in link")
	}

	hostRaw, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		return nil, fmt.Errorf("invalid host:port: %w", err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port: %w", err)
	}

	// Strip brackets from IPv6
	address := strings.Trim(hostRaw, "[]")

	q := u.Query()
	vl := &VLESSLink{
		UUID:       uuid,
		Address:    address,
		Port:       port,
		Encryption: q.Get("encryption"),
		Security:   q.Get("security"),
		Transport:  q.Get("type"),
		Remark:     u.Fragment,
	}

	switch vl.Transport {
	case "kcp":
		vl.HeaderType = q.Get("headerType")
		vl.Seed = q.Get("seed")
	case "xhttp":
		vl.Path = q.Get("path")
		vl.Host = q.Get("host")
		vl.Mode = q.Get("mode")
	}

	return vl, nil
}
