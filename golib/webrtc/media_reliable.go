package webrtc

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"time"
)

// reliableRTP provides reliable, ordered delivery over unreliable RTP.
//
// Header per frame: [2B seqnum][1B flags][payload]
//
//	flags: 0x00=data, 0x01=ack, 0x02=retransmit
const (
	reliableFlagData       = 0x00
	reliableFlagAck        = 0x01
	reliableFlagRetransmit = 0x02

	reliableHeaderSize  = 3
	reliableAckInterval = 50 * time.Millisecond
	reliableMaxRetries  = 10
	reliableRTOInit     = 100 * time.Millisecond
	reliableWindowSize  = 256
)

type reliableRTP struct {
	inner io.ReadWriteCloser

	// Write side
	writeMu    sync.Mutex
	writeSeq   uint16
	unacked    map[uint16]*reliableFrame
	unackedMu  sync.Mutex

	// Read side
	readSeq     uint16
	readBuf     map[uint16][]byte
	readMu      sync.Mutex
	readCond    *sync.Cond
	readPending []byte // leftover bytes from a partially consumed packet

	done chan struct{}
}

type reliableFrame struct {
	data    []byte
	sentAt  time.Time
	retries int
}

func newReliableRTP(inner io.ReadWriteCloser) *reliableRTP {
	r := &reliableRTP{
		inner:   inner,
		unacked: make(map[uint16]*reliableFrame),
		readBuf: make(map[uint16][]byte),
		done:    make(chan struct{}),
	}
	r.readCond = sync.NewCond(&r.readMu)
	go r.receiveLoop()
	go r.retransmitLoop()
	return r
}

func (r *reliableRTP) Write(p []byte) (int, error) {
	r.writeMu.Lock()
	seq := r.writeSeq
	r.writeSeq++
	r.writeMu.Unlock()

	frame := make([]byte, reliableHeaderSize+len(p))
	binary.BigEndian.PutUint16(frame[0:2], seq)
	frame[2] = reliableFlagData
	copy(frame[3:], p)

	r.unackedMu.Lock()
	r.unacked[seq] = &reliableFrame{data: frame, sentAt: time.Now()}
	r.unackedMu.Unlock()

	_, err := r.inner.Write(frame)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (r *reliableRTP) Read(p []byte) (int, error) {
	r.readMu.Lock()
	defer r.readMu.Unlock()

	// Serve leftover bytes from a previously partially-consumed packet first.
	if len(r.readPending) > 0 {
		n := copy(p, r.readPending)
		r.readPending = r.readPending[n:]
		return n, nil
	}

	for {
		select {
		case <-r.done:
			return 0, io.EOF
		default:
		}

		if data, ok := r.readBuf[r.readSeq]; ok {
			delete(r.readBuf, r.readSeq)
			r.readSeq++
			n := copy(p, data)
			if n < len(data) {
				r.readPending = data[n:]
			}
			return n, nil
		}

		r.readCond.Wait()
	}
}

func (r *reliableRTP) Close() error {
	select {
	case <-r.done:
	default:
		close(r.done)
	}
	r.readCond.Broadcast()
	return r.inner.Close()
}

func (r *reliableRTP) receiveLoop() {
	buf := make([]byte, 65536)
	for {
		select {
		case <-r.done:
			return
		default:
		}

		n, err := r.inner.Read(buf)
		if err != nil {
			return
		}
		if n < reliableHeaderSize {
			continue
		}

		seq := binary.BigEndian.Uint16(buf[0:2])
		flags := buf[2]

		switch flags {
		case reliableFlagData, reliableFlagRetransmit:
			payload := make([]byte, n-reliableHeaderSize)
			copy(payload, buf[3:n])

			r.readMu.Lock()
			// Enforce window size to prevent unbounded memory growth
			// from out-of-order or malicious packets.
			if len(r.readBuf) < reliableWindowSize {
				r.readBuf[seq] = payload
			}
			r.readMu.Unlock()
			r.readCond.Signal()

			// Send ACK asynchronously to avoid blocking the receive pipeline.
			// A blocked sendAck (waiting on RTP write) would stall the entire
			// receive loop, causing readCh to fill and packets to be dropped.
			go r.sendAck(seq)

		case reliableFlagAck:
			r.unackedMu.Lock()
			delete(r.unacked, seq)
			r.unackedMu.Unlock()
		}
	}
}

func (r *reliableRTP) sendAck(seq uint16) {
	ack := make([]byte, reliableHeaderSize)
	binary.BigEndian.PutUint16(ack[0:2], seq)
	ack[2] = reliableFlagAck
	r.inner.Write(ack) //nolint:errcheck
}

func (r *reliableRTP) retransmitLoop() {
	ticker := time.NewTicker(reliableAckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			r.unackedMu.Lock()
			now := time.Now()
			for seq, frame := range r.unacked {
				if now.Sub(frame.sentAt) > reliableRTOInit*(1<<frame.retries) {
					if frame.retries >= reliableMaxRetries {
						delete(r.unacked, seq)
						continue
					}
					// Retransmit
					retransmit := make([]byte, len(frame.data))
					copy(retransmit, frame.data)
					retransmit[2] = reliableFlagRetransmit
					frame.retries++
					frame.sentAt = now
					go func(data []byte) {
						r.inner.Write(data) //nolint:errcheck
					}(retransmit)
				}
			}
			r.unackedMu.Unlock()
		}
	}
}

// readTargetAddrReliable reads a target address from a reliable stream.
func readTargetAddrReliable(r io.Reader) (string, error) {
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
