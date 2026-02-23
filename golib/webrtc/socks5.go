package webrtc

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/xtaci/smux"

	"natproxy/golib/applog"
)

const relayInactivityTimeout = 10 * time.Minute

// DNSChannelTarget is the magic target address that tells the server
// to enter DNS relay mode instead of dialing a TCP target.
const DNSChannelTarget = "__dns__"

// relayBufSize is the buffer size for bidirectional relay. 128KB is much
// larger than the default 32KB used by io.Copy, improving throughput by
// reducing syscall overhead on high-bandwidth streams.
const relayBufSize = 128 * 1024

// relayBufPool reuses relay buffers across connections. With hundreds of
// concurrent connections, this avoids allocating 2×128KB per connection and
// significantly reduces GC pressure.
var relayBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, relayBufSize)
		return &buf
	},
}

// serveSocks5 accepts TCP connections on listener, performs a SOCKS5 CONNECT
// handshake, then bridges each connection to a new smux stream. Only SOCKS5
// CONNECT is supported (no BIND, no UDP ASSOCIATE).
// If padding is true, each smux stream is wrapped with traffic padding.
func serveSocks5(listener net.Listener, openStream func() (*smux.Stream, error), done <-chan struct{}, padding bool, paddingMax int) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-done:
				return
			default:
			}
			applog.Warnf("socks5: accept error: %v", err)
			return
		}
		go handleSocks5Conn(conn, openStream, padding, paddingMax)
	}
}

// handleSocks5Conn handles one SOCKS5 CONNECT request.
func handleSocks5Conn(conn net.Conn, openStream func() (*smux.Stream, error), padding bool, paddingMax int) {
	defer conn.Close()

	// --- Version/method negotiation ---
	// Client sends: VER(1) NMETHODS(1) METHODS(NMETHODS)
	buf := make([]byte, 258)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	if buf[0] != 0x05 {
		return // not SOCKS5
	}
	nmethods := int(buf[1])
	if _, err := io.ReadFull(conn, buf[:nmethods]); err != nil {
		return
	}
	// Reply: no authentication required
	conn.Write([]byte{0x05, 0x00})

	// --- CONNECT request ---
	// VER(1) CMD(1) RSV(1) ATYP(1) DST.ADDR(variable) DST.PORT(2)
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return
	}
	if buf[1] != 0x01 { // only CONNECT
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // command not supported
		return
	}

	var host string
	switch buf[3] { // ATYP
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
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // address type not supported
		return
	}

	// Read port (2 bytes, big-endian)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(buf[:2])
	target := net.JoinHostPort(host, strconv.Itoa(int(port)))

	// Open smux stream and send target address header.
	stream, err := openStream()
	if err != nil {
		applog.Warnf("socks5: open stream failed: %v", err)
		conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // general failure
		return
	}
	defer stream.Close()

	// Optionally wrap with traffic padding
	var streamRWC io.ReadWriteCloser = stream
	if padding {
		streamRWC = newPaddedStream(stream, 0, paddingMax)
	}

	// Send target addr to remote: [2-byte length][target string]
	addrBytes := []byte(target)
	hdr := make([]byte, 2+len(addrBytes))
	binary.BigEndian.PutUint16(hdr[:2], uint16(len(addrBytes)))
	copy(hdr[2:], addrBytes)
	if _, err := streamRWC.Write(hdr); err != nil {
		applog.Warnf("socks5: write target header failed: %v", err)
		conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	// Reply success
	// VER(1) REP(1) RSV(1) ATYP(1) BND.ADDR(4) BND.PORT(2)
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// Bidirectional relay.
	relay(conn, streamRWC)
}

// relay copies data bidirectionally between two connections using pooled
// buffers. Both sides are wrapped with an inactivity timeout — if no data
// flows for relayInactivityTimeout, the connection is closed automatically.
// When one direction finishes, it signals half-close to the other side so
// the peer knows no more data is coming, preventing hung connections.
func relay(a, b io.ReadWriteCloser) {
	aa := newActivityConn(a, relayInactivityTimeout)
	bb := newActivityConn(b, relayInactivityTimeout)
	defer aa.Close()
	defer bb.Close()

	done := make(chan struct{})

	go func() {
		bufp := relayBufPool.Get().(*[]byte)
		io.CopyBuffer(bb, aa, *bufp)
		relayBufPool.Put(bufp)

		// Half-close: tell b we're done writing so the remote peer
		// sees EOF and can finish its side cleanly.
		if tc, ok := b.(interface{ CloseWrite() error }); ok {
			tc.CloseWrite()
		}
		done <- struct{}{}
	}()

	bufp := relayBufPool.Get().(*[]byte)
	io.CopyBuffer(aa, bb, *bufp)
	relayBufPool.Put(bufp)

	if tc, ok := a.(interface{ CloseWrite() error }); ok {
		tc.CloseWrite()
	}
	<-done
}

// readTargetAddr reads the [2-byte len][addr string] header from a stream.
func readTargetAddr(r io.Reader) (string, error) {
	var lenBuf [2]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return "", fmt.Errorf("read addr length: %w", err)
	}
	addrLen := binary.BigEndian.Uint16(lenBuf[:])
	if addrLen == 0 || addrLen > 512 {
		return "", fmt.Errorf("invalid addr length: %d", addrLen)
	}
	addrBuf := make([]byte, addrLen)
	if _, err := io.ReadFull(r, addrBuf); err != nil {
		return "", fmt.Errorf("read addr: %w", err)
	}
	return string(addrBuf), nil
}
