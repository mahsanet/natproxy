package webrtc

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"natproxy/golib/applog"
)

// deriveSessionID creates a deterministic per-PeerConnection session ID
// for relay registration and logging. Index 0 returns the base unchanged.
func deriveSessionID(base string, index int) string {
	if index == 0 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, index)
}

// distributeChannels splits totalChannels across npc PeerConnections.
// Each PC gets at least total/npc channels; the remainder is distributed
// to the first PCs one by one.
func distributeChannels(total, npc int) []int {
	if npc <= 0 {
		return nil
	}
	dist := make([]int, npc)
	base := total / npc
	extra := total % npc
	for i := range dist {
		dist[i] = base
		if i < extra {
			dist[i]++
		}
	}
	return dist
}

const maxPeerConnections = 8

// ---- ServerGroup ----

// ServerGroup wraps N Server instances, each with its own PeerConnection
// and SCTP cwnd. It provides the same interface as a single Server.
type ServerGroup struct {
	servers []*Server
	mu      sync.Mutex
}

// StartServerGroup spins up npc peer connections, splitting data channels among them.
// Returns one SDP offer per connection.
func StartServerGroup(npc int, iceServers []string, obfsKey []byte, relayAddr, baseSessionID string, opts ServerOptions) (*ServerGroup, []string, error) {
	if npc <= 0 {
		npc = 1
	}
	if npc > maxPeerConnections {
		applog.Warnf("webrtc group: capping npc from %d to %d", npc, maxPeerConnections)
		npc = maxPeerConnections
	}

	totalChannels := opts.NumChannels
	if totalChannels == 0 {
		totalChannels = 6
	}
	channelDist := distributeChannels(totalChannels, npc)

	servers := make([]*Server, 0, npc)
	offers := make([]string, 0, npc)

	for i := 0; i < npc; i++ {
		pcOpts := opts
		pcOpts.NumChannels = channelDist[i]

		sid := deriveSessionID(baseSessionID, i)
		srv, sdp, err := StartServer(iceServers, obfsKey, relayAddr, sid, pcOpts)
		if err != nil {
			// Clean up already-started servers
			for _, s := range servers {
				s.Stop()
			}
			return nil, nil, fmt.Errorf("start server PC %d: %w", i, err)
		}
		servers = append(servers, srv)
		offers = append(offers, sdp)
	}

	g := &ServerGroup{servers: servers}
	applog.Infof("webrtc group: started %d server PeerConnections (channels=%v)", npc, channelDist)
	return g, offers, nil
}

// AcceptAnswers sets the remote SDP answers for all PeerConnections.
func (g *ServerGroup) AcceptAnswers(answers []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(answers) != len(g.servers) {
		return fmt.Errorf("expected %d answers, got %d", len(g.servers), len(answers))
	}
	for i, srv := range g.servers {
		if err := srv.AcceptAnswer(answers[i]); err != nil {
			return fmt.Errorf("accept answer PC %d: %w", i, err)
		}
	}
	return nil
}

// WaitConnected returns once at least one peer connection is up.
// Only errors if every PC failed. Others keep connecting in the background.
func (g *ServerGroup) WaitConnected(timeout time.Duration) error {
	g.mu.Lock()
	servers := make([]*Server, len(g.servers))
	copy(servers, g.servers)
	g.mu.Unlock()

	type result struct {
		idx int
		err error
	}
	ch := make(chan result, len(servers))
	for i, srv := range servers {
		go func(idx int, s *Server) {
			ch <- result{idx, s.WaitConnected(timeout)}
		}(i, srv)
	}

	var lastErr error
	failed := 0
	total := len(servers)
	for failed < total {
		r := <-ch
		if r.err != nil {
			failed++
			applog.Warnf("webrtc group: server PC %d failed: %v (%d/%d failed)", r.idx, r.err, failed, total)
			lastErr = fmt.Errorf("PC %d: %w", r.idx, r.err)
		} else {
			applog.Infof("webrtc group: server PC %d connected", r.idx)
			return nil // at least one PC connected — success
		}
	}
	return lastErr // all PCs failed
}

// Stop tears down all PeerConnections.
func (g *ServerGroup) Stop() {
	g.mu.Lock()
	servers := make([]*Server, len(g.servers))
	copy(servers, g.servers)
	g.mu.Unlock()

	for _, s := range servers {
		s.Stop()
	}
}

// IsAlive returns true if any PeerConnection is alive.
func (g *ServerGroup) IsAlive() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, s := range g.servers {
		if s.IsAlive() {
			return true
		}
	}
	return false
}

// GetClientCount returns the total active smux streams across all PCs.
func (g *ServerGroup) GetClientCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	total := 0
	for _, s := range g.servers {
		total += s.GetClientCount()
	}
	return total
}

// GetChannelCount returns the total active smux sessions across all PCs.
func (g *ServerGroup) GetChannelCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	total := 0
	for _, s := range g.servers {
		total += s.GetChannelCount()
	}
	return total
}

// GetStats returns aggregated byte counters across all PCs.
func (g *ServerGroup) GetStats() (bytesUp, bytesDown int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, s := range g.servers {
		up, down := s.GetStats()
		bytesUp += up
		bytesDown += down
	}
	return
}

// GetStreamDistribution returns per-session stream counts across all PCs.
func (g *ServerGroup) GetStreamDistribution() []int {
	g.mu.Lock()
	defer g.mu.Unlock()
	var dist []int
	for _, s := range g.servers {
		dist = append(dist, s.GetStreamDistribution()...)
	}
	return dist
}

// Count returns the number of PeerConnections in the group.
func (g *ServerGroup) Count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.servers)
}

// GetRawConn returns the raw UDP PacketConn from the first server,
// used for STUN keepalive in manual signaling mode.
func (g *ServerGroup) GetRawConn() net.PacketConn {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.servers) == 0 {
		return nil
	}
	return g.servers[0].rawConn
}

// ---- ClientGroup ----

// ClientGroup wraps N Client instances and provides a unified DialStream
// that picks the least-loaded client.
type ClientGroup struct {
	clients    []*Client
	nextClient uint32
	mu         sync.Mutex
	done       chan struct{}
	stopOnce   sync.Once

	// OnReconnectNeeded is called when ICE fails on any client.
	// Set by api.go to trigger fast reconnect.
	OnReconnectNeeded func()
}

// StartClientGroup connects npc peer connections using the provided SDP offers.
// Returns one SDP answer per connection.
func StartClientGroup(npc int, sdpOffers []string, iceServers []string, protectFn func(int) bool, obfsKey []byte, relayAddr, baseSessionID string, opts ClientOptions) (*ClientGroup, []string, error) {
	if npc <= 0 {
		npc = 1
	}
	if npc > maxPeerConnections {
		applog.Warnf("webrtc group: capping npc from %d to %d", npc, maxPeerConnections)
		npc = maxPeerConnections
		if len(sdpOffers) > npc {
			sdpOffers = sdpOffers[:npc]
		}
	}
	if len(sdpOffers) != npc {
		return nil, nil, fmt.Errorf("expected %d SDP offers, got %d", npc, len(sdpOffers))
	}

	totalChannels := opts.NumChannels
	if totalChannels == 0 {
		totalChannels = 6
	}
	channelDist := distributeChannels(totalChannels, npc)

	clients := make([]*Client, 0, npc)
	answers := make([]string, 0, npc)

	for i := 0; i < npc; i++ {
		pcOpts := opts
		pcOpts.NumChannels = channelDist[i]

		sid := deriveSessionID(baseSessionID, i)
		cli, sdpAnswer, err := StartClient(sdpOffers[i], iceServers, protectFn, obfsKey, relayAddr, sid, pcOpts)
		if err != nil {
			for _, c := range clients {
				c.Stop()
			}
			return nil, nil, fmt.Errorf("start client PC %d: %w", i, err)
		}
		clients = append(clients, cli)
		answers = append(answers, sdpAnswer)
	}

	g := &ClientGroup{clients: clients, done: make(chan struct{})}

	// Wire up per-client reconnect callbacks so any ICE failure
	// triggers the group-level OnReconnectNeeded at most once.
	reconnectOnce := &sync.Once{}
	for _, cli := range clients {
		cli.onReconnectNeeded = func() {
			reconnectOnce.Do(func() {
				if g.OnReconnectNeeded != nil {
					g.OnReconnectNeeded()
				}
			})
		}
	}

	applog.Infof("webrtc group: started %d client PeerConnections (channels=%v)", npc, channelDist)
	return g, answers, nil
}

// WaitConnected returns once at least one peer connection is up.
// Only errors if every PC failed. Others keep connecting in the background.
func (g *ClientGroup) WaitConnected(timeout time.Duration) error {
	g.mu.Lock()
	clients := make([]*Client, len(g.clients))
	copy(clients, g.clients)
	g.mu.Unlock()

	type result struct {
		idx int
		err error
	}
	ch := make(chan result, len(clients))
	for i, cli := range clients {
		go func(idx int, c *Client) {
			ch <- result{idx, c.WaitConnected(timeout)}
		}(i, cli)
	}

	var lastErr error
	failed := 0
	total := len(clients)
	for failed < total {
		r := <-ch
		if r.err != nil {
			failed++
			applog.Warnf("webrtc group: client PC %d failed: %v (%d/%d failed)", r.idx, r.err, failed, total)
			lastErr = fmt.Errorf("PC %d: %w", r.idx, r.err)
		} else {
			applog.Infof("webrtc group: client PC %d connected", r.idx)
			return nil
		}
	}
	return lastErr
}

// WaitConnectedCtx is like WaitConnected but also bails early if the context is done.
func (g *ClientGroup) WaitConnectedCtx(ctx context.Context, timeout time.Duration) error {
	g.mu.Lock()
	clients := make([]*Client, len(g.clients))
	copy(clients, g.clients)
	g.mu.Unlock()

	type result struct {
		idx int
		err error
	}
	ch := make(chan result, len(clients))
	for i, cli := range clients {
		go func(idx int, c *Client) {
			ch <- result{idx, c.WaitConnectedCtx(ctx, timeout)}
		}(i, cli)
	}

	var lastErr error
	failed := 0
	total := len(clients)
	for failed < total {
		r := <-ch
		if r.err != nil {
			failed++
			applog.Warnf("webrtc group: client PC %d failed: %v (%d/%d failed)", r.idx, r.err, failed, total)
			lastErr = fmt.Errorf("PC %d: %w", r.idx, r.err)
		} else {
			applog.Infof("webrtc group: client PC %d connected (ctx)", r.idx)
			return nil
		}
	}
	return lastErr
}

// DialStream opens a stream on the least-loaded client PeerConnection.
func (g *ClientGroup) DialStream(target string) (io.ReadWriteCloser, error) {
	g.mu.Lock()
	clients := g.clients
	g.mu.Unlock()

	if len(clients) == 0 {
		return nil, fmt.Errorf("no client PeerConnections available")
	}

	if len(clients) == 1 {
		return clients[0].DialStream(target)
	}

	// Pick the client with the fewest active streams
	var best *Client
	bestCount := int(^uint(0) >> 1)
	for _, c := range clients {
		n := c.GetStreamCount()
		if n < bestCount {
			bestCount = n
			best = c
		}
	}

	if best == nil {
		best = clients[0]
	}

	return best.DialStream(target)
}

// Stop tears down all client PeerConnections. Safe to call from multiple goroutines.
func (g *ClientGroup) Stop() {
	g.stopOnce.Do(func() {
		close(g.done)
	})

	g.mu.Lock()
	clients := make([]*Client, len(g.clients))
	copy(clients, g.clients)
	g.mu.Unlock()

	for _, c := range clients {
		c.Stop()
	}
}

// IsAlive returns true if any client PeerConnection is alive.
func (g *ClientGroup) IsAlive() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.clients {
		if c.IsAlive() {
			return true
		}
	}
	return false
}

// ProtectSockets calls protectFn on the raw UDP socket fd of each client
// PeerConnection. Used in the two-phase connection flow: WebRTC connects
// first without TUN, then sockets are protected before TUN is created.
func (g *ClientGroup) ProtectSockets(protectFn func(int) bool) error {
	g.mu.Lock()
	clients := make([]*Client, len(g.clients))
	copy(clients, g.clients)
	g.mu.Unlock()

	for i, c := range clients {
		if c.socketFD <= 0 {
			continue
		}
		if !protectFn(c.socketFD) {
			return fmt.Errorf("protect socket fd %d (client %d) failed", c.socketFD, i)
		}
		applog.Infof("webrtc group: protected socket fd %d (client %d)", c.socketFD, i)
	}
	return nil
}

// Count returns the number of PeerConnections in the group.
func (g *ClientGroup) Count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.clients)
}

// GetChannelCount returns the total active smux sessions across all PCs.
func (g *ClientGroup) GetChannelCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	total := 0
	for _, c := range g.clients {
		total += c.GetChannelCount()
	}
	return total
}

// GetStreamCount returns the total active smux streams across all PCs.
func (g *ClientGroup) GetStreamCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	total := 0
	for _, c := range g.clients {
		total += c.GetStreamCount()
	}
	return total
}

// ServeSocks5 starts a local SOCKS5 listener that load-balances across
// all client PeerConnections. Blocks until the group is stopped.
func (g *ClientGroup) ServeSocks5(socksPort int) {
	listenAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		applog.Errorf("webrtc client group: SOCKS5 listen on %s failed: %v", listenAddr, err)
		return
	}
	applog.Infof("webrtc client group: SOCKS5 listening on %s", listenAddr)

	go func() {
		<-g.done
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-g.done:
				return
			default:
			}
			applog.Warnf("socks5 group: accept error: %v", err)
			return
		}
		go g.handleGroupSocks5Conn(conn)
	}
}

// handleGroupSocks5Conn handles one SOCKS5 CONNECT request using DialStream.
func (g *ClientGroup) handleGroupSocks5Conn(conn net.Conn) {
	defer conn.Close()

	// --- Version/method negotiation ---
	buf := make([]byte, 258)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	if buf[0] != 0x05 {
		return
	}
	nmethods := int(buf[1])
	if _, err := io.ReadFull(conn, buf[:nmethods]); err != nil {
		return
	}
	conn.Write([]byte{0x05, 0x00})

	// --- CONNECT request ---
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return
	}
	if buf[1] != 0x01 {
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var host string
	switch buf[3] {
	case 0x01: // IPv4
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return
		}
		host = net.IP(buf[:4]).String()
	case 0x03: // Domain
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return
		}
		domainLen := int(buf[0])
		if _, err := io.ReadFull(conn, buf[:domainLen]); err != nil {
			return
		}
		host = string(buf[:domainLen])
	case 0x04: // IPv6
		if _, err := io.ReadFull(conn, buf[:16]); err != nil {
			return
		}
		host = net.IP(buf[:16]).String()
	default:
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(buf[:2])
	target := net.JoinHostPort(host, strconv.Itoa(int(port)))

	// DialStream handles openStream + padding + target header
	stream, err := g.DialStream(target)
	if err != nil {
		applog.Warnf("socks5 group: dial stream failed: %v", err)
		conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer stream.Close()

	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	relay(conn, stream)
}
