package webrtc

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"sync"
)

const (
	paddingFlagNone     = 0x00 // no padding, real data
	paddingFlagPad      = 0x01 // padded real data
	paddingFlagDecoy    = 0x02 // decoy message (discard entirely)
	paddingFlagBurstEnd = 0x03 // padding burst end signal
)

// paddedStream wraps an io.ReadWriteCloser, adding random-length padding to
// each write to obscure traffic patterns. The peer must also use paddedStream
// to correctly strip the padding on reads.
//
// Wire format per write:
//
//	flags 0x00: [payloadLen(2)][payload(payloadLen)]
//	flags 0x01: [padLen(2)][pad(padLen)][payloadLen(2)][payload(payloadLen)]
//	flags 0x02: [dataLen(2)][random(dataLen)]  (decoy, silently discarded)
//	flags 0x03: (no payload, silently consumed)
type paddedStream struct {
	inner  io.ReadWriteCloser
	minPad int
	maxPad int

	// Padding burst — sent once before the first real write.
	burstOnce sync.Once // ensures burst is sent at most once

	// Read state: buffered data from a partially consumed frame
	readBuf []byte

	// Write state: reusable buffer to avoid per-Write allocation.
	// Safe because writes are serialized per paddedStream (one writer
	// per relay direction, burst writes go to inner directly).
	writeBuf []byte
}

func newPaddedStream(inner io.ReadWriteCloser, minPad, maxPad int) *paddedStream {
	return &paddedStream{
		inner:  inner,
		minPad: minPad,
		maxPad: maxPad,
	}
}

// growWriteBuf returns ps.writeBuf with at least n bytes capacity,
// growing it if needed. Avoids per-Write allocation after warmup.
func (ps *paddedStream) growWriteBuf(n int) []byte {
	if cap(ps.writeBuf) < n {
		ps.writeBuf = make([]byte, n)
	}
	return ps.writeBuf[:n]
}

func (ps *paddedStream) Write(p []byte) (int, error) {
	// Send padding burst once before the first real write.
	ps.burstOnce.Do(func() { sendPaddingBurst(ps) })

	padLen := 0
	if ps.maxPad > ps.minPad {
		padLen = ps.minPad + rand.Intn(ps.maxPad-ps.minPad)
	} else if ps.maxPad > 0 {
		padLen = ps.minPad
	}

	if padLen == 0 {
		// Fast path: no padding
		// [flags(1)][payloadLen(2)][payload]
		buf := ps.growWriteBuf(1 + 2 + len(p))
		buf[0] = paddingFlagNone
		binary.BigEndian.PutUint16(buf[1:3], uint16(len(p)))
		copy(buf[3:], p)
		_, err := ps.inner.Write(buf)
		if err != nil {
			return 0, err
		}
		return len(p), nil
	}

	// [flags(1)][padLen(2)][pad(padLen)][payloadLen(2)][payload]
	buf := ps.growWriteBuf(1 + 2 + padLen + 2 + len(p))
	buf[0] = paddingFlagPad
	binary.BigEndian.PutUint16(buf[1:3], uint16(padLen))
	rand.Read(buf[3 : 3+padLen])
	binary.BigEndian.PutUint16(buf[3+padLen:5+padLen], uint16(len(p)))
	copy(buf[5+padLen:], p)

	_, err := ps.inner.Write(buf)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// WriteDecoy sends a decoy message that the peer will silently discard.
func (ps *paddedStream) WriteDecoy(size int) error {
	if size <= 0 {
		size = 64 + rand.Intn(961) // 64-1024 bytes
	}
	// [flags(1)][dataLen(2)][random data]
	buf := make([]byte, 1+2+size)
	buf[0] = paddingFlagDecoy
	binary.BigEndian.PutUint16(buf[1:3], uint16(size))
	rand.Read(buf[3:])
	_, err := ps.inner.Write(buf)
	return err
}

// WriteBurstEnd sends the burst-end signal.
func (ps *paddedStream) WriteBurstEnd() error {
	_, err := ps.inner.Write([]byte{paddingFlagBurstEnd})
	return err
}

func (ps *paddedStream) Read(p []byte) (int, error) {
	// Serve from buffer first
	if len(ps.readBuf) > 0 {
		n := copy(p, ps.readBuf)
		ps.readBuf = ps.readBuf[n:]
		return n, nil
	}

	for {
		// Read flags byte
		var flags [1]byte
		if _, err := io.ReadFull(ps.inner, flags[:]); err != nil {
			return 0, err
		}

		switch flags[0] {
		case paddingFlagNone:
			// Read 2-byte payload length
			var lenBuf [2]byte
			if _, err := io.ReadFull(ps.inner, lenBuf[:]); err != nil {
				return 0, fmt.Errorf("padding: read payload len: %w", err)
			}
			payloadLen := int(binary.BigEndian.Uint16(lenBuf[:]))
			if payloadLen == 0 {
				continue
			}

			// Read full payload
			payload := make([]byte, payloadLen)
			if _, err := io.ReadFull(ps.inner, payload); err != nil {
				return 0, fmt.Errorf("padding: read payload: %w", err)
			}

			// Return what fits in p, buffer the rest
			n := copy(p, payload)
			if n < payloadLen {
				ps.readBuf = payload[n:]
			}
			return n, nil

		case paddingFlagPad:
			// Read 2-byte pad length
			var padLenBuf [2]byte
			if _, err := io.ReadFull(ps.inner, padLenBuf[:]); err != nil {
				return 0, fmt.Errorf("padding: read padLen: %w", err)
			}
			padLen := int(binary.BigEndian.Uint16(padLenBuf[:]))

			// Discard pad bytes without allocating
			if padLen > 0 {
				if _, err := io.CopyN(io.Discard, ps.inner, int64(padLen)); err != nil {
					return 0, fmt.Errorf("padding: discard pad: %w", err)
				}
			}

			// Read 2-byte payload length
			var lenBuf [2]byte
			if _, err := io.ReadFull(ps.inner, lenBuf[:]); err != nil {
				return 0, fmt.Errorf("padding: read payload len: %w", err)
			}
			payloadLen := int(binary.BigEndian.Uint16(lenBuf[:]))
			if payloadLen == 0 {
				continue
			}

			// Read full payload
			payload := make([]byte, payloadLen)
			if _, err := io.ReadFull(ps.inner, payload); err != nil {
				return 0, fmt.Errorf("padding: read payload: %w", err)
			}

			// Return what fits in p, buffer the rest
			n := copy(p, payload)
			if n < payloadLen {
				ps.readBuf = payload[n:]
			}
			return n, nil

		case paddingFlagDecoy:
			// Decoy: read 2-byte length + discard data, then loop to next frame.
			var decoyLenBuf [2]byte
			if _, err := io.ReadFull(ps.inner, decoyLenBuf[:]); err != nil {
				return 0, fmt.Errorf("padding: read decoy len: %w", err)
			}
			decoyLen := int(binary.BigEndian.Uint16(decoyLenBuf[:]))
			if decoyLen > 0 {
				if _, err := io.CopyN(io.Discard, ps.inner, int64(decoyLen)); err != nil {
					return 0, fmt.Errorf("padding: discard decoy: %w", err)
				}
			}
			// Silently consumed — loop to read next frame.
			continue

		case paddingFlagBurstEnd:
			// Burst-end signal: silently consumed, loop to read next frame.
			continue

		default:
			return 0, fmt.Errorf("padding: unknown flags byte: 0x%02x", flags[0])
		}
	}
}

func (ps *paddedStream) Close() error {
	return ps.inner.Close()
}
