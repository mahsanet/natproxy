package orchestrator

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"natproxy/cli/internal/display"
	"natproxy/cli/internal/types"
	"natproxy/golib/applog"
	"natproxy/golib/signaling"
	webrtcpkg "natproxy/golib/webrtc"
	"natproxy/golib/xray"
)

type ClientOrchestrator struct {
	mu               sync.Mutex
	running          bool
	connecting       bool
	connectCancel    context.CancelFunc
	socksPort        int
	webrtcClient     *webrtcpkg.Client
	webrtcClientGroup *webrtcpkg.ClientGroup
	cfg              types.ClientConfig
	lastLatencyMs    int

	// Manual signaling state
	pendingManualGroup *webrtcpkg.ClientGroup
}

func NewClientOrchestrator(cfg types.ClientConfig) *ClientOrchestrator {
	return &ClientOrchestrator{cfg: cfg}
}

func (c *ClientOrchestrator) StartClient(connectionCode string) error {
	c.mu.Lock()
	if c.running || c.connecting {
		c.mu.Unlock()
		return fmt.Errorf("client already running")
	}
	c.connecting = true
	ctx, cancel := context.WithCancel(context.Background())
	c.connectCancel = cancel
	c.mu.Unlock()

	applog.SetMaskIPs(c.cfg.MaskIPs)
	applog.Info("Starting client...")

	connected := false
	defer func() {
		if !connected {
			cancel()
			c.mu.Lock()
			c.connecting = false
			c.connectCancel = nil
			c.mu.Unlock()
		}
	}()

	info, err := types.DecodeConnectionInfo(connectionCode)
	if err != nil {
		return err
	}

	// Backward compatibility
	if info.Protocol == "" {
		if info.Transport == "socks" {
			info.Protocol = "socks"
		} else {
			info.Protocol = "vless"
		}
	}
	if info.ProxySettings == "" {
		ps := map[string]interface{}{
			"protocol":  info.Protocol,
			"transport": info.Transport,
		}
		data, _ := json.Marshal(ps)
		info.ProxySettings = string(data)
	}

	applog.Infof("Connecting via %s/%s (method=%s)", info.Protocol, info.Transport, info.Method)

	cfg := c.cfg

	if info.Method == "holepunch" && info.Transport == "webrtc" && info.SessionID != "" {
		return c.startClientWebRTC(ctx, info, cfg, &connected)
	}

	return c.startClientXray(info, cfg, &connected)
}

func (c *ClientOrchestrator) startClientWebRTC(ctx context.Context, info *types.ConnectionInfo, cfg types.ClientConfig, connected *bool) error {
	applog.Infof("WebRTC hole punch: session=%s", info.SessionID)

	sigURL := cfg.SignalingURL
	if err := validateSignalingURL(sigURL); err != nil {
		return err
	}

	npc := info.NumPeerConns
	if npc <= 0 {
		npc = 1
	}

	iceServers := []string{
		"stun:" + cfg.StunServer,
		"stun:" + defaultStunServer2,
	}

	obfsKey, _ := hex.DecodeString(info.ObfsKey)
	if len(obfsKey) > 0 {
		applog.Info("WebRTC: obfuscation key present, enabling UDP obfuscation")
	}

	relayAddr := info.RelayAddr
	if relayAddr != "" {
		applog.Infof("WebRTC: relay fallback: %s", relayAddr)
	}

	// Determine transport mode from connection info
	var transportMode webrtcpkg.TransportMode
	if info.TransportV == 2 {
		transportMode = webrtcpkg.TransportMediaStream
	}

	// Build client options: synced fields from connection info, per-side from config
	clientOpts := webrtcpkg.ClientOptions{
		Padding:              info.Padding,
		PaddingMax:           info.PaddingMax,
		ObfsKey:              obfsKey,
		TransportMode:        transportMode,
		NumChannels:          info.NumChannels,
		SmuxStreamBuffer:     info.SmuxStreamBuffer,
		SmuxSessionBuffer:    info.SmuxSessionBuffer,
		SmuxFrameSize:        info.SmuxFrameSize,
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
			DTLSSkipVerify:     boolPtr(cfg.DTLSSkipVerify),
			SCTPZeroChecksum:   boolPtr(cfg.SCTPZeroChecksum),
			DisableCloseByDTLS: boolPtr(cfg.DisableCloseByDTLS),
		},
	}

	// Try SSE offer stream for real-time updates; fall back to polling.
	applog.Infof("Getting %d SDP offers from signaling: %s/session/%s/offer", npc, sigURL, info.SessionID)
	sseCtx, sseCancel := context.WithCancel(ctx)
	offerCh, sseErr := signaling.StreamSDPOffers(sseCtx, sigURL, info.SessionID, nil)

	var sdpOffers []string
	var err error
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

	// If connection code says npc but server posted fewer offers, adjust
	if len(sdpOffers) < npc {
		applog.Warnf("Expected %d SDP offers but got %d, adjusting npc", npc, len(sdpOffers))
		npc = len(sdpOffers)
	}

	c.mu.Lock()
	if !c.connecting {
		c.mu.Unlock()
		sseCancel()
		return fmt.Errorf("connection cancelled")
	}
	c.mu.Unlock()

	// SSE refresh-aware loop: if server refreshes offers while we're
	// creating the client group, restart with the latest offers.
	var wrtcGroup *webrtcpkg.ClientGroup
	var sdpAnswers []string
	for {
		// No TUN on desktop, no socket protection needed
		wrtcGroup, sdpAnswers, err = webrtcpkg.StartClientGroup(npc, sdpOffers, iceServers, nil, obfsKey, relayAddr, info.SessionID, clientOpts)
		if err != nil {
			sseCancel()
			return fmt.Errorf("start WebRTC client group: %w", err)
		}

		c.mu.Lock()
		if !c.connecting {
			c.mu.Unlock()
			wrtcGroup.Stop()
			sseCancel()
			return fmt.Errorf("connection cancelled")
		}
		c.mu.Unlock()

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

	applog.Infof("Posting %d SDP answers to signaling: %s/session/%s/answer", npc, sigURL, info.SessionID)
	if err := signaling.PostSDPAnswers(sigURL, info.SessionID, sdpAnswers, nil); err != nil {
		wrtcGroup.Stop()
		return fmt.Errorf("post SDP answers: %w", err)
	}
	applog.Success("SDP answers posted to signaling server")

	applog.Info("Waiting for WebRTC connections to establish...")
	timeout := 65*time.Second + time.Duration(npc-1)*15*time.Second
	if err := wrtcGroup.WaitConnected(timeout); err != nil {
		wrtcGroup.Stop()
		return fmt.Errorf("WebRTC connection: %w", err)
	}
	applog.Successf("WebRTC connections established (%d PCs)", npc)

	go wrtcGroup.ServeSocks5(cfg.SocksPort)

	c.mu.Lock()
	if !c.connecting {
		c.mu.Unlock()
		wrtcGroup.Stop()
		return fmt.Errorf("connection cancelled")
	}
	c.webrtcClientGroup = wrtcGroup
	c.socksPort = cfg.SocksPort
	c.connecting = false
	c.running = true
	*connected = true
	c.mu.Unlock()

	applog.Successf("Client connected (WebRTC, %d PeerConnections)", npc)

	// Monitor WebRTC health — stop client when all ICE connections die
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			c.mu.Lock()
			if !c.running {
				c.mu.Unlock()
				return
			}
			group := c.webrtcClientGroup
			c.mu.Unlock()
			if group != nil && !group.IsAlive() {
				applog.Warn("WebRTC connection lost, stopping client...")
				c.StopClient()
				return
			}
		}
	}()

	// Test latency after connect
	go func() {
		time.Sleep(2 * time.Second)
		ms, err := TestLatency(cfg.SocksPort)
		if err != nil {
			applog.Infof("Auto latency test: error: %v", err)
		} else {
			applog.Infof("Auto latency test: %dms", ms)
			c.mu.Lock()
			c.lastLatencyMs = int(ms)
			c.mu.Unlock()
		}
	}()

	return nil
}

func (c *ClientOrchestrator) startClientXray(info *types.ConnectionInfo, cfg types.ClientConfig, connected *bool) error {
	targetIP := info.PublicIP
	targetPort := info.Port

	applog.Infof("xray path: connecting to %s:%d via %s/%s", targetIP, targetPort, info.Protocol, info.Transport)

	c.mu.Lock()
	if !c.connecting {
		c.mu.Unlock()
		return fmt.Errorf("connection cancelled")
	}
	c.mu.Unlock()

	// Desktop: no socket protection needed
	applog.Infof("Building xray %s/%s client config (server=%s:%d, socks=127.0.0.1:%d)", info.Protocol, info.Transport, targetIP, targetPort, cfg.SocksPort)
	configJSON, err := xray.BuildClientConfig(targetIP, targetPort, info.UUID, cfg.SocksPort, info.ProxySettings)
	if err != nil {
		return fmt.Errorf("build client config: %w", err)
	}

	applog.Info("Starting xray-core client...")
	if err := xray.StartXray(configJSON); err != nil {
		return fmt.Errorf("start xray: %w", err)
	}

	c.mu.Lock()
	if !c.connecting {
		c.mu.Unlock()
		xray.StopXray()
		return fmt.Errorf("connection cancelled")
	}
	c.socksPort = cfg.SocksPort
	c.connecting = false
	c.running = true
	*connected = true
	c.mu.Unlock()

	applog.Success("Client connected")

	go func() {
		time.Sleep(2 * time.Second)
		ms, err := TestLatency(cfg.SocksPort)
		if err != nil {
			applog.Infof("Auto latency test: error: %v", err)
		} else {
			applog.Infof("Auto latency test: %dms", ms)
			c.mu.Lock()
			c.lastLatencyMs = int(ms)
			c.mu.Unlock()
		}
	}()

	return nil
}

// StartClientManual decodes a manual offer code (M1:...), creates a WebRTC
// client group, and returns the manual answer code (M1A:...) for the user to
// share back with the server. Call WaitManualConnection after the server
// accepts the answer.
func (c *ClientOrchestrator) StartClientManual(offerCode string) (string, error) {
	c.mu.Lock()
	if c.running || c.connecting {
		c.mu.Unlock()
		return "", fmt.Errorf("client already running")
	}
	c.connecting = true
	c.mu.Unlock()

	applog.SetMaskIPs(c.cfg.MaskIPs)
	applog.Info("Processing manual offer code...")

	offer, err := signaling.DecodeManualOffer(offerCode)
	if err != nil {
		c.mu.Lock()
		c.connecting = false
		c.mu.Unlock()
		return "", fmt.Errorf("decode manual offer: %w", err)
	}

	sdp, err := signaling.DecompressSDP(offer.CompressedSDP)
	if err != nil {
		c.mu.Lock()
		c.connecting = false
		c.mu.Unlock()
		return "", fmt.Errorf("decompress offer SDP: %w", err)
	}

	cfg := c.cfg

	iceServers := []string{
		"stun:" + cfg.StunServer,
		"stun:" + defaultStunServer2,
	}

	obfsKey, _ := hex.DecodeString(offer.ObfsKey)

	var transportMode webrtcpkg.TransportMode
	if offer.TransportV == 2 {
		transportMode = webrtcpkg.TransportMediaStream
	}

	clientOpts := webrtcpkg.ClientOptions{
		Padding:              offer.Padding,
		PaddingMax:           offer.PaddingMax,
		ObfsKey:              obfsKey,
		TransportMode:        transportMode,
		NumChannels:          offer.NumChannels,
		SmuxStreamBuffer:     offer.SmuxStreamBuffer,
		SmuxSessionBuffer:    offer.SmuxSessionBuffer,
		SmuxFrameSize:        offer.SmuxFrameSize,
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
			DTLSSkipVerify:     boolPtr(cfg.DTLSSkipVerify),
			SCTPZeroChecksum:   boolPtr(cfg.SCTPZeroChecksum),
			DisableCloseByDTLS: boolPtr(cfg.DisableCloseByDTLS),
		},
	}

	sessionID := fmt.Sprintf("manual-%d", time.Now().UnixNano())

	wrtcGroup, sdpAnswers, err := webrtcpkg.StartClientGroup(1, []string{sdp}, iceServers, nil, obfsKey, offer.RelayAddr, sessionID, clientOpts)
	if err != nil {
		c.mu.Lock()
		c.connecting = false
		c.mu.Unlock()
		return "", fmt.Errorf("start WebRTC client group: %w", err)
	}

	// Compress the answer SDP
	compressedSDP, err := signaling.CompressSDP(sdpAnswers[0])
	if err != nil {
		wrtcGroup.Stop()
		c.mu.Lock()
		c.connecting = false
		c.mu.Unlock()
		return "", fmt.Errorf("compress answer SDP: %w", err)
	}

	answer := &signaling.ManualAnswer{
		Version:       3,
		CompressedSDP: compressedSDP,
	}

	answerCode, err := signaling.EncodeManualAnswer(answer)
	if err != nil {
		wrtcGroup.Stop()
		c.mu.Lock()
		c.connecting = false
		c.mu.Unlock()
		return "", fmt.Errorf("encode manual answer: %w", err)
	}

	c.mu.Lock()
	c.pendingManualGroup = wrtcGroup
	c.mu.Unlock()

	applog.Successf("Manual answer code generated (%d chars)", len(answerCode))
	return answerCode, nil
}

// WaitManualConnection waits for the ICE connection to establish on the
// pending manual client group, then starts the SOCKS5 proxy.
func (c *ClientOrchestrator) WaitManualConnection(timeout time.Duration) error {
	c.mu.Lock()
	group := c.pendingManualGroup
	if group == nil {
		c.mu.Unlock()
		return fmt.Errorf("no pending manual connection")
	}
	c.mu.Unlock()

	applog.Info("Waiting for WebRTC connection to establish...")
	if err := group.WaitConnected(timeout); err != nil {
		group.Stop()
		c.mu.Lock()
		c.pendingManualGroup = nil
		c.connecting = false
		c.mu.Unlock()
		return fmt.Errorf("WebRTC connection: %w", err)
	}

	applog.Success("WebRTC connection established")

	cfg := c.cfg
	go group.ServeSocks5(cfg.SocksPort)

	c.mu.Lock()
	c.webrtcClientGroup = group
	c.pendingManualGroup = nil
	c.socksPort = cfg.SocksPort
	c.connecting = false
	c.running = true
	c.mu.Unlock()

	applog.Successf("Client connected (manual WebRTC, SOCKS5 on port %d)", cfg.SocksPort)

	// Monitor WebRTC health
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			c.mu.Lock()
			if !c.running {
				c.mu.Unlock()
				return
			}
			g := c.webrtcClientGroup
			c.mu.Unlock()
			if g != nil && !g.IsAlive() {
				applog.Warn("WebRTC connection lost, stopping client...")
				c.StopClient()
				return
			}
		}
	}()

	// Test latency after connect
	go func() {
		time.Sleep(2 * time.Second)
		ms, err := TestLatency(cfg.SocksPort)
		if err != nil {
			applog.Infof("Auto latency test: error: %v", err)
		} else {
			applog.Infof("Auto latency test: %dms", ms)
			c.mu.Lock()
			c.lastLatencyMs = int(ms)
			c.mu.Unlock()
		}
	}()

	return nil
}

func (c *ClientOrchestrator) StopClient() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connecting {
		c.connecting = false
		if c.connectCancel != nil {
			c.connectCancel()
			c.connectCancel = nil
		}
		applog.Info("Cancelling client connection...")
	}

	if !c.running {
		return nil
	}

	applog.Info("Stopping client...")

	if c.pendingManualGroup != nil {
		c.pendingManualGroup.Stop()
		c.pendingManualGroup = nil
	}

	if c.webrtcClientGroup != nil {
		c.webrtcClientGroup.Stop()
		c.webrtcClientGroup = nil
	} else if c.webrtcClient != nil {
		c.webrtcClient.Stop()
		c.webrtcClient = nil
	} else {
		if err := xray.StopXray(); err != nil {
			applog.Errorf("Stop xray failed: %v", err)
			return fmt.Errorf("stop xray: %w", err)
		}
	}

	c.running = false
	c.socksPort = 0
	applog.Success("Client disconnected")
	return nil
}

func (c *ClientOrchestrator) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

func (c *ClientOrchestrator) GetSocksPort() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.socksPort
}

func (c *ClientOrchestrator) GetLastLatencyMs() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastLatencyMs
}

func (c *ClientOrchestrator) GetMetrics(socksAddr string) display.ClientMetrics {
	c.mu.Lock()
	defer c.mu.Unlock()

	m := display.ClientMetrics{
		Connected: c.running,
		SocksAddr: socksAddr,
		LatencyMs: c.lastLatencyMs,
	}

	if c.webrtcClientGroup != nil {
		m.DataChannels = c.webrtcClientGroup.GetChannelCount()
		m.PeerConns = c.webrtcClientGroup.Count()
		m.SmuxStreams = c.webrtcClientGroup.GetStreamCount()
	} else if c.webrtcClient != nil {
		m.DataChannels = c.webrtcClient.GetChannelCount()
		m.PeerConns = 1
		m.SmuxStreams = c.webrtcClient.GetStreamCount()
	}

	return m
}
