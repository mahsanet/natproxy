package webrtc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/hkdf"

	"natproxy/golib/applog"
)

const (
	nonceSize  = 12 // bytes — used by GCM
	gcmTagSize = 16 // AES-GCM authentication tag

	// obfsBufCap is the pool buffer capacity. Covers pion's default
	// receiveMTU (8192) plus nonce + tag overhead. Packets exceeding
	// this (extremely rare) fall back to per-call allocation.
	obfsBufCap = nonceSize + 8192 + gcmTagSize
)

// obfsBufPool reuses packet buffers across encrypt/decrypt calls.
// At line rate (~50k pps), this eliminates ~75MB/s of GC churn.
var obfsBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, obfsBufCap)
		return &b
	},
}

// ObfuscatedPacketConn wraps a net.PacketConn, encrypting all outgoing
// packets and decrypting all incoming packets using AES-256-GCM with
// HKDF-derived directional keys.
//
// Wire format: [12B nonce][AES-GCM ciphertext + 16B auth tag]
//
// GCM nonce structure: [4B random prefix][8B monotonic counter]
// The counter enables anti-replay.
type ObfuscatedPacketConn struct {
	net.PacketConn
	sendAEAD     cipher.AEAD
	recvAEAD     cipher.AEAD
	nonceCounter atomic.Uint64
	noncePrefix  [4]byte
	recvFilter   *replayFilter // set externally for anti-replay

	// Diagnostic counters
	pktsSent      atomic.Uint64
	pktsRecv      atomic.Uint64
	decryptErrors atomic.Uint64
	replayRejects atomic.Uint64
	tooShort      atomic.Uint64
	loggedErrors  atomic.Uint64 // limits per-packet error logging
	isServer      bool

	done chan struct{} // closed on Close() to stop logStats goroutine
}

// NewObfuscatedPacketConn wraps conn with AES-256-GCM encryption.
// key must be exactly 32 bytes.
func NewObfuscatedPacketConn(conn net.PacketConn, key []byte, isServer bool) (*ObfuscatedPacketConn, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("obfs: key must be 32 bytes, got %d", len(key))
	}

	opc := &ObfuscatedPacketConn{
		PacketConn: conn,
		isServer:   isServer,
		done:       make(chan struct{}),
	}

	clientKey := make([]byte, 32)
	serverKey := make([]byte, 32)

	r := hkdf.New(sha256.New, key, nil, []byte("natproxy-client-to-server"))
	if _, err := io.ReadFull(r, clientKey); err != nil {
		return nil, fmt.Errorf("obfs: derive client key: %w", err)
	}
	r = hkdf.New(sha256.New, key, nil, []byte("natproxy-server-to-client"))
	if _, err := io.ReadFull(r, serverKey); err != nil {
		return nil, fmt.Errorf("obfs: derive server key: %w", err)
	}

	clientAEAD, err := newGCM(clientKey)
	if err != nil {
		return nil, fmt.Errorf("obfs: create client AEAD: %w", err)
	}
	serverAEAD, err := newGCM(serverKey)
	if err != nil {
		return nil, fmt.Errorf("obfs: create server AEAD: %w", err)
	}

	if isServer {
		opc.sendAEAD = serverAEAD
		opc.recvAEAD = clientAEAD
	} else {
		opc.sendAEAD = clientAEAD
		opc.recvAEAD = serverAEAD
	}

	// Random nonce prefix for this connection
	if _, err := rand.Read(opc.noncePrefix[:]); err != nil {
		return nil, fmt.Errorf("obfs: generate nonce prefix: %w", err)
	}

	// TODO: anti-replay temporarily disabled for debugging
	// opc.recvFilter = newReplayFilter()

	// Start diagnostic stats logger
	//go opc.logStats()

	return opc, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Close stops the logStats goroutine and closes the underlying connection.
func (c *ObfuscatedPacketConn) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return c.PacketConn.Close()
}

// WriteTo encrypts p and writes to the underlying connection.
func (c *ObfuscatedPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	return c.writeToGCM(p, addr)
}

// ReadFrom reads and decrypts a packet from the underlying connection.
func (c *ObfuscatedPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	return c.readFromGCM(p)
}

// --- GCM mode ---

func (c *ObfuscatedPacketConn) writeToGCM(p []byte, addr net.Addr) (int, error) {
	// Build nonce: [4B prefix][8B counter]
	var nonce [nonceSize]byte
	copy(nonce[:4], c.noncePrefix[:])
	counter := c.nonceCounter.Add(1)
	binary.BigEndian.PutUint64(nonce[4:], counter)

	// Seal: nonce + ciphertext + tag
	// output = nonce(12) + encrypted(len(p)) + tag(16)
	totalSize := nonceSize + len(p) + gcmTagSize

	// Use pooled buffer when packet fits; fall back to allocation for
	// rare oversized packets.
	var buf []byte
	var pooled *[]byte
	if totalSize <= obfsBufCap {
		pooled = obfsBufPool.Get().(*[]byte)
		buf = (*pooled)[:nonceSize]
	} else {
		buf = make([]byte, nonceSize, totalSize)
	}
	copy(buf, nonce[:])
	buf = c.sendAEAD.Seal(buf, nonce[:], p, nil)

	sent := c.pktsSent.Add(1)
	if sent <= 5 {
		applog.Infof("obfs[%s]: WriteTo #%d → %s (plain=%d, wire=%d, seq=%d)",
			c.role(), sent, addr, len(p), len(buf), counter)
	}

	_, err := c.PacketConn.WriteTo(buf, addr)

	if pooled != nil {
		obfsBufPool.Put(pooled)
	}

	if err != nil {
		applog.Warnf("obfs[%s]: WriteTo %s failed: %v", c.role(), addr, err)
		return 0, err
	}
	return len(p), nil
}

func (c *ObfuscatedPacketConn) readFromGCM(p []byte) (int, net.Addr, error) {
	// Loop until we get a valid decrypted packet. Undecryptable packets
	// (late STUN responses, random probes, corrupted data) are silently
	// discarded. Returning an error here would kill pion's UDPMux read
	// loop and make the entire ICE agent non-functional.

	// Use pooled buffer for the encrypted read. The buffer is obtained
	// once and reused across retry iterations within this call.
	needed := nonceSize + len(p) + gcmTagSize
	var buf []byte
	var pooled *[]byte
	if needed <= obfsBufCap {
		pooled = obfsBufPool.Get().(*[]byte)
		buf = (*pooled)[:needed]
	} else {
		buf = make([]byte, needed)
	}
	defer func() {
		if pooled != nil {
			obfsBufPool.Put(pooled)
		}
	}()

	for {
		n, addr, err := c.PacketConn.ReadFrom(buf)
		if err != nil {
			// Real socket error (closed, timeout) — propagate.
			return 0, addr, err
		}

		if n < nonceSize+gcmTagSize {
			c.tooShort.Add(1)
			if c.loggedErrors.Add(1) <= 10 {
				applog.Warnf("obfs[%s]: ReadFrom %s: packet too short (%d bytes, need %d) — skipping",
					c.role(), addr, n, nonceSize+gcmTagSize)
			}
			continue
		}

		nonce := buf[:nonceSize]
		ciphertext := buf[nonceSize:n]

		plaintext, err := c.recvAEAD.Open(p[:0], nonce, ciphertext, nil)
		if err != nil {
			c.decryptErrors.Add(1)
			if c.loggedErrors.Add(1) <= 10 {
				applog.Warnf("obfs[%s]: ReadFrom %s: GCM decrypt FAILED (wire=%d, ciphertext=%d, first4=%x) — skipping",
					c.role(), addr, n, len(ciphertext), buf[:min(4, n)])
			}
			continue
		}

		// Anti-replay check (if filter is set)
		if c.recvFilter != nil {
			counter := binary.BigEndian.Uint64(nonce[4:12])
			if !c.recvFilter.Check(counter) {
				c.replayRejects.Add(1)
				if c.loggedErrors.Add(1) <= 10 {
					applog.Warnf("obfs[%s]: ReadFrom %s: replay rejected (seq=%d) — skipping",
						c.role(), addr, counter)
				}
				continue
			}
		}

		recv := c.pktsRecv.Add(1)
		if recv <= 5 {
			applog.Infof("obfs[%s]: ReadFrom #%d ← %s (wire=%d, plain=%d)",
				c.role(), recv, addr, n, len(plaintext))
		}

		return len(plaintext), addr, nil
	}
}

// role returns "server" or "client" for log prefixes.
func (c *ObfuscatedPacketConn) role() string {
	if c.isServer {
		return "server"
	}
	return "client"
}

// logStats periodically logs obfuscation layer packet counters.
// Stops when the connection is closed via the done channel.
func (c *ObfuscatedPacketConn) logStats() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range 20 { // log for ~60s then stop
		select {
		case <-c.done:
			return
		case <-ticker.C:
		}
		sent := c.pktsSent.Load()
		recv := c.pktsRecv.Load()
		decErr := c.decryptErrors.Load()
		replay := c.replayRejects.Load()
		short := c.tooShort.Load()
		applog.Infof("obfs[%s] stats: sent=%d recv=%d decryptErr=%d replayReject=%d tooShort=%d",
			c.role(), sent, recv, decErr, replay, short)
		if sent > 0 || recv > 0 {
			// Once we see traffic, reduce frequency
			ticker.Reset(5 * time.Second)
		}
	}
}
