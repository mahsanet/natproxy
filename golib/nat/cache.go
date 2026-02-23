package nat

import (
	"sync"
	"time"
)

// natCache caches NAT type detection results with a TTL to avoid
// repeated STUN probes on every connection attempt.
type natCache struct {
	mu      sync.Mutex
	result  *NATType
	updated time.Time
	ttl     time.Duration
}

var globalNATCache = &natCache{ttl: 5 * time.Minute}

// CachedDetectNATType returns a cached NAT type result if available and fresh,
// otherwise performs a full detection and caches the result.
func CachedDetectNATType(server1, server2 string) (*NATType, error) {
	globalNATCache.mu.Lock()
	if globalNATCache.result != nil && time.Since(globalNATCache.updated) < globalNATCache.ttl {
		result := globalNATCache.result
		globalNATCache.mu.Unlock()
		return result, nil
	}
	globalNATCache.mu.Unlock()

	result, err := DetectNATTypeFull(server1, server2)
	if err != nil {
		return nil, err
	}

	globalNATCache.mu.Lock()
	globalNATCache.result = result
	globalNATCache.updated = time.Now()
	globalNATCache.mu.Unlock()

	return result, nil
}

// InvalidateNATCache clears the cached NAT type, forcing re-detection
// on the next call. Should be called on network changes.
func InvalidateNATCache() {
	globalNATCache.mu.Lock()
	globalNATCache.result = nil
	globalNATCache.updated = time.Time{}
	globalNATCache.mu.Unlock()
}
