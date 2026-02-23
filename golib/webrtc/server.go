package webrtc

import (
	crypto_rand "crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pionwebrtc "github.com/pion/webrtc/v4"
	"github.com/xtaci/smux"

	"natproxy/golib/applog"
)

// generateRandomDCLabel creates random 8-32 hex char labels to prevent fingerprinting.
func generateRandomDCLabel() string {
	// Random length between 4-16 bytes (8-32 hex chars)
	lenBuf := make([]byte, 1)
	crypto_rand.Read(lenBuf)
	n := 4 + int(lenBuf[0])%13 // 4-16 bytes
	buf := make([]byte, n)
	crypto_rand.Read(buf)
	return hex.EncodeToString(buf)
}

// newSmuxConfig returns a tuned smux configuration for proxy tunneling.
func newSmuxConfig(streamBufKB, sessBufKB, frameSize, keepAliveSec, keepAliveTimeoutSec int) *smux.Config {
	cfg := smux.DefaultConfig()
	cfg.MaxStreamBuffer = streamBufKB * 1024
	cfg.MaxReceiveBuffer = sessBufKB * 1024
	cfg.MaxFrameSize = frameSize
	cfg.KeepAliveInterval = time.Duration(keepAliveSec) * time.Second
	cfg.KeepAliveTimeout = time.Duration(keepAliveTimeoutSec) * time.Second
	return cfg
}

// newSmuxMediaConfig returns a smux config for media transport.
// MaxFrameSize limited to 1400 so frame + headers (1411) stay under pion's
// 1460-byte NACK buffer limit.
func newSmuxMediaConfig(streamBufKB, sessBufKB, keepAliveSec, keepAliveTimeoutSec int) *smux.Config {
	cfg := smux.DefaultConfig()
	cfg.MaxStreamBuffer = streamBufKB * 1024
	cfg.MaxReceiveBuffer = sessBufKB * 1024
	cfg.MaxFrameSize = 1400 // limited by RTP payload size
	cfg.KeepAliveInterval = time.Duration(keepAliveSec) * time.Second
	cfg.KeepAliveTimeout = time.Duration(keepAliveTimeoutSec) * time.Second
	return cfg
}

// Server manages a WebRTC PeerConnection that accepts proxied connections
// from a remote client via multiple data channels + smux multiplexers.
type Server struct {
	pc        *pionwebrtc.PeerConnection
	muxCloser io.Closer          // UDP mux socket (for cleanup)
	rawConn   net.PacketConn     // underlying raw UDP socket (for STUN keepalive)

	sessions  []*smux.Session // one per data channel
	connected chan struct{}   // closed when ALL data channels are open
	done      chan struct{}   // closed on Stop()
	failed    chan struct{}   // closed on ICE/PeerConnection failure

	activeStreams  atomic.Int32
	readyCount    atomic.Int32
	failedCount   atomic.Int32
	connectedOnce sync.Once
	failedOnce    sync.Once
	iceAlive      atomic.Bool // true when ICE is connected/completed
	bytesUp       atomic.Int64 // bytes relayed: client → internet
	bytesDown     atomic.Int64 // bytes relayed: internet → client
	mu            sync.Mutex

	// Configurable settings
	numChannels          int
	smuxStreamBuffer     int // KB
	smuxSessionBuffer    int // KB
	smuxFrameSize        int // bytes
	smuxKeepAlive        int // seconds
	smuxKeepAliveTimeout int // seconds
	dcMaxBuffered        int // KB
	dcLowMark            int // KB

	// Server-wide stream limiting
	totalStreamSem chan struct{}

	// Traffic padding
	padding    bool
	paddingMax int

	// Transport mode
	transportMode TransportMode
	obfsKey       []byte           // needed for media transport seed
	mediaSetup    *MediaTrackSetup // pre-prepared track (media mode only)
	mediaOnce     sync.Once        // ensures setupMediaTransport runs at most once

	// Per-client rate limiting (0 = unlimited)
	rateLimitUp   int64
	rateLimitDown int64
}

// ServerOptions configures optional server behavior.
type ServerOptions struct {
	Padding       bool
	PaddingMax    int
	RateLimitUp   int64          // bytes/sec, 0 = unlimited
	RateLimitDown int64          // bytes/sec, 0 = unlimited
	DisableIPv6   bool
	TransportMode TransportMode  // data channels (default) or media stream
	ObfsKey       []byte         // obfs key, used as seed for media transport codec selection

	// Tunable channel/smux/DC settings
	NumPeerConnections   int // parallel PeerConnections (default 1)
	NumChannels          int // parallel data channels (default 6)
	SmuxStreamBuffer     int // per-stream receive window in KB (default 2048)
	SmuxSessionBuffer    int // session-wide receive buffer in KB (default 8192)
	SmuxFrameSize        int // max smux frame size in bytes (default 32768)
	SmuxKeepAlive        int // keepalive interval in seconds (default 10)
	SmuxKeepAliveTimeout int // keepalive timeout in seconds (default 300)
	DCMaxBuffered        int // DC backpressure high water in KB (default 2048)
	DCLowMark            int // DC backpressure low water in KB (default 512)
	MaxTotalStreams       int // server-wide stream limit (0 = unlimited)

	// Peer (SCTP/DTLS/ICE/UDP) settings
	PeerConfig
}

// StartServer creates a WebRTC PeerConnection as the offerer, creates
// numChannels "proxy-N" data channels, and returns the local SDP offer
// (with ICE candidates). The caller should post this offer to the signaling
// server.
// If obfsKey is non-nil, all UDP traffic is obfuscated.
// If relayAddr is non-empty, a relay candidate is injected as ICE fallback.
func StartServer(iceServers []string, obfsKey []byte, relayAddr string, sessionID string, opts ...ServerOptions) (*Server, string, error) {
	var o ServerOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	applyServerDefaults(&o)

	peerCfg := o.PeerConfig
	result, err := newPeerConnection(iceServers, nil, obfsKey, relayAddr, sessionID, true, peerCfg)
	if err != nil {
		return nil, "", err
	}

	s := &Server{
		pc:                   result.pc,
		muxCloser:            result.closer,
		rawConn:              result.rawConn,
		sessions:             make([]*smux.Session, 0, o.NumChannels),
		connected:            make(chan struct{}),
		done:                 make(chan struct{}),
		failed:               make(chan struct{}),
		numChannels:          o.NumChannels,
		smuxStreamBuffer:     o.SmuxStreamBuffer,
		smuxSessionBuffer:    o.SmuxSessionBuffer,
		smuxFrameSize:        o.SmuxFrameSize,
		smuxKeepAlive:        o.SmuxKeepAlive,
		smuxKeepAliveTimeout: o.SmuxKeepAliveTimeout,
		dcMaxBuffered:        o.DCMaxBuffered,
		dcLowMark:            o.DCLowMark,
		totalStreamSem:       makeSemaphore(o.MaxTotalStreams),
		padding:              o.Padding,
		paddingMax:           o.PaddingMax,
		rateLimitUp:          o.RateLimitUp,
		rateLimitDown:        o.RateLimitDown,
		transportMode:        o.TransportMode,
		obfsKey:              o.ObfsKey,
	}

	if s.transportMode == TransportMediaStream {
		// Phase 11: Media stream transport — add RTP tracks before SDP creation
		// so they're included in the offer. The reliable layer + smux session
		// are set up after SDP negotiation in setupMediaTransport().
		setup, err := PrepareMediaTrack(result.pc, s.obfsKey)
		if err != nil {
			result.pc.Close()
			if result.closer != nil {
				result.closer.Close()
			}
			return nil, "", fmt.Errorf("prepare media track: %w", err)
		}
		s.mediaSetup = setup
		applog.Info("webrtc server: using media stream transport mode")
	} else {
		// Create numChannels ordered data channels. Each becomes an independent
		// SCTP stream with its own smux multiplexer — a packet loss on one channel
		// does not stall streams on other channels.
		// Labels are randomized to prevent fingerprinting (Phase 1).
		for i := 0; i < s.numChannels; i++ {
			label := generateRandomDCLabel()
			dc, err := result.pc.CreateDataChannel(label, nil)
			if err != nil {
				result.pc.Close()
				if result.closer != nil {
					result.closer.Close()
				}
				return nil, "", fmt.Errorf("create data channel %s: %w", label, err)
			}

			// Capture loop variables for the closure.
			channelLabel := label
			dcRef := dc
			dc.OnOpen(func() {
				applog.Infof("webrtc server: data channel '%s' opened", channelLabel)

				raw, err := dcRef.Detach()
				if err != nil {
					applog.Errorf("webrtc server: detach %s: %v", channelLabel, err)
					s.failedCount.Add(1)
					s.checkAllChannelsReady()
					return
				}

				sess, err := smux.Server(
					newDCStreamConn(raw, dcRef, s.dcMaxBuffered*1024, s.dcLowMark*1024),
					newSmuxConfig(s.smuxStreamBuffer, s.smuxSessionBuffer, s.smuxFrameSize, s.smuxKeepAlive, s.smuxKeepAliveTimeout),
				)
				if err != nil {
					applog.Errorf("webrtc server: smux %s: %v", channelLabel, err)
					s.failedCount.Add(1)
					s.checkAllChannelsReady()
					return
				}

				s.mu.Lock()
				s.sessions = append(s.sessions, sess)
				s.mu.Unlock()

				go s.acceptLoop(sess)

				s.readyCount.Add(1)
				s.checkAllChannelsReady()
			})
		}
	}

	result.pc.OnICEConnectionStateChange(func(state pionwebrtc.ICEConnectionState) {
		applog.Infof("webrtc server: ICE state: %s", state.String())
		switch state {
		case pionwebrtc.ICEConnectionStateConnected, pionwebrtc.ICEConnectionStateCompleted:
			s.iceAlive.Store(true)
		case pionwebrtc.ICEConnectionStateDisconnected:
			s.iceAlive.Store(false)
			applog.Warn("webrtc server: ICE disconnected (may recover)")
		case pionwebrtc.ICEConnectionStateFailed, pionwebrtc.ICEConnectionStateClosed:
			s.iceAlive.Store(false)
			s.failedOnce.Do(func() { close(s.failed) })
			applog.Warn("webrtc server: ICE connection lost, closing smux sessions")
			s.closeSessions()
		}
	})

	result.pc.OnConnectionStateChange(func(state pionwebrtc.PeerConnectionState) {
		applog.Infof("webrtc server: PeerConnection state: %s", state.String())
		switch state {
		case pionwebrtc.PeerConnectionStateConnected:
			// For media transport mode, set up the transport once DTLS/SRTP is ready.
			if s.transportMode == TransportMediaStream {
				s.mediaOnce.Do(func() { go s.setupMediaTransport() })
			}
		case pionwebrtc.PeerConnectionStateFailed, pionwebrtc.PeerConnectionStateClosed:
			s.iceAlive.Store(false)
			s.failedOnce.Do(func() { close(s.failed) })
			s.closeSessions()
		}
	})

	result.pc.OnICECandidate(func(c *pionwebrtc.ICECandidate) {
		if c != nil {
			applog.Infof("webrtc server: ICE candidate: %s %s %s:%d", c.Protocol, c.Typ, c.Address, c.Port)
		}
	})

	// Create offer and gather ICE candidates.
	offer, err := result.pc.CreateOffer(nil)
	if err != nil {
		result.pc.Close()
		if result.closer != nil {
			result.closer.Close()
		}
		return nil, "", fmt.Errorf("create offer: %w", err)
	}

	gatherComplete := pionwebrtc.GatheringCompletePromise(result.pc)

	if err := result.pc.SetLocalDescription(offer); err != nil {
		result.pc.Close()
		if result.closer != nil {
			result.closer.Close()
		}
		return nil, "", fmt.Errorf("set local description: %w", err)
	}

	<-gatherComplete

	sdp := result.pc.LocalDescription().SDP

	// Inject manually-discovered srflx candidate into the SDP. Pion's
	// built-in STUN creates separate sockets that bypass UDPMux/obfuscation,
	// so we must inject the correct srflx ourselves.
	sdp = injectSrflxCandidate(sdp, result.publicIP, result.publicPort, result.localPort)

	// Inject relay candidate as low-priority fallback for restrictive NATs.
	sdp = injectRelayCandidate(sdp, result.relayAddr, result.localPort)

	// Phase 5: Filter bogon candidates from SDP.
	sdp = filterBogonCandidates(sdp)

	applog.Infof("webrtc server: SDP offer ready (%d bytes, srflx=%s:%d, relay=%s, channels=%d)",
		len(sdp), result.publicIP, result.publicPort, result.relayAddr, s.numChannels)

	// Log SDP candidates for diagnostics
	for _, line := range strings.Split(sdp, "\r\n") {
		if strings.HasPrefix(line, "a=candidate:") || strings.HasPrefix(line, "a=end-of-candidates") {
			applog.Infof("webrtc server: SDP → %s", line)
		}
	}

	return s, sdp, nil
}

// AcceptAnswer sets the remote SDP answer from the client.
func (s *Server) AcceptAnswer(sdpAnswer string) error {
	answer := pionwebrtc.SessionDescription{
		Type: pionwebrtc.SDPTypeAnswer,
		SDP:  sdpAnswer,
	}
	// Log remote SDP candidates for diagnostics
	for _, line := range strings.Split(sdpAnswer, "\r\n") {
		if strings.HasPrefix(line, "a=candidate:") {
			applog.Infof("webrtc server: remote SDP ← %s", line)
		}
	}

	if err := s.pc.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("set remote description: %w", err)
	}
	applog.Info("webrtc server: remote answer accepted")

	// Media transport setup is triggered by OnConnectionStateChange (connected)
	// to ensure DTLS/SRTP is ready before sending RTP.

	return nil
}

// setupMediaTransport creates the reliable RTP + smux session for media mode.
func (s *Server) setupMediaTransport() {
	transport, err := NewMediaReliableTransport(s.pc, s.mediaSetup)
	if err != nil {
		applog.Errorf("webrtc server: media transport: %v", err)
		return
	}

	sess, err := smux.Server(transport, newSmuxMediaConfig(s.smuxStreamBuffer, s.smuxSessionBuffer, s.smuxKeepAlive, s.smuxKeepAliveTimeout))
	if err != nil {
		applog.Errorf("webrtc server: media smux: %v", err)
		transport.Close()
		return
	}

	s.mu.Lock()
	s.sessions = append(s.sessions, sess)
	s.mu.Unlock()

	go s.acceptLoop(sess)

	applog.Info("webrtc server: media transport ready")
	close(s.connected)
}

// checkAllChannelsReady closes s.connected when all channels have reported
// (either successfully or with failure), as long as at least one succeeded.
func (s *Server) checkAllChannelsReady() {
	total := s.readyCount.Load() + s.failedCount.Load()
	if total == int32(s.numChannels) && s.readyCount.Load() > 0 {
		s.connectedOnce.Do(func() {
			applog.Infof("webrtc server: all %d channels reported (%d ready, %d failed)",
				s.numChannels, s.readyCount.Load(), s.failedCount.Load())
			close(s.connected)
		})
	}
}

// WaitConnected blocks until all data channels are open, ICE fails, or timeout.
func (s *Server) WaitConnected(timeout time.Duration) error {
	select {
	case <-s.connected:
		return nil
	case <-s.failed:
		return fmt.Errorf("webrtc server: ICE/PeerConnection failed before channels opened")
	case <-time.After(timeout):
		return fmt.Errorf("webrtc server: connection timeout after %v (%d/%d channels ready)",
			timeout, s.readyCount.Load(), s.numChannels)
	case <-s.done:
		return fmt.Errorf("webrtc server: stopped")
	}
}

// GetClientCount returns the number of active smux streams (proxy connections).
func (s *Server) GetClientCount() int {
	return int(s.activeStreams.Load())
}

// GetChannelCount returns the number of active smux sessions (data channels).
func (s *Server) GetChannelCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// IsAlive returns true if the ICE connection is in connected or completed state.
func (s *Server) IsAlive() bool {
	return s.iceAlive.Load()
}

// closeSessions closes all smux sessions (triggered by ICE failure).
func (s *Server) closeSessions() {
	s.mu.Lock()
	sessions := make([]*smux.Session, len(s.sessions))
	copy(sessions, s.sessions)
	s.sessions = s.sessions[:0]
	s.mu.Unlock()
	for _, sess := range sessions {
		sess.Close()
	}
}

// Stop tears down the server.
func (s *Server) Stop() {
	select {
	case <-s.done:
		return
	default:
	}
	close(s.done)

	s.mu.Lock()
	sessions := make([]*smux.Session, len(s.sessions))
	copy(sessions, s.sessions)
	s.mu.Unlock()

	for _, sess := range sessions {
		sess.Close()
	}
	s.pc.Close()
	if s.muxCloser != nil {
		s.muxCloser.Close()
	}
	applog.Infof("webrtc server: stopped (%d channels closed)", len(sessions))
}

// countingRWC wraps an io.ReadWriteCloser and counts bytes flowing through it.
type countingRWC struct {
	inner        io.ReadWriteCloser
	bytesRead    *atomic.Int64
	bytesWritten *atomic.Int64
}

func (c *countingRWC) Read(p []byte) (int, error) {
	n, err := c.inner.Read(p)
	if n > 0 {
		c.bytesRead.Add(int64(n))
	}
	return n, err
}

func (c *countingRWC) Write(p []byte) (int, error) {
	n, err := c.inner.Write(p)
	if n > 0 {
		c.bytesWritten.Add(int64(n))
	}
	return n, err
}

func (c *countingRWC) Close() error {
	return c.inner.Close()
}

// GetStats returns the total bytes relayed through this server.
func (s *Server) GetStats() (bytesUp, bytesDown int64) {
	return s.bytesUp.Load(), s.bytesDown.Load()
}

// GetStreamDistribution returns the number of active smux streams per session.
func (s *Server) GetStreamDistribution() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	dist := make([]int, len(s.sessions))
	for i, sess := range s.sessions {
		dist[i] = sess.NumStreams()
	}
	return dist
}

// makeSemaphore returns a buffered channel of the given capacity, or nil if n <= 0 (unlimited).
func makeSemaphore(n int) chan struct{} {
	if n <= 0 {
		return nil
	}
	return make(chan struct{}, n)
}

// acceptLoop accepts smux streams and proxies each to the target.
func (s *Server) acceptLoop(sess *smux.Session) {
	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			select {
			case <-s.done:
			default:
				applog.Warnf("webrtc server: accept stream: %v", err)
			}
			return
		}

		// Server-wide backpressure (nil = unlimited).
		if s.totalStreamSem != nil {
			select {
			case s.totalStreamSem <- struct{}{}:
			case <-s.done:
				stream.Close()
				return
			}
		}

		s.activeStreams.Add(1)
		go func() {
			defer func() {
				if s.totalStreamSem != nil {
					<-s.totalStreamSem
				}
			}()
			defer s.activeStreams.Add(-1)
			s.handleStream(stream)
		}()
	}
}

// handleStream reads the target address header from a stream, dials the
// target, and relays data bidirectionally.
func (s *Server) handleStream(stream *smux.Stream) {
	streamDone := make(chan struct{})
	defer close(streamDone)

	// relayed tracks whether relay() ran. If it did, the activityConn wrappers
	// inside relay() already closed the stream — skip the drain-and-close to
	// avoid a double-close race on the smux stream.
	relayed := false
	defer func() {
		if !relayed {
			// Drain any remaining data before closing to avoid RST on the smux stream.
			stream.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			io.Copy(io.Discard, stream)
			stream.Close()
		}
	}()

	// Optionally wrap stream with padding (must match client side)
	var streamRWC io.ReadWriteCloser = stream
	if s.padding {
		streamRWC = newPaddedStream(stream, 0, s.paddingMax)
	}

	// Count bytes flowing through the stream.
	// Reads = data from client (upload), Writes = data to client (download).
	streamRWC = &countingRWC{inner: streamRWC, bytesRead: &s.bytesUp, bytesWritten: &s.bytesDown}

	target, err := readTargetAddr(streamRWC)
	if err != nil {
		applog.Warnf("webrtc server: read target addr: %v", err)
		return
	}

	// DNS channel: persistent multiplexed DNS relay (no TCP dial).
	if target == DNSChannelTarget {
		handleDNSStream(streamRWC)
		return
	}

	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		applog.Warnf("webrtc server: dial %s: %v", target, err)
		return
	}
	defer conn.Close()

	// Disable Nagle's algorithm to avoid delays on small writes (HTTP headers,
	// TLS handshake messages). The smux layer already handles framing.
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}

	// Force-close TCP connection when server stops to prevent lingering relays.
	go func() {
		select {
		case <-s.done:
			conn.Close()
		case <-streamDone:
		}
	}()

	// Optionally apply rate limiting to the outbound connection
	var relayConn io.ReadWriteCloser = conn
	if s.rateLimitUp > 0 || s.rateLimitDown > 0 {
		relayConn = newThrottledConn(conn, s.rateLimitUp, s.rateLimitDown)
	}

	relayed = true
	relay(streamRWC, relayConn)
}
