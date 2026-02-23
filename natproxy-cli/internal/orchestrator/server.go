package orchestrator

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"natproxy/cli/internal/display"
	"natproxy/cli/internal/types"
	"natproxy/golib/applog"
	"natproxy/golib/nat"
	"natproxy/golib/signaling"
	webrtcpkg "natproxy/golib/webrtc"
	"natproxy/golib/xray"
)

const defaultRelayPort = "3478"

type ServerOrchestrator struct {
	mu                sync.Mutex
	running           bool
	method            string // "upnp", "holepunch", or "manual"
	info              *types.ConnectionInfo
	webrtcServerGroup *webrtcpkg.ServerGroup // pending server group waiting for next client
	drainingGroups    []*webrtcpkg.ServerGroup // old groups with active streams
	upnpExternalPort  int
	upnpProtocol      string
	discoveryID       string
	heartbeatStop     chan struct{}
	cfg               types.ServerConfig
	startTime         time.Time

	vlessLink string // VLESS link for UPnP/xray-core path

	// Manual signaling state
	manualMode        bool
	stunKeepaliveDone chan struct{}
}

func NewServerOrchestrator(cfg types.ServerConfig) *ServerOrchestrator {
	return &ServerOrchestrator{cfg: cfg}
}

func (s *ServerOrchestrator) StartServer() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return "", fmt.Errorf("server already running")
	}

	applog.SetMaskIPs(s.cfg.MaskIPs)
	applog.Info("Starting server...")

	cfg := s.cfg
	uuid := cfg.UUID
	if uuid == "" {
		uuid = types.GenerateUUID()
	}
	listenPort := cfg.ListenPort

	var externalIP string
	var mappedPort int
	var err error
	method := ""
	transport := cfg.Transport
	protocol := cfg.Protocol

	upnpProto := "TCP"
	if protocol == "vless" && transport == "kcp" {
		upnpProto = "UDP"
	}

	// Validate signaling URL early so we fail fast instead of waiting
	// for UPnP to timeout when the fallback can't work anyway.
	sigErr := validateSignalingURL(cfg.SignalingURL)
	if cfg.NatMethod != "upnp" && sigErr != nil {
		return "", sigErr
	}

	// NAT traversal based on configured method
	if cfg.NatMethod != "holepunch" {
		applog.Info("Attempting UPnP port mapping...")
		externalIP, mappedPort, err = nat.TryUPnP(listenPort, listenPort, upnpProto, nat.UPnPOptions{
			LeaseDuration:  cfg.UpnpLeaseDuration,
			MappingRetries: cfg.UpnpRetries,
			SSDPTimeout:    time.Duration(cfg.SsdpTimeout) * time.Second,
		})
		if err == nil {
			method = "upnp"
			applog.Successf("UPnP mapped port %d → %d (%s)", listenPort, mappedPort, upnpProto)
		} else {
			applog.Warnf("UPnP failed: %v", err)
		}
	}

	// UPnP path: use xray-core
	if method == "upnp" {
		proxySettingsJSON := cfg.BuildProxySettingsJSON()

		applog.Infof("Building xray %s/%s server config (listen=%s:%d, uuid=%s)", protocol, transport, "0.0.0.0", listenPort, uuid)
		configJSON, err := xray.BuildServerConfig("0.0.0.0", listenPort, uuid, proxySettingsJSON)
		if err != nil {
			return "", fmt.Errorf("build server config: %w", err)
		}

		if err := xray.StartXray(configJSON); err != nil {
			return "", fmt.Errorf("start xray: %w", err)
		}

		applog.Successf("Server started (%s/%s/%s) on %s:%d", method, protocol, transport, externalIP, mappedPort)

		s.info = &types.ConnectionInfo{
			PublicIP:      externalIP,
			Port:          mappedPort,
			UUID:          uuid,
			Transport:     transport,
			Method:        method,
			Protocol:      protocol,
			ProxySettings: proxySettingsJSON,
		}
		s.method = "upnp"
		s.upnpExternalPort = mappedPort
		s.upnpProtocol = upnpProto
		s.running = true
		s.startTime = time.Now()
		s.vlessLink = xray.GenerateVLESSLink(uuid, externalIP, mappedPort, proxySettingsJSON, "natproxy")

		return s.info.Encode(), nil
	}

	// Holepunch path
	if cfg.NatMethod == "upnp" {
		return "", fmt.Errorf("NAT traversal failed: UPnP-only mode selected but UPnP failed")
	}

	if sigErr != nil {
		return "", sigErr
	}

	applog.Info("Falling back to WebRTC hole punch...")

	iceServers := []string{
		"stun:" + cfg.StunServer,
		"stun:" + defaultStunServer2,
	}

	obfsKey := make([]byte, 32)
	if _, err := crypto_rand.Read(obfsKey); err != nil {
		return "", fmt.Errorf("generate obfs key: %w", err)
	}
	applog.Info("Generated UDP obfuscation key for WebRTC path")

	sessionID := types.GenerateUUID()
	applog.Infof("Holepunch session ID: %s", sessionID)

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
			DTLSSkipVerify:     boolPtr(cfg.DTLSSkipVerify),
			SCTPZeroChecksum:   boolPtr(cfg.SCTPZeroChecksum),
			DisableCloseByDTLS: boolPtr(cfg.DisableCloseByDTLS),
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

	applog.Infof("Posting %d SDP offers to signaling: %s/session/%s/offer", npc, cfg.SignalingURL, sessionID)
	if err := signaling.PostSDPOffers(cfg.SignalingURL, sessionID, sdpOffers, nil); err != nil {
		wrtcGroup.Stop()
		return "", fmt.Errorf("post SDP offers: %w", err)
	}
	applog.Success("SDP offers posted to signaling server")

	s.webrtcServerGroup = wrtcGroup
	s.method = "holepunch"

	// Accept loop: wait for clients and handle reconnections.
	// STUN keepalive on the raw socket keeps the NAT mapping alive
	// without creating new groups every cycle.
	sigURL := cfg.SignalingURL
	sid := sessionID
	serverOpts := srvOpts // capture for goroutine
	serverNPC := npc
	stunKAAddrs := []string{cfg.StunServer, defaultStunServer2}
	go func() {
		currentOffers := sdpOffers // track for re-posting without new group

		// Start STUN keepalive on the raw socket to maintain the NAT
		// mapping without creating new groups every 25s.
		var stunStop chan struct{}
		if raw := wrtcGroup.GetRawConn(); raw != nil {
			stunStop = webrtcpkg.StartSTUNKeepalive(raw, stunKAAddrs, 20*time.Second)
		}
		defer func() {
			if stunStop != nil {
				close(stunStop)
			}
		}()

		for {
			s.mu.Lock()
			running := s.running
			s.mu.Unlock()
			if !running {
				return
			}

			applog.Infof("Waiting for client SDP answers on session %s (SSE)...", sid)
			sseCtx, sseCancel := context.WithTimeout(context.Background(), sdpPollTimeout)
			sdpAnswers, err := signaling.WaitSDPAnswersSSE(sseCtx, sigURL, sid, nil)
			sseCancel()
			if err != nil {
				applog.Infof("SSE wait failed (%v), falling back to polling...", err)
				sdpAnswers, err = pollForSDPAnswers(sigURL, sid, sdpPollTimeout)
			}
			if err != nil {
				// No client yet — re-POST same offers to keep signaling
				// session alive. STUN keepalive keeps the NAT mapping alive.
				s.mu.Lock()
				if !s.running {
					s.mu.Unlock()
					return
				}
				s.mu.Unlock()

				if err := signaling.PostSDPOffers(sigURL, sid, currentOffers, nil); err != nil {
					applog.Warnf("Re-post SDP offers failed: %v", err)
				}

				s.cleanupDrainingGroups()
				continue
			}

			applog.Infof("Client SDP answers received (%d PCs)", len(sdpAnswers))

			// Clear stale answers synchronously to prevent the next SSE
			// wait from picking up this same answer again.
			signaling.PostSDPAnswers(sigURL, sid, nil, nil)

			if err := wrtcGroup.AcceptAnswers(sdpAnswers); err != nil {
				applog.Errorf("Accept SDP answers failed: %v", err)
				continue
			}

			applog.Info("Waiting for WebRTC connections to establish...")
			timeout := 65*time.Second + time.Duration(serverNPC-1)*15*time.Second
			if err := wrtcGroup.WaitConnected(timeout); err != nil {
				applog.Errorf("WebRTC connection failed: %v", err)
			} else {
				applog.Success("WebRTC connections established with client")
			}

			// Prep for next client
			s.mu.Lock()
			if !s.running {
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()

			// Stop STUN keepalive on old group before creating new one.
			if stunStop != nil {
				close(stunStop)
				stunStop = nil
			}

			newGroup, newOffers, err := startServerGroupWithSrflx(serverNPC, iceServers, obfsKey, relayAddr, sid, serverOpts)
			if err != nil {
				applog.Warnf("Re-create WebRTC server group failed: %v", err)
				return
			}

			// Clear answers synchronously before posting new offers
			signaling.PostSDPAnswers(sigURL, sid, nil, nil)

			if err := signaling.PostSDPOffers(sigURL, sid, newOffers, nil); err != nil {
				applog.Warnf("Re-post SDP offers failed: %v", err)
				newGroup.Stop()
				return
			}

			currentOffers = newOffers

			s.mu.Lock()
			if wrtcGroup != nil && wrtcGroup.IsAlive() {
				s.drainingGroups = append(s.drainingGroups, wrtcGroup)
			} else if wrtcGroup != nil {
				wrtcGroup.Stop()
			}
			s.webrtcServerGroup = newGroup
			wrtcGroup = newGroup
			s.mu.Unlock()

			// Start STUN keepalive on the new group's socket.
			if raw := newGroup.GetRawConn(); raw != nil {
				stunStop = webrtcpkg.StartSTUNKeepalive(raw, stunKAAddrs, 20*time.Second)
			}
			applog.Info("Ready for next client connection")

			s.cleanupDrainingGroups()
		}
	}()

	// Set transport version based on mode
	transportV := 0
	if transportMode == webrtcpkg.TransportMediaStream {
		transportV = 2
	}

	s.info = &types.ConnectionInfo{
		Method:            "holepunch",
		Transport:         "webrtc",
		SessionID:         sessionID,
		Protocol:          "webrtc",
		ObfsKey:           hex.EncodeToString(obfsKey),
		RelayAddr:         relayAddr,
		Padding:           cfg.PaddingEnabled,
		Version:           2,
		SigV:              2,
		TransportV:        transportV,
		NumPeerConns:      npc,
		NumChannels:       cfg.NumChannels,
		SmuxStreamBuffer:  cfg.SmuxStreamBuffer,
		SmuxSessionBuffer: cfg.SmuxSessionBuffer,
		SmuxFrameSize:     cfg.SmuxFrameSize,
		DCMaxBuffered:        cfg.DCMaxBuffered,
		DCLowMark:            cfg.DCLowMark,
		PaddingMax:           cfg.PaddingMax,
		SmuxKeepAlive:        cfg.SmuxKeepAlive,
		SmuxKeepAliveTimeout: cfg.SmuxKeepAliveTimeout,
	}
	s.running = true
	s.startTime = time.Now()

	applog.Successf("Server started (holepunch/webrtc), session=%s", sessionID)
	return s.info.Encode(), nil
}

// StartServerManual creates a WebRTC server with npc=1 and returns a manual
// offer code (M1:...) for out-of-band exchange. No signaling server is used.
func (s *ServerOrchestrator) StartServerManual() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return "", fmt.Errorf("server already running")
	}

	applog.SetMaskIPs(s.cfg.MaskIPs)
	applog.Info("Starting server (manual signaling)...")

	cfg := s.cfg

	iceServers := []string{
		"stun:" + cfg.StunServer,
		"stun:" + defaultStunServer2,
	}

	obfsKey := make([]byte, 32)
	if _, err := crypto_rand.Read(obfsKey); err != nil {
		return "", fmt.Errorf("generate obfs key: %w", err)
	}

	sessionID := types.GenerateUUID()

	var relayAddr string
	if cfg.UseRelay {
		relayAddr = deriveRelayAddr(cfg.SignalingURL)
		if relayAddr != "" {
			applog.Infof("UDP relay fallback enabled: %s", relayAddr)
		}
	}

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
			DTLSSkipVerify:     boolPtr(cfg.DTLSSkipVerify),
			SCTPZeroChecksum:   boolPtr(cfg.SCTPZeroChecksum),
			DisableCloseByDTLS: boolPtr(cfg.DisableCloseByDTLS),
		},
	}
	if transportMode == webrtcpkg.TransportMediaStream {
		srvOpts.ObfsKey = obfsKey
	}

	// Force npc=1 for manual mode (keeps codes short)
	wrtcGroup, sdpOffers, err := startServerGroupWithSrflx(1, iceServers, obfsKey, relayAddr, sessionID, srvOpts)
	if err != nil {
		return "", fmt.Errorf("start WebRTC server group: %w", err)
	}

	// Compress SDP into manual offer
	compressedSDP, err := signaling.CompressSDP(sdpOffers[0])
	if err != nil {
		wrtcGroup.Stop()
		return "", fmt.Errorf("compress SDP: %w", err)
	}

	transportV := 0
	if transportMode == webrtcpkg.TransportMediaStream {
		transportV = 2
	}

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

	// Start STUN keepalive to preserve NAT mapping while user copies codes
	rawConn := wrtcGroup.GetRawConn()
	if rawConn != nil {
		stunAddrs := make([]string, 0, len(iceServers))
		for _, s := range iceServers {
			stunAddrs = append(stunAddrs, strings.TrimPrefix(s, "stun:"))
		}
		s.stunKeepaliveDone = webrtcpkg.StartSTUNKeepalive(rawConn, stunAddrs, 20*time.Second)
	}

	s.webrtcServerGroup = wrtcGroup
	s.method = "manual"
	s.manualMode = true
	s.running = true
	s.startTime = time.Now()

	applog.Successf("Manual offer code generated (%d chars)", len(offerCode))
	return offerCode, nil
}

// AcceptManualAnswer decodes a manual answer code (M1A:...), feeds the SDP
// answer to the server group, and waits for the ICE connection to establish.
func (s *ServerOrchestrator) AcceptManualAnswer(answerCode string) error {
	s.mu.Lock()
	if !s.running || !s.manualMode {
		s.mu.Unlock()
		return fmt.Errorf("server not in manual mode")
	}
	group := s.webrtcServerGroup
	s.mu.Unlock()

	answer, err := signaling.DecodeManualAnswer(answerCode)
	if err != nil {
		return fmt.Errorf("decode manual answer: %w", err)
	}

	sdp, err := signaling.DecompressSDP(answer.CompressedSDP)
	if err != nil {
		return fmt.Errorf("decompress answer SDP: %w", err)
	}

	if err := group.AcceptAnswers([]string{sdp}); err != nil {
		return fmt.Errorf("accept SDP answer: %w", err)
	}

	// Stop STUN keepalive — ICE takes over now
	s.mu.Lock()
	if s.stunKeepaliveDone != nil {
		close(s.stunKeepaliveDone)
		s.stunKeepaliveDone = nil
	}
	s.mu.Unlock()

	applog.Info("Waiting for WebRTC connection to establish...")
	if err := group.WaitConnected(65 * time.Second); err != nil {
		return fmt.Errorf("WebRTC connection: %w", err)
	}

	applog.Success("WebRTC connection established with client")
	return nil
}

func (s *ServerOrchestrator) StopServer() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	applog.Info("Stopping server...")

	if s.discoveryID != "" {
		if s.heartbeatStop != nil {
			close(s.heartbeatStop)
			s.heartbeatStop = nil
		}
		UnregisterDiscovery(s.cfg.DiscoveryURL, s.discoveryID)
		s.discoveryID = ""
	}

	// Stop STUN keepalive if active (manual mode)
	if s.stunKeepaliveDone != nil {
		close(s.stunKeepaliveDone)
		s.stunKeepaliveDone = nil
	}
	s.manualMode = false

	switch s.method {
	case "holepunch", "manual":
		if s.webrtcServerGroup != nil {
			s.webrtcServerGroup.Stop()
			s.webrtcServerGroup = nil
		}
		for _, g := range s.drainingGroups {
			g.Stop()
		}
		s.drainingGroups = nil
	default: // "upnp"
		if err := xray.StopXray(); err != nil {
			applog.Errorf("Stop xray failed: %v", err)
			return fmt.Errorf("stop xray: %w", err)
		}
		if s.info != nil && s.info.Method == "upnp" {
			nat.RemoveUPnPMapping(s.upnpExternalPort, s.upnpProtocol)
			applog.Infof("UPnP port mapping removed (%s)", s.upnpProtocol)
		}
		xray.ResetClientTracker()
	}

	s.running = false
	s.info = nil
	s.method = ""
	applog.Success("Server stopped")
	return nil
}

func (s *ServerOrchestrator) RegisterDiscovery(connectionCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.info == nil {
		return fmt.Errorf("server not running")
	}

	discoveryURL := s.cfg.DiscoveryURL
	if err := validateSignalingURL(discoveryURL); err != nil {
		return err
	}

	// Auto-generate a friendly name if none provided
	name := s.cfg.DiscoveryName
	if name == "" {
		name = generateFriendlyName()
	}

	listingID, err := RegisterDiscovery(
		discoveryURL,
		name,
		s.cfg.DiscoveryRoom,
		connectionCode,
		s.info.Method,
		s.info.Transport,
		s.info.Protocol,
	)
	if err != nil {
		return err
	}

	s.discoveryID = listingID
	s.heartbeatStop = make(chan struct{})

	params := HeartbeatParams{
		SigURL:         discoveryURL,
		Name:           name,
		Room:           s.cfg.DiscoveryRoom,
		ConnectionCode: connectionCode,
		Method:         s.info.Method,
		Transport:      s.info.Transport,
		Protocol:       s.info.Protocol,
	}
	go StartHeartbeat(params, listingID, s.heartbeatStop, func(newID string) {
		s.mu.Lock()
		s.discoveryID = newID
		s.mu.Unlock()
	})

	applog.Infof("Registered on discovery with ID: %s (name: %s)", listingID, name)
	return nil
}

func (s *ServerOrchestrator) GetClientCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch s.method {
	case "holepunch", "manual":
		count := 0
		if s.webrtcServerGroup != nil && s.webrtcServerGroup.IsAlive() {
			count++
		}
		for _, g := range s.drainingGroups {
			if g.IsAlive() {
				count++
			}
		}
		return count
	default:
		return xray.GetClientCount()
	}
}

func (s *ServerOrchestrator) GetMethod() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.method
}

func (s *ServerOrchestrator) GetVLESSLink() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vlessLink
}

func (s *ServerOrchestrator) GetUptime() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return 0
	}
	return time.Since(s.startTime)
}

func (s *ServerOrchestrator) GetMetrics() display.ServerMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()

	m := display.ServerMetrics{
		Method: s.method,
	}
	if s.running {
		m.Uptime = time.Since(s.startTime)
	}

	switch s.method {
	case "holepunch", "manual":
		count := 0
		allGroups := make([]*webrtcpkg.ServerGroup, 0, 1+len(s.drainingGroups))
		if s.webrtcServerGroup != nil {
			allGroups = append(allGroups, s.webrtcServerGroup)
		}
		allGroups = append(allGroups, s.drainingGroups...)
		for _, g := range allGroups {
			if g.IsAlive() {
				count++
				up, down := g.GetStats()
				m.BytesUp += up
				m.BytesDown += down
				m.DataChannels += g.GetChannelCount()
				m.PeerConns += g.Count()
				m.StreamDist = append(m.StreamDist, g.GetStreamDistribution()...)
			}
		}
		m.ClientCount = count
		for _, n := range m.StreamDist {
			m.SmuxStreams += n
		}
	default:
		m.ClientCount = xray.GetClientCount()
	}

	m.Goroutines = runtime.NumGoroutine()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	m.HeapMB = float64(mem.HeapAlloc) / (1024 * 1024)

	return m
}

func boolPtr(b bool) *bool { return &b }

// Retry WebRTC server creation until we get a valid srflx candidate.
// Without srflx the server is unreachable from external networks.
const stunRetryMax = 10

func startServerGroupWithSrflx(npc int, iceServers []string, obfsKey []byte, relayAddr, sessionID string, opts webrtcpkg.ServerOptions) (*webrtcpkg.ServerGroup, []string, error) {
	for attempt := 1; attempt <= stunRetryMax; attempt++ {
		group, sdps, err := webrtcpkg.StartServerGroup(npc, iceServers, obfsKey, relayAddr, sessionID, opts)
		if err != nil {
			return nil, nil, err
		}
		// Check that at least the first offer has a srflx candidate
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
		time.Sleep(3 * time.Second)
	}
	return nil, nil, fmt.Errorf("STUN discovery failed after %d attempts — no srflx candidate", stunRetryMax)
}

const defaultStunServer2 = "stun1.l.google.com:19302"

func deriveRelayAddr(signalingURL string) string {
	u, err := url.Parse(signalingURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return u.Hostname() + ":" + defaultRelayPort
}

// Pick a random name like "Snow Bunny"
func generateFriendlyName() string {
	adjectives := []string{
		"Swift", "Brave", "Cool", "Wise", "Bright", "Wild", "Free", "Happy",
		"Noble", "Kind", "Calm", "Bold", "Quick", "Lucky", "Smart", "Strong",
		"Gentle", "Quiet", "Warm", "Silent", "Fierce", "Loyal", "Proud", "Graceful",
		"Eager", "Mighty", "Clever", "Jolly", "Serene", "Vivid", "Nimble", "Radiant",
		"Cosmic", "Crystal", "Golden", "Silver", "Azure", "Crimson", "Emerald", "Amber",
		"Mystic", "Arctic", "Solar", "Lunar", "Stellar", "Thunder", "Storm", "Frost",
		"Shadow", "Ghost", "Spirit", "Dream", "Echo", "Spark", "Blaze", "Flash",
		"Cyber", "Digital", "Quantum", "Neon", "Pixel", "Matrix", "Binary", "Zenith",
	}

	animals := []string{
		"Fox", "Wolf", "Bear", "Eagle", "Lion", "Tiger", "Hawk", "Owl",
		"Lynx", "Puma", "Raven", "Falcon", "Otter", "Badger", "Cobra", "Viper",
		"Leopard", "Cheetah", "Panther", "Dragon", "Phoenix", "Griffin", "Unicorn", "Pegasus",
		"Dolphin", "Shark", "Whale", "Orca", "Manta", "Squid", "Kraken", "Hydra",
		"Bunny", "Rabbit", "Deer", "Moose", "Elk", "Bison", "Yak", "Ram",
		"Sparrow", "Robin", "Jay", "Crow", "Magpie", "Swan", "Crane", "Heron",
		"Panda", "Koala", "Sloth", "Lemur", "Monkey", "Gorilla", "Chimp", "Orangutan",
		"Husky", "Corgi", "Shiba", "Akita", "Collie", "Retriever", "Beagle", "Dingo",
	}

	// crypto/rand because math/rand is too predictable
	adjIdx := secureRandInt(len(adjectives))
	animalIdx := secureRandInt(len(animals))

	return adjectives[adjIdx] + " " + animals[animalIdx]
}

func secureRandInt(max int) int {
	var b [4]byte
	crypto_rand.Read(b[:])
	val := int(b[0]) | int(b[1])<<8 | int(b[2])<<16 | int(b[3])<<24
	if val < 0 {
		val = -val
	}
	return val % max
}

// validateSignalingURL checks that the signaling URL is not the placeholder default.
func validateSignalingURL(sigURL string) error {
	if strings.Contains(sigURL, "[IP]") {
		return fmt.Errorf("signaling URL not configured (still set to %q). Use --signaling-url or set signaling_url in ~/.natproxy-cli", sigURL)
	}
	return nil
}

// Poll for 25s before refreshing (NAT timeouts are ~30-60s)
const sdpPollTimeout = 25 * time.Second

// pollForSDPAnswers polls the signaling server for SDP answers (JSON array).
// Returns the parsed []string of SDP answers, or error on timeout.
func pollForSDPAnswers(signalingURL, sessionID string, timeout time.Duration) ([]string, error) {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 10 * time.Second}
	answerURL := fmt.Sprintf("%s/session/%s/answer", signalingURL, sessionID)

	for time.Now().Before(deadline) {
		resp, err := client.Get(answerURL)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			time.Sleep(2 * time.Second)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		var payload struct {
			SDP string `json:"sdp"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		if payload.SDP == "" {
			time.Sleep(2 * time.Second)
			continue
		}

		// Parse as JSON array of SDP strings
		var answers []string
		if err := json.Unmarshal([]byte(payload.SDP), &answers); err != nil {
			// Backward compatibility: single raw SDP string
			answers = []string{payload.SDP}
		}

		// Skip empty/cleared answers (e.g. "null" → nil slice)
		if len(answers) == 0 {
			time.Sleep(2 * time.Second)
			continue
		}
		return answers, nil
	}

	return nil, fmt.Errorf("no SDP answers within %v", timeout)
}

// cleanupDrainingGroups removes dead groups from the draining list.
func (s *ServerOrchestrator) cleanupDrainingGroups() {
	s.mu.Lock()
	defer s.mu.Unlock()
	alive := s.drainingGroups[:0]
	for _, g := range s.drainingGroups {
		if g.IsAlive() {
			alive = append(alive, g)
		} else {
			g.Stop()
		}
	}
	s.drainingGroups = alive
}
