package webrtc

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"

	"natproxy/golib/applog"
)

const (
	// Primary and fallback DNS resolvers for the server side.
	dnsRelayPrimary  = "8.8.8.8:53"
	dnsRelayFallback = "1.1.1.1:53"

	// Timeouts for UDP DNS resolution on the server side.
	dnsRelayDialTimeout = 2 * time.Second
	dnsRelayReadTimeout = 3 * time.Second
)

// handleDNSStream services a persistent DNS relay channel. It reads
// DNS-over-TCP framed queries from the stream, resolves each via UDP,
// and writes DNS-over-TCP framed responses back. Multiple queries can
// be in-flight concurrently — responses are matched by transaction ID
// on the client side.
func handleDNSStream(stream io.ReadWriteCloser) {
	// Note: caller (handleStream) handles closing the underlying smux stream.
	var writeMu sync.Mutex
	var lenBuf [2]byte

	applog.Info("webrtc server: DNS relay channel opened")
	defer applog.Info("webrtc server: DNS relay channel closed")

	for {
		// Read 2-byte length prefix
		if _, err := io.ReadFull(stream, lenBuf[:]); err != nil {
			return // stream closed or error
		}
		queryLen := binary.BigEndian.Uint16(lenBuf[:])
		if queryLen == 0 || queryLen > 4096 {
			applog.Warnf("webrtc server: DNS relay invalid query length: %d", queryLen)
			return
		}

		query := make([]byte, queryLen)
		if _, err := io.ReadFull(stream, query); err != nil {
			return
		}

		// Resolve concurrently — each query gets its own goroutine
		go func(q []byte) {
			resp, err := resolveDNSUDP(q)
			if err != nil {
				applog.Warnf("webrtc server: DNS relay resolve failed: %v", err)
				return
			}

			// Write DNS-over-TCP response back: [2B len][response]
			frame := make([]byte, 2+len(resp))
			binary.BigEndian.PutUint16(frame[:2], uint16(len(resp)))
			copy(frame[2:], resp)

			writeMu.Lock()
			_, err = stream.Write(frame)
			writeMu.Unlock()
			if err != nil {
				return // stream closed
			}
		}(query)
	}
}

// resolveDNSUDP sends a raw DNS query via UDP and returns the response.
// Uses UDP (not TCP) to avoid 3-way handshake overhead — each query is
// a single UDP round-trip. Falls back to a secondary resolver on failure.
func resolveDNSUDP(query []byte) ([]byte, error) {
	resp, err := dnsUDPQuery(dnsRelayPrimary, query)
	if err != nil {
		// Try fallback resolver
		resp, err = dnsUDPQuery(dnsRelayFallback, query)
		if err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// dnsUDPQuery sends a DNS query to a single resolver via UDP.
func dnsUDPQuery(addr string, query []byte) ([]byte, error) {
	conn, err := net.DialTimeout("udp", addr, dnsRelayDialTimeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(dnsRelayReadTimeout))

	if _, err := conn.Write(query); err != nil {
		return nil, err
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}
