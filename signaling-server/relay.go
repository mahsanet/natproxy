package main

import (
	"encoding/hex"
	"log"
	"net"
	"sync"
	"time"
)

// relayMagic is the 4-byte prefix that identifies a registration packet.
// Data packets (obfuscated ICE/DTLS/SCTP) will not start with this exact
// sequence (probability 1/2^32 per packet).
var relayMagic = [4]byte{0xDE, 0xAD, 0xBE, 0xEF}

const (
	relayRegSize    = 4 + 16 + 1   // magic(4) + sessionHash(16) + role(1)
	relaySessionTTL = 5 * time.Minute
)

type relayPeer struct {
	addr     *net.UDPAddr
	lastSeen time.Time
}

type relaySession struct {
	server *relayPeer
	client *relayPeer
}

type addrInfo struct {
	sessionID string
	isServer  bool
}

// UDPRelay forwards UDP packets between two peers in the same session.
//
// Registration protocol (fire-and-forget, no response):
//
//	[0xDEADBEEF (4 bytes)][SHA256(sessionID)[:16] (16 bytes)][role (1 byte: 0=server, 1=client)]
//
// All other packets are forwarded to the other peer based on source-address
// routing. The relay never decrypts packet contents — it forwards the
// obfuscated bytes as-is.
type UDPRelay struct {
	conn *net.UDPConn

	mu       sync.Mutex
	sessions map[string]*relaySession // hex(sessionHash) → session
	addrMap  map[string]addrInfo      // udpAddr.String() → session info
}

// NewUDPRelay binds a UDP socket and sets up the relay state.
func NewUDPRelay(addr string) (*UDPRelay, error) {
	udpAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		return nil, err
	}
	return &UDPRelay{
		conn:     conn,
		sessions: make(map[string]*relaySession),
		addrMap:  make(map[string]addrInfo),
	}, nil
}

// Run is the relay's main read loop — call it in a goroutine.
func (r *UDPRelay) Run() {
	buf := make([]byte, 65535)
	for {
		n, addr, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("relay: read error: %v", err)
			continue
		}

		// Check for registration packet (exact size + magic prefix).
		if n == relayRegSize &&
			buf[0] == relayMagic[0] && buf[1] == relayMagic[1] &&
			buf[2] == relayMagic[2] && buf[3] == relayMagic[3] {
			r.handleRegistration(buf[:n], addr)
			continue
		}

		// Data packet — forward to the other peer in the session.
		r.handleData(buf[:n], addr)
	}
}

func (r *UDPRelay) handleRegistration(data []byte, addr *net.UDPAddr) {
	sessionID := hex.EncodeToString(data[4:20])
	role := data[20] // 0 = server, 1 = client

	r.mu.Lock()
	defer r.mu.Unlock()

	sess, ok := r.sessions[sessionID]
	if !ok {
		sess = &relaySession{}
		r.sessions[sessionID] = sess
	}

	peer := &relayPeer{addr: addr, lastSeen: time.Now()}
	addrKey := addr.String()

	if role == 0 {
		if sess.server != nil {
			delete(r.addrMap, sess.server.addr.String())
		}
		sess.server = peer
		r.addrMap[addrKey] = addrInfo{sessionID: sessionID, isServer: true}
		log.Printf("relay: server registered session=%s… addr=%s", sessionID[:8], addr)
	} else {
		if sess.client != nil {
			delete(r.addrMap, sess.client.addr.String())
		}
		sess.client = peer
		r.addrMap[addrKey] = addrInfo{sessionID: sessionID, isServer: false}
		log.Printf("relay: client registered session=%s… addr=%s", sessionID[:8], addr)
	}
}

func (r *UDPRelay) handleData(data []byte, from *net.UDPAddr) {
	fromKey := from.String()

	r.mu.Lock()
	info, ok := r.addrMap[fromKey]
	if !ok {
		r.mu.Unlock()
		return
	}

	sess := r.sessions[info.sessionID]
	if sess == nil {
		r.mu.Unlock()
		return
	}

	var target *net.UDPAddr
	if info.isServer && sess.client != nil {
		target = sess.client.addr
		sess.server.lastSeen = time.Now()
	} else if !info.isServer && sess.server != nil {
		target = sess.server.addr
		sess.client.lastSeen = time.Now()
	}
	r.mu.Unlock()

	if target != nil {
		r.conn.WriteToUDP(data, target)
	}
}

// CleanupExpired drops sessions where both peers have gone quiet past the TTL.
func (r *UDPRelay) CleanupExpired() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for id, sess := range r.sessions {
		serverExpired := sess.server == nil || now.Sub(sess.server.lastSeen) > relaySessionTTL
		clientExpired := sess.client == nil || now.Sub(sess.client.lastSeen) > relaySessionTTL

		if serverExpired && clientExpired {
			if sess.server != nil {
				delete(r.addrMap, sess.server.addr.String())
			}
			if sess.client != nil {
				delete(r.addrMap, sess.client.addr.String())
			}
			delete(r.sessions, id)
		}
	}
}
