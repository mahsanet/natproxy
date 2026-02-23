package xray

import (
	"encoding/json"
	"strings"
	"testing"
)

func makeProxySettingsJSON(ps proxySettings) string {
	data, _ := json.Marshal(ps)
	return string(data)
}

func TestGenerateVLESSLink_KCP_DTLS(t *testing.T) {
	ps := defaultProxySettings()
	ps.Protocol = "vless"
	ps.Transport = "kcp"
	ps.FinalMaskType = "header-dtls"

	link := GenerateVLESSLink("test-uuid-1234", "1.2.3.4", 443, makeProxySettingsJSON(ps), "natproxy")
	if link == "" {
		t.Fatal("expected non-empty VLESS link")
	}
	if !strings.HasPrefix(link, "vless://") {
		t.Fatalf("expected vless:// prefix, got: %s", link)
	}
	if !strings.Contains(link, "test-uuid-1234") {
		t.Errorf("link missing UUID: %s", link)
	}
	if !strings.Contains(link, "1.2.3.4") {
		t.Errorf("link missing address: %s", link)
	}
	if !strings.Contains(link, "443") {
		t.Errorf("link missing port: %s", link)
	}
	if !strings.Contains(link, "type=kcp") {
		t.Errorf("link missing type=kcp: %s", link)
	}
	if !strings.Contains(link, "headerType=dtls") {
		t.Errorf("link missing headerType=dtls: %s", link)
	}
	if !strings.Contains(link, "#natproxy") {
		t.Errorf("link missing remark: %s", link)
	}
}

func TestGenerateVLESSLink_KCP_SRTP(t *testing.T) {
	ps := defaultProxySettings()
	ps.Protocol = "vless"
	ps.Transport = "kcp"
	ps.FinalMaskType = "header-srtp"

	link := GenerateVLESSLink("uuid-srtp", "10.0.0.1", 8080, makeProxySettingsJSON(ps), "test")
	if !strings.Contains(link, "headerType=srtp") {
		t.Errorf("expected headerType=srtp, got: %s", link)
	}
}

func TestGenerateVLESSLink_KCP_AES128GCM_WithSeed(t *testing.T) {
	ps := defaultProxySettings()
	ps.Protocol = "vless"
	ps.Transport = "kcp"
	ps.FinalMaskType = "mkcp-aes128gcm"
	ps.FinalMaskPassword = "mysecretpassword"

	link := GenerateVLESSLink("uuid-aes", "192.168.1.1", 9999, makeProxySettingsJSON(ps), "aes-test")
	if !strings.Contains(link, "headerType=none") {
		t.Errorf("expected headerType=none for mkcp-aes128gcm: %s", link)
	}
	if !strings.Contains(link, "seed=mysecretpassword") {
		t.Errorf("expected seed=mysecretpassword: %s", link)
	}
}

func TestGenerateVLESSLink_KCP_WechatVideo(t *testing.T) {
	ps := defaultProxySettings()
	ps.Protocol = "vless"
	ps.Transport = "kcp"
	ps.FinalMaskType = "header-wechat"

	link := GenerateVLESSLink("uuid-wc", "1.2.3.4", 443, makeProxySettingsJSON(ps), "wechat")
	if !strings.Contains(link, "headerType=wechat-video") {
		t.Errorf("expected headerType=wechat-video: %s", link)
	}
}

func TestGenerateVLESSLink_XHTTP(t *testing.T) {
	ps := defaultProxySettings()
	ps.Protocol = "vless"
	ps.Transport = "xhttp"
	ps.XhttpPath = "/proxy"
	ps.XhttpHost = "example.com"
	ps.XhttpMode = "stream-one"

	link := GenerateVLESSLink("uuid-xhttp", "5.6.7.8", 80, makeProxySettingsJSON(ps), "xhttp-test")
	if !strings.Contains(link, "type=xhttp") {
		t.Errorf("expected type=xhttp: %s", link)
	}
	if !strings.Contains(link, "path=%2Fproxy") {
		t.Errorf("expected path=/proxy (URL encoded): %s", link)
	}
	if !strings.Contains(link, "host=example.com") {
		t.Errorf("expected host=example.com: %s", link)
	}
	if !strings.Contains(link, "mode=stream-one") {
		t.Errorf("expected mode=stream-one: %s", link)
	}
}

func TestGenerateVLESSLink_SocksReturnsEmpty(t *testing.T) {
	ps := defaultProxySettings()
	ps.Protocol = "socks"

	link := GenerateVLESSLink("uuid-socks", "1.2.3.4", 1080, makeProxySettingsJSON(ps), "socks")
	if link != "" {
		t.Errorf("expected empty link for socks protocol, got: %s", link)
	}
}

func TestGenerateVLESSLink_IPv6(t *testing.T) {
	ps := defaultProxySettings()
	ps.Protocol = "vless"
	ps.Transport = "kcp"
	ps.FinalMaskType = "header-dtls"

	link := GenerateVLESSLink("uuid-ipv6", "2001:db8::1", 443, makeProxySettingsJSON(ps), "ipv6")
	if !strings.Contains(link, "[2001:db8::1]") {
		t.Errorf("expected bracketed IPv6 address: %s", link)
	}
}

func TestParseVLESSLink_KCP(t *testing.T) {
	link := "vless://my-uuid@1.2.3.4:443?encryption=none&security=none&type=kcp&headerType=dtls#myserver"
	vl, err := ParseVLESSLink(link)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if vl.UUID != "my-uuid" {
		t.Errorf("UUID: got %q, want %q", vl.UUID, "my-uuid")
	}
	if vl.Address != "1.2.3.4" {
		t.Errorf("Address: got %q, want %q", vl.Address, "1.2.3.4")
	}
	if vl.Port != 443 {
		t.Errorf("Port: got %d, want %d", vl.Port, 443)
	}
	if vl.Transport != "kcp" {
		t.Errorf("Transport: got %q, want %q", vl.Transport, "kcp")
	}
	if vl.HeaderType != "dtls" {
		t.Errorf("HeaderType: got %q, want %q", vl.HeaderType, "dtls")
	}
	if vl.Remark != "myserver" {
		t.Errorf("Remark: got %q, want %q", vl.Remark, "myserver")
	}
}

func TestParseVLESSLink_XHTTP(t *testing.T) {
	link := "vless://uuid-x@5.6.7.8:80?encryption=none&security=none&type=xhttp&path=%2Fproxy&host=example.com&mode=auto#xtest"
	vl, err := ParseVLESSLink(link)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if vl.Transport != "xhttp" {
		t.Errorf("Transport: got %q, want %q", vl.Transport, "xhttp")
	}
	if vl.Path != "/proxy" {
		t.Errorf("Path: got %q, want %q", vl.Path, "/proxy")
	}
	if vl.Host != "example.com" {
		t.Errorf("Host: got %q, want %q", vl.Host, "example.com")
	}
	if vl.Mode != "auto" {
		t.Errorf("Mode: got %q, want %q", vl.Mode, "auto")
	}
}

func TestParseVLESSLink_IPv6(t *testing.T) {
	link := "vless://uuid6@[2001:db8::1]:443?encryption=none&security=none&type=kcp&headerType=srtp#ipv6test"
	vl, err := ParseVLESSLink(link)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if vl.Address != "2001:db8::1" {
		t.Errorf("Address: got %q, want %q", vl.Address, "2001:db8::1")
	}
}

func TestParseVLESSLink_InvalidPrefix(t *testing.T) {
	_, err := ParseVLESSLink("vmess://something")
	if err == nil {
		t.Error("expected error for non-vless prefix")
	}
}

func TestParseVLESSLink_MissingUUID(t *testing.T) {
	_, err := ParseVLESSLink("vless://@1.2.3.4:443?type=kcp")
	if err == nil {
		t.Error("expected error for missing UUID")
	}
}

func TestRoundtrip_KCP(t *testing.T) {
	ps := defaultProxySettings()
	ps.Protocol = "vless"
	ps.Transport = "kcp"
	ps.FinalMaskType = "header-srtp"
	ps.FinalMaskPassword = "secret123"

	link := GenerateVLESSLink("round-uuid", "203.0.113.5", 12345, makeProxySettingsJSON(ps), "roundtrip")
	vl, err := ParseVLESSLink(link)
	if err != nil {
		t.Fatalf("roundtrip parse error: %v", err)
	}
	if vl.UUID != "round-uuid" {
		t.Errorf("UUID mismatch: %q", vl.UUID)
	}
	if vl.Address != "203.0.113.5" {
		t.Errorf("Address mismatch: %q", vl.Address)
	}
	if vl.Port != 12345 {
		t.Errorf("Port mismatch: %d", vl.Port)
	}
	if vl.Transport != "kcp" {
		t.Errorf("Transport mismatch: %q", vl.Transport)
	}
	if vl.HeaderType != "srtp" {
		t.Errorf("HeaderType mismatch: %q", vl.HeaderType)
	}
	if vl.Seed != "secret123" {
		t.Errorf("Seed mismatch: %q", vl.Seed)
	}
	if vl.Remark != "roundtrip" {
		t.Errorf("Remark mismatch: %q", vl.Remark)
	}
}

func TestRoundtrip_XHTTP(t *testing.T) {
	ps := defaultProxySettings()
	ps.Protocol = "vless"
	ps.Transport = "xhttp"
	ps.XhttpPath = "/mypath"
	ps.XhttpHost = "myhost.com"
	ps.XhttpMode = "packet-up"

	link := GenerateVLESSLink("xhttp-uuid", "10.0.0.1", 8443, makeProxySettingsJSON(ps), "xhttp-rt")
	vl, err := ParseVLESSLink(link)
	if err != nil {
		t.Fatalf("roundtrip parse error: %v", err)
	}
	if vl.Transport != "xhttp" {
		t.Errorf("Transport mismatch: %q", vl.Transport)
	}
	if vl.Path != "/mypath" {
		t.Errorf("Path mismatch: %q", vl.Path)
	}
	if vl.Host != "myhost.com" {
		t.Errorf("Host mismatch: %q", vl.Host)
	}
	if vl.Mode != "packet-up" {
		t.Errorf("Mode mismatch: %q", vl.Mode)
	}
}

func TestFinalMaskToHeaderType_AllMappings(t *testing.T) {
	tests := []struct {
		fm       string
		expected string
	}{
		{"header-srtp", "srtp"},
		{"header-dtls", "dtls"},
		{"header-wechat", "wechat-video"},
		{"header-utp", "utp"},
		{"header-wireguard", "wireguard"},
		{"header-dns", "dns"},
		{"mkcp-original", "none"},
		{"mkcp-aes128gcm", "none"},
		{"unknown", "none"},
		{"", "none"},
	}
	for _, tt := range tests {
		got := MapFinalMaskToHeaderType(tt.fm)
		if got != tt.expected {
			t.Errorf("MapFinalMaskToHeaderType(%q) = %q, want %q", tt.fm, got, tt.expected)
		}
	}
}

func TestHeaderTypeToFinalMask_AllMappings(t *testing.T) {
	tests := []struct {
		ht       string
		expected string
	}{
		{"srtp", "header-srtp"},
		{"dtls", "header-dtls"},
		{"wechat-video", "header-wechat"},
		{"utp", "header-utp"},
		{"wireguard", "header-wireguard"},
		{"dns", "header-dns"},
		{"none", "mkcp-original"},
		{"unknown", "none"},
		{"", "none"},
	}
	for _, tt := range tests {
		got := MapHeaderTypeToFinalMask(tt.ht)
		if got != tt.expected {
			t.Errorf("MapHeaderTypeToFinalMask(%q) = %q, want %q", tt.ht, got, tt.expected)
		}
	}
}

func TestGenerateVLESSLink_EmptySettingsJSON(t *testing.T) {
	// Empty settings defaults to socks protocol, should return ""
	link := GenerateVLESSLink("uuid", "1.2.3.4", 443, "", "test")
	if link != "" {
		t.Errorf("expected empty link for default (socks) settings, got: %s", link)
	}
}
