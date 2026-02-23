// Package webrtc provides WebRTC-based NAT traversal for the hole-punch path.
// It replaces the manual STUN + UDP punch + mKCP + xray-core pipeline with
// pion/webrtc (ICE/DTLS/SCTP) + smux multiplexing.
package webrtc

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"math/rand"

	ice "github.com/pion/ice/v4"
	"github.com/pion/stun/v2"
	pionwebrtc "github.com/pion/webrtc/v4"

	"natproxy/golib/applog"
)

// jitter adds ±pct random jitter to a duration to prevent timing fingerprinting.
func jitter(base time.Duration, pct float64) time.Duration {
	j := float64(base) * pct * (2*rand.Float64() - 1)
	return base + time.Duration(j)
}

// defaultICEServers used when none are provided.
var defaultICEServers = []string{
	"stun:stun.l.google.com:19302",
	"stun:stun1.l.google.com:19302",
}

// peerConnResult holds the results from newPeerConnection. The caller uses
// publicIP/publicPort to inject a srflx candidate into the SDP, since pion's
// built-in STUN gathering creates separate sockets that bypass UDPMux (and
// thus the obfuscation layer).
type peerConnResult struct {
	pc         *pionwebrtc.PeerConnection
	closer     io.Closer          // UDP socket (for cleanup)
	rawConn    net.PacketConn     // underlying raw UDP socket (for STUN keepalive)
	publicIP   string             // STUN-discovered public IP (empty if STUN failed)
	publicPort int                // STUN-discovered public port
	localPort  int                // local UDP socket port (for srflx rport)
	relayAddr  string             // relay address registered with (for candidate injection)
	socketFD   int                // raw UDP socket fd (for deferred protect)
}

// newPeerConnection creates a WebRTC PeerConnection configured with a single
// UDPMux socket, optional UDP obfuscation, and DTLS fingerprint randomization.
//
// If protectFn is non-nil, the UDP socket is protected via the provided
// callback (Android VpnService.protect) to avoid TUN routing loops.
//
// If obfsKey is non-nil (16 bytes), the UDP socket is wrapped with AES-128-CTR
// obfuscation so every byte on the wire appears as random data.
//
// ICE servers are NOT passed to pion's PeerConnection — pion's built-in STUN
// gathering creates separate sockets that bypass UDPMux (and the obfuscation
// wrapper). Instead, STUN is done manually on the raw socket before applying
// obfuscation, and the result is returned so the caller can inject a srflx
// candidate directly into the SDP.
//
// If relayAddr is non-empty, a registration packet is sent to the UDP relay
// (before obfuscation wrapping) so the relay can forward packets when direct
// connectivity fails (port-restricted / symmetric NAT).
//
// DTLS ClientHello/ServerHello randomization hooks are always applied as
// defense-in-depth.
func newPeerConnection(iceServers []string, protectFn func(int) bool, obfsKey []byte, relayAddr string, sessionID string, isServer bool, peerCfg PeerConfig) (*peerConnResult, error) {
	applyPeerDefaults(&peerCfg)

	if len(iceServers) == 0 {
		iceServers = defaultICEServers
	}

	se := pionwebrtc.SettingEngine{}
	se.DetachDataChannels() // required for dc.Detach() to work

	// --- Phase 1: SettingEngine hardening ---
	se.SetDTLSInsecureSkipHelloVerify(*peerCfg.DTLSSkipVerify)
	se.EnableSCTPZeroChecksum(*peerCfg.SCTPZeroChecksum)
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)         // No mDNS leak
	se.SetICEMaxBindingRequests(16)                                 // Better connectivity in restrictive NATs
	se.SetSCTPMaxReceiveBufferSize(uint32(peerCfg.SCTPRecvBuffer) * 1024)

	// --- Phase 2: Jittered ICE timeouts to prevent timing fingerprinting ---
	se.SetICETimeouts(
		jitter(time.Duration(peerCfg.ICEDisconnTimeout)*time.Millisecond, 0.15),
		jitter(time.Duration(peerCfg.ICEFailedTimeout)*time.Millisecond, 0.15),
		jitter(time.Duration(peerCfg.ICEKeepalive)*time.Millisecond, 0.20),
	)
	se.SetDTLSRetransmissionInterval(jitter(time.Duration(peerCfg.DTLSRetransmit)*time.Millisecond, 0.10))

	// Prevent automatic PeerConnection.Close() when DTLS transport closes.
	se.DisableCloseByDTLS(*peerCfg.DisableCloseByDTLS)

	// Cap SCTP retransmission timeout with jitter (default 60s).
	se.SetSCTPRTOMax(jitter(time.Duration(peerCfg.SCTPRTOMax)*time.Millisecond, 0.15))

	// DTLS fingerprint randomization (defense-in-depth, always active).
	// Phase 3 upgrades this to seeded truncation + shuffle.
	if len(obfsKey) > 0 {
		SetDTLSRandomSeed(obfsKey, isServer)
	}
	se.SetDTLSClientHelloMessageHook(randomizeDTLSClientHello)
	se.SetDTLSServerHelloMessageHook(randomizeDTLSServerHello)

	// Pre-resolve STUN hostnames. On the client path (protectFn != nil),
	// use a protected DNS resolver since the TUN intercepts system DNS.
	resolved := resolveICEServers(iceServers, protectFn)
	applog.Infof("webrtc: resolved ICE servers: %v", resolved)

	// Create a UDP socket. On the client path, protect it to bypass TUN.
	var udpConn net.PacketConn
	var err error

	if protectFn != nil {
		protectControl := func(network, address string, c syscall.RawConn) error {
			var protectErr error
			c.Control(func(fd uintptr) {
				if !protectFn(int(fd)) {
					protectErr = fmt.Errorf("protect fd %d failed", fd)
				}
			})
			return protectErr
		}
		lc := net.ListenConfig{Control: protectControl}
		udpConn, err = lc.ListenPacket(context.Background(), "udp4", ":0")
	} else {
		udpConn, err = net.ListenPacket("udp4", ":0")
	}
	if err != nil {
		return nil, fmt.Errorf("create UDP socket: %w", err)
	}

	// Extract the raw fd for deferred protect (two-phase connection).
	var socketFD int
	if uc, ok := udpConn.(*net.UDPConn); ok {
		raw, err := uc.SyscallConn()
		if err == nil {
			raw.Control(func(fd uintptr) {
				socketFD = int(fd)
			})
		}
	}

	// Increase UDP socket buffers to reduce packet loss during heavy traffic.
	// Default is ~208KB which overflows easily during speedtests, causing SCTP
	// T3-rtx to fire and RTO to escalate exponentially. With 8MB buffers,
	// packets queue in the kernel instead of being dropped. The actual size
	// may be capped by net.core.rmem_max / net.core.wmem_max on Linux.
	if uc, ok := udpConn.(*net.UDPConn); ok {
		uc.SetReadBuffer(peerCfg.UDPReadBuffer * 1024)
		uc.SetWriteBuffer(peerCfg.UDPWriteBuffer * 1024)
	}

	localAddr := udpConn.LocalAddr().(*net.UDPAddr)
	localPort := localAddr.Port
	applog.Infof("webrtc: ICE UDP socket created on %s (protected=%v)", udpConn.LocalAddr(), protectFn != nil)

	// Manual STUN discovery on the RAW socket (before obfuscation wrapping).
	// STUN servers don't speak our obfuscation protocol — STUN must happen
	// on the unwrapped connection. Try all servers with fallbacks.
	stunAddrs := extractAllSTUNAddrs(resolved)
	// Resolve fallback STUN servers concurrently using the protected resolver.
	// On the client path, system DNS goes through the TUN (which isn't connected
	// yet), so raw net.ResolveUDPAddr would block/fail for hostnames.
	fallbackResolved := resolveFallbackSTUN(protectFn)
	for _, fb := range fallbackResolved {
		found := false
		for _, existing := range stunAddrs {
			if existing == fb {
				found = true
				break
			}
		}
		if !found {
			stunAddrs = append(stunAddrs, fb)
		}
	}
	var publicIP string
	var publicPort int
	if ip, err := discoverReflexiveAddrMulti(udpConn, stunAddrs); err != nil {
		applog.Warnf("webrtc: STUN discovery failed on all servers: %v", err)
	} else {
		publicIP = ip.IP
		publicPort = ip.Port
		applog.Infof("webrtc: STUN discovered %s:%d (will inject as srflx)", publicIP, publicPort)
	}

	// Register with UDP relay (before obfuscation wrapping). This sends
	// an unencrypted registration packet so the relay learns our
	// NAT-mapped address for session-based forwarding. Must happen on the
	// raw socket — the relay doesn't speak the obfuscation protocol.
	if relayAddr != "" && sessionID != "" {
		registerRelay(udpConn, relayAddr, sessionID, isServer)
	}

	// Wrap the raw socket with a diagnostic logger to see all packets
	// arriving BEFORE obfuscation decryption. This helps diagnose whether
	// packets reach us at all vs. fail at the decryption stage.
	loggedConn := &diagPacketConn{PacketConn: udpConn, label: roleLabel(isServer)}
	var baseConn net.PacketConn = loggedConn

	// Apply obfuscation wrapper if a key is provided. After this point,
	// all ICE connectivity checks, DTLS handshake, and SCTP data are
	// encrypted with AES-128-CTR using per-packet random nonces.
	var muxConn net.PacketConn = baseConn
	if len(obfsKey) > 0 {
		obfsConn, err := NewObfuscatedPacketConn(baseConn, obfsKey, isServer)
		if err != nil {
			udpConn.Close()
			return nil, fmt.Errorf("create obfuscated conn: %w", err)
		}
		muxConn = obfsConn
		if len(obfsKey) == 32 {
			applog.Info("webrtc: UDP obfuscation enabled (AES-256-GCM)")
		} else {
			applog.Info("webrtc: UDP obfuscation enabled (AES-128-CTR legacy)")
		}
	}

	// Create UDPMux on the (possibly obfuscated) socket.
	mux := pionwebrtc.NewICEUDPMux(nil, muxConn)
	se.SetICEUDPMux(mux)

	// Set STUN gather timeout with jitter — we do NOT pass ICE servers to the
	// PeerConnection (pion's srflx gathering creates separate sockets that
	// bypass UDPMux and the obfuscation layer). Srflx candidates are injected
	// manually into the SDP by the caller.
	se.SetSTUNGatherTimeout(jitter(1*time.Second, 0.15))

	// Client path: filter out TUN interface and VPN subnet.
	if protectFn != nil {
		se.SetInterfaceFilter(func(iface string) bool {
			return !strings.HasPrefix(iface, "tun") && !strings.HasPrefix(iface, "vpn")
		})
		se.SetIPFilter(func(ip net.IP) bool {
			tunNet := &net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(24, 32)}
			return !tunNet.Contains(ip)
		})
	}

	api := pionwebrtc.NewAPI(pionwebrtc.WithSettingEngine(se))

	// NO ICE servers passed — prevents pion from creating separate STUN
	// sockets that would bypass the UDPMux/obfuscation layer.
	pc, err := api.NewPeerConnection(pionwebrtc.Configuration{})
	if err != nil {
		udpConn.Close()
		return nil, fmt.Errorf("create peer connection: %w", err)
	}

	return &peerConnResult{
		pc:         pc,
		closer:     udpConn,
		rawConn:    udpConn,
		publicIP:   publicIP,
		publicPort: publicPort,
		localPort:  localPort,
		relayAddr:  relayAddr,
		socketFD:   socketFD,
	}, nil
}

// injectPortMappedCandidate appends a port-mapped (UPnP/NAT-PMP/PCP) address
// as a host-type ICE candidate with the highest priority. This is used when
// port mapping succeeds to inject the mapped address into the WebRTC SDP
// instead of using a separate xray-core path.
func injectPortMappedCandidate(sdp, externalIP string, externalPort, localPort int) string {
	if externalIP == "" || externalPort == 0 {
		return sdp
	}

	// Priority: type-preference=126 (host), local-pref=65535, component=1
	// = (2^24 * 126) + (2^8 * 65535) + 255 = 2130706175
	candidateLine := fmt.Sprintf(
		"a=candidate:portmap 1 udp 2130706175 %s %d typ host raddr 0.0.0.0 rport %d\r\n",
		externalIP, externalPort, localPort,
	)

	if idx := strings.Index(sdp, "a=end-of-candidates"); idx != -1 {
		return sdp[:idx] + candidateLine + sdp[idx:]
	}
	return sdp + candidateLine
}

// injectSrflxCandidate appends a server-reflexive ICE candidate line to the
// SDP. This is needed because pion's STUN gathering creates separate sockets
// that bypass UDPMux — so we do manual STUN on the UDPMux socket and inject
// the result directly.
func injectSrflxCandidate(sdp string, publicIP string, publicPort, localPort int) string {
	if publicIP == "" || publicPort == 0 {
		return sdp
	}

	// ICE candidate priority: type-preference=100 (srflx), local-pref=65535, component=1
	// Priority = (2^24 * type) + (2^8 * local) + (256 - component)
	// = (2^24 * 100) + (2^8 * 65535) + 255 = 1694498815
	candidateLine := fmt.Sprintf(
		"a=candidate:obfs1 1 udp 1694498815 %s %d typ srflx raddr 0.0.0.0 rport %d\r\n",
		publicIP, publicPort, localPort,
	)

	// Insert before the first end-of-candidates or at end of media section
	if idx := strings.Index(sdp, "a=end-of-candidates"); idx != -1 {
		return sdp[:idx] + candidateLine + sdp[idx:]
	}
	// Fallback: append before the last line
	return sdp + candidateLine
}

// stunResult holds the result of a STUN discovery.
type stunResult struct {
	IP   string
	Port int
}

// resolveICEServers pre-resolves STUN server hostnames to IP addresses.
func resolveICEServers(iceServers []string, protectFn func(int) bool) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var resolver *net.Resolver
	if protectFn != nil {
		resolver = newProtectedResolver(protectFn)
	} else {
		resolver = net.DefaultResolver
	}

	resolved := make([]string, len(iceServers))
	for i, s := range iceServers {
		addr, found := strings.CutPrefix(s, "stun:")
		if !found {
			resolved[i] = s
			continue
		}

		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			resolved[i] = s
			continue
		}

		if net.ParseIP(host) != nil {
			resolved[i] = s
			continue
		}

		ips, err := resolver.LookupHost(ctx, host)
		if err != nil || len(ips) == 0 {
			applog.Warnf("webrtc: DNS resolve %s failed: %v (keeping hostname)", host, err)
			resolved[i] = s
			continue
		}

		chosenIP := ips[0]
		for _, ip := range ips {
			if net.ParseIP(ip).To4() != nil {
				chosenIP = ip
				break
			}
		}

		resolved[i] = "stun:" + net.JoinHostPort(chosenIP, port)
		applog.Infof("webrtc: resolved %s → %s", host, chosenIP)
	}

	return resolved
}

// newProtectedResolver creates a DNS resolver that uses protected TCP
// connections to public DNS servers, bypassing the TUN interface.
func newProtectedResolver(protectFn func(int) bool) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Control: func(network, address string, c syscall.RawConn) error {
					var protectErr error
					c.Control(func(fd uintptr) {
						if !protectFn(int(fd)) {
							protectErr = fmt.Errorf("protect DNS socket failed")
						}
					})
					return protectErr
				},
			}
			for _, dns := range []string{"8.8.8.8:53", "1.1.1.1:53"} {
				conn, err := d.DialContext(ctx, "tcp", dns)
				if err == nil {
					return conn, nil
				}
				applog.Warnf("webrtc: protected DNS to %s failed: %v", dns, err)
			}
			return nil, fmt.Errorf("all protected DNS servers unreachable")
		},
	}
}

// extractAllSTUNAddrs returns host:port for every STUN server, deduplicating.
func extractAllSTUNAddrs(iceServers []string) []string {
	seen := map[string]bool{}
	var addrs []string
	for _, s := range iceServers {
		if addr, found := strings.CutPrefix(s, "stun:"); found {
			if !seen[addr] {
				seen[addr] = true
				addrs = append(addrs, addr)
			}
		}
	}
	return addrs
}

// fallbackSTUNServers are non-Google STUN servers used when the configured
// servers (which may all resolve to the same blocked IP) fail.
var fallbackSTUNServers = []string{
	"stun.cloudflare.com:3478",
	"stun.nextcloud.com:443",
	"stun.sipnet.ru:3478",
}

// resolveFallbackSTUN resolves STUN hostnames in parallel and returns
// whatever succeeded within a 2-second window.
//
// On Android the TUN isn't up yet at this point, so normal DNS would stall ~12s
// per hostname. The protected resolver bypasses TUN with TCP DNS to 8.8.8.8/1.1.1.1.
func resolveFallbackSTUN(protectFn func(int) bool) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var resolver *net.Resolver
	if protectFn != nil {
		resolver = newProtectedResolver(protectFn)
	} else {
		resolver = net.DefaultResolver
	}

	ch := make(chan string, len(fallbackSTUNServers))
	for _, s := range fallbackSTUNServers {
		go func(server string) {
			host, port, err := net.SplitHostPort(server)
			if err != nil {
				return
			}
			// Already an IP — no resolution needed.
			if net.ParseIP(host) != nil {
				ch <- server
				return
			}
			ips, err := resolver.LookupHost(ctx, host)
			if err != nil || len(ips) == 0 {
				applog.Warnf("webrtc: resolve fallback STUN %s: %v", host, err)
				return
			}
			// Prefer IPv4.
			chosen := ips[0]
			for _, ip := range ips {
				if net.ParseIP(ip).To4() != nil {
					chosen = ip
					break
				}
			}
			ch <- net.JoinHostPort(chosen, port)
		}(s)
	}

	// Collect results within timeout. Don't wait for slow DNS.
	var resolved []string
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	received := 0
	for received < len(fallbackSTUNServers) {
		select {
		case addr := <-ch:
			resolved = append(resolved, addr)
			received++
		case <-timer.C:
			return resolved
		}
	}
	return resolved
}

// discoverReflexiveAddrMulti sends STUN binding requests to all servers
// concurrently and returns the first successful response. DNS resolution
// and sending happen in goroutines so one slow server can't block others.
// Reading starts immediately — pre-resolved IP servers respond within
// milliseconds, well before the 3-second deadline.
func discoverReflexiveAddrMulti(conn net.PacketConn, stunServers []string) (*stunResult, error) {
	if len(stunServers) == 0 {
		return nil, fmt.Errorf("no STUN servers configured")
	}

	type txnInfo struct {
		msg    *stun.Message
		server string
	}
	var mu sync.Mutex
	var txns []txnInfo

	// Send STUN requests concurrently. Each goroutine resolves DNS (if
	// needed) and sends a binding request. Pre-resolved IP addresses
	// resolve instantly; hostnames resolve in parallel with each other.
	for _, server := range stunServers {
		go func(s string) {
			remoteAddr, err := net.ResolveUDPAddr("udp4", s)
			if err != nil {
				applog.Warnf("webrtc: resolve STUN %s: %v", s, err)
				return
			}
			msg := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
			if _, err := conn.WriteTo(msg.Raw, remoteAddr); err != nil {
				applog.Warnf("webrtc: send STUN to %s: %v", s, err)
				return
			}
			mu.Lock()
			txns = append(txns, txnInfo{msg: msg, server: s})
			mu.Unlock()
		}(server)
	}

	// Brief pause to let goroutines for already-resolved IPs send their
	// requests. This is just a hint — the read loop handles the timing.
	time.Sleep(50 * time.Millisecond)

	// Read responses. The first pre-resolved server typically responds
	// within 20-100ms, well before the 3-second deadline.
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return nil, fmt.Errorf("set deadline: %w", err)
	}
	defer conn.SetReadDeadline(time.Time{})

	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			mu.Lock()
			count := len(txns)
			mu.Unlock()
			return nil, fmt.Errorf("STUN recv (sent to %d servers): %w", count, err)
		}

		resp := new(stun.Message)
		resp.Raw = buf[:n]
		if err := resp.Decode(); err != nil {
			continue
		}

		// Match transaction ID to one of our requests.
		mu.Lock()
		var matched string
		for _, txn := range txns {
			if resp.TransactionID == txn.msg.TransactionID {
				matched = txn.server
				break
			}
		}
		count := len(txns)
		mu.Unlock()

		if matched == "" {
			continue // stale or unrelated packet
		}

		var xorAddr stun.XORMappedAddress
		if err := xorAddr.GetFrom(resp); err != nil {
			applog.Warnf("webrtc: STUN response from %s missing address: %v", matched, err)
			continue
		}

		applog.Infof("webrtc: STUN result from %s: %s:%d (first of %d probes)", matched, xorAddr.IP.String(), xorAddr.Port, count)
		return &stunResult{IP: xorAddr.IP.String(), Port: xorAddr.Port}, nil
	}
}

// registerRelay sends a registration packet to the UDP relay so the relay
// learns our NAT-mapped address. Must be called on the raw socket BEFORE
// obfuscation wrapping. The packet is fire-and-forget (no response expected).
//
// Sends multiple times to account for packet loss and keep the NAT pinhole
// open until ICE connectivity checks begin.
func registerRelay(conn net.PacketConn, relayAddr string, sessionID string, isServer bool) {
	addr, err := net.ResolveUDPAddr("udp4", relayAddr)
	if err != nil {
		applog.Warnf("webrtc: resolve relay %s: %v", relayAddr, err)
		return
	}

	// Build: [magic(4)][SHA256(sessionID)[:16]](16)][role(1)]
	var pkt [21]byte
	pkt[0], pkt[1], pkt[2], pkt[3] = 0xDE, 0xAD, 0xBE, 0xEF
	h := sha256.Sum256([]byte(sessionID))
	copy(pkt[4:20], h[:16])
	if !isServer {
		pkt[20] = 0x01
	}

	for i := 0; i < 3; i++ {
		if _, err := conn.WriteTo(pkt[:], addr); err != nil {
			applog.Warnf("webrtc: relay registration send %d: %v", i, err)
		}
		if i < 2 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	applog.Infof("webrtc: registered with relay %s (server=%v)", relayAddr, isServer)
}

// injectRelayCandidate appends a relay ICE candidate to the SDP. The relay
// candidate has the lowest possible priority so pion prefers direct
// connectivity (host/srflx) and only falls back to the relay when direct
// hole-punching fails.
func injectRelayCandidate(sdp string, relayAddr string, localPort int) string {
	if relayAddr == "" {
		return sdp
	}

	host, portStr, err := net.SplitHostPort(relayAddr)
	if err != nil {
		applog.Warnf("webrtc: bad relay addr %s: %v", relayAddr, err)
		return sdp
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return sdp
	}

	// Relay priority: type-preference=0, local-pref=65535, component=1
	// Priority = (2^24 * 0) + (2^8 * 65535) + 255 = 16777215
	candidateLine := fmt.Sprintf(
		"a=candidate:relay1 1 udp 16777215 %s %d typ relay raddr 0.0.0.0 rport %d\r\n",
		host, port, localPort,
	)

	if idx := strings.Index(sdp, "a=end-of-candidates"); idx != -1 {
		return sdp[:idx] + candidateLine + sdp[idx:]
	}
	return sdp + candidateLine
}

// roleLabel returns "server" or "client" for log prefixes.
func roleLabel(isServer bool) string {
	if isServer {
		return "server"
	}
	return "client"
}

// diagPacketConn wraps a net.PacketConn with diagnostic logging to observe
// raw UDP traffic before any obfuscation processing.
type diagPacketConn struct {
	net.PacketConn
	label   string
	rawRecv atomic.Uint64
	rawSent atomic.Uint64
}

func (d *diagPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := d.PacketConn.ReadFrom(p)
	if err != nil {
		return n, addr, err
	}
	count := d.rawRecv.Add(1)
	if count <= 10 {
		// Log first bytes to identify packet type:
		// STUN starts with 0x00/0x01, DTLS with 0x14-0x19, RTP with 0x80-0x9f
		first4 := make([]byte, min(4, n))
		copy(first4, p[:len(first4)])
		applog.Infof("raw[%s]: ReadFrom #%d ← %s (%d bytes, first4=%x)",
			d.label, count, addr, n, first4)
	}
	return n, addr, err
}

func (d *diagPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	count := d.rawSent.Add(1)
	if count <= 10 {
		applog.Infof("raw[%s]: WriteTo #%d → %s (%d bytes)",
			d.label, count, addr, len(p))
	}
	return d.PacketConn.WriteTo(p, addr)
}
