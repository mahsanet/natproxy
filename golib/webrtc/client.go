package webrtc

import (
	"context"
	"encoding/binary"
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

// Client manages a WebRTC PeerConnection that multiplexes streams over
// data channels (or media transport) to a remote WebRTC server via smux.
type Client struct {
	pc        *pionwebrtc.PeerConnection
	muxCloser io.Closer // UDP mux socket (for cleanup)
	socketFD  int       // raw UDP socket fd (for deferred protect)

	sessions    []*smux.Session // one per data channel
	nextSession atomic.Uint32   // round-robin counter
	connected   chan struct{}   // closed when ALL data channels are open
	done        chan struct{}   // closed on Stop()
	failed      chan struct{}   // closed on ICE/PeerConnection failure

	readyCount    atomic.Int32
	failedCount   atomic.Int32
	connectedOnce sync.Once
	failedOnce    sync.Once
	stopOnce      sync.Once
	iceAlive      atomic.Bool
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

	// Traffic padding
	padding    bool
	paddingMax int

	// Transport mode
	transportMode TransportMode
	obfsKey       []byte           // needed for media transport seed
	mediaSetup    *MediaTrackSetup // pre-prepared track (media mode only)
	mediaOnce     sync.Once        // ensures setupMediaTransport runs at most once

	// ICE reconnect callback (set by api.go for fast reconnect)
	onReconnectNeeded func()
}

// ClientOptions configures optional client behavior.
type ClientOptions struct {
	Padding       bool
	PaddingMax    int
	TransportMode TransportMode // data channels (default) or media stream
	ObfsKey       []byte        // obfs key, used as seed for media transport codec selection

	// Synced channel/smux/DC settings (must match server)
	NumPeerConnections   int // parallel PeerConnections (default 1)
	NumChannels          int // parallel data channels (default 6)
	SmuxStreamBuffer     int // per-stream receive window in KB (default 2048)
	SmuxSessionBuffer    int // session-wide receive buffer in KB (default 8192)
	SmuxFrameSize        int // max smux frame size in bytes (default 32768)
	SmuxKeepAlive        int // keepalive interval in seconds (default 10)
	SmuxKeepAliveTimeout int // keepalive timeout in seconds (default 300)
	DCMaxBuffered        int // DC backpressure high water in KB (default 2048)
	DCLowMark            int // DC backpressure low water in KB (default 512)

	// Peer (SCTP/DTLS/ICE/UDP) settings
	PeerConfig
}

// StartClient creates a WebRTC PeerConnection as the answerer. It accepts the
// server's SDP offer and returns the SDP answer to post back to the signaling
// server. Use DialStream for direct tunneling or ServeSocks5 for SOCKS5 proxy.
// If obfsKey is non-nil, all UDP traffic is obfuscated.
// If relayAddr is non-empty, a relay candidate is injected as ICE fallback.
func StartClient(sdpOffer string, iceServers []string, protectFn func(int) bool, obfsKey []byte, relayAddr string, sessionID string, opts ...ClientOptions) (*Client, string, error) {
	var o ClientOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	applyClientDefaults(&o)

	peerCfg := o.PeerConfig
	result, err := newPeerConnection(iceServers, protectFn, obfsKey, relayAddr, sessionID, false, peerCfg)
	if err != nil {
		return nil, "", err
	}

	c := &Client{
		pc:                   result.pc,
		muxCloser:            result.closer,
		socketFD:             result.socketFD,
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
		padding:              o.Padding,
		paddingMax:           o.PaddingMax,
		transportMode:        o.TransportMode,
		obfsKey:              o.ObfsKey,
	}

	// Log remote SDP candidates for diagnostics
	for _, line := range strings.Split(sdpOffer, "\r\n") {
		if strings.HasPrefix(line, "a=candidate:") {
			applog.Infof("webrtc client: remote SDP ← %s", line)
		}
	}

	// Set remote description (server's offer).
	offer := pionwebrtc.SessionDescription{
		Type: pionwebrtc.SDPTypeOffer,
		SDP:  sdpOffer,
	}
	if err := result.pc.SetRemoteDescription(offer); err != nil {
		result.pc.Close()
		if result.closer != nil {
			result.closer.Close()
		}
		return nil, "", fmt.Errorf("set remote description: %w", err)
	}

	if c.transportMode == TransportMediaStream {
		// Phase 11: Media stream transport — add RTP tracks before SDP answer
		// creation so they're included in the answer. The reliable layer + smux
		// session are set up after ICE connects in setupMediaTransport().
		setup, err := PrepareMediaTrack(result.pc, c.obfsKey)
		if err != nil {
			result.pc.Close()
			if result.closer != nil {
				result.closer.Close()
			}
			return nil, "", fmt.Errorf("prepare media track: %w", err)
		}
		c.mediaSetup = setup
		applog.Info("webrtc client: using media stream transport mode")
	} else {
		// Handle the data channels created by the server. The server creates
		// numChannels channels named "proxy-0" through "proxy-N". Each one gets
		// its own smux.Client session so streams are spread across independent
		// SCTP associations, eliminating head-of-line blocking.
		result.pc.OnDataChannel(func(dc *pionwebrtc.DataChannel) {
			label := dc.Label()
			dcRef := dc
			applog.Infof("webrtc client: data channel '%s' received", label)

			dc.OnOpen(func() {
				applog.Infof("webrtc client: data channel '%s' opened", label)

				raw, err := dcRef.Detach()
				if err != nil {
					applog.Errorf("webrtc client: detach %s: %v", label, err)
					c.failedCount.Add(1)
					c.checkAllChannelsReady()
					return
				}

				sess, err := smux.Client(
					newDCStreamConn(raw, dcRef, c.dcMaxBuffered*1024, c.dcLowMark*1024),
					newSmuxConfig(c.smuxStreamBuffer, c.smuxSessionBuffer, c.smuxFrameSize, c.smuxKeepAlive, c.smuxKeepAliveTimeout),
				)
				if err != nil {
					applog.Errorf("webrtc client: smux %s: %v", label, err)
					c.failedCount.Add(1)
					c.checkAllChannelsReady()
					return
				}

				c.mu.Lock()
				c.sessions = append(c.sessions, sess)
				c.mu.Unlock()

				c.readyCount.Add(1)
				c.checkAllChannelsReady()
			})
		})
	}

	result.pc.OnICEConnectionStateChange(func(state pionwebrtc.ICEConnectionState) {
		applog.Infof("webrtc client: ICE state: %s", state.String())
		switch state {
		case pionwebrtc.ICEConnectionStateConnected, pionwebrtc.ICEConnectionStateCompleted:
			c.iceAlive.Store(true)
		case pionwebrtc.ICEConnectionStateFailed:
			c.iceAlive.Store(false)
			c.failedOnce.Do(func() { close(c.failed) })
			if c.onReconnectNeeded != nil {
				applog.Warn("webrtc client: ICE failed, requesting reconnect")
				go c.onReconnectNeeded()
			} else {
				applog.Warn("webrtc client: ICE failed, closing sessions")
				c.closeSessions()
			}
		case pionwebrtc.ICEConnectionStateClosed:
			c.iceAlive.Store(false)
			c.failedOnce.Do(func() { close(c.failed) })
			c.closeSessions()
		case pionwebrtc.ICEConnectionStateDisconnected:
			applog.Warn("webrtc client: ICE disconnected (may recover)")
		}
	})

	result.pc.OnConnectionStateChange(func(state pionwebrtc.PeerConnectionState) {
		applog.Infof("webrtc client: PeerConnection state: %s", state.String())
		switch state {
		case pionwebrtc.PeerConnectionStateConnected:
			// For media transport mode, set up the transport once DTLS/SRTP is ready.
			if c.transportMode == TransportMediaStream {
				c.mediaOnce.Do(func() { go c.setupMediaTransport() })
			}
		case pionwebrtc.PeerConnectionStateFailed, pionwebrtc.PeerConnectionStateClosed:
			c.failedOnce.Do(func() { close(c.failed) })
		}
	})

	result.pc.OnICECandidate(func(candidate *pionwebrtc.ICECandidate) {
		if candidate != nil {
			applog.Infof("webrtc client: ICE candidate: %s %s %s:%d", candidate.Protocol, candidate.Typ, candidate.Address, candidate.Port)
		}
	})

	// Create answer.
	answer, err := result.pc.CreateAnswer(nil)
	if err != nil {
		result.pc.Close()
		if result.closer != nil {
			result.closer.Close()
		}
		return nil, "", fmt.Errorf("create answer: %w", err)
	}

	gatherComplete := pionwebrtc.GatheringCompletePromise(result.pc)

	if err := result.pc.SetLocalDescription(answer); err != nil {
		result.pc.Close()
		if result.closer != nil {
			result.closer.Close()
		}
		return nil, "", fmt.Errorf("set local description: %w", err)
	}

	<-gatherComplete

	sdpAnswer := result.pc.LocalDescription().SDP

	// Inject manually-discovered srflx candidate into the SDP.
	sdpAnswer = injectSrflxCandidate(sdpAnswer, result.publicIP, result.publicPort, result.localPort)

	// Inject relay candidate as low-priority fallback for restrictive NATs.
	sdpAnswer = injectRelayCandidate(sdpAnswer, result.relayAddr, result.localPort)

	// Phase 5: Filter bogon candidates from SDP.
	sdpAnswer = filterBogonCandidates(sdpAnswer)

	applog.Infof("webrtc client: SDP answer ready (%d bytes, srflx=%s:%d, relay=%s, channels=%d)",
		len(sdpAnswer), result.publicIP, result.publicPort, result.relayAddr, c.numChannels)

	// Log local SDP candidates for diagnostics
	for _, line := range strings.Split(sdpAnswer, "\r\n") {
		if strings.HasPrefix(line, "a=candidate:") || strings.HasPrefix(line, "a=end-of-candidates") {
			applog.Infof("webrtc client: SDP → %s", line)
		}
	}

	return c, sdpAnswer, nil
}

// checkAllChannelsReady closes c.connected when all channels have reported
// (either successfully or with failure), as long as at least one succeeded.
func (c *Client) checkAllChannelsReady() {
	total := c.readyCount.Load() + c.failedCount.Load()
	if total == int32(c.numChannels) && c.readyCount.Load() > 0 {
		c.connectedOnce.Do(func() {
			applog.Infof("webrtc client: all %d channels reported (%d ready, %d failed)",
				c.numChannels, c.readyCount.Load(), c.failedCount.Load())
			close(c.connected)
		})
	}
}

// WaitConnected blocks until all data channels are open, ICE fails, or timeout.
func (c *Client) WaitConnected(timeout time.Duration) error {
	select {
	case <-c.connected:
		return nil
	case <-c.failed:
		return fmt.Errorf("webrtc client: ICE/PeerConnection failed before channels opened")
	case <-time.After(timeout):
		if c.transportMode == TransportMediaStream {
			return fmt.Errorf("webrtc client: media transport timeout after %v", timeout)
		}
		return fmt.Errorf("webrtc client: connection timeout after %v (%d/%d channels ready)",
			timeout, c.readyCount.Load(), c.numChannels)
	case <-c.done:
		return fmt.Errorf("webrtc client: stopped")
	}
}

// WaitConnectedCtx is like WaitConnected but also respects context cancellation.
func (c *Client) WaitConnectedCtx(ctx context.Context, timeout time.Duration) error {
	select {
	case <-c.connected:
		return nil
	case <-c.failed:
		return fmt.Errorf("webrtc client: ICE/PeerConnection failed before channels opened")
	case <-ctx.Done():
		return fmt.Errorf("cancelled: %w", ctx.Err())
	case <-time.After(timeout):
		if c.transportMode == TransportMediaStream {
			return fmt.Errorf("webrtc client: media transport timeout after %v", timeout)
		}
		return fmt.Errorf("webrtc client: connection timeout after %v (%d/%d channels ready)",
			timeout, c.readyCount.Load(), c.numChannels)
	case <-c.done:
		return fmt.Errorf("webrtc client: stopped")
	}
}

// setupMediaTransport creates the reliable RTP + smux session for media mode.
func (c *Client) setupMediaTransport() {
	transport, err := NewMediaReliableTransport(c.pc, c.mediaSetup)
	if err != nil {
		applog.Errorf("webrtc client: media transport: %v", err)
		return
	}

	sess, err := smux.Client(transport, newSmuxMediaConfig(c.smuxStreamBuffer, c.smuxSessionBuffer, c.smuxKeepAlive, c.smuxKeepAliveTimeout))
	if err != nil {
		applog.Errorf("webrtc client: media smux: %v", err)
		transport.Close()
		return
	}

	c.mu.Lock()
	c.sessions = append(c.sessions, sess)
	c.mu.Unlock()

	if c.readyCount.Add(1) == 1 {
		applog.Info("webrtc client: media transport ready")
		close(c.connected)
	}
}

// GetChannelCount returns the number of active smux sessions (data channels).
func (c *Client) GetChannelCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sessions)
}

// GetStreamCount returns the total number of active smux streams across all sessions.
func (c *Client) GetStreamCount() int {
	c.mu.Lock()
	sessions := c.sessions
	c.mu.Unlock()
	total := 0
	for _, sess := range sessions {
		total += sess.NumStreams()
	}
	return total
}

// IsAlive returns true if the ICE connection is connected/completed.
func (c *Client) IsAlive() bool {
	return c.iceAlive.Load()
}

// openStream opens a new smux stream on the least-loaded session. This spreads
// connections across data channels to minimize head-of-line blocking and
// maximize throughput.
func (c *Client) openStream() (*smux.Stream, error) {
	c.mu.Lock()
	sessions := c.sessions
	c.mu.Unlock()

	if len(sessions) == 0 {
		return nil, fmt.Errorf("no smux sessions available")
	}

	// Don't scan, just pick the first available session
	if len(sessions) == 1 {
		return sessions[0].OpenStream()
	}

	// Pick the session with the fewest active streams
	var best *smux.Session
	bestCount := int(^uint(0) >> 1) // max int
	for _, sess := range sessions {
		if sess.IsClosed() {
			continue
		}
		n := sess.NumStreams()
		if n < bestCount {
			bestCount = n
			best = sess
		}
	}

	if best == nil {
		// All sessions closed — fall back to round-robin on the full list
		// in case NumStreams/IsClosed is stale
		idx := c.nextSession.Add(1) % uint32(len(sessions))
		return sessions[idx].OpenStream()
	}

	return best.OpenStream()
}

// DialStream opens a new smux stream to the given target address. The target
// is written as a [2B len][target] header on the stream, matching the wire
// protocol expected by the server. The returned ReadWriteCloser is optionally
// wrapped with traffic padding if enabled.
func (c *Client) DialStream(target string) (io.ReadWriteCloser, error) {
	stream, err := c.openStream()
	if err != nil {
		return nil, err
	}

	paddingMax := c.paddingMax
	if paddingMax == 0 && c.padding {
		paddingMax = 256
	}

	var streamRWC io.ReadWriteCloser = stream
	if c.padding {
		streamRWC = newPaddedStream(stream, 0, paddingMax)
	}

	// Send target addr to remote: [2-byte length][target string]
	addrBytes := []byte(target)
	hdr := make([]byte, 2+len(addrBytes))
	binary.BigEndian.PutUint16(hdr[:2], uint16(len(addrBytes)))
	copy(hdr[2:], addrBytes)
	if _, err := streamRWC.Write(hdr); err != nil {
		streamRWC.Close()
		return nil, fmt.Errorf("write target header: %w", err)
	}

	return streamRWC, nil
}

// ServeSocks5 starts a local SOCKS5 listener on the given port and accepts
// connections, bridging them to smux streams. This blocks until the client is
// stopped. Only needed for desktop/CLI use — Android uses DialStream directly.
func (c *Client) ServeSocks5(socksPort int) {
	c.mu.Lock()
	n := len(c.sessions)
	c.mu.Unlock()

	if n == 0 {
		applog.Warn("webrtc client: ServeSocks5 called without smux sessions")
		return
	}

	listenAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		applog.Errorf("webrtc client: SOCKS5 listen on %s failed: %v", listenAddr, err)
		return
	}
	applog.Infof("webrtc client: SOCKS5 listening on %s", listenAddr)

	// Close listener when client stops.
	go func() {
		<-c.done
		listener.Close()
	}()

	paddingMax := c.paddingMax
	if paddingMax == 0 && c.padding {
		paddingMax = 256
	}
	serveSocks5(listener, func() (*smux.Stream, error) {
		return c.openStream()
	}, c.done, c.padding, paddingMax)
}

// closeSessions closes all smux sessions (triggered by ICE failure).
func (c *Client) closeSessions() {
	c.mu.Lock()
	sessions := make([]*smux.Session, len(c.sessions))
	copy(sessions, c.sessions)
	c.sessions = c.sessions[:0]
	c.mu.Unlock()
	for _, sess := range sessions {
		sess.Close()
	}
}

// Stop tears down the client. Safe to call from multiple goroutines.
func (c *Client) Stop() {
	c.stopOnce.Do(func() {
		close(c.done)

		c.mu.Lock()
		sessions := make([]*smux.Session, len(c.sessions))
		copy(sessions, c.sessions)
		c.mu.Unlock()

		for _, sess := range sessions {
			sess.Close()
		}
		c.pc.Close()
		if c.muxCloser != nil {
			c.muxCloser.Close()
		}
		applog.Infof("webrtc client: stopped (%d channels closed)", len(sessions))
	})
}
