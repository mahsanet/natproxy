// Package xray wraps xray-core for starting/stopping the proxy engine.
package xray

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"

	"natproxy/golib/applog"

	xlog "github.com/xtls/xray-core/common/log"
	xcore "github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
	"github.com/xtls/xray-core/transport/internet"

	// Register only the protocols and transports we need to reduce binary size.
	// Using the full distro/all import adds ~30-50MB.
	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/dns"
	_ "github.com/xtls/xray-core/app/log"
	_ "github.com/xtls/xray-core/app/proxyman"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/proxy/dns"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/socks"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
	_ "github.com/xtls/xray-core/proxy/vless/outbound"
	_ "github.com/xtls/xray-core/transport/internet/kcp"
	_ "github.com/xtls/xray-core/transport/internet/splithttp"
	_ "github.com/xtls/xray-core/transport/internet/tcp"

	// FinalMask: UDP packet obfuscation for mKCP
	_ "github.com/xtls/xray-core/transport/internet/finalmask/header/dns"
	_ "github.com/xtls/xray-core/transport/internet/finalmask/header/dtls"
	_ "github.com/xtls/xray-core/transport/internet/finalmask/header/srtp"
	_ "github.com/xtls/xray-core/transport/internet/finalmask/header/utp"
	_ "github.com/xtls/xray-core/transport/internet/finalmask/header/wechat"
	_ "github.com/xtls/xray-core/transport/internet/finalmask/header/wireguard"
	_ "github.com/xtls/xray-core/transport/internet/finalmask/mkcp/aes128gcm"
	_ "github.com/xtls/xray-core/transport/internet/finalmask/mkcp/original"
)

// applogHandler routes xray-core log messages to our applog.
type applogHandler struct{}

func (h *applogHandler) Handle(msg xlog.Message) {
	switch m := msg.(type) {
	case *xlog.AccessMessage:
		trackClientFromAccess(m)
		if m.Status == xlog.AccessRejected {
			applog.Errorf("[xray] %s", m.String())
		} else {
			applog.Infof("[xray] %s", m.String())
		}
	case *xlog.GeneralMessage:
		// TODO: re-enable debug logs when needed for troubleshooting
		if m.Severity == xlog.Severity_Debug {
			return
		}
		s := msg.String()
		if s == "" {
			return
		}
		switch m.Severity {
		case xlog.Severity_Error:
			applog.Errorf("[xray] %s", s)
		case xlog.Severity_Warning:
			applog.Warnf("[xray] %s", s)
		default:
			applog.Infof("[xray] %s", s)
		}
	default:
		s := msg.String()
		if s != "" {
			applog.Infof("[xray] %s", s)
		}
	}
}

func init() {
	xlog.RegisterHandler(&applogHandler{})
}

var (
	mu        sync.Mutex
	instance  *xcore.Instance
	protectFn func(fd int) bool

	clientMu      sync.Mutex
	clientTracker = make(map[string]time.Time)
)

// trackClientFromAccess extracts the client IP from an access log message
// and records it in the tracker with the current timestamp.
func trackClientFromAccess(m *xlog.AccessMessage) {
	if m.Status != xlog.AccessAccepted || m.From == nil {
		return
	}
	from := fmt.Sprintf("%v", m.From)
	ip := extractIP(from)
	if ip == "" || ip == "127.0.0.1" || ip == "::1" {
		return
	}
	clientMu.Lock()
	clientTracker[ip] = time.Now()
	clientMu.Unlock()
}

// extractIP pulls an IP out of xray's address strings.
// They come in two flavors: "tcp:IP:port" (net.Destination) or plain "IP:port" (net.Addr).
func extractIP(addr string) string {
	// Strip protocol prefix if present
	if strings.HasPrefix(addr, "tcp:") || strings.HasPrefix(addr, "udp:") {
		addr = addr[4:]
	}
	// Remove port suffix (last colon)
	if idx := strings.LastIndex(addr, ":"); idx >= 0 {
		addr = addr[:idx]
	}
	return addr
}

// GetClientCount returns the number of unique client IPs seen in the last 10 seconds
func GetClientCount() int {
	clientMu.Lock()
	defer clientMu.Unlock()

	cutoff := time.Now().Add(-10 * time.Second)
	for ip, lastSeen := range clientTracker {
		if lastSeen.Before(cutoff) {
			delete(clientTracker, ip)
		}
	}
	return len(clientTracker)
}

// ResetClientTracker clears the client tracking state.
func ResetClientTracker() {
	clientMu.Lock()
	defer clientMu.Unlock()
	clientTracker = make(map[string]time.Time)
}

// StartXray loads the given JSON config and starts an xray-core instance.
func StartXray(configJSON []byte) error {
	mu.Lock()
	defer mu.Unlock()

	if instance != nil {
		return fmt.Errorf("xray already running")
	}

	applog.Info("Starting xray-core...")

	config, err := serial.LoadJSONConfig(bytes.NewReader(configJSON))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	inst, err := xcore.New(config)
	if err != nil {
		return fmt.Errorf("create instance: %w", err)
	}

	if err := inst.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	// Re-register our log handler: xray's app/log service replaces it during
	// xcore.New() with its own handler that writes to stdout/file. By
	// re-registering here, all runtime log messages (including AccessMessages
	// for client tracking) flow through our applog ring buffer.
	xlog.RegisterHandler(&applogHandler{})

	instance = inst
	applog.Success("Xray-core started")
	return nil
}

// StopXray stops the running xray-core instance and releases resources.
func StopXray() error {
	mu.Lock()
	defer mu.Unlock()

	if instance == nil {
		return nil
	}

	err := instance.Close()
	instance = nil
	if err == nil {
		applog.Info("Xray-core stopped")
	}
	return err
}

// RegisterProtectFunc registers the Android VPN socket protection callback.
// Every outbound socket created by xray-core will call this function
// so the socket is excluded from the TUN interface (preventing routing loops).
func RegisterProtectFunc(fn func(fd int) bool) {
	mu.Lock()
	defer mu.Unlock()
	protectFn = fn

	applog.Info("Registering socket protect with xray dialer controller...")
	if err := internet.RegisterDialerController(func(network, address string, c syscall.RawConn) error {
		c.Control(func(fd uintptr) {
			if protectFn != nil {
				ok := protectFn(int(fd))
				applog.Infof("Protect fd=%d for %s %s → %v", fd, network, address, ok)
			}
		})
		return nil
	}); err != nil {
		applog.Errorf("RegisterDialerController FAILED: %v", err)
	} else {
		applog.Success("RegisterDialerController OK")
	}
}
