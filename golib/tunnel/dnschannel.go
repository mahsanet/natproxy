// Package tunnel provides DNS channel multiplexing over persistent smux streams.
// A dnsChannelPool maintains 2 persistent streams using the "__dns__" magic
// target, multiplexing DNS queries/responses by transaction ID. This avoids
// opening a new smux stream for every DNS lookup.
package tunnel

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"natproxy/golib/applog"
)

const (
	// DNSChannelTarget is the magic target address that tells the server to
	// enter DNS relay mode instead of dialing a TCP target.
	DNSChannelTarget = "__dns__"

	// Number of persistent DNS channels in the pool.
	dnsChannelCount = 2

	// DNS query batching: collect queries for up to this duration before
	// flushing them as a single concatenated write.
	dnsBatchWindow = 2 * time.Millisecond

	// Maximum queries per batch. Flush immediately when reached.
	dnsBatchMaxQueries = 16
)

// dnsResult carries a DNS response or error back to the caller.
type dnsResult struct {
	response []byte
	err      error
}

// dnsBatchItem is a queued DNS query waiting to be batched.
type dnsBatchItem struct {
	query    []byte
	resultCh chan dnsResult
}

// dnsChannel wraps a single persistent smux stream for DNS multiplexing.
// Writes are serialized via a mutex. A readLoop goroutine dispatches
// responses to callers by matching DNS transaction IDs.
type dnsChannel struct {
	stream  io.ReadWriteCloser
	writeMu sync.Mutex
	pending sync.Map // map[uint16]chan []byte
	done    chan struct{}
}

// newDNSChannel wraps a stream and starts the read loop.
func newDNSChannel(stream io.ReadWriteCloser) *dnsChannel {
	ch := &dnsChannel{
		stream: stream,
		done:   make(chan struct{}),
	}
	go ch.readLoop()
	return ch
}

// query sends a DNS query and waits for the response matched by txID.
func (ch *dnsChannel) query(dnsQuery []byte, timeout time.Duration) ([]byte, error) {
	if len(dnsQuery) < 12 {
		return nil, fmt.Errorf("dns query too short")
	}

	txID := binary.BigEndian.Uint16(dnsQuery[:2])
	respCh := make(chan []byte, 1)

	// Register pending query by txID
	ch.pending.Store(txID, respCh)
	defer ch.pending.Delete(txID)

	// Write DNS-over-TCP frame: [2B length][query]
	frame := make([]byte, 2+len(dnsQuery))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(dnsQuery)))
	copy(frame[2:], dnsQuery)

	ch.writeMu.Lock()
	_, err := ch.stream.Write(frame)
	ch.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("dns channel write: %w", err)
	}

	// Wait for response or timeout
	select {
	case resp := <-respCh:
		if resp == nil {
			return nil, fmt.Errorf("dns channel closed")
		}
		return resp, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("dns channel query timeout")
	case <-ch.done:
		return nil, fmt.Errorf("dns channel closed")
	}
}

// writeBatch writes multiple DNS-over-TCP frames in a single write call.
// This results in one smux frame / one SCTP chunk for the entire batch.
func (ch *dnsChannel) writeBatch(frames []byte) error {
	ch.writeMu.Lock()
	defer ch.writeMu.Unlock()
	_, err := ch.stream.Write(frames)
	return err
}

// readLoop reads DNS-over-TCP frames from the stream and dispatches them
// to waiting callers by transaction ID.
func (ch *dnsChannel) readLoop() {
	defer func() {
		close(ch.done)
		// Close all pending channels so waiters don't hang
		ch.pending.Range(func(key, value any) bool {
			if respCh, ok := value.(chan []byte); ok {
				select {
				case respCh <- nil:
				default:
				}
			}
			ch.pending.Delete(key)
			return true
		})
	}()

	var lenBuf [2]byte
	for {
		// Read 2-byte length prefix
		if _, err := io.ReadFull(ch.stream, lenBuf[:]); err != nil {
			return
		}
		respLen := binary.BigEndian.Uint16(lenBuf[:])
		if respLen == 0 || respLen > 4096 {
			return // invalid frame
		}

		resp := make([]byte, respLen)
		if _, err := io.ReadFull(ch.stream, resp); err != nil {
			return
		}

		if len(resp) < 2 {
			continue
		}

		// Dispatch by transaction ID
		txID := binary.BigEndian.Uint16(resp[:2])
		if val, ok := ch.pending.LoadAndDelete(txID); ok {
			if respCh, ok := val.(chan []byte); ok {
				select {
				case respCh <- resp:
				default:
				}
			}
		}
	}
}

// close shuts down the channel.
func (ch *dnsChannel) close() {
	ch.stream.Close()
}

// dnsChannelPool manages a pool of persistent DNS channels. Handles
// reconnection and query batching.
type dnsChannelPool struct {
	dialStream func(string) (io.ReadWriteCloser, error)
	tunnelDone <-chan struct{}

	mu       sync.Mutex
	channels []*dnsChannel

	// Batching state
	batchMu    sync.Mutex
	batchQueue []dnsBatchItem
	batchTimer *time.Timer
	roundRobin atomic.Uint32
}

// newDNSChannelPool creates a pool but defers channel creation until first use.
func newDNSChannelPool(dialStream func(string) (io.ReadWriteCloser, error), tunnelDone <-chan struct{}) *dnsChannelPool {
	return &dnsChannelPool{
		dialStream: dialStream,
		tunnelDone: tunnelDone,
	}
}

// pickChannel returns a channel from the pool, creating channels as needed.
// Uses round-robin selection across available channels.
func (p *dnsChannelPool) pickChannel() (*dnsChannel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Prune dead channels
	alive := p.channels[:0]
	for _, ch := range p.channels {
		select {
		case <-ch.done:
			ch.close()
		default:
			alive = append(alive, ch)
		}
	}
	p.channels = alive

	// Open channels if needed
	for len(p.channels) < dnsChannelCount {
		ch, err := p.openChannel()
		if err != nil {
			if len(p.channels) > 0 {
				break // use what we have
			}
			return nil, err
		}
		p.channels = append(p.channels, ch)
	}

	if len(p.channels) == 0 {
		return nil, fmt.Errorf("no dns channels available")
	}

	// Round-robin
	idx := p.roundRobin.Add(1) % uint32(len(p.channels))
	return p.channels[idx], nil
}

// openChannel dials a new __dns__ stream.
// The dialStream function (Client.DialStream) already writes the target
// header [2B len]["__dns__"] and applies padding, so the returned stream
// is ready for DNS-over-TCP framing immediately.
func (p *dnsChannelPool) openChannel() (*dnsChannel, error) {
	stream, err := p.dialStream(DNSChannelTarget)
	if err != nil {
		return nil, fmt.Errorf("dial dns channel: %w", err)
	}

	applog.Info("tunnel: DNS channel opened")
	return newDNSChannel(stream), nil
}

// submitQuery submits a DNS query through the batching system.
// Queries are batched for up to dnsBatchWindow (2ms) or until
// dnsBatchMaxQueries are collected, then flushed as a single write.
func (p *dnsChannelPool) submitQuery(query []byte, timeout time.Duration) ([]byte, error) {
	resultCh := make(chan dnsResult, 1)
	item := dnsBatchItem{
		query:    query,
		resultCh: resultCh,
	}

	p.batchMu.Lock()
	p.batchQueue = append(p.batchQueue, item)
	queueLen := len(p.batchQueue)

	if queueLen >= dnsBatchMaxQueries {
		// Flush immediately
		batch := p.batchQueue
		p.batchQueue = nil
		if p.batchTimer != nil {
			p.batchTimer.Stop()
			p.batchTimer = nil
		}
		p.batchMu.Unlock()
		go p.flushBatch(batch)
	} else if p.batchTimer == nil {
		// Start batch window timer
		p.batchTimer = time.AfterFunc(dnsBatchWindow, func() {
			p.batchMu.Lock()
			batch := p.batchQueue
			p.batchQueue = nil
			p.batchTimer = nil
			p.batchMu.Unlock()
			if len(batch) > 0 {
				p.flushBatch(batch)
			}
		})
		p.batchMu.Unlock()
	} else {
		p.batchMu.Unlock()
	}

	// Wait for result
	select {
	case result := <-resultCh:
		return result.response, result.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("dns batch query timeout")
	case <-p.tunnelDone:
		return nil, fmt.Errorf("tunnel stopped")
	}
}

// flushBatch sends all queued queries through a single DNS channel.
// Each query gets registered by txID for response dispatch. All frames
// are concatenated into a single write for minimal overhead.
func (p *dnsChannelPool) flushBatch(batch []dnsBatchItem) {
	ch, err := p.pickChannel()
	if err != nil {
		for _, item := range batch {
			item.resultCh <- dnsResult{err: err}
		}
		return
	}

	if len(batch) == 1 {
		// Single query — use direct path (no batching overhead)
		item := batch[0]
		resp, err := ch.query(item.query, dnsTunnelTimeout)
		item.resultCh <- dnsResult{response: resp, err: err}
		return
	}

	// Build concatenated DNS-over-TCP frames
	totalSize := 0
	for _, item := range batch {
		totalSize += 2 + len(item.query)
	}
	buf := make([]byte, 0, totalSize)
	for _, item := range batch {
		frame := make([]byte, 2)
		binary.BigEndian.PutUint16(frame, uint16(len(item.query)))
		buf = append(buf, frame...)
		buf = append(buf, item.query...)
	}

	// Register all txIDs before writing
	for _, item := range batch {
		if len(item.query) >= 2 {
			txID := binary.BigEndian.Uint16(item.query[:2])
			respCh := make(chan []byte, 1)
			ch.pending.Store(txID, respCh)

			// Capture for goroutine
			capturedItem := item
			capturedTxID := txID
			capturedRespCh := respCh
			go func() {
				defer ch.pending.Delete(capturedTxID)
				select {
				case resp := <-capturedRespCh:
					if resp == nil {
						capturedItem.resultCh <- dnsResult{err: fmt.Errorf("dns channel closed")}
					} else {
						capturedItem.resultCh <- dnsResult{response: resp}
					}
				case <-time.After(dnsTunnelTimeout):
					ch.pending.Delete(capturedTxID)
					capturedItem.resultCh <- dnsResult{err: fmt.Errorf("dns batch query timeout")}
				case <-ch.done:
					capturedItem.resultCh <- dnsResult{err: fmt.Errorf("dns channel closed")}
				}
			}()
		}
	}

	// Single write for all frames
	if err := ch.writeBatch(buf); err != nil {
		applog.Warnf("tunnel: DNS batch write failed: %v", err)
	}
}

// close shuts down all channels in the pool.
func (p *dnsChannelPool) close() {
	p.batchMu.Lock()
	if p.batchTimer != nil {
		p.batchTimer.Stop()
		p.batchTimer = nil
	}
	// Fail any pending batch items
	for _, item := range p.batchQueue {
		item.resultCh <- dnsResult{err: fmt.Errorf("dns channel pool closed")}
	}
	p.batchQueue = nil
	p.batchMu.Unlock()

	p.mu.Lock()
	channels := p.channels
	p.channels = nil
	p.mu.Unlock()

	for _, ch := range channels {
		ch.close()
	}
}
