// Package tunnel bridges an Android TUN file descriptor to a SOCKS5 proxy
// using sing-tun's system stack. TCP connections from the TUN are presented
// as kernel-backed net.Conn objects (no userspace TCP) and forwarded through
// the SOCKS5 proxy. UDP port 53 (DNS) is relayed to a public resolver.
package tunnel

import (
	"container/list"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/net/proxy"

	"natproxy/golib/applog"

	singtun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const (
	dnsPort     = 53
	udpBufSize  = 4096
	copyBufSize = 128 * 1024 // 128KB relay buffers

	// DNS-over-TCP through SOCKS5 tunnel timeout. If DNS through the tunnel
	// takes longer than this, fall back to direct UDP DNS.
	dnsTunnelTimeout = 3 * time.Second

	// dnsCacheMaxTTL caps the TTL parsed from DNS responses to avoid stale
	// entries from authoritative servers that set very long TTLs.
	dnsCacheMaxTTL = 300 * time.Second

	// dnsCacheNXDomainTTL is the cache duration for NXDOMAIN responses.
	// Android appends search domain suffixes (.lan, .local) causing frequent
	// NXDOMAIN queries — caching these eliminates redundant round-trips.
	dnsCacheNXDomainTTL = 30 * time.Second

	// dnsCacheMaxEntries caps memory usage. Each entry is ~4KB worst case,
	// so 512 entries ≈ 2MB — negligible on a phone.
	dnsCacheMaxEntries = 512

	// dnsPrefetchWindow is the fraction of original TTL remaining that
	// triggers a background prefetch (10% = refresh when 90% expired).
	dnsPrefetchWindow = 0.10

	// dnsRcodeNXDOMAIN is the DNS RCODE for non-existent domain.
	dnsRcodeNXDOMAIN = 3
)

// copyBufPool reuses relay buffers across TCP connections. With many
// in-flight connections, each needing 2×128KB, pooling saves significant
// allocation churn and reduces GC pressure.
var copyBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, copyBufSize)
		return &buf
	},
}

// dnsCacheEntry holds a cached DNS response with its expiry time and
// original TTL for prefetch window calculation.
type dnsCacheEntry struct {
	key      string
	response []byte
	expiry   time.Time
	ttl      time.Duration // original TTL (for prefetch window calc)
	element  *list.Element
}

// dnsCache is an LRU cache for DNS responses keyed by the query body
// (minus the 2-byte transaction ID which changes per request). Uses a
// doubly-linked list for O(1) LRU eviction and a map for O(1) lookups.
type dnsCache struct {
	mu          sync.Mutex
	entries     map[string]*dnsCacheEntry
	lru         *list.List // front=MRU, back=LRU
	prefetching sync.Map   // dedup concurrent prefetches: key → bool
}

func newDNSCache() *dnsCache {
	return &dnsCache{
		entries: make(map[string]*dnsCacheEntry),
		lru:     list.New(),
	}
}

// get looks up a DNS query in the cache, patching the transaction ID before returning.
// shouldPrefetch is true when the entry's still valid but TTL is almost gone (<10% remaining).
func (c *dnsCache) get(query []byte) ([]byte, bool, bool) {
	if len(query) < 12 {
		return nil, false, false
	}
	key := string(query[2:]) // skip transaction ID

	c.mu.Lock()
	entry, ok := c.entries[key]
	if !ok {
		c.mu.Unlock()
		return nil, false, false
	}

	now := time.Now()
	if now.After(entry.expiry) {
		// Expired — remove from cache
		c.lru.Remove(entry.element)
		delete(c.entries, key)
		c.mu.Unlock()
		return nil, false, false
	}

	// Move to front (MRU)
	c.lru.MoveToFront(entry.element)

	// Clone and patch transaction ID
	resp := make([]byte, len(entry.response))
	copy(resp, entry.response)
	resp[0] = query[0]
	resp[1] = query[1]

	// Check if we're in the prefetch window
	remaining := entry.expiry.Sub(now)
	shouldPrefetch := entry.ttl > 0 && float64(remaining) < float64(entry.ttl)*dnsPrefetchWindow
	c.mu.Unlock()

	return resp, true, shouldPrefetch
}

// put adds a DNS response to the cache, evicting the LRU entry on overflow.
// NXDOMAIN responses get a shorter TTL so failures don't get stuck in cache.
func (c *dnsCache) put(query, response []byte) {
	if len(query) < 12 || len(response) < 12 {
		return
	}
	key := string(query[2:])

	// Determine TTL: parse from response, cap at max, use NXDOMAIN TTL for negative responses
	ttl := parseDNSTTL(response)
	if isDNSNXDomain(response) && ttl > dnsCacheNXDomainTTL {
		ttl = dnsCacheNXDomainTTL
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Update existing entry if present
	if existing, ok := c.entries[key]; ok {
		existing.response = append([]byte(nil), response...)
		existing.expiry = time.Now().Add(ttl)
		existing.ttl = ttl
		c.lru.MoveToFront(existing.element)
		return
	}

	// Evict LRU entry if at capacity
	for c.lru.Len() >= dnsCacheMaxEntries {
		back := c.lru.Back()
		if back == nil {
			break
		}
		evicted := back.Value.(*dnsCacheEntry)
		c.lru.Remove(back)
		delete(c.entries, evicted.key)
	}

	entry := &dnsCacheEntry{
		key:      key,
		response: append([]byte(nil), response...),
		expiry:   time.Now().Add(ttl),
		ttl:      ttl,
	}
	entry.element = c.lru.PushFront(entry)
	c.entries[key] = entry
}

// skipDNSName advances past a DNS name in wire format (label sequences + compression
// pointers, RFC 1035 §4.1.4). Returns -1 if the data is malformed.
func skipDNSName(data []byte, offset int) int {
	for {
		if offset >= len(data) {
			return -1
		}
		labelLen := int(data[offset])
		if labelLen == 0 {
			return offset + 1 // root label
		}
		if labelLen&0xC0 == 0xC0 {
			// Compression pointer (2 bytes)
			if offset+1 >= len(data) {
				return -1
			}
			return offset + 2
		}
		if labelLen&0xC0 != 0 {
			return -1 // invalid label type
		}
		offset += 1 + labelLen
	}
}

// parseDNSTTL extracts the minimum TTL from a DNS response's answer and
// authority sections. Falls back to dnsCacheMaxTTL on parse error.
// Caps all TTLs at dnsCacheMaxTTL.
func parseDNSTTL(resp []byte) time.Duration {
	if len(resp) < 12 {
		return dnsCacheMaxTTL
	}

	qdcount := int(binary.BigEndian.Uint16(resp[4:6]))
	ancount := int(binary.BigEndian.Uint16(resp[6:8]))
	nscount := int(binary.BigEndian.Uint16(resp[8:10]))

	// Skip header (12 bytes) and question section
	offset := 12
	for i := 0; i < qdcount; i++ {
		offset = skipDNSName(resp, offset)
		if offset < 0 {
			return dnsCacheMaxTTL
		}
		offset += 4 // QTYPE(2) + QCLASS(2)
		if offset > len(resp) {
			return dnsCacheMaxTTL
		}
	}

	// Walk answer + authority RRs, collect minimum TTL
	minTTL := dnsCacheMaxTTL
	found := false
	for i := 0; i < ancount+nscount; i++ {
		offset = skipDNSName(resp, offset)
		if offset < 0 || offset+10 > len(resp) {
			break
		}
		// TYPE(2) + CLASS(2) + TTL(4) + RDLENGTH(2) = 10 bytes
		ttlSec := binary.BigEndian.Uint32(resp[offset+4 : offset+8])
		rdLen := int(binary.BigEndian.Uint16(resp[offset+8 : offset+10]))
		offset += 10 + rdLen
		if offset > len(resp) {
			break
		}

		ttl := time.Duration(ttlSec) * time.Second
		if ttl < minTTL {
			minTTL = ttl
			found = true
		} else if !found {
			found = true
		}
	}

	if !found || minTTL <= 0 {
		return dnsCacheMaxTTL
	}
	if minTTL > dnsCacheMaxTTL {
		return dnsCacheMaxTTL
	}
	return minTTL
}

// isDNSNXDomain checks if a DNS response has RCODE=NXDOMAIN (3).
func isDNSNXDomain(resp []byte) bool {
	if len(resp) < 4 {
		return false
	}
	return resp[3]&0x0F == dnsRcodeNXDOMAIN
}

// TunnelOptions holds optional configuration for StartTunnelWithOptions.
type TunnelOptions struct {
	DNS2Addr       string                                        // secondary DNS resolver address
	AllowDirectDNS bool                                          // allow fallback to direct UDP DNS (ISP leak)
	DialStream     func(target string) (io.ReadWriteCloser, error) // direct stream opener (bypasses SOCKS5)
}

// Tunnel holds the sing-tun stack that reads/writes the TUN fd.
type Tunnel struct {
	stack      singtun.Stack
	tunDev     singtun.Tun
	cancel     context.CancelFunc
	done       chan struct{}
	stopOnce   sync.Once
	stopErr    error
	protectFn  func(fd int) bool                               // VpnService.protect() callback
	socksAddr  string                                           // SOCKS5 proxy address (xray path)
	dnsAddr    string                                           // DNS resolver address
	dns2Addr   string                                           // secondary DNS resolver
	allowDirectDNS bool                                         // allow fallback to direct UDP DNS
	dns        *dnsCache                                        // DNS response cache
	dnsPool      *dnsChannelPool                                  // persistent DNS channel pool (WebRTC path)
	dialStream   func(target string) (io.ReadWriteCloser, error) // direct stream opener (bypasses SOCKS5)
	dialStreamMu sync.RWMutex                                    // protects dialStream for hot-swap

	bytesUp   atomic.Int64
	bytesDown atomic.Int64
}

// StartTunnel creates a sing-tun network stack attached to the TUN file
// descriptor and forwards TCP through socksAddr (SOCKS5) and DNS through dnsAddr.
// protectFn is VpnService.protect() — it must be called on any socket that
// should bypass the TUN (i.e. the DNS relay socket).
// If dialStream is provided, TCP connections use it directly instead of SOCKS5.
func StartTunnel(tunFd int, socksAddr string, tunAddr string, mtu int, dnsAddr string, protectFn func(fd int) bool, dialStream ...func(string) (io.ReadWriteCloser, error)) (*Tunnel, error) {
	opts := TunnelOptions{}
	if len(dialStream) > 0 && dialStream[0] != nil {
		opts.DialStream = dialStream[0]
	}
	return StartTunnelWithOptions(tunFd, socksAddr, tunAddr, mtu, dnsAddr, protectFn, opts)
}

// StartTunnelWithOptions creates a sing-tun network stack with full configuration.
func StartTunnelWithOptions(tunFd int, socksAddr string, tunAddr string, mtu int, dnsAddr string, protectFn func(fd int) bool, opts TunnelOptions) (*Tunnel, error) {
	// Duplicate the fd so Go owns an independent copy — Android keeps the
	// original fd in the ParcelFileDescriptor from VpnService.establish().
	dupFd, err := dupFD(tunFd)
	if err != nil {
		return nil, fmt.Errorf("dup tun fd: %w", err)
	}

	// Parse TUN address for sing-tun Options.
	addr, err := netip.ParseAddr(tunAddr)
	if err != nil {
		syscall.Close(dupFd)
		return nil, fmt.Errorf("parse tun addr %q: %w", tunAddr, err)
	}
	prefix := netip.PrefixFrom(addr, 24)

	tunOpts := singtun.Options{
		FileDescriptor: dupFd,
		MTU:            uint32(mtu),
		Inet4Address:   []netip.Prefix{prefix},
	}

	tunDev, err := singtun.New(tunOpts)
	if err != nil {
		syscall.Close(dupFd)
		return nil, fmt.Errorf("create tun device: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	t := &Tunnel{
		tunDev:         tunDev,
		cancel:         cancel,
		done:           make(chan struct{}),
		protectFn:      protectFn,
		socksAddr:      socksAddr,
		dnsAddr:        dnsAddr,
		dns2Addr:       opts.DNS2Addr,
		allowDirectDNS: opts.AllowDirectDNS,
		dns:            newDNSCache(),
		dialStream:     opts.DialStream,
	}

	// Initialize DNS channel pool for persistent DNS streams.
	if opts.DialStream != nil {
		// WebRTC path: use the direct stream dialer (opens __dns__ channels).
		t.dnsPool = newDNSChannelPool(opts.DialStream, t.done)
	} else if socksAddr != "" {
		// xray/UPnP path: persistent DNS-over-TCP connections through SOCKS5.
		// The xray server routes port 53 to its dns outbound which resolves via UDP,
		// so these TCP connections work even when the ISP blocks TCP to port 53.
		dnsTarget := net.JoinHostPort(dnsAddr, fmt.Sprintf("%d", dnsPort))
		t.dnsPool = newDNSChannelPool(func(_ string) (io.ReadWriteCloser, error) {
			dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
			if err != nil {
				return nil, err
			}
			return dialer.Dial("tcp", dnsTarget)
		}, t.done)
	}

	handler := &proxyHandler{tunnel: t}

	// Try "system" stack first (no userspace TCP, best performance).
	// Fall back to "gvisor" if unsupported on this Android version.
	var stackName string
	stack, err := singtun.NewStack("system", singtun.StackOptions{
		Context:    ctx,
		Tun:        tunDev,
		TunOptions: tunOpts,
		Handler:    handler,
		UDPTimeout: 30 * time.Second,
	})
	if err != nil {
		applog.Warnf("system stack unavailable (%v), falling back to gvisor", err)
		stack, err = singtun.NewStack("gvisor", singtun.StackOptions{
			Context:    ctx,
			Tun:        tunDev,
			TunOptions: tunOpts,
			Handler:    handler,
			UDPTimeout: 30 * time.Second,
		})
		if err != nil {
			cancel()
			tunDev.Close()
			return nil, fmt.Errorf("create stack: %w", err)
		}
		stackName = "gvisor"
	} else {
		stackName = "system"
	}

	t.stack = stack

	if err := stack.Start(); err != nil {
		cancel()
		tunDev.Close()
		return nil, fmt.Errorf("start stack: %w", err)
	}

	applog.Successf("Tunnel started: tun=%s socks=%s dns=%s dns2=%s directDNS=%v mtu=%d stack=%s",
		tunAddr, socksAddr, dnsAddr, opts.DNS2Addr, opts.AllowDirectDNS, mtu, stackName)
	return t, nil
}

// Stop tears down the tunnel. Safe to call concurrently from multiple goroutines.
func (t *Tunnel) Stop() error {
	t.stopOnce.Do(func() {
		close(t.done)
		if t.dnsPool != nil {
			t.dnsPool.close()
		}
		t.cancel()
		t.stack.Close()
		t.stopErr = t.tunDev.Close()
		applog.Info("Tunnel stopped")
	})
	return t.stopErr
}

// SwapDialStream atomically replaces the tunnel's stream dialer function.
// Used for fast reconnect: rebuild the WebRTC layer, then swap in the new dialer
// without tearing down the TUN interface.
func (t *Tunnel) SwapDialStream(fn func(string) (io.ReadWriteCloser, error)) {
	t.dialStreamMu.Lock()
	t.dialStream = fn
	t.dialStreamMu.Unlock()
	if t.dnsPool != nil {
		t.dnsPool.close()
		t.dnsPool = newDNSChannelPool(fn, t.done)
	}
	applog.Info("tunnel: dialStream swapped (fast reconnect)")
}

// GetStats returns cumulative byte counters.
func (t *Tunnel) GetStats() (bytesUp, bytesDown int64) {
	return t.bytesUp.Load(), t.bytesDown.Load()
}

// proxyHandler implements singtun.Handler, forwarding TCP through SOCKS5
// and relaying DNS UDP queries.
type proxyHandler struct {
	tunnel *Tunnel
}

// PrepareConnection is called before establishing each connection.
// Returning nil accepts the connection for proxying.
func (h *proxyHandler) PrepareConnection(network string, source M.Socksaddr, destination M.Socksaddr) error {
	return nil // accept all connections
}

// NewConnectionEx handles TCP connections from the TUN.
func (h *proxyHandler) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	defer func() {
		conn.Close()
		if onClose != nil {
			onClose(nil)
		}
	}()
	h.tunnel.handleTCP(conn, destination.String())
}

// NewPacketConnectionEx handles UDP connections from the TUN (DNS only).
func (h *proxyHandler) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	defer func() {
		conn.Close()
		if onClose != nil {
			onClose(nil)
		}
	}()
	if destination.Port != dnsPort {
		return // only handle DNS
	}
	h.tunnel.handleUDP(conn, destination)
}

// handleTCP forwards a single TCP connection through the tunnel.
// Uses dialStream directly when available (WebRTC), otherwise SOCKS5 (xray).
func (t *Tunnel) handleTCP(srcConn net.Conn, dstAddr string) {
	// Disable Nagle's algorithm on the TUN-side TCP connection to avoid
	// up to 200ms latency on small writes (TLS handshake, HTTP headers).
	if tc, ok := srcConn.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}

	var dstConn io.ReadWriteCloser
	var err error

	t.dialStreamMu.RLock()
	ds := t.dialStream
	t.dialStreamMu.RUnlock()

	if ds != nil {
		dstConn, err = ds(dstAddr)
		if err != nil {
			applog.Warnf("tunnel: dial stream %s failed: %v", dstAddr, err)
			return
		}
	} else {
		dialer, dialErr := proxy.SOCKS5("tcp", t.socksAddr, nil, proxy.Direct)
		if dialErr != nil {
			applog.Warnf("tunnel: SOCKS5 dialer error: %v", dialErr)
			return
		}
		dstConn, err = dialer.Dial("tcp", dstAddr)
		if err != nil {
			applog.Warnf("tunnel: SOCKS5 dial %s failed: %v", dstAddr, err)
			return
		}
	}
	defer dstConn.Close()

	// Close both connections when the tunnel is stopped so io.CopyBuffer unblocks.
	// handleDone prevents this goroutine from leaking after the connection completes.
	handleDone := make(chan struct{})
	defer close(handleDone)
	go func() {
		select {
		case <-t.done:
			srcConn.Close()
			dstConn.Close()
		case <-handleDone:
		}
	}()

	// Bidirectional copy with byte counting using pooled 128KB buffers.
	copyDone := make(chan struct{})
	go func() {
		bufp := copyBufPool.Get().(*[]byte)
		n, _ := io.CopyBuffer(dstConn, srcConn, *bufp)
		copyBufPool.Put(bufp)
		t.bytesUp.Add(n)

		if tc, ok := dstConn.(interface{ CloseWrite() error }); ok {
			tc.CloseWrite()
		}
		copyDone <- struct{}{}
	}()

	bufp := copyBufPool.Get().(*[]byte)
	n, _ := io.CopyBuffer(srcConn, dstConn, *bufp)
	copyBufPool.Put(bufp)
	t.bytesDown.Add(n)

	<-copyDone
}

// handleUDP relays a DNS UDP packet. Checks the cache first, then tries
// DNS-over-TCP through the SOCKS5 tunnel (encrypted, private), and falls
// back to direct UDP if the tunnel path is too slow or unavailable.
func (t *Tunnel) handleUDP(pktConn N.PacketConn, destination M.Socksaddr) {
	// Read the DNS query from the TUN-side client.
	b := buf.New()
	defer b.Release()

	src, err := pktConn.ReadPacket(b)
	if err != nil || b.Len() == 0 {
		return
	}
	query := make([]byte, b.Len())
	copy(query, b.Bytes())
	t.bytesUp.Add(int64(len(query)))

	// Check cache first — avoids a full SOCKS5 TCP round-trip for repeated
	// lookups. Browsers typically resolve the same handful of domains
	// hundreds of times per session.
	if resp, ok, shouldPrefetch := t.dns.get(query); ok {
		if shouldPrefetch {
			go t.prefetchDNS(query)
		}
		t.bytesDown.Add(int64(len(resp)))
		rb := buf.As(resp)
		pktConn.WritePacket(rb, src)
		return
	}

	// DNS resolution cascade (most private → least private):
	// 1. DNS1 via tunnel
	// 2. DNS2 via tunnel (if configured)
	// 3. DNS1 direct UDP (only if allowDirectDNS)
	// 4. Drop query
	resp, err := t.dnsViaTunnel(t.dnsAddr, query)
	if err != nil && t.dns2Addr != "" {
		resp, err = t.dnsViaTunnel(t.dns2Addr, query)
	}
	if err != nil && t.allowDirectDNS {
		resp, err = t.dnsDirect(t.dnsAddr, query)
	}
	if err != nil {
		applog.Warnf("tunnel: DNS resolution failed for all resolvers")
		return
	}

	// Cache the response for future lookups.
	t.dns.put(query, resp)

	t.bytesDown.Add(int64(len(resp)))
	rb := buf.As(resp)
	pktConn.WritePacket(rb, src)
}

// dnsViaTunnel sends a DNS query through the tunnel. Tries the dedicated
// DNS channel first (persistent stream, fast), falls back to per-stream
// DNS-over-TCP (opens a new smux stream per query).
func (t *Tunnel) dnsViaTunnel(dnsAddr string, query []byte) ([]byte, error) {
	if t.dnsPool != nil {
		resp, err := t.dnsPool.submitQuery(query, dnsTunnelTimeout)
		if err == nil {
			return resp, nil
		}
		applog.Warnf("tunnel: DNS channel query failed: %v, falling back to per-stream", err)
	}
	return t.dnsViaStream(dnsAddr, query)
}

// dnsViaStream sends a DNS query over TCP through the tunnel using a
// per-query stream. Uses dialStream directly when available (WebRTC),
// otherwise SOCKS5 (xray).
func (t *Tunnel) dnsViaStream(dnsAddr string, query []byte) ([]byte, error) {
	target := net.JoinHostPort(dnsAddr, fmt.Sprintf("%d", dnsPort))

	var conn io.ReadWriteCloser
	var err error

	t.dialStreamMu.RLock()
	ds := t.dialStream
	t.dialStreamMu.RUnlock()

	if ds != nil {
		conn, err = ds(target)
		if err != nil {
			return nil, fmt.Errorf("dial dns via stream: %w", err)
		}
	} else {
		dialer, dialErr := proxy.SOCKS5("tcp", t.socksAddr, nil, proxy.Direct)
		if dialErr != nil {
			return nil, fmt.Errorf("socks5 dialer: %w", dialErr)
		}
		conn, err = dialer.Dial("tcp", target)
		if err != nil {
			return nil, fmt.Errorf("dial dns via socks5: %w", err)
		}
	}
	defer conn.Close()

	// smux.Stream and paddedStream don't implement SetDeadline, so use
	// a timer to force-close the stream if the DNS exchange takes too long.
	// This prevents indefinitely leaked streams when the server side hangs.
	streamDone := make(chan struct{})
	defer close(streamDone)
	timer := time.AfterFunc(dnsTunnelTimeout, func() {
		select {
		case <-streamDone:
		default:
			conn.Close()
		}
	})
	defer timer.Stop()

	// DNS-over-TCP: prefix query with 2-byte length.
	tcpMsg := make([]byte, 2+len(query))
	binary.BigEndian.PutUint16(tcpMsg[:2], uint16(len(query)))
	copy(tcpMsg[2:], query)
	if _, err := conn.Write(tcpMsg); err != nil {
		return nil, err
	}

	// Read 2-byte length prefix, then response.
	var lenBuf [2]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil, err
	}
	respLen := binary.BigEndian.Uint16(lenBuf[:])
	if respLen == 0 || respLen > udpBufSize {
		return nil, fmt.Errorf("dns response length %d out of range", respLen)
	}
	resp := make([]byte, respLen)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// prefetchDNS refreshes a cache entry in the background before it expires.
// Called when a cache hit is within the prefetch window (last 10% of TTL).
func (t *Tunnel) prefetchDNS(query []byte) {
	if len(query) < 12 {
		return
	}
	key := string(query[2:])

	// Dedup: skip if already prefetching this query
	if _, loaded := t.dns.prefetching.LoadOrStore(key, true); loaded {
		return
	}
	defer t.dns.prefetching.Delete(key)

	resp, err := t.dnsViaTunnel(t.dnsAddr, query)
	if err != nil && t.dns2Addr != "" {
		resp, err = t.dnsViaTunnel(t.dns2Addr, query)
	}
	if err != nil {
		return // stale cached entry still serves until expiry
	}
	t.dns.put(query, resp)
}

// dnsDirect sends a DNS query via a protected UDP socket (bypasses TUN).
func (t *Tunnel) dnsDirect(dnsAddr string, query []byte) ([]byte, error) {
	dialer := net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			var protectErr error
			c.Control(func(fd uintptr) {
				if t.protectFn != nil && !t.protectFn(int(fd)) {
					protectErr = fmt.Errorf("protect fd %d failed", fd)
				}
			})
			return protectErr
		},
	}
	dnsConn, err := dialer.Dial("udp", net.JoinHostPort(dnsAddr, fmt.Sprintf("%d", dnsPort)))
	if err != nil {
		return nil, fmt.Errorf("dns direct dial: %w", err)
	}
	defer dnsConn.Close()

	dnsConn.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := dnsConn.Write(query); err != nil {
		return nil, err
	}

	resp := make([]byte, udpBufSize)
	rn, err := dnsConn.Read(resp)
	if err != nil {
		return nil, err
	}
	return resp[:rn], nil
}
