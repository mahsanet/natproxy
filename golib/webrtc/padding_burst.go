package webrtc

import (
	"encoding/binary"
	"math/rand"

	"natproxy/golib/applog"
)

// sendPaddingBurst sends 3-8 decoy frames then a burst-end signal as a single
// concatenated write. Batching into one write reduces SCTP chunks from 4-9
// down to 1, cutting the latency added to the first real write.
func sendPaddingBurst(ps *paddedStream) {
	count := 3 + rand.Intn(6) // 3-8 frames

	// Pre-calculate total size for a single allocation.
	sizes := make([]int, count)
	totalSize := 0
	for i := range sizes {
		sizes[i] = 64 + rand.Intn(961) // 64-1024 bytes per decoy
		totalSize += 1 + 2 + sizes[i]  // flags + len + data
	}
	totalSize += 1 // burst-end flag

	buf := make([]byte, totalSize)
	offset := 0
	for i, size := range sizes {
		buf[offset] = paddingFlagDecoy
		binary.BigEndian.PutUint16(buf[offset+1:offset+3], uint16(size))
		rand.Read(buf[offset+3 : offset+3+size])
		offset += 1 + 2 + size
		_ = i
	}
	buf[offset] = paddingFlagBurstEnd

	if _, err := ps.inner.Write(buf); err != nil {
		applog.Warnf("webrtc: padding burst failed: %v", err)
		return
	}
	applog.Infof("webrtc: sent padding burst (%d decoy frames, %d bytes)", count, totalSize)
}
