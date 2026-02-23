package main

import "time"

const (
	cleanupInterval       = 1 * time.Minute
	sessionExpiry         = 5 * time.Minute
	listingHeartbeatExpiry = 90 * time.Second
	maxBodySize           = 16384 // 16KB: SDP offers/answers can be ~4KB each
	maxNameLength         = 50
)

func cleanupLoop(relay *UDPRelay) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		mu.Lock()
		cutoff := time.Now().Add(-sessionExpiry)
		for id, s := range sessions {
			if s.lastActive.Before(cutoff) {
				delete(sessions, id)
			}
		}
		mu.Unlock()

		var expired bool
		lmu.Lock()
		listingCutoff := time.Now().Add(-listingHeartbeatExpiry)
		for id, l := range listings {
			if l.LastHeartbeat.Before(listingCutoff) {
				delete(listings, id)
				expired = true
			}
		}
		lmu.Unlock()
		if expired {
			broadcastListings()
		}

		// Safety-net: remove SSE subscribers whose done channel is closed.
		ssesMu.Lock()
		for sub := range subscribers {
			select {
			case <-sub.done:
				delete(subscribers, sub)
			default:
			}
		}
		ssesMu.Unlock()

		// Safety-net: remove offer SSE subscribers whose done channel is closed.
		offerSubsMu.Lock()
		for sid, subs := range offerSubs {
			alive := subs[:0]
			for _, sub := range subs {
				select {
				case <-sub.done:
				default:
					alive = append(alive, sub)
				}
			}
			if len(alive) == 0 {
				delete(offerSubs, sid)
			} else {
				offerSubs[sid] = alive
			}
		}
		offerSubsMu.Unlock()

		if relay != nil {
			relay.CleanupExpired()
		}
	}
}
