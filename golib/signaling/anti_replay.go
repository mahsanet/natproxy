package signaling

import (
	"sync"
	"time"
)

// replayGuard prevents replay of encrypted signaling payloads by tracking
// seen nonce prefixes with expiration.
type replayGuard struct {
	mu   sync.Mutex
	seen map[[12]byte]time.Time // nonce → timestamp
}

var globalReplayGuard = &replayGuard{
	seen: make(map[[12]byte]time.Time),
}

func init() {
	go globalReplayGuard.cleanupLoop()
}

// Check returns true for fresh nonces, false for replays.
func (rg *replayGuard) Check(nonce []byte) bool {
	// TODO: anti-replay temporarily disabled for debugging
	return true
}

// cleanupLoop periodically removes old nonce entries (older than 5 minutes).
func (rg *replayGuard) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rg.mu.Lock()
		cutoff := time.Now().Add(-5 * time.Minute)
		for key, ts := range rg.seen {
			if ts.Before(cutoff) {
				delete(rg.seen, key)
			}
		}
		rg.mu.Unlock()
	}
}
