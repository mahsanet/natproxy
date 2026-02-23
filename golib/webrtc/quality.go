package webrtc

import (
	"encoding/json"
	"sync"
	"time"
)

// QualityTracker tracks connection success/failure per peer for intelligent selection.
type QualityTracker struct {
	mu      sync.Mutex
	records map[string]*peerRecord
}

type peerRecord struct {
	Successes   int       `json:"successes"`
	Failures    int       `json:"failures"`
	LastAttempt time.Time `json:"last_attempt"`
	AvgRTTMs    float64   `json:"avg_rtt_ms"`
}

var globalQualityTracker = &QualityTracker{
	records: make(map[string]*peerRecord),
}

// RecordSuccess records a successful connection to a peer.
func (qt *QualityTracker) RecordSuccess(peer string, rttMs float64) {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	r, ok := qt.records[peer]
	if !ok {
		r = &peerRecord{}
		qt.records[peer] = r
	}
	r.Successes++
	r.LastAttempt = time.Now()
	if r.AvgRTTMs == 0 {
		r.AvgRTTMs = rttMs
	} else {
		r.AvgRTTMs = r.AvgRTTMs*0.7 + rttMs*0.3
	}
}

// RecordFailure records a failed connection attempt to a peer.
func (qt *QualityTracker) RecordFailure(peer string) {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	r, ok := qt.records[peer]
	if !ok {
		r = &peerRecord{}
		qt.records[peer] = r
	}
	r.Failures++
	r.LastAttempt = time.Now()
}

// Score returns a quality score for a peer (0.0-1.0).
func (qt *QualityTracker) Score(peer string) float64 {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	r, ok := qt.records[peer]
	if !ok {
		return 0.5
	}

	total := r.Successes + r.Failures
	if total == 0 {
		return 0.5
	}

	score := float64(r.Successes) / float64(total)

	age := time.Since(r.LastAttempt).Hours()
	if age > 0 {
		decay := 1.0 / (1.0 + age/24.0)
		score = 0.5 + (score-0.5)*decay
	}

	return score
}

// ToJSON returns the quality data as a JSON string.
func (qt *QualityTracker) ToJSON() string {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	data, _ := json.Marshal(qt.records)
	return string(data)
}

// GetConnectionQuality returns quality data for all tracked peers as JSON.
func GetConnectionQuality() string {
	return globalQualityTracker.ToJSON()
}
