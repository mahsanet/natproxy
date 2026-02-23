// Go mobile API for the Android app.
// All complex types are JSON strings (gomobile restriction).
package golib

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/net/proxy"

	"natproxy/golib/applog"
	"natproxy/golib/nat"
	"natproxy/golib/signaling"
	"natproxy/golib/tunnel"
	"natproxy/golib/util"
	webrtcpkg "natproxy/golib/webrtc"
	"natproxy/golib/xray"
)

// Socket protection callback for Android VPN
type ProtectFunc interface {
	Protect(fd int) bool
}

const (
	maxReconnects = 2

	defaultSignalingURL = "http://[IP]:5601"
	defaultStunServer1  = "stun.l.google.com:19302"
	defaultStunServer2  = "stun1.l.google.com:19302"

	// Discovery heartbeat
	discoveryHeartbeatInterval = 20 * time.Second

	// Latency test
	latencyTestURL        = "http://cp.cloudflare.com"
	latencyDialTimeout    = 5 * time.Second
	latencyRequestTimeout = 10 * time.Second
	latencyAutoDelay      = 2 * time.Second // delay before auto latency test
)

var (
	mu               sync.Mutex
	serverRunning    bool
	clientRunning    bool
	clientConnecting bool // true while StartClient does slow networking without mu
	clientSocksPort  int
	serverInfo       *connectionInfo
	activeTunnel     *tunnel.Tunnel

	// WebRTC hole-punch path state
	activeWebRTCServerGroup    *webrtcpkg.ServerGroup   // latest server group (accepts new clients)
	drainingWebRTCServerGroups []*webrtcpkg.ServerGroup  // old server groups with active streams
	activeWebRTCClientGroup    *webrtcpkg.ClientGroup
	pendingWebRTCClientGroup   *webrtcpkg.ClientGroup    // connected but TUN not yet started
	serverMethod               string                    // "upnp" or "holepunch" — tracks which path was used
	serverCancel               context.CancelFunc        // cancels server accept-loop goroutine

	clientDoneCh chan struct{} // closed when WebRTC client disconnects (ICE death or manual stop)

	clientConnectCtx    context.Context
	clientConnectCancel context.CancelFunc

	// Fast reconnect state
	savedConnInfo   connectionInfo
	savedClientCfg  clientConfig
	savedProtectFn  func(int) bool
	savedICEServers []string
	savedClientOpts webrtcpkg.ClientOptions
	reconnectCount  int

	discoveryListingID    string
	discoverySignalingURL string
	heartbeatStop         chan struct{}

	// Manual signaling state
	manualMode        bool
	manualOfferCode   string
	stunKeepaliveDone chan struct{}

	// Connection metrics
	serverStartTime   time.Time
	clientStartTime   time.Time
	totalPeersEver    atomic.Int32
	serverRateTracker rateTracker
	clientRateTracker rateTracker

	// Cached MemStats — avoid STW pause on every status poll
	cachedMemStats     runtime.MemStats
	cachedMemStatsTime time.Time
	cachedMemStatsMu   sync.Mutex
)

// getCachedHeapMB returns heap usage in MB, refreshing the underlying
// runtime.ReadMemStats at most once every 5 seconds to avoid STW pauses.
func getCachedHeapMB() float64 {
	cachedMemStatsMu.Lock()
	defer cachedMemStatsMu.Unlock()
	if time.Since(cachedMemStatsTime) > 5*time.Second {
		runtime.ReadMemStats(&cachedMemStats)
		cachedMemStatsTime = time.Now()
	}
	return float64(cachedMemStats.HeapAlloc) / (1024 * 1024)
}

// rateTracker computes bandwidth rates from cumulative byte counters.
// It is designed to be called from a polling loop (e.g. every 1-2s).
type rateTracker struct {
	prevUp, prevDown int64
	prevTime         time.Time
	rateUp, rateDown float64 // bytes/sec
}

func (rt *rateTracker) update(bytesUp, bytesDown int64) (float64, float64) {
	now := time.Now()
	if !rt.prevTime.IsZero() {
		dt := now.Sub(rt.prevTime).Seconds()
		if dt > 0 {
			rt.rateUp = float64(bytesUp-rt.prevUp) / dt
			rt.rateDown = float64(bytesDown-rt.prevDown) / dt
			if rt.rateUp < 0 {
				rt.rateUp = 0
			}
			if rt.rateDown < 0 {
				rt.rateDown = 0
			}
		}
	}
	rt.prevUp = bytesUp
	rt.prevDown = bytesDown
	rt.prevTime = now
	return rt.rateUp, rt.rateDown
}

func (rt *rateTracker) reset() {
	rt.prevUp = 0
	rt.prevDown = 0
	rt.prevTime = time.Time{}
	rt.rateUp = 0
	rt.rateDown = 0
}

type connectionInfo struct {
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
	Version       int    `json:"v,omitempty"`            // protocol version
	SigV          int    `json:"sig_v,omitempty"`        // 0=plain, 2=encrypted
	TransportV    int    `json:"transport_v,omitempty"`  // 0/1=datachannel, 2=media stream

	// Synced WebRTC settings (server → client, must match)
	NumPeerConns     int `json:"npc,omitempty"` // parallel PeerConnections
	NumChannels      int `json:"nc,omitempty"`  // parallel data channels
	SmuxStreamBuffer int `json:"ssb,omitempty"` // per-stream buffer KB
	SmuxSessionBuffer int `json:"srb,omitempty"` // session buffer KB
	SmuxFrameSize    int `json:"sfr,omitempty"` // frame size bytes
	DCMaxBuffered        int `json:"dcb,omitempty"` // DC high water KB
	DCLowMark            int `json:"dcl,omitempty"` // DC low water KB
	PaddingMax           int `json:"pm,omitempty"`  // padding max bytes
	SmuxKeepAlive        int `json:"ska,omitempty"` // keepalive interval seconds
	SmuxKeepAliveTimeout int `json:"skt,omitempty"` // keepalive timeout seconds
}

type serverConfig struct {
	ListenPort   int    `json:"listenPort"`
	StunServer   string `json:"stunServer"`
	SignalingURL string `json:"signalingUrl"`
	DiscoveryURL string `json:"discoveryUrl"`
	NatMethod    string `json:"natMethod"`
	UseRelay     bool   `json:"useRelay"`

	// Protocol & Transport
	Protocol  string `json:"protocol"`  // "socks" or "vless"
	Transport string `json:"transport"` // "kcp" or "xhttp"

	// SOCKS
	SocksAuth     string `json:"socksAuth"`
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
	XhttpMode string `json:"xhttpMode"`

	// FinalMask
	FinalMaskType     string `json:"finalMaskType"`
	FinalMaskPassword string `json:"finalMaskPassword"`
	FinalMaskDomain   string `json:"finalMaskDomain"`

	// NAT Traversal - UPnP
	UpnpLeaseDuration int `json:"upnpLeaseDuration"`
	UpnpRetries       int `json:"upnpRetries"`
	SsdpTimeout       int `json:"ssdpTimeout"`

	// Rate limiting (bytes/sec, 0 = unlimited)
	RateLimitUp   int64 `json:"rateLimitUp"`
	RateLimitDown int64 `json:"rateLimitDown"`

	// WebRTC transport
	NumPeerConnections int    `json:"numPeerConnections"` // parallel PeerConnections (default 1)
	TransportMode      string `json:"transportMode"`      // "datachannel" or "media"
	DisableIPv6        bool   `json:"disableIPv6"`

	// WebRTC performance tuning
	NumChannels          int  `json:"numChannels"`          // parallel data channels (default 6)
	SmuxStreamBuffer     int  `json:"smuxStreamBuffer"`     // per-stream receive window in KB (default 2048)
	SmuxSessionBuffer    int  `json:"smuxSessionBuffer"`    // session-wide receive buffer in KB (default 8192)
	SmuxFrameSize        int  `json:"smuxFrameSize"`        // max smux frame size in bytes (default 32768)
	SmuxKeepAlive        int  `json:"smuxKeepAlive"`        // keepalive interval in seconds (default 10)
	SmuxKeepAliveTimeout int  `json:"smuxKeepAliveTimeout"` // keepalive timeout in seconds (default 300)
	DCMaxBuffered        int  `json:"dcMaxBuffered"`        // DC backpressure high water in KB (default 2048)
	DCLowMark            int  `json:"dcLowMark"`            // DC backpressure low water in KB (default 512)

	// Padding
	PaddingEnabled bool `json:"paddingEnabled"` // enable traffic padding (default false)
	PaddingMax     int  `json:"paddingMax"`     // max padding bytes per write (default 256)

	// SCTP/DTLS/ICE tuning (independent per side)
	SCTPRecvBuffer     int   `json:"sctpRecvBuffer"`     // SCTP receive buffer in KB (default 8192)
	SCTPRTOMax         int   `json:"sctpRTOMax"`         // SCTP max retransmit timeout in ms (default 2500)
	UDPReadBuffer      int   `json:"udpReadBuffer"`      // kernel UDP read buffer in KB (default 8192)
	UDPWriteBuffer     int   `json:"udpWriteBuffer"`     // kernel UDP write buffer in KB (default 8192)
	ICEDisconnTimeout  int   `json:"iceDisconnTimeout"`  // ICE disconnected timeout in ms (default 15000)
	ICEFailedTimeout   int   `json:"iceFailedTimeout"`   // ICE failed timeout in ms (default 25000)
	ICEKeepalive       int   `json:"iceKeepalive"`       // ICE keepalive interval in ms (default 2000)
	DTLSRetransmit     int   `json:"dtlsRetransmit"`     // DTLS retransmission interval in ms (default 100)
	DTLSSkipVerify     *bool `json:"dtlsSkipVerify"`     // skip DTLS HelloVerify (default true)
	SCTPZeroChecksum   *bool `json:"sctpZeroChecksum"`   // SCTP zero checksum (default true)
	DisableCloseByDTLS *bool `json:"disableCloseByDTLS"` // prevent DTLS close → PC close (default true)

	// Logging
	MaskIPs bool `json:"maskIPs"` // mask last 2 octets in logs (default false)

	// UUID (empty = random)
	UUID string `json:"uuid"`
}

type clientConfig struct {
	SocksPort    int    `json:"socksPort"`
	TunAddress   string `json:"tunAddress"`
	MTU          int    `json:"mtu"`
	DNS1         string `json:"dns1"`
	DNS2         string `json:"dns2"`
	StunServer   string `json:"stunServer"`
	SignalingURL string `json:"signalingUrl"`

	// DNS privacy control
	AllowDirectDNS bool `json:"allowDirectDNS"` // allow fallback to ISP DNS (default false)

	// SCTP/DTLS/ICE tuning (independent per side)
	SCTPRecvBuffer     int   `json:"sctpRecvBuffer"`
	SCTPRTOMax         int   `json:"sctpRTOMax"`
	UDPReadBuffer      int   `json:"udpReadBuffer"`
	UDPWriteBuffer     int   `json:"udpWriteBuffer"`
	ICEDisconnTimeout  int   `json:"iceDisconnTimeout"`
	ICEFailedTimeout   int   `json:"iceFailedTimeout"`
	ICEKeepalive       int   `json:"iceKeepalive"`
	DTLSRetransmit     int   `json:"dtlsRetransmit"`
	DTLSSkipVerify     *bool `json:"dtlsSkipVerify"`
	SCTPZeroChecksum   *bool `json:"sctpZeroChecksum"`
	DisableCloseByDTLS *bool `json:"disableCloseByDTLS"`

	// Logging
	MaskIPs bool `json:"maskIPs"`
}

// buildProxySettingsJSON constructs the proxySettings JSON from serverConfig fields.
func buildProxySettingsJSON(cfg *serverConfig) string {
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

// === Server API ===

// StartServer tries UPnP first, falls back to WebRTC hole punch.
func StartServer(settingsJSON string) (string, error) {
	mu.Lock()
	defer mu.Unlock()

	if serverRunning {
		return "", fmt.Errorf("server already running")
	}

	applog.Info("Starting server...")

	// Parse settings with defaults
	cfg := serverConfig{
		ListenPort:          10853,
		StunServer:          defaultStunServer1,
		SignalingURL:        defaultSignalingURL,
		NatMethod:           "auto",
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
		UpnpLeaseDuration:   3600,
		UpnpRetries:         3,
		SsdpTimeout:         3,
	}
	if settingsJSON != "" {
		if err := json.Unmarshal([]byte(settingsJSON), &cfg); err != nil {
			return "", fmt.Errorf("parse settings: %w", err)
		}
	}

	applog.SetMaskIPs(cfg.MaskIPs)

	uuid := cfg.UUID
	if uuid == "" {
		uuid = generateUUID()
	}
	listenPort := cfg.ListenPort

	var externalIP string
	var mappedPort int
	var err error
	method := ""
	transport := cfg.Transport
	protocol := cfg.Protocol

	// Determine UPnP mapping protocol based on proxy protocol/transport
	upnpProto := "TCP"
	if protocol == "vless" && transport == "kcp" {
		upnpProto = "UDP"
	}

	// NAT traversal based on configured method
	if cfg.NatMethod != "holepunch" {
		// Try port mapping cascade: UPnP → NAT-PMP → PCP
		applog.Info("Attempting port mapping (UPnP → NAT-PMP → PCP)...")
		externalIP, mappedPort, method, err = nat.TryPortMapping(listenPort, listenPort, upnpProto, nat.PortMapOptions{
			UPnPOptions: nat.UPnPOptions{
				LeaseDuration:  cfg.UpnpLeaseDuration,
				MappingRetries: cfg.UpnpRetries,
				SSDPTimeout:    time.Duration(cfg.SsdpTimeout) * time.Second,
			},
		})
		if err == nil {
			applog.Successf("Port mapping via %s: %d → %d (%s)", method, listenPort, mappedPort, upnpProto)
		} else {
			applog.Warnf("Port mapping failed: %v", err)
			method = ""
		}
	}

	// Port-mapped path (UPnP/NAT-PMP/PCP): use xray-core
	if method != "" {
		// Build proxySettings JSON for xray config
		proxySettingsJSON := buildProxySettingsJSON(&cfg)

		// Build and start xray-core server
		applog.Infof("Building xray %s/%s server config (listen=%s:%d, uuid=%s)", protocol, transport, "0.0.0.0", listenPort, uuid)
		configJSON, err := xray.BuildServerConfig("0.0.0.0", listenPort, uuid, proxySettingsJSON)
		if err != nil {
			return "", fmt.Errorf("build server config: %w", err)
		}

		if err := xray.StartXray(configJSON); err != nil {
			return "", fmt.Errorf("start xray: %w", err)
		}

		applog.Successf("Server started (%s/%s/%s) on %s:%d", method, protocol, transport, externalIP, mappedPort)

		serverInfo = &connectionInfo{
			PublicIP:      externalIP,
			Port:          mappedPort,
			UUID:          uuid,
			Transport:     transport,
			Method:        method,
			Protocol:      protocol,
			ProxySettings: proxySettingsJSON,
		}
		serverMethod = method
		serverRunning = true
		serverStartTime = time.Now()

		infoJSON, _ := json.Marshal(serverInfo)
		connectionCode := encodeBase64(infoJSON)

		vlessLink := xray.GenerateVLESSLink(uuid, externalIP, mappedPort, proxySettingsJSON, "natproxy")
		result := map[string]string{"code": connectionCode}
		if vlessLink != "" {
			result["vless"] = vlessLink
		}
		resultJSON, _ := json.Marshal(result)
		return string(resultJSON), nil
	}

	// Holepunch path: use WebRTC + smux
	if cfg.NatMethod == "upnp" {
		return "", fmt.Errorf("NAT traversal failed: port mapping mode selected but all methods failed")
	}

	applog.Info("Falling back to WebRTC hole punch...")

	iceServers := []string{
		"stun:" + cfg.StunServer,
		"stun:" + defaultStunServer2,
	}

	// Generate obfuscation key for DPI resistance (32 bytes → AES-256-GCM)
	obfsKey := make([]byte, 32)
	if _, err := crypto_rand.Read(obfsKey); err != nil {
		return "", fmt.Errorf("generate obfs key: %w", err)
	}
	applog.Info("Generated UDP obfuscation key for WebRTC path (AES-256-GCM)")

	sessionID := generateUUID()
	applog.Infof("Holepunch session ID: %s", sessionID)

	// Derive UDP relay address from signaling server for NAT fallback
	// (only when explicitly enabled — disabled by default).
	var relayAddr string
	if cfg.UseRelay {
		relayAddr = deriveRelayAddr(cfg.SignalingURL)
		if relayAddr != "" {
			applog.Infof("UDP relay fallback enabled: %s", relayAddr)
		}
	}

	// Map transport mode config to enum
	var transportMode webrtcpkg.TransportMode
	if cfg.TransportMode == "media" {
		transportMode = webrtcpkg.TransportMediaStream
	}

	srvOpts := webrtcpkg.ServerOptions{
		Padding:              cfg.PaddingEnabled,
		PaddingMax:           cfg.PaddingMax,
		RateLimitUp:          cfg.RateLimitUp,
		RateLimitDown:        cfg.RateLimitDown,
		DisableIPv6:          cfg.DisableIPv6,
		TransportMode:        transportMode,
		NumChannels:          cfg.NumChannels,
		SmuxStreamBuffer:     cfg.SmuxStreamBuffer,
		SmuxSessionBuffer:    cfg.SmuxSessionBuffer,
		SmuxFrameSize:        cfg.SmuxFrameSize,
		SmuxKeepAlive:        cfg.SmuxKeepAlive,
		SmuxKeepAliveTimeout: cfg.SmuxKeepAliveTimeout,
		DCMaxBuffered:        cfg.DCMaxBuffered,
		DCLowMark:            cfg.DCLowMark,
		PeerConfig: webrtcpkg.PeerConfig{
			SCTPRecvBuffer:     cfg.SCTPRecvBuffer,
			SCTPRTOMax:         cfg.SCTPRTOMax,
			UDPReadBuffer:      cfg.UDPReadBuffer,
			UDPWriteBuffer:     cfg.UDPWriteBuffer,
			ICEDisconnTimeout:  cfg.ICEDisconnTimeout,
			ICEFailedTimeout:   cfg.ICEFailedTimeout,
			ICEKeepalive:       cfg.ICEKeepalive,
			DTLSRetransmit:     cfg.DTLSRetransmit,
			DTLSSkipVerify:     cfg.DTLSSkipVerify,
			SCTPZeroChecksum:   cfg.SCTPZeroChecksum,
			DisableCloseByDTLS: cfg.DisableCloseByDTLS,
		},
	}
	if transportMode == webrtcpkg.TransportMediaStream {
		srvOpts.ObfsKey = obfsKey
	}
	npc := cfg.NumPeerConnections
	if npc <= 0 {
		npc = 1
	}

	wrtcGroup, sdpOffers, err := startServerGroupWithSrflx(npc, iceServers, obfsKey, relayAddr, sessionID, srvOpts)
	if err != nil {
		return "", fmt.Errorf("start WebRTC server group: %w", err)
	}

	// Post SDP offers to signaling (server path — no TUN active, no protection needed)
	applog.Infof("Posting %d SDP offers to signaling: %s/session/%s/offer", npc, cfg.SignalingURL, sessionID)
	if err := signaling.PostSDPOffers(cfg.SignalingURL, sessionID, sdpOffers, nil); err != nil {
		wrtcGroup.Stop()
		return "", fmt.Errorf("post SDP offers: %w", err)
	}
	applog.Success("SDP offers posted to signaling server")

	activeWebRTCServerGroup = wrtcGroup
	serverMethod = "holepunch"

	// Create cancellable context for the accept-loop goroutine
	serverCtx, srvCancel := context.WithCancel(context.Background())
	serverCancel = srvCancel

	// Accept loop: wait for clients and handle reconnections.
	// STUN keepalive on the raw socket keeps the NAT mapping alive
	// without creating new groups every cycle.
	sigURL := cfg.SignalingURL
	sid := sessionID
	serverOpts := srvOpts // capture for goroutine
	serverNPC := npc
	stunKAAddrs := []string{cfg.StunServer, defaultStunServer2}
	go func() {
		var prevWrtcGroup *webrtcpkg.ServerGroup
		currentOffers := sdpOffers // track for re-posting without new group

		// Start STUN keepalive on the raw socket to maintain the NAT
		// mapping. This replaces the expensive group-cycling approach
		// that created new sockets + STUN discovery every 25s.
		var stunStop chan struct{}
		if raw := wrtcGroup.GetRawConn(); raw != nil {
			stunStop = webrtcpkg.StartSTUNKeepalive(raw, stunKAAddrs, 20*time.Second)
		}

		defer func() {
			if stunStop != nil {
				close(stunStop)
			}
			if prevWrtcGroup != nil {
				prevWrtcGroup.Stop()
			}
		}()
		for {
			select {
			case <-serverCtx.Done():
				return
			default:
			}

			mu.Lock()
			running := serverRunning
			mu.Unlock()
			if !running {
				return
			}

			applog.Infof("Waiting for client SDP answers on session %s (SSE)...", sid)
			sseCtx, sseCancel := context.WithTimeout(serverCtx, sdpPollTimeout)
			sdpAnswersRaw, err := signaling.WaitSDPAnswersSSE(sseCtx, sigURL, sid, nil)
			sseCancel()
			if err != nil {
				applog.Infof("SSE wait failed (%v), falling back to polling...", err)
				sdpAnswersRaw, err = pollForSDPAnswers(serverCtx, sigURL, sid, sdpPollTimeout, nil)
			}
			if err != nil {
				// No client yet — re-POST same offers to keep signaling
				// session alive. STUN keepalive on the raw socket keeps
				// the NAT mapping alive without creating new groups.
				mu.Lock()
				if !serverRunning {
					mu.Unlock()
					return
				}
				mu.Unlock()

				if err := signaling.PostSDPOffers(sigURL, sid, currentOffers, nil); err != nil {
					applog.Warnf("Re-post SDP offers failed: %v", err)
				}

				cleanupDrainingServerGroups()
				continue
			}

			applog.Infof("Client SDP answers received (%d PCs)", len(sdpAnswersRaw))

			// Clear stale answers synchronously to prevent the next SSE
			// wait from picking up this same answer again.
			signaling.PostSDPAnswers(sigURL, sid, nil, nil)

			// Try AcceptAnswers on both current and previous groups.
			// Answers may belong to either generation due to the race at
			// the NAT keepalive boundary. Wrong ICE credentials cause ICE
			// auth failure (timeout), not an AcceptAnswers error.
			currGroup := wrtcGroup
			prevGroup := prevWrtcGroup

			currErr := currGroup.AcceptAnswers(sdpAnswersRaw)
			if currErr != nil {
				applog.Warnf("Accept SDP answers on current group: %v", currErr)
			}

			var prevErr error
			if prevGroup != nil {
				prevErr = prevGroup.AcceptAnswers(sdpAnswersRaw)
				if prevErr != nil {
					applog.Warnf("Accept SDP answers on previous group: %v", prevErr)
				}
			}

			if currErr != nil && (prevGroup == nil || prevErr != nil) {
				applog.Errorf("Accept SDP answers failed on all groups")
				continue
			}

			// Race WaitConnected concurrently. The group with matching ICE
			// credentials connects; the mismatched one times out.
			applog.Info("Waiting for WebRTC connections (racing current + previous)...")
			timeout := 65*time.Second + time.Duration(serverNPC-1)*15*time.Second

			type raceResult struct {
				group *webrtcpkg.ServerGroup
				label string
				err   error
			}
			raceCh := make(chan raceResult, 2)
			raceCount := 0

			if currErr == nil {
				raceCount++
				go func() {
					raceCh <- raceResult{currGroup, "current", currGroup.WaitConnected(timeout)}
				}()
			}
			if prevGroup != nil && prevErr == nil {
				raceCount++
				go func() {
					raceCh <- raceResult{prevGroup, "previous", prevGroup.WaitConnected(timeout)}
				}()
			}

			// Wait for first success or all failures.
			var winner *webrtcpkg.ServerGroup
			for i := 0; i < raceCount; i++ {
				r := <-raceCh
				if r.err == nil && winner == nil {
					winner = r.group
					applog.Successf("WebRTC connected via %s group", r.label)
					break // Don't wait for the loser
				}
				applog.Warnf("WebRTC %s group failed: %v", r.label, r.err)
			}

			if winner != nil {
				totalPeersEver.Add(1)

				// Stop the losing group to unblock its goroutine.
				if currErr == nil && winner != currGroup {
					currGroup.Stop()
				}
				if prevGroup != nil && prevErr == nil && winner != prevGroup {
					prevGroup.Stop()
				}

				// Ensure wrtcGroup points to the winner
				if winner != wrtcGroup {
					mu.Lock()
					activeWebRTCServerGroup = winner
					mu.Unlock()
					wrtcGroup = winner
				}
				prevWrtcGroup = nil
			} else {
				applog.Errorf("WebRTC connection failed on all groups")
				// Clean up previous group since it failed
				if prevGroup != nil {
					prevGroup.Stop()
				}
				prevWrtcGroup = nil
			}

			// Clean up prevWrtcGroup — connection succeeded, prev is stale
			if prevWrtcGroup != nil {
				if prevWrtcGroup.IsAlive() {
					mu.Lock()
					drainingWebRTCServerGroups = append(drainingWebRTCServerGroups, prevWrtcGroup)
					mu.Unlock()
				} else {
					prevWrtcGroup.Stop()
				}
				prevWrtcGroup = nil
			}

			// Prep for next client
			mu.Lock()
			if !serverRunning {
				mu.Unlock()
				return
			}
			mu.Unlock()

			// Stop STUN keepalive on old group before creating new one.
			if stunStop != nil {
				close(stunStop)
				stunStop = nil
			}

			newGroup, newOffers, err := startServerGroupWithSrflx(serverNPC, iceServers, obfsKey, relayAddr, sid, serverOpts)
			if err != nil {
				applog.Warnf("Re-create WebRTC server group failed: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}

			signaling.PostSDPAnswers(sigURL, sid, nil, nil)

			if err := signaling.PostSDPOffers(sigURL, sid, newOffers, nil); err != nil {
				applog.Warnf("Re-post SDP offers failed: %v", err)
				newGroup.Stop()
				time.Sleep(2 * time.Second)
				continue
			}

			currentOffers = newOffers

			mu.Lock()
			// Move old group to draining list, swap in new for next accept.
			if wrtcGroup != nil && wrtcGroup.IsAlive() {
				drainingWebRTCServerGroups = append(drainingWebRTCServerGroups, wrtcGroup)
			} else if wrtcGroup != nil {
				wrtcGroup.Stop()
			}
			activeWebRTCServerGroup = newGroup
			wrtcGroup = newGroup
			mu.Unlock()

			// Start STUN keepalive on the new group's socket.
			if raw := newGroup.GetRawConn(); raw != nil {
				stunStop = webrtcpkg.StartSTUNKeepalive(raw, stunKAAddrs, 20*time.Second)
			}
			applog.Info("Ready for next client connection")

			cleanupDrainingServerGroups()
		}
	}()

	// Set transport version based on mode
	transportV := 0
	if transportMode == webrtcpkg.TransportMediaStream {
		transportV = 2
	}

	serverInfo = &connectionInfo{
		Method:               "holepunch",
		Transport:            "webrtc",
		SessionID:            sessionID,
		Protocol:             "webrtc",
		ObfsKey:              hex.EncodeToString(obfsKey),
		RelayAddr:            relayAddr,
		Padding:              cfg.PaddingEnabled,
		Version:              2,
		SigV:                 2,
		TransportV:           transportV,
		NumPeerConns:         npc,
		NumChannels:          cfg.NumChannels,
		SmuxStreamBuffer:     cfg.SmuxStreamBuffer,
		SmuxSessionBuffer:    cfg.SmuxSessionBuffer,
		SmuxFrameSize:        cfg.SmuxFrameSize,
		DCMaxBuffered:        cfg.DCMaxBuffered,
		DCLowMark:            cfg.DCLowMark,
		PaddingMax:           cfg.PaddingMax,
		SmuxKeepAlive:        cfg.SmuxKeepAlive,
		SmuxKeepAliveTimeout: cfg.SmuxKeepAliveTimeout,
	}
	serverRunning = true
	serverStartTime = time.Now()

	infoJSON, _ := json.Marshal(serverInfo)
	applog.Successf("Server started (holepunch/webrtc), session=%s", sessionID)
	return encodeBase64(infoJSON), nil
}

// === Discovery API ===

// RegisterServerDiscovery lists the server for discovery. settingsJSON needs signalingUrl, discoveryUrl, displayName, and room.
func RegisterServerDiscovery(connectionCode string, settingsJSON string) (string, error) {
	var cfg struct {
		SignalingURL string `json:"signalingUrl"`
		DiscoveryURL string `json:"discoveryUrl"`
		DisplayName  string `json:"displayName"`
		Room         string `json:"room"`
	}
	if settingsJSON != "" {
		if err := json.Unmarshal([]byte(settingsJSON), &cfg); err != nil {
			return "", fmt.Errorf("parse settings: %w", err)
		}
	}
	if cfg.SignalingURL == "" {
		cfg.SignalingURL = defaultSignalingURL
	}
	// Use dedicated discovery URL if provided; fall back to signaling URL.
	if cfg.DiscoveryURL == "" {
		cfg.DiscoveryURL = cfg.SignalingURL
	}

	// Unregister existing listing if any (prevents duplicates on app restart)
	mu.Lock()
	oldID := discoveryListingID
	oldURL := discoverySignalingURL
	oldStop := heartbeatStop
	discoveryListingID = ""
	discoverySignalingURL = ""
	heartbeatStop = nil
	mu.Unlock()

	if oldStop != nil {
		close(oldStop)
	}
	if oldID != "" {
		_ = signaling.DeregisterServer(oldURL, oldID)
	}

	mu.Lock()
	method := ""
	transport := ""
	protocol := ""
	if serverInfo != nil {
		method = serverInfo.Method
		transport = serverInfo.Transport
		protocol = serverInfo.Protocol
	}
	mu.Unlock()

	listingID, err := signaling.RegisterServer(cfg.DiscoveryURL, cfg.DisplayName, cfg.Room, connectionCode, method, transport, protocol)
	if err != nil {
		return "", err
	}

	mu.Lock()
	discoveryListingID = listingID
	discoverySignalingURL = cfg.DiscoveryURL
	heartbeatStop = make(chan struct{})
	stopCh := heartbeatStop
	sigURL := cfg.DiscoveryURL
	lID := listingID
	mu.Unlock()

	// Capture registration params for re-registration on expiry
	regName := cfg.DisplayName
	regRoom := cfg.Room
	regCode := connectionCode
	regMethod := method
	regTransport := transport
	regProtocol := protocol

	// Start heartbeat goroutine with auto re-registration
	go func() {
		currentID := lID
		ticker := time.NewTicker(discoveryHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				err := signaling.HeartbeatServer(sigURL, currentID)
				if err == nil {
					continue
				}
				if errors.Is(err, signaling.ErrListingExpired) {
					applog.Warnf("Discovery listing expired, re-registering...")
					newID, regErr := signaling.RegisterServer(sigURL, regName, regRoom, regCode, regMethod, regTransport, regProtocol)
					if regErr != nil {
						applog.Warnf("Discovery re-registration failed: %v", regErr)
						continue
					}
					currentID = newID
					mu.Lock()
					discoveryListingID = newID
					mu.Unlock()
					applog.Infof("Re-registered on discovery with ID: %s", newID)
				} else {
					applog.Warnf("Discovery heartbeat failed: %v", err)
				}
			}
		}
	}()

	applog.Infof("Registered on discovery with ID: %s", listingID)
	return listingID, nil
}

// UnregisterServerDiscovery removes the server from the signaling server discovery.
func UnregisterServerDiscovery() error {
	mu.Lock()
	lID := discoveryListingID
	sigURL := discoverySignalingURL
	stopCh := heartbeatStop
	discoveryListingID = ""
	discoverySignalingURL = ""
	heartbeatStop = nil
	mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}
	if lID == "" {
		return nil
	}
	return signaling.DeregisterServer(sigURL, lID)
}

// ListAvailableServers returns a JSON array of available servers from the signaling server.
func ListAvailableServers(signalingURL string, room string) (string, error) {
	return signaling.ListServers(signalingURL, room)
}

// StopServer stops the proxy server and cleans up resources.
func StopServer() error {
	mu.Lock()
	defer mu.Unlock()

	if !serverRunning {
		return nil
	}

	applog.Info("Stopping server...")

	// Auto-unregister from discovery
	if discoveryListingID != "" {
		if heartbeatStop != nil {
			close(heartbeatStop)
			heartbeatStop = nil
		}
		signaling.DeregisterServer(discoverySignalingURL, discoveryListingID)
		discoveryListingID = ""
		discoverySignalingURL = ""
	}

	// Cancel the accept-loop goroutine first so it exits promptly
	if serverCancel != nil {
		serverCancel()
		serverCancel = nil
	}

	switch serverMethod {
	case "holepunch":
		if activeWebRTCServerGroup != nil {
			activeWebRTCServerGroup.Stop()
			activeWebRTCServerGroup = nil
		}
		for _, g := range drainingWebRTCServerGroups {
			g.Stop()
		}
		drainingWebRTCServerGroups = nil
	default: // "upnp"
		if err := xray.StopXray(); err != nil {
			applog.Errorf("Stop xray failed: %v", err)
			return fmt.Errorf("stop xray: %w", err)
		}

		// Clean up UPnP port mapping
		if serverInfo != nil && serverInfo.Method == "upnp" {
			upnpProto := "TCP"
			if serverInfo.Protocol == "vless" && serverInfo.Transport == "kcp" {
				upnpProto = "UDP"
			}
			nat.RemoveUPnPMapping(serverInfo.Port, upnpProto)
			applog.Infof("UPnP port mapping removed (%s)", upnpProto)
		}
		xray.ResetClientTracker()
	}

	// Clean up manual signaling state
	if stunKeepaliveDone != nil {
		close(stunKeepaliveDone)
		stunKeepaliveDone = nil
	}
	manualMode = false
	manualOfferCode = ""

	serverRunning = false
	serverInfo = nil
	serverMethod = ""
	serverStartTime = time.Time{}
	totalPeersEver.Store(0)
	serverRateTracker.reset()
	applog.Success("Server stopped")
	return nil
}

// === Manual Signaling API ===

// StartServerManual creates a WebRTC offer for out-of-band sharing (no signaling server).
// The returned "M1:..." code needs to be shared with the client manually.
func StartServerManual(settingsJSON string) (string, error) {
	mu.Lock()
	defer mu.Unlock()

	if serverRunning {
		return "", fmt.Errorf("server already running")
	}

	applog.Info("Starting manual signaling server...")

	// Parse settings with defaults
	cfg := serverConfig{
		ListenPort:          10853,
		StunServer:          defaultStunServer1,
		SignalingURL:        defaultSignalingURL,
		NatMethod:           "holepunch",
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
	}
	if settingsJSON != "" {
		if err := json.Unmarshal([]byte(settingsJSON), &cfg); err != nil {
			return "", fmt.Errorf("parse settings: %w", err)
		}
	}

	applog.SetMaskIPs(cfg.MaskIPs)

	iceServers := []string{
		"stun:" + cfg.StunServer,
		"stun:" + defaultStunServer2,
	}

	// Generate obfuscation key (32 bytes → AES-256-GCM)
	obfsKey := make([]byte, 32)
	if _, err := crypto_rand.Read(obfsKey); err != nil {
		return "", fmt.Errorf("generate obfs key: %w", err)
	}
	applog.Info("Manual: generated UDP obfuscation key (AES-256-GCM)")

	sessionID := generateUUID()

	// Derive relay address if enabled
	var relayAddr string
	if cfg.UseRelay {
		relayAddr = deriveRelayAddr(cfg.SignalingURL)
		if relayAddr != "" {
			applog.Infof("Manual: relay fallback enabled: %s", relayAddr)
		}
	}

	// Map transport mode
	var transportMode webrtcpkg.TransportMode
	if cfg.TransportMode == "media" {
		transportMode = webrtcpkg.TransportMediaStream
	}

	srvOpts := webrtcpkg.ServerOptions{
		Padding:              cfg.PaddingEnabled,
		PaddingMax:           cfg.PaddingMax,
		RateLimitUp:          cfg.RateLimitUp,
		RateLimitDown:        cfg.RateLimitDown,
		DisableIPv6:          cfg.DisableIPv6,
		TransportMode:        transportMode,
		NumChannels:          cfg.NumChannels,
		SmuxStreamBuffer:     cfg.SmuxStreamBuffer,
		SmuxSessionBuffer:    cfg.SmuxSessionBuffer,
		SmuxFrameSize:        cfg.SmuxFrameSize,
		SmuxKeepAlive:        cfg.SmuxKeepAlive,
		SmuxKeepAliveTimeout: cfg.SmuxKeepAliveTimeout,
		DCMaxBuffered:        cfg.DCMaxBuffered,
		DCLowMark:            cfg.DCLowMark,
		PeerConfig: webrtcpkg.PeerConfig{
			SCTPRecvBuffer:     cfg.SCTPRecvBuffer,
			SCTPRTOMax:         cfg.SCTPRTOMax,
			UDPReadBuffer:      cfg.UDPReadBuffer,
			UDPWriteBuffer:     cfg.UDPWriteBuffer,
			ICEDisconnTimeout:  cfg.ICEDisconnTimeout,
			ICEFailedTimeout:   cfg.ICEFailedTimeout,
			ICEKeepalive:       cfg.ICEKeepalive,
			DTLSRetransmit:     cfg.DTLSRetransmit,
			DTLSSkipVerify:     cfg.DTLSSkipVerify,
			SCTPZeroChecksum:   cfg.SCTPZeroChecksum,
			DisableCloseByDTLS: cfg.DisableCloseByDTLS,
		},
	}
	if transportMode == webrtcpkg.TransportMediaStream {
		srvOpts.ObfsKey = obfsKey
	}

	// Force npc=1 for manual mode (keeps code size manageable)
	wrtcGroup, sdpOffers, err := startServerGroupWithSrflx(1, iceServers, obfsKey, relayAddr, sessionID, srvOpts)
	if err != nil {
		return "", fmt.Errorf("start WebRTC server group: %w", err)
	}

	// Compress SDP
	compressedSDP, err := signaling.CompressSDP(sdpOffers[0])
	if err != nil {
		wrtcGroup.Stop()
		return "", fmt.Errorf("compress SDP: %w", err)
	}

	// Set transport version
	transportV := 0
	if transportMode == webrtcpkg.TransportMediaStream {
		transportV = 2
	}

	// Build manual offer
	offer := &signaling.ManualOffer{
		Version:              3,
		ObfsKey:              hex.EncodeToString(obfsKey),
		RelayAddr:            relayAddr,
		NumChannels:          cfg.NumChannels,
		SmuxStreamBuffer:     cfg.SmuxStreamBuffer,
		SmuxSessionBuffer:    cfg.SmuxSessionBuffer,
		SmuxFrameSize:        cfg.SmuxFrameSize,
		DCMaxBuffered:        cfg.DCMaxBuffered,
		DCLowMark:            cfg.DCLowMark,
		PaddingMax:           cfg.PaddingMax,
		Padding:              cfg.PaddingEnabled,
		TransportV:           transportV,
		SmuxKeepAlive:        cfg.SmuxKeepAlive,
		SmuxKeepAliveTimeout: cfg.SmuxKeepAliveTimeout,
		CompressedSDP:        compressedSDP,
	}

	offerCode, err := signaling.EncodeManualOffer(offer)
	if err != nil {
		wrtcGroup.Stop()
		return "", fmt.Errorf("encode manual offer: %w", err)
	}

	// Start STUN keepalive on the raw UDP socket
	rawConn := wrtcGroup.GetRawConn()
	if rawConn != nil {
		stunAddrs := []string{cfg.StunServer, defaultStunServer2}
		stunKeepaliveDone = webrtcpkg.StartSTUNKeepalive(rawConn, stunAddrs, 20*time.Second)
	}

	activeWebRTCServerGroup = wrtcGroup
	serverMethod = "holepunch"
	manualMode = true
	manualOfferCode = offerCode
	serverRunning = true
	serverStartTime = time.Now()

	applog.Successf("Manual server started, offer code length=%d", len(offerCode))
	return offerCode, nil
}

// AcceptManualAnswer accepts a manually-shared answer code from the remote client.
// Blocks until the ICE connection is established or timeout.
func AcceptManualAnswer(answerCode string) error {
	mu.Lock()
	if !serverRunning || !manualMode || activeWebRTCServerGroup == nil {
		mu.Unlock()
		return fmt.Errorf("no manual server running")
	}
	wrtcGroup := activeWebRTCServerGroup
	mu.Unlock()

	answerCode = strings.TrimSpace(answerCode)
	applog.Infof("AcceptManualAnswer: decoding answer code (%d chars)", len(answerCode))

	answer, err := signaling.DecodeManualAnswer(answerCode)
	if err != nil {
		return fmt.Errorf("decode manual answer: %w", err)
	}

	sdp, err := signaling.DecompressSDP(answer.CompressedSDP)
	if err != nil {
		return fmt.Errorf("decompress SDP: %w", err)
	}

	applog.Infof("AcceptManualAnswer: SDP decompressed (%d bytes)", len(sdp))

	if err := wrtcGroup.AcceptAnswers([]string{sdp}); err != nil {
		return fmt.Errorf("accept SDP answer: %w", err)
	}

	// Stop STUN keepalive — ICE will handle keepalive from here
	mu.Lock()
	if stunKeepaliveDone != nil {
		close(stunKeepaliveDone)
		stunKeepaliveDone = nil
	}
	mu.Unlock()

	// Wait for connection (65s timeout for single PC)
	timeout := 65 * time.Second
	applog.Info("AcceptManualAnswer: waiting for WebRTC connection...")
	if err := wrtcGroup.WaitConnected(timeout); err != nil {
		return fmt.Errorf("WebRTC connection: %w", err)
	}

	totalPeersEver.Add(1)
	applog.Success("AcceptManualAnswer: WebRTC connected")
	return nil
}

// GetManualOfferCode returns the current manual offer code, or empty if not in manual mode.
// Mostly used to re-display the code after the app comes back from background.
func GetManualOfferCode() string {
	mu.Lock()
	defer mu.Unlock()
	return manualOfferCode
}

// ProcessManualOffer processes a manually-shared offer code on the client side.
// Creates a WebRTC client group and returns the answer code to share back.
func ProcessManualOffer(offerCode string, settingsJSON string) (string, error) {
	mu.Lock()
	if clientRunning || clientConnecting {
		mu.Unlock()
		return "", fmt.Errorf("client already running")
	}
	clientConnecting = true
	clientConnectCtx, clientConnectCancel = context.WithCancel(context.Background())
	mu.Unlock()

	applog.Info("ProcessManualOffer: starting...")

	connected := false
	defer func() {
		if !connected {
			mu.Lock()
			clientConnecting = false
			if pendingWebRTCClientGroup != nil {
				pendingWebRTCClientGroup.Stop()
				pendingWebRTCClientGroup = nil
			}
			if clientConnectCancel != nil {
				clientConnectCancel()
				clientConnectCancel = nil
			}
			mu.Unlock()
		}
	}()

	// Parse client settings
	cfg := clientConfig{
		SocksPort:    10808,
		TunAddress:   "10.0.0.2",
		MTU:          1400,
		DNS1:         "8.8.8.8",
		DNS2:         "1.1.1.1",
		StunServer:   defaultStunServer1,
		SignalingURL: defaultSignalingURL,
	}
	if settingsJSON != "" {
		if err := json.Unmarshal([]byte(settingsJSON), &cfg); err != nil {
			return "", fmt.Errorf("parse settings: %w", err)
		}
	}

	applog.SetMaskIPs(cfg.MaskIPs)

	offerCode = strings.TrimSpace(offerCode)
	offer, err := signaling.DecodeManualOffer(offerCode)
	if err != nil {
		return "", fmt.Errorf("decode manual offer: %w", err)
	}

	sdp, err := signaling.DecompressSDP(offer.CompressedSDP)
	if err != nil {
		return "", fmt.Errorf("decompress SDP: %w", err)
	}

	applog.Infof("ProcessManualOffer: SDP decompressed (%d bytes), nc=%d", len(sdp), offer.NumChannels)

	iceServers := []string{
		"stun:" + cfg.StunServer,
		"stun:" + defaultStunServer2,
	}

	obfsKey, _ := hex.DecodeString(offer.ObfsKey)
	if len(obfsKey) > 0 {
		applog.Info("ProcessManualOffer: obfuscation key present")
	}

	sessionID := generateUUID()

	clientOpts := webrtcpkg.ClientOptions{
		Padding:              offer.Padding,
		PaddingMax:           offer.PaddingMax,
		NumChannels:          offer.NumChannels,
		SmuxStreamBuffer:     offer.SmuxStreamBuffer,
		SmuxSessionBuffer:    offer.SmuxSessionBuffer,
		SmuxFrameSize:        offer.SmuxFrameSize,
		SmuxKeepAlive:        offer.SmuxKeepAlive,
		SmuxKeepAliveTimeout: offer.SmuxKeepAliveTimeout,
		DCMaxBuffered:        offer.DCMaxBuffered,
		DCLowMark:            offer.DCLowMark,
		PeerConfig: webrtcpkg.PeerConfig{
			SCTPRecvBuffer:     cfg.SCTPRecvBuffer,
			SCTPRTOMax:         cfg.SCTPRTOMax,
			UDPReadBuffer:      cfg.UDPReadBuffer,
			UDPWriteBuffer:     cfg.UDPWriteBuffer,
			ICEDisconnTimeout:  cfg.ICEDisconnTimeout,
			ICEFailedTimeout:   cfg.ICEFailedTimeout,
			ICEKeepalive:       cfg.ICEKeepalive,
			DTLSRetransmit:     cfg.DTLSRetransmit,
			DTLSSkipVerify:     cfg.DTLSSkipVerify,
			SCTPZeroChecksum:   cfg.SCTPZeroChecksum,
			DisableCloseByDTLS: cfg.DisableCloseByDTLS,
		},
	}
	if offer.TransportV == 2 {
		clientOpts.TransportMode = webrtcpkg.TransportMediaStream
		clientOpts.ObfsKey = obfsKey
	}

	// Start WebRTC client group with NO protectFn (no TUN yet)
	wrtcGroup, sdpAnswers, err := webrtcpkg.StartClientGroup(1, []string{sdp}, iceServers, nil, obfsKey, offer.RelayAddr, sessionID, clientOpts)
	if err != nil {
		return "", fmt.Errorf("start WebRTC client group: %w", err)
	}

	// Compress the answer SDP
	compressedAnswer, err := signaling.CompressSDP(sdpAnswers[0])
	if err != nil {
		wrtcGroup.Stop()
		return "", fmt.Errorf("compress answer SDP: %w", err)
	}

	manualAnswer := &signaling.ManualAnswer{
		Version:       3,
		CompressedSDP: compressedAnswer,
	}

	answerCodeStr, err := signaling.EncodeManualAnswer(manualAnswer)
	if err != nil {
		wrtcGroup.Stop()
		return "", fmt.Errorf("encode manual answer: %w", err)
	}

	// Build a connectionInfo for saved state (needed by StartTunnel)
	info := connectionInfo{
		Method:               "holepunch",
		Transport:            "webrtc",
		SessionID:            sessionID,
		Protocol:             "webrtc",
		ObfsKey:              offer.ObfsKey,
		RelayAddr:            offer.RelayAddr,
		Padding:              offer.Padding,
		Version:              3,
		TransportV:           offer.TransportV,
		NumPeerConns:         1,
		NumChannels:          offer.NumChannels,
		SmuxStreamBuffer:     offer.SmuxStreamBuffer,
		SmuxSessionBuffer:    offer.SmuxSessionBuffer,
		SmuxFrameSize:        offer.SmuxFrameSize,
		DCMaxBuffered:        offer.DCMaxBuffered,
		DCLowMark:            offer.DCLowMark,
		PaddingMax:           offer.PaddingMax,
		SmuxKeepAlive:        offer.SmuxKeepAlive,
		SmuxKeepAliveTimeout: offer.SmuxKeepAliveTimeout,
	}

	// Save for StartTunnel (same pattern as ConnectWebRTC)
	mu.Lock()
	pendingWebRTCClientGroup = wrtcGroup
	savedConnInfo = info
	savedClientCfg = cfg
	savedICEServers = iceServers
	savedClientOpts = clientOpts
	mu.Unlock()

	connected = true
	applog.Successf("ProcessManualOffer: answer code ready (%d chars)", len(answerCodeStr))
	return answerCodeStr, nil
}

// WaitManualConnection waits for the WebRTC connection to establish on the
// client side after the server has accepted the answer. Called between
// ProcessManualOffer and StartTunnel.
func WaitManualConnection(timeoutSec int) error {
	mu.Lock()
	if !clientConnecting || pendingWebRTCClientGroup == nil {
		mu.Unlock()
		return fmt.Errorf("no pending manual connection")
	}
	wrtcGroup := pendingWebRTCClientGroup
	ctx := clientConnectCtx
	mu.Unlock()

	timeout := time.Duration(timeoutSec) * time.Second
	applog.Infof("WaitManualConnection: waiting up to %v...", timeout)

	if err := wrtcGroup.WaitConnectedCtx(ctx, timeout); err != nil {
		return fmt.Errorf("WebRTC connection: %w", err)
	}

	applog.Success("WaitManualConnection: connected")
	return nil
}

// GetServerStatus returns a JSON string with server status information.
func GetServerStatus() string {
	mu.Lock()
	defer mu.Unlock()

	var clientCount int
	var bytesUp, bytesDown int64
	var dataChannels, peerConns int
	var streamDist []int

	switch serverMethod {
	case "holepunch":
		if activeWebRTCServerGroup != nil {
			clientCount += activeWebRTCServerGroup.GetClientCount()
			up, down := activeWebRTCServerGroup.GetStats()
			bytesUp += up
			bytesDown += down
			dataChannels = activeWebRTCServerGroup.GetChannelCount()
			peerConns = activeWebRTCServerGroup.Count()
			streamDist = activeWebRTCServerGroup.GetStreamDistribution()
		}
		for _, g := range drainingWebRTCServerGroups {
			clientCount += g.GetClientCount()
			up, down := g.GetStats()
			bytesUp += up
			bytesDown += down
		}
	default:
		clientCount = xray.GetClientCount()
	}

	rateUp, rateDown := serverRateTracker.update(bytesUp, bytesDown)

	var uptimeSec float64
	if serverRunning && !serverStartTime.IsZero() {
		uptimeSec = time.Since(serverStartTime).Seconds()
	}

	heapMB := getCachedHeapMB()

	status := map[string]interface{}{
		"running":      serverRunning,
		"clients":      clientCount,
		"upnp":         serverInfo != nil && serverInfo.Method == "upnp",
		"publicIP":     "",
		"protocol":     "",
		"transport":    "",
		"totalPeers":   int(totalPeersEver.Load()),
		"bytesUp":      bytesUp,
		"bytesDown":    bytesDown,
		"rateUp":       rateUp,
		"rateDown":     rateDown,
		"uptimeSec":    uptimeSec,
		"goroutines":   runtime.NumGoroutine(),
		"heapMB":       heapMB,
		"dataChannels":    dataChannels,
		"peerConnections": peerConns,
		"smuxStreams":     clientCount,
		"streamDist":      streamDist,
	}
	if serverInfo != nil {
		status["publicIP"] = serverInfo.PublicIP
		status["protocol"] = serverInfo.Protocol
		status["transport"] = serverInfo.Transport
		infoJSON, _ := json.Marshal(serverInfo)
		status["connectionCode"] = encodeBase64(infoJSON)
		vl := xray.GenerateVLESSLink(serverInfo.UUID, serverInfo.PublicIP, serverInfo.Port, serverInfo.ProxySettings, "natproxy")
		if vl != "" {
			status["vlessLink"] = vl
		}
	}

	status["manualMode"] = manualMode
	if manualMode && manualOfferCode != "" {
		status["offerCode"] = manualOfferCode
	}

	data, _ := json.Marshal(status)
	return string(data)
}

// cleanupDrainingServerGroups stops and removes draining WebRTC server groups
// that have no active streams. Called periodically from the accept loop.
func cleanupDrainingServerGroups() {
	mu.Lock()
	defer mu.Unlock()

	remaining := drainingWebRTCServerGroups[:0]
	for _, g := range drainingWebRTCServerGroups {
		if !g.IsAlive() {
			g.Stop()
		} else {
			remaining = append(remaining, g)
		}
	}
	drainingWebRTCServerGroups = remaining
}

// === Client API ===

// StartClient connects to a server using the provided connection code.
// tunFd is the TUN file descriptor from Android VpnService.
// protectFd is a callback to VpnService.protect() for socket protection.
// settingsJSON is a JSON string with client configuration overrides.
//
// Drops mutex during slow network ops to avoid blocking Android UI
func StartClient(connectionCode string, tunFd int, protectFd ProtectFunc, settingsJSON string) error {
	mu.Lock()
	if clientRunning || clientConnecting {
		mu.Unlock()
		return fmt.Errorf("client already running")
	}
	clientConnecting = true
	clientConnectCtx, clientConnectCancel = context.WithCancel(context.Background())
	mu.Unlock()

	applog.Info("Starting client...")

	// Clear connecting flag on errors
	connected := false
	defer func() {
		if !connected {
			mu.Lock()
			clientConnecting = false
			if clientConnectCancel != nil {
				clientConnectCancel()
				clientConnectCancel = nil
			}
			mu.Unlock()
		}
	}()

	// Parse settings with defaults (fast, no lock needed)
	cfg := clientConfig{
		SocksPort:    10808,
		TunAddress:   "10.0.0.2",
		MTU:          1400, // ~1400 accounts for WebRTC overhead (IP+UDP+DTLS+SCTP+obfs ≈ 81 bytes)
		DNS1:         "8.8.8.8",
		DNS2:         "1.1.1.1",
		StunServer:   defaultStunServer1,
		SignalingURL: defaultSignalingURL,
	}
	if settingsJSON != "" {
		if err := json.Unmarshal([]byte(settingsJSON), &cfg); err != nil {
			return fmt.Errorf("parse settings: %w", err)
		}
	}

	applog.SetMaskIPs(cfg.MaskIPs)

	// Decode connection info (fast)
	infoJSON, err := decodeBase64(connectionCode)
	if err != nil {
		return fmt.Errorf("invalid connection code: %w", err)
	}

	var info connectionInfo
	if err := json.Unmarshal(infoJSON, &info); err != nil {
		return fmt.Errorf("parse connection info: %w", err)
	}

	applog.Infof("Connecting via %s/%s (method=%s)", info.Protocol, info.Transport, info.Method)

	protectCallback := func(fd int) bool {
		return protectFd.Protect(fd)
	}

	// --- Slow networking: runs WITHOUT holding mu ---

	if info.Method == "holepunch" && info.Transport == "webrtc" && info.SessionID != "" {
		// WebRTC hole punch path
		return startClientWebRTC(clientConnectCtx, info, cfg, tunFd, protectCallback, &connected)
	}

	// UPnP / legacy xray-core path
	return startClientXray(info, cfg, tunFd, protectCallback, &connected)
}

// ConnectWebRTC establishes a WebRTC connection WITHOUT requiring a TUN
// interface or VPN service. Since no TUN exists, sockets route normally
// and no protection is needed.
//
// On success, the connected group is stored in pendingWebRTCClientGroup.
// The caller must then start the VPN service and call StartTunnel to
// protect sockets and begin routing traffic through the tunnel.
func ConnectWebRTC(connectionCode string, settingsJSON string) error {
	mu.Lock()
	if clientRunning || clientConnecting {
		mu.Unlock()
		return fmt.Errorf("client already running")
	}
	clientConnecting = true
	clientConnectCtx, clientConnectCancel = context.WithCancel(context.Background())
	ctx := clientConnectCtx
	mu.Unlock()

	applog.Info("ConnectWebRTC: starting...")

	connected := false
	defer func() {
		if !connected {
			mu.Lock()
			clientConnecting = false
			if pendingWebRTCClientGroup != nil {
				pendingWebRTCClientGroup.Stop()
				pendingWebRTCClientGroup = nil
			}
			if clientConnectCancel != nil {
				clientConnectCancel()
				clientConnectCancel = nil
			}
			mu.Unlock()
		}
	}()

	// Parse settings
	cfg := clientConfig{
		SocksPort:    10808,
		TunAddress:   "10.0.0.2",
		MTU:          1400,
		DNS1:         "8.8.8.8",
		DNS2:         "1.1.1.1",
		StunServer:   defaultStunServer1,
		SignalingURL: defaultSignalingURL,
	}
	if settingsJSON != "" {
		if err := json.Unmarshal([]byte(settingsJSON), &cfg); err != nil {
			return fmt.Errorf("parse settings: %w", err)
		}
	}

	applog.SetMaskIPs(cfg.MaskIPs)

	// Decode connection info
	infoJSON, err := decodeBase64(connectionCode)
	if err != nil {
		return fmt.Errorf("invalid connection code: %w", err)
	}
	var info connectionInfo
	if err := json.Unmarshal(infoJSON, &info); err != nil {
		return fmt.Errorf("parse connection info: %w", err)
	}

	if info.Method != "holepunch" || info.Transport != "webrtc" || info.SessionID == "" {
		return fmt.Errorf("ConnectWebRTC only supports WebRTC holepunch connections")
	}

	applog.Infof("ConnectWebRTC: session=%s", info.SessionID)

	sigURL := cfg.SignalingURL
	npc := info.NumPeerConns
	if npc <= 0 {
		npc = 1
	}

	// No protectFn — no TUN exists, traffic routes normally

	iceServers := []string{
		"stun:" + cfg.StunServer,
		"stun:" + defaultStunServer2,
	}

	obfsKey, _ := hex.DecodeString(info.ObfsKey)
	if len(obfsKey) > 0 {
		applog.Info("ConnectWebRTC: obfuscation key present")
	}

	relayAddr := info.RelayAddr
	if relayAddr != "" {
		applog.Infof("ConnectWebRTC: relay fallback: %s", relayAddr)
	}

	clientOpts := webrtcpkg.ClientOptions{
		Padding:              info.Padding,
		PaddingMax:           info.PaddingMax,
		NumChannels:          info.NumChannels,
		SmuxStreamBuffer:     info.SmuxStreamBuffer,
		SmuxSessionBuffer:    info.SmuxSessionBuffer,
		SmuxFrameSize:        info.SmuxFrameSize,
		SmuxKeepAlive:        info.SmuxKeepAlive,
		SmuxKeepAliveTimeout: info.SmuxKeepAliveTimeout,
		DCMaxBuffered:        info.DCMaxBuffered,
		DCLowMark:            info.DCLowMark,
		PeerConfig: webrtcpkg.PeerConfig{
			SCTPRecvBuffer:     cfg.SCTPRecvBuffer,
			SCTPRTOMax:         cfg.SCTPRTOMax,
			UDPReadBuffer:      cfg.UDPReadBuffer,
			UDPWriteBuffer:     cfg.UDPWriteBuffer,
			ICEDisconnTimeout:  cfg.ICEDisconnTimeout,
			ICEFailedTimeout:   cfg.ICEFailedTimeout,
			ICEKeepalive:       cfg.ICEKeepalive,
			DTLSRetransmit:     cfg.DTLSRetransmit,
			DTLSSkipVerify:     cfg.DTLSSkipVerify,
			SCTPZeroChecksum:   cfg.SCTPZeroChecksum,
			DisableCloseByDTLS: cfg.DisableCloseByDTLS,
		},
	}
	if info.TransportV == 2 {
		clientOpts.TransportMode = webrtcpkg.TransportMediaStream
		clientOpts.ObfsKey = obfsKey
	}

	// Try SSE offer stream for real-time updates; fall back to polling.
	applog.Infof("Getting %d SDP offers from signaling: %s/session/%s/offer", npc, sigURL, info.SessionID)
	sseCtx, sseCancel := context.WithCancel(ctx)
	offerCh, sseErr := signaling.StreamSDPOffers(sseCtx, sigURL, info.SessionID, nil)

	var sdpOffers []string
	if sseErr != nil {
		sseCancel()
		applog.Infof("Offer SSE failed, falling back to polling: %v", sseErr)
		sdpOffers, err = signaling.GetSDPOffersCtx(ctx, sigURL, info.SessionID, nil)
		if err != nil {
			return fmt.Errorf("get SDP offers: %w", err)
		}
	} else {
		var ok bool
		sdpOffers, ok = <-offerCh
		if !ok {
			sseCancel()
			applog.Info("Offer SSE stream closed before delivering, falling back to polling")
			sdpOffers, err = signaling.GetSDPOffersCtx(ctx, sigURL, info.SessionID, nil)
			if err != nil {
				return fmt.Errorf("get SDP offers: %w", err)
			}
		}
	}
	applog.Infof("SDP offers received (%d PCs)", len(sdpOffers))

	if ctx.Err() != nil {
		sseCancel()
		return fmt.Errorf("connection cancelled: %w", ctx.Err())
	}

	// SSE refresh-aware loop: if server refreshes offers while we're
	// creating the client group, restart with the latest offers.
	var wrtcGroup *webrtcpkg.ClientGroup
	var sdpAnswers []string
	for {
		wrtcGroup, sdpAnswers, err = webrtcpkg.StartClientGroup(npc, sdpOffers, iceServers, nil, obfsKey, relayAddr, info.SessionID, clientOpts)
		if err != nil {
			sseCancel()
			return fmt.Errorf("start WebRTC client group: %w", err)
		}

		if ctx.Err() != nil {
			wrtcGroup.Stop()
			sseCancel()
			return fmt.Errorf("connection cancelled: %w", ctx.Err())
		}

		// Before posting answers, check if newer offers arrived via SSE.
		if sseErr == nil {
			var latest []string
			drain := true
			for drain {
				select {
				case newer, chOk := <-offerCh:
					if chOk {
						latest = newer
					} else {
						drain = false
					}
				default:
					drain = false
				}
			}
			if latest != nil {
				applog.Info("ConnectWebRTC: server refreshed offers, restarting with latest")
				wrtcGroup.Stop()
				sdpOffers = latest
				continue
			}
		}
		break
	}
	sseCancel()

	// Post SDP answers
	applog.Infof("Posting %d SDP answers to signaling", len(sdpAnswers))
	if err := signaling.PostSDPAnswersCtx(ctx, sigURL, info.SessionID, sdpAnswers, nil); err != nil {
		wrtcGroup.Stop()
		return fmt.Errorf("post SDP answers: %w", err)
	}
	applog.Success("SDP answers posted")

	// Wait for connection
	applog.Infof("Waiting for %d WebRTC connections to establish...", npc)
	timeout := 65*time.Second + time.Duration(npc-1)*15*time.Second
	if err := wrtcGroup.WaitConnectedCtx(ctx, timeout); err != nil {
		wrtcGroup.Stop()
		return fmt.Errorf("WebRTC connection: %w", err)
	}
	applog.Successf("WebRTC connections established (%d PCs)", npc)

	// Save for StartTunnel and reconnect
	mu.Lock()
	pendingWebRTCClientGroup = wrtcGroup
	savedConnInfo = info
	savedClientCfg = cfg
	savedICEServers = iceServers
	savedClientOpts = clientOpts
	mu.Unlock()

	connected = true
	return nil
}

// StartTunnel protects the WebRTC sockets, creates the TUN tunnel, and
// transitions the client to the running state. Must be called after
// ConnectWebRTC succeeds and the VPN service is started.
func StartTunnel(tunFd int, protectFd ProtectFunc, settingsJSON string) error {
	mu.Lock()
	if !clientConnecting || pendingWebRTCClientGroup == nil {
		mu.Unlock()
		return fmt.Errorf("no pending WebRTC connection (call ConnectWebRTC first)")
	}
	wrtcGroup := pendingWebRTCClientGroup
	pendingWebRTCClientGroup = nil
	info := savedConnInfo
	cfg := savedClientCfg
	mu.Unlock()

	protectCallback := func(fd int) bool {
		return protectFd.Protect(fd)
	}

	// Protect WebRTC UDP sockets so they bypass the TUN
	applog.Info("StartTunnel: protecting WebRTC sockets...")
	if err := wrtcGroup.ProtectSockets(protectCallback); err != nil {
		wrtcGroup.Stop()
		mu.Lock()
		clientConnecting = false
		mu.Unlock()
		return fmt.Errorf("protect sockets: %w", err)
	}

	// Start TUN tunnel
	applog.Info("StartTunnel: creating TUN tunnel...")
	tun, err := tunnel.StartTunnelWithOptions(tunFd, "", cfg.TunAddress, cfg.MTU, cfg.DNS1, protectCallback, tunnel.TunnelOptions{
		DNS2Addr:       cfg.DNS2,
		AllowDirectDNS: cfg.AllowDirectDNS,
		DialStream:     wrtcGroup.DialStream,
	})
	if err != nil {
		wrtcGroup.Stop()
		mu.Lock()
		clientConnecting = false
		mu.Unlock()
		return fmt.Errorf("start tunnel: %w", err)
	}

	// Save protect fn for reconnect
	savedProtectFn = protectCallback

	// Update state
	mu.Lock()
	activeTunnel = tun
	activeWebRTCClientGroup = wrtcGroup
	clientConnecting = false
	clientRunning = true
	clientStartTime = time.Now()
	clientDoneCh = make(chan struct{})
	mu.Unlock()

	// Wire up fast reconnect
	wrtcGroup.OnReconnectNeeded = func() {
		applog.Warn("ICE failure detected, triggering fast reconnect")
		go attemptFastReconnect()
	}

	// Start health monitor
	go monitorClientHealth(wrtcGroup, clientDoneCh)

	applog.Successf("StartTunnel: client connected (WebRTC, %d PCs)", info.NumPeerConns)

	// Network change callback
	webrtcpkg.OnNetworkChanged(func() {
		applog.Warn("Network changed — WebRTC connection may be stale")
		mu.Lock()
		if !clientRunning {
			mu.Unlock()
			return
		}
		mu.Unlock()
	})

	// Auto latency test
	go func() {
		time.Sleep(latencyAutoDelay)
		result := TestLatency()
		applog.Infof("Auto latency test: %s", result)
	}()

	return nil
}

// HTTP client with protected sockets (bypasses TUN)
func newProtectedHTTPClient(protectFn func(int) bool) *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: 10 * time.Second,
				Control: func(network, address string, c syscall.RawConn) error {
					var protectErr error
					c.Control(func(fd uintptr) {
						if !protectFn(int(fd)) {
							protectErr = fmt.Errorf("protect signaling socket failed")
						}
					})
					return protectErr
				},
			}).DialContext,
		},
	}
}

// startClientWebRTC handles the WebRTC hole punch connection flow.
func startClientWebRTC(ctx context.Context, info connectionInfo, cfg clientConfig, tunFd int, protectFn func(int) bool, connected *bool) error {
	applog.Infof("WebRTC hole punch: session=%s", info.SessionID)

	sigURL := cfg.SignalingURL

	// Protect HTTP sockets so they don't route through the TUN
	protectedHTTP := newProtectedHTTPClient(protectFn)

	npc := info.NumPeerConns
	if npc <= 0 {
		npc = 1
	}

	// Start WebRTC client group
	iceServers := []string{
		"stun:" + cfg.StunServer,
		"stun:" + defaultStunServer2,
	}

	obfsKey, _ := hex.DecodeString(info.ObfsKey)
	if len(obfsKey) > 0 {
		applog.Info("WebRTC: obfuscation key present, enabling UDP obfuscation")
	}

	// Use relay if server enabled it
	relayAddr := info.RelayAddr
	if relayAddr != "" {
		applog.Infof("WebRTC: relay fallback: %s", relayAddr)
	}

	clientOpts := webrtcpkg.ClientOptions{
		Padding:              info.Padding,
		PaddingMax:           info.PaddingMax,
		NumChannels:          info.NumChannels,
		SmuxStreamBuffer:     info.SmuxStreamBuffer,
		SmuxSessionBuffer:    info.SmuxSessionBuffer,
		SmuxFrameSize:        info.SmuxFrameSize,
		SmuxKeepAlive:        info.SmuxKeepAlive,
		SmuxKeepAliveTimeout: info.SmuxKeepAliveTimeout,
		DCMaxBuffered:        info.DCMaxBuffered,
		DCLowMark:            info.DCLowMark,
		PeerConfig: webrtcpkg.PeerConfig{
			SCTPRecvBuffer:     cfg.SCTPRecvBuffer,
			SCTPRTOMax:         cfg.SCTPRTOMax,
			UDPReadBuffer:      cfg.UDPReadBuffer,
			UDPWriteBuffer:     cfg.UDPWriteBuffer,
			ICEDisconnTimeout:  cfg.ICEDisconnTimeout,
			ICEFailedTimeout:   cfg.ICEFailedTimeout,
			ICEKeepalive:       cfg.ICEKeepalive,
			DTLSRetransmit:     cfg.DTLSRetransmit,
			DTLSSkipVerify:     cfg.DTLSSkipVerify,
			SCTPZeroChecksum:   cfg.SCTPZeroChecksum,
			DisableCloseByDTLS: cfg.DisableCloseByDTLS,
		},
	}
	if info.TransportV == 2 {
		clientOpts.TransportMode = webrtcpkg.TransportMediaStream
		clientOpts.ObfsKey = obfsKey
	}

	// Try SSE offer stream for real-time updates; fall back to polling.
	applog.Infof("Getting %d SDP offers from signaling: %s/session/%s/offer", npc, sigURL, info.SessionID)
	sseCtx, sseCancel := context.WithCancel(ctx)
	offerCh, sseErr := signaling.StreamSDPOffers(sseCtx, sigURL, info.SessionID, protectedHTTP)

	var sdpOffers []string
	var err error
	if sseErr != nil {
		sseCancel()
		applog.Infof("Offer SSE failed, falling back to polling: %v", sseErr)
		sdpOffers, err = signaling.GetSDPOffersCtx(ctx, sigURL, info.SessionID, protectedHTTP)
		if err != nil {
			return fmt.Errorf("get SDP offers: %w", err)
		}
	} else {
		var ok bool
		sdpOffers, ok = <-offerCh
		if !ok {
			sseCancel()
			applog.Info("Offer SSE stream closed before delivering, falling back to polling")
			sdpOffers, err = signaling.GetSDPOffersCtx(ctx, sigURL, info.SessionID, protectedHTTP)
			if err != nil {
				return fmt.Errorf("get SDP offers: %w", err)
			}
		}
	}
	applog.Infof("SDP offers received (%d PCs)", len(sdpOffers))

	if ctx.Err() != nil {
		sseCancel()
		return fmt.Errorf("connection cancelled: %w", ctx.Err())
	}

	// SSE refresh-aware loop: if server refreshes offers while we're
	// creating the client group, restart with the latest offers.
	var wrtcGroup *webrtcpkg.ClientGroup
	var sdpAnswers []string
	for {
		wrtcGroup, sdpAnswers, err = webrtcpkg.StartClientGroup(npc, sdpOffers, iceServers, protectFn, obfsKey, relayAddr, info.SessionID, clientOpts)
		if err != nil {
			sseCancel()
			return fmt.Errorf("start WebRTC client group: %w", err)
		}

		if ctx.Err() != nil {
			wrtcGroup.Stop()
			sseCancel()
			return fmt.Errorf("connection cancelled: %w", ctx.Err())
		}

		// Before posting answers, check if newer offers arrived via SSE.
		if sseErr == nil {
			var latest []string
			drain := true
			for drain {
				select {
				case newer, chOk := <-offerCh:
					if chOk {
						latest = newer
					} else {
						drain = false
					}
				default:
					drain = false
				}
			}
			if latest != nil {
				applog.Info("WebRTC: server refreshed offers, restarting with latest")
				wrtcGroup.Stop()
				sdpOffers = latest
				continue
			}
		}
		break
	}
	sseCancel()

	// Post SDP answers (JSON array)
	applog.Infof("Posting %d SDP answers to signaling: %s/session/%s/answer", len(sdpAnswers), sigURL, info.SessionID)
	if err := signaling.PostSDPAnswersCtx(ctx, sigURL, info.SessionID, sdpAnswers, protectedHTTP); err != nil {
		wrtcGroup.Stop()
		return fmt.Errorf("post SDP answers: %w", err)
	}
	applog.Success("SDP answers posted to signaling server")

	// Wait for connection
	applog.Infof("Waiting for %d WebRTC connections to establish...", npc)
	timeout := 65*time.Second + time.Duration(npc-1)*15*time.Second
	if err := wrtcGroup.WaitConnected(timeout); err != nil {
		wrtcGroup.Stop()
		return fmt.Errorf("WebRTC connection: %w", err)
	}
	applog.Successf("WebRTC connections established (%d PCs)", npc)

	// Start TUN tunnel with direct stream dialer (bypasses SOCKS5)
	tun, err := tunnel.StartTunnelWithOptions(tunFd, "", cfg.TunAddress, cfg.MTU, cfg.DNS1, protectFn, tunnel.TunnelOptions{
		DNS2Addr:       cfg.DNS2,
		AllowDirectDNS: cfg.AllowDirectDNS,
		DialStream:     wrtcGroup.DialStream,
	})
	if err != nil {
		wrtcGroup.Stop()
		return fmt.Errorf("start tunnel: %w", err)
	}

	// Save reconnect params for fast reconnect
	savedConnInfo = info
	savedClientCfg = cfg
	savedProtectFn = protectFn
	savedICEServers = iceServers
	savedClientOpts = clientOpts

	// Update state
	mu.Lock()
	if ctx.Err() != nil {
		mu.Unlock()
		tun.Stop()
		wrtcGroup.Stop()
		return fmt.Errorf("connection cancelled: %w", ctx.Err())
	}
	activeTunnel = tun
	activeWebRTCClientGroup = wrtcGroup
	clientConnecting = false
	clientRunning = true
	clientStartTime = time.Now()
	clientDoneCh = make(chan struct{})
	*connected = true
	mu.Unlock()

	// Wire up fast reconnect callback on the client group.
	wrtcGroup.OnReconnectNeeded = func() {
		applog.Warn("ICE failure detected, triggering fast reconnect")
		go attemptFastReconnect()
	}

	// Start health monitor to detect ICE death and auto-cleanup.
	// Pass the connection-specific done channel so the monitor exits
	// when THIS connection is stopped, not a future one.
	go monitorClientHealth(wrtcGroup, clientDoneCh)

	applog.Successf("Client connected (WebRTC, %d PCs)", npc)

	// Register network change callback to detect WiFi→cellular transitions etc.
	webrtcpkg.OnNetworkChanged(func() {
		applog.Warn("Network changed — WebRTC connection may be stale")
		mu.Lock()
		if !clientRunning {
			mu.Unlock()
			return
		}
		mu.Unlock()
		// The WebRTC ICE connection will detect the change via consent checks.
		// Log it for visibility — actual disconnect is handled by ICE failure.
	})

	// Test latency
	go func() {
		time.Sleep(latencyAutoDelay)
		result := TestLatency()
		applog.Infof("Auto latency test: %s", result)
	}()

	return nil
}

// startClientXray handles the UPnP / legacy xray-core connection flow.
func startClientXray(info connectionInfo, cfg clientConfig, tunFd int, protectFn func(int) bool, connected *bool) error {
	targetIP := info.PublicIP
	targetPort := info.Port

	applog.Infof("xray path: connecting to %s:%d via %s/%s", targetIP, targetPort, info.Protocol, info.Transport)

	// Check cancellation before starting xray
	mu.Lock()
	if !clientConnecting {
		mu.Unlock()
		return fmt.Errorf("connection cancelled")
	}
	mu.Unlock()

	// Protect xray sockets
	xray.RegisterProtectFunc(protectFn)

	// Start xray
	applog.Infof("Building xray %s/%s client config (server=%s:%d, socks=127.0.0.1:%d)", info.Protocol, info.Transport, targetIP, targetPort, cfg.SocksPort)
	configJSON, err := xray.BuildClientConfig(targetIP, targetPort, info.UUID, cfg.SocksPort, info.ProxySettings)
	if err != nil {
		return fmt.Errorf("build client config: %w", err)
	}

	applog.Info("Starting xray-core client...")
	if err := xray.StartXray(configJSON); err != nil {
		return fmt.Errorf("start xray: %w", err)
	}

	// Start TUN tunnel
	socksAddr := fmt.Sprintf("127.0.0.1:%d", cfg.SocksPort)
	tun, err := tunnel.StartTunnelWithOptions(tunFd, socksAddr, cfg.TunAddress, cfg.MTU, cfg.DNS1, protectFn, tunnel.TunnelOptions{
		DNS2Addr:       cfg.DNS2,
		AllowDirectDNS: cfg.AllowDirectDNS,
	})
	if err != nil {
		xray.StopXray() // rollback
		return fmt.Errorf("start tunnel: %w", err)
	}

	// Update state
	mu.Lock()
	if !clientConnecting {
		// Cancelled while connecting, tear down
		mu.Unlock()
		tun.Stop()
		xray.StopXray()
		return fmt.Errorf("connection cancelled")
	}
	activeTunnel = tun
	clientSocksPort = cfg.SocksPort
	clientConnecting = false
	clientRunning = true
	clientStartTime = time.Now()
	clientDoneCh = make(chan struct{})
	*connected = true
	mu.Unlock()

	applog.Success("Client connected")

	// Test latency
	go func() {
		time.Sleep(latencyAutoDelay)
		result := TestLatency()
		applog.Infof("Auto latency test: %s", result)
	}()

	return nil
}

// Stop client and clean up. Cancels mid-connect if needed.
func StopClient() error {
	mu.Lock()
	defer mu.Unlock()

	// Cancel mid-connect
	if clientConnecting {
		clientConnecting = false
		if clientConnectCancel != nil {
			clientConnectCancel()
			clientConnectCancel = nil
		}
		// Clean up pending WebRTC group from ConnectWebRTC
		if pendingWebRTCClientGroup != nil {
			pendingWebRTCClientGroup.Stop()
			pendingWebRTCClientGroup = nil
		}
		applog.Info("Cancelling client connection...")
	}

	if !clientRunning {
		return nil
	}

	applog.Info("Stopping client...")

	// Signal any waiter (WaitClientDisconnect)
	if clientDoneCh != nil {
		select {
		case <-clientDoneCh:
		default:
			close(clientDoneCh)
		}
		clientDoneCh = nil
	}

	// Stop tunnel first (drain SOCKS5 connections)
	if activeTunnel != nil {
		activeTunnel.Stop()
		activeTunnel = nil
	}

	// Clear network change callbacks
	webrtcpkg.ClearNetworkCallbacks()

	// Stop the proxy engine (WebRTC or xray)
	if activeWebRTCClientGroup != nil {
		activeWebRTCClientGroup.Stop()
		activeWebRTCClientGroup = nil
	} else {
		if err := xray.StopXray(); err != nil {
			applog.Errorf("Stop xray failed: %v", err)
			return fmt.Errorf("stop xray: %w", err)
		}
	}

	clientRunning = false
	clientSocksPort = 0
	clientStartTime = time.Time{}
	clientRateTracker.reset()
	if clientConnectCancel != nil {
		clientConnectCancel()
		clientConnectCancel = nil
	}
	savedProtectFn = nil
	reconnectCount = 0
	applog.Success("Client disconnected")
	return nil
}

// monitorClientHealth polls the WebRTC client group health and auto-stops
// the client when all ICE connections die.
// doneCh is the connection-specific channel created during startClientWebRTC.
// Listening on it (instead of the global clientDoneCh) ensures the monitor
// exits when its own connection is torn down, preventing a stale goroutine
// from killing a subsequent reconnection.
func monitorClientHealth(group *webrtcpkg.ClientGroup, doneCh chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-doneCh:
			return // this connection was stopped
		case <-ticker.C:
			if !group.IsAlive() {
				// Don't auto-stop — reconnect callback handles recovery.
				// Just log for visibility.
				applog.Warn("Health monitor: connection not alive (reconnect may be in progress)")
			} else {
				mu.Lock()
				reconnectCount = 0
				if activeWebRTCClientGroup != nil {
					group = activeWebRTCClientGroup // pick up swapped group
				}
				mu.Unlock()
			}
		}
	}
}

// attemptFastReconnect rebuilds the WebRTC layer and hot-swaps the tunnel's
// dialStream without tearing down the TUN interface. Called from the ICE
// failure callback on the client group.
func attemptFastReconnect() {
	mu.Lock()
	if !clientRunning || reconnectCount >= maxReconnects {
		mu.Unlock()
		if reconnectCount >= maxReconnects {
			applog.Warnf("Max reconnects (%d) reached, stopping client", maxReconnects)
			StopClient()
		}
		return
	}
	reconnectCount++
	attempt := reconnectCount
	oldGroup := activeWebRTCClientGroup
	info := savedConnInfo
	cfg := savedClientCfg
	protectFn := savedProtectFn
	iceServers := savedICEServers
	clientOpts := savedClientOpts
	mu.Unlock()

	applog.Infof("Fast reconnect attempt %d/%d", attempt, maxReconnects)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	protectedHTTP := newProtectedHTTPClient(protectFn)
	obfsKey, _ := hex.DecodeString(info.ObfsKey)
	npc := info.NumPeerConns
	if npc <= 0 {
		npc = 1
	}

	sdpOffers, err := signaling.GetSDPOffersCtx(ctx, cfg.SignalingURL, info.SessionID, protectedHTTP)
	if err != nil {
		applog.Errorf("Reconnect: get offers failed: %v", err)
		StopClient()
		return
	}

	newGroup, sdpAnswers, err := webrtcpkg.StartClientGroup(npc, sdpOffers, iceServers, protectFn, obfsKey, info.RelayAddr, info.SessionID, clientOpts)
	if err != nil {
		applog.Errorf("Reconnect: start client group failed: %v", err)
		StopClient()
		return
	}

	if err := signaling.PostSDPAnswersCtx(ctx, cfg.SignalingURL, info.SessionID, sdpAnswers, protectedHTTP); err != nil {
		newGroup.Stop()
		applog.Errorf("Reconnect: post answers failed: %v", err)
		StopClient()
		return
	}

	timeout := 65*time.Second + time.Duration(npc-1)*15*time.Second
	if err := newGroup.WaitConnectedCtx(ctx, timeout); err != nil {
		newGroup.Stop()
		applog.Errorf("Reconnect: wait connected failed: %v", err)
		StopClient()
		return
	}

	// Hot-swap: replace tunnel's dialStream without tearing down TUN.
	mu.Lock()
	if !clientRunning || activeTunnel == nil {
		mu.Unlock()
		newGroup.Stop()
		applog.Warn("Reconnect: client stopped during reconnect, aborting")
		return
	}
	activeTunnel.SwapDialStream(newGroup.DialStream)
	activeWebRTCClientGroup = newGroup
	// Re-wire reconnect callback on new group
	newGroup.OnReconnectNeeded = func() {
		applog.Warn("ICE failure detected, triggering fast reconnect")
		go attemptFastReconnect()
	}
	mu.Unlock()

	oldGroup.Stop()
	applog.Successf("Fast reconnect succeeded (attempt %d)", attempt)
}

// WaitClientDisconnect blocks until the connection drops (ICE failure or stop).
// Android calls this in a goroutine to know when to tear down the TUN.
func WaitClientDisconnect() string {
	mu.Lock()
	ch := clientDoneCh
	mu.Unlock()
	if ch == nil {
		return "not_connected"
	}
	<-ch
	return "disconnected"
}

// GetClientStatus returns a JSON string with client status information.
func GetClientStatus() string {
	mu.Lock()
	defer mu.Unlock()

	var bytesUp, bytesDown int64
	if activeTunnel != nil {
		bytesUp, bytesDown = activeTunnel.GetStats()
	}

	rateUp, rateDown := clientRateTracker.update(bytesUp, bytesDown)

	var uptimeSec float64
	if clientRunning && !clientStartTime.IsZero() {
		uptimeSec = time.Since(clientStartTime).Seconds()
	}

	var smuxStreams, dataChannels, peerConns int
	if activeWebRTCClientGroup != nil {
		smuxStreams = activeWebRTCClientGroup.GetStreamCount()
		dataChannels = activeWebRTCClientGroup.GetChannelCount()
		peerConns = activeWebRTCClientGroup.Count()
	}

	status := map[string]interface{}{
		"connected":       clientRunning,
		"connecting":      clientConnecting,
		"bytesUp":         bytesUp,
		"bytesDown":       bytesDown,
		"rateUp":          rateUp,
		"rateDown":        rateDown,
		"uptimeSec":       uptimeSec,
		"smuxStreams":     smuxStreams,
		"dataChannels":    dataChannels,
		"peerConnections": peerConns,
	}

	data, _ := json.Marshal(status)
	return string(data)
}

// === Utility ===

const defaultRelayPort = "3478"

// deriveRelayAddr grabs the hostname from the signaling URL and appends the default relay port.
func deriveRelayAddr(signalingURL string) string {
	u, err := url.Parse(signalingURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return net.JoinHostPort(u.Hostname(), defaultRelayPort)
}

// DetectNATType probes the NAT type of the current network.
// Result JSON has nat_type, mapping, filtering, public_ip, and public_port.
func DetectNATType() string {
	natResult, err := nat.DetectNATTypeFull(
		defaultStunServer1,
		defaultStunServer2,
	)
	if err != nil {
		result := map[string]interface{}{
			"nat_type":    "unknown",
			"public_ip":   "",
			"public_port": 0,
		}
		data, _ := json.Marshal(result)
		return string(data)
	}
	result := map[string]interface{}{
		"nat_type":    natTypeLabel(natResult.Mapping, natResult.Filtering),
		"mapping":     natResult.Mapping.String(),
		"filtering":   natResult.Filtering.String(),
		"public_ip":   natResult.MappedIP,
		"public_port": natResult.MappedPort,
	}
	data, _ := json.Marshal(result)
	return string(data)
}

// natTypeLabel returns a human-readable NAT type label from RFC 5780 values.
func natTypeLabel(m nat.NATMapping, f nat.NATFiltering) string {
	switch {
	case m == nat.MappingEndpointIndependent && f == nat.FilteringEndpointIndependent:
		return "full_cone"
	case m == nat.MappingEndpointIndependent && f == nat.FilteringAddressDependent:
		return "restricted_cone"
	case m == nat.MappingEndpointIndependent && f == nat.FilteringAddressPortDependent:
		return "port_restricted"
	case m == nat.MappingAddressPortDependent || m == nat.MappingAddressDependent:
		return "symmetric"
	case m == nat.MappingEndpointIndependent && f == nat.FilteringUnknown:
		return "cone"
	default:
		return "unknown"
	}
}

// OnNetworkChanged notifies the Go layer of a network connectivity change.
// Called from the Android platform layer (ConnectivityManager callback).
// networkType is one of: "wifi", "cellular", "available", "lost", "other".
func OnNetworkChanged(networkType string) {
	webrtcpkg.NotifyNetworkChange(networkType)
}

// GetPublicIP returns the public IP address discovered via STUN.
func GetPublicIP() string {
	ip, _, err := nat.DiscoverPublicAddr(defaultStunServer1)
	if err != nil {
		return ""
	}
	return ip
}

// === Latency Test ===

// TestLatency pings through the proxy tunnel to measure RTT.
// Uses DialStream for WebRTC or SOCKS5 for the xray path.
// Result JSON: {"latency_ms": 123} or {"error": "msg"}.
func TestLatency() string {
	mu.Lock()
	running := clientRunning
	port := clientSocksPort
	wrtcGroup := activeWebRTCClientGroup
	mu.Unlock()

	if !running {
		return marshalLatencyError("client not connected")
	}

	var transport *http.Transport

	if wrtcGroup != nil {
		// WebRTC path: dial through smux streams directly
		transport = &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				rwc, err := wrtcGroup.DialStream(addr)
				if err != nil {
					return nil, err
				}
				return &rwcConn{rwc: rwc}, nil
			},
		}
	} else {
		// xray path: dial through local SOCKS5 proxy
		socksAddr := fmt.Sprintf("127.0.0.1:%d", port)
		dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, &net.Dialer{Timeout: latencyDialTimeout})
		if err != nil {
			return marshalLatencyError(fmt.Sprintf("socks5 dialer: %v", err))
		}
		transport = &http.Transport{
			Dial: dialer.Dial,
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   latencyRequestTimeout,
	}

	start := time.Now()
	resp, err := client.Get(latencyTestURL)
	if err != nil {
		return marshalLatencyError(fmt.Sprintf("request failed: %v", err))
	}
	resp.Body.Close()
	ms := time.Since(start).Milliseconds()

	data, _ := json.Marshal(map[string]int64{"latency_ms": ms})
	return string(data)
}

// rwcConn wraps an io.ReadWriteCloser into a net.Conn for http.Transport.
type rwcConn struct {
	rwc io.ReadWriteCloser
}

func (c *rwcConn) Read(p []byte) (int, error)         { return c.rwc.Read(p) }
func (c *rwcConn) Write(p []byte) (int, error)        { return c.rwc.Write(p) }
func (c *rwcConn) Close() error                       { return c.rwc.Close() }
func (c *rwcConn) LocalAddr() net.Addr                { return dummyAddr{} }
func (c *rwcConn) RemoteAddr() net.Addr               { return dummyAddr{} }
func (c *rwcConn) SetDeadline(t time.Time) error      { return nil }
func (c *rwcConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *rwcConn) SetWriteDeadline(t time.Time) error { return nil }

type dummyAddr struct{}

func (dummyAddr) Network() string { return "tcp" }
func (dummyAddr) String() string  { return "webrtc" }

func marshalLatencyError(msg string) string {
	data, _ := json.Marshal(map[string]string{"error": msg})
	return string(data)
}

// === STUN retry ===

// Retry WebRTC server creation until we get a valid srflx candidate.
// Without srflx the server is unreachable from external networks.
const stunRetryMax = 10

func startServerGroupWithSrflx(npc int, iceServers []string, obfsKey []byte, relayAddr, sessionID string, opts webrtcpkg.ServerOptions) (*webrtcpkg.ServerGroup, []string, error) {
	backoff := util.NewBackoff(1*time.Second, 15*time.Second, 0.5)
	for attempt := 1; attempt <= stunRetryMax; attempt++ {
		group, sdps, err := webrtcpkg.StartServerGroup(npc, iceServers, obfsKey, relayAddr, sessionID, opts)
		if err != nil {
			return nil, nil, err
		}
		// Check that at least the first SDP has a srflx candidate
		hasSrflx := false
		for _, sdp := range sdps {
			if strings.Contains(sdp, "typ srflx") {
				hasSrflx = true
				break
			}
		}
		if hasSrflx {
			return group, sdps, nil
		}
		applog.Warnf("STUN failed — no srflx candidate (attempt %d/%d), retrying...", attempt, stunRetryMax)
		group.Stop()
		time.Sleep(backoff.Next())
	}
	return nil, nil, fmt.Errorf("STUN discovery failed after %d attempts — no srflx candidate", stunRetryMax)
}

// === SDP polling ===

// Poll for 25s before refreshing (NAT timeouts are ~30-60s)
const sdpPollTimeout = 25 * time.Second

// pollForSDPAnswers polls for a multi-SDP answer (JSON array) with timeout.
// Used as fallback when SSE delivery fails.
func pollForSDPAnswers(ctx context.Context, signalingURL, sessionID string, timeout time.Duration, httpClient *http.Client) ([]string, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	deadline := time.Now().Add(timeout)
	answerURL := fmt.Sprintf("%s/session/%s/answer", signalingURL, sessionID)
	backoff := util.NewBackoff(1*time.Second, 5*time.Second, 0.5)

	type sdpPayload struct {
		SDP string `json:"sdp"`
	}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, "GET", answerURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			time.Sleep(backoff.Next())
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			time.Sleep(backoff.Next())
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			time.Sleep(backoff.Next())
			continue
		}

		var payload sdpPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			time.Sleep(backoff.Next())
			continue
		}

		if payload.SDP == "" {
			time.Sleep(backoff.Next())
			continue
		}

		// Parse JSON array of SDP answers (backward compat: single SDP string)
		var answers []string
		if err := json.Unmarshal([]byte(payload.SDP), &answers); err != nil {
			answers = []string{payload.SDP}
		}

		// Skip empty/cleared answers (e.g. "null" → nil slice)
		if len(answers) == 0 {
			time.Sleep(backoff.Next())
			continue
		}

		return answers, nil
	}

	return nil, fmt.Errorf("no SDP answers within %v", timeout)
}

// === Logging API ===

// GetLogs returns new log entries since the given cursor as a JSON string.
func GetLogs(cursor int) string {
	return applog.GetLogs(cursor)
}

// ClearLogs resets the log buffer.
func ClearLogs() {
	applog.ClearLogs()
}
