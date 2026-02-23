package webrtc

import (
	"io"

	pionwebrtc "github.com/pion/webrtc/v4"
)

// dcStreamConn wraps a detached WebRTC data channel (message-oriented SCTP)
// into a stream-oriented connection suitable for smux.
//
// pion's SCTP Read returns io.ErrShortBuffer when the provided read buffer is
// smaller than the queued SCTP message. smux's recvLoop reads with small
// buffers (8-byte frame header), which causes "short buffer" errors when the
// SCTP message contains a full smux frame (header + payload). This wrapper
// reads complete SCTP messages into a persistent buffer and serves the bytes
// as a continuous stream.
//
// When a DataChannel reference is provided, Write applies backpressure by
// blocking when the channel's buffered amount exceeds maxBuffered, resuming
// when it drops below lowMark.
type dcStreamConn struct {
	rwc        io.ReadWriteCloser
	dc         *pionwebrtc.DataChannel // nil = no flow control
	readBuf    []byte                  // reusable buffer for reading full SCTP messages
	pending    []byte                  // unconsumed portion of the last message
	writeCh    chan struct{}            // signaled by OnBufferedAmountLow
	done       chan struct{}            // closed on Close() to wake blocked writers
	maxBuffered uint64                  // DC backpressure high water mark (bytes)
	lowMark     uint64                  // DC backpressure low water mark (bytes)
}

func newDCStreamConn(rwc io.ReadWriteCloser, dc *pionwebrtc.DataChannel, maxBuffered, lowMark int) *dcStreamConn {
	if maxBuffered <= 0 {
		maxBuffered = 512 * 1024 // 512KB default
	}
	if lowMark <= 0 {
		lowMark = 128 * 1024 // 128KB default
	}

	c := &dcStreamConn{
		rwc:         rwc,
		dc:          dc,
		readBuf:     make([]byte, 65536), // max SCTP message size
		writeCh:     make(chan struct{}, 1),
		done:        make(chan struct{}),
		maxBuffered: uint64(maxBuffered),
		lowMark:     uint64(lowMark),
	}

	if dc != nil {
		dc.SetBufferedAmountLowThreshold(c.lowMark)
		dc.OnBufferedAmountLow(func() {
			select {
			case c.writeCh <- struct{}{}:
			default:
			}
		})
	}

	return c
}

// Read is NOT goroutine-safe. c.pending aliases into c.readBuf to avoid
// copying; concurrent reads would corrupt data. smux serializes reads via
// its recvLoop, so this is safe in the current architecture.
func (c *dcStreamConn) Read(p []byte) (int, error) {
	if len(c.pending) > 0 {
		n := copy(p, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}

	n, err := c.rwc.Read(c.readBuf)
	if err != nil {
		return 0, err
	}

	m := copy(p, c.readBuf[:n])
	if m < n {
		c.pending = c.readBuf[m:n]
	}
	return m, nil
}

func (c *dcStreamConn) Write(p []byte) (int, error) {
	// Apply backpressure: block if the data channel's send buffer is full.
	// Select on done to avoid goroutine leaks when Close() is called.
	if c.dc != nil {
		for c.dc.BufferedAmount() > c.maxBuffered {
			select {
			case <-c.writeCh:
			case <-c.done:
				return 0, io.ErrClosedPipe
			}
		}
	}
	return c.rwc.Write(p)
}

func (c *dcStreamConn) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return c.rwc.Close()
}
