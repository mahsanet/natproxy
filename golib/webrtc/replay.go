package webrtc

import "sync"

const replayWindowSize = 8192 // packets tracked

// replayFilter implements a sliding-window bitmap for anti-replay protection.
// It tracks the highest seen sequence number and a bitset of recently seen
// sequence numbers within the window.
type replayFilter struct {
	mu     sync.Mutex
	maxSeq uint64
	bitmap [128]uint64 // 128 * 64 = 8192 bits
}

func newReplayFilter() *replayFilter {
	return &replayFilter{}
}

// Check returns true if the sequence number is new, false if it's a replay or outside the window.
// Marks the sequence as seen on success.
func (rf *replayFilter) Check(seq uint64) bool {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if seq > rf.maxSeq {
		// New high: advance the window
		shift := seq - rf.maxSeq
		if shift >= replayWindowSize {
			// Entire window is stale, clear it
			rf.bitmap = [128]uint64{}
		} else {
			rf.advanceWindow(shift)
		}
		rf.maxSeq = seq
		rf.setBit(0) // current seq is at position 0 relative to maxSeq
		return true
	}

	// Within window?
	diff := rf.maxSeq - seq
	if diff >= replayWindowSize {
		return false // too old
	}

	// Check if already seen
	if rf.getBit(diff) {
		return false // duplicate
	}

	rf.setBit(diff)
	return true
}

func (rf *replayFilter) setBit(offset uint64) {
	idx := offset / 64
	bit := offset % 64
	rf.bitmap[idx] |= 1 << bit
}

func (rf *replayFilter) getBit(offset uint64) bool {
	idx := offset / 64
	bit := offset % 64
	return rf.bitmap[idx]&(1<<bit) != 0
}

func (rf *replayFilter) advanceWindow(shift uint64) {
	// Shift the bitmap by 'shift' bits to make room for new sequence numbers.
	// This is equivalent to shifting the entire 8192-bit bitset right by 'shift'.
	wordShift := shift / 64
	bitShift := shift % 64

	if wordShift >= 128 {
		rf.bitmap = [128]uint64{}
		return
	}

	// Shift by whole words first
	if wordShift > 0 {
		for i := uint64(127); i >= wordShift; i-- {
			rf.bitmap[i] = rf.bitmap[i-wordShift]
		}
		for i := uint64(0); i < wordShift; i++ {
			rf.bitmap[i] = 0
		}
	}

	// Shift by remaining bits
	if bitShift > 0 {
		for i := 127; i > 0; i-- {
			rf.bitmap[i] = (rf.bitmap[i] << bitShift) | (rf.bitmap[i-1] >> (64 - bitShift))
		}
		rf.bitmap[0] <<= bitShift
	}
}
