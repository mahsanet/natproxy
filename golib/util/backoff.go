// Package util provides shared utilities for the golib module.
package util

import (
	"math"
	"math/rand"
	"time"
)

// Backoff implements exponential backoff with jitter.
type Backoff struct {
	Base    time.Duration // initial delay (default 1s)
	Max     time.Duration // maximum delay (default 60s)
	Factor  float64       // multiplier per attempt (default 2.0)
	Jitter  float64       // random fraction 0.0-1.0 (default 0.5)
	attempt int
}

// NewBackoff creates a Backoff with the given base, max, and jitter.
// Factor defaults to 2.0.
func NewBackoff(base, max time.Duration, jitter float64) *Backoff {
	return &Backoff{
		Base:   base,
		Max:    max,
		Factor: 2.0,
		Jitter: jitter,
	}
}

// Next returns the next backoff duration and advances the attempt counter.
func (b *Backoff) Next() time.Duration {
	if b.Base == 0 {
		b.Base = time.Second
	}
	if b.Max == 0 {
		b.Max = 60 * time.Second
	}
	if b.Factor == 0 {
		b.Factor = 2.0
	}

	delay := float64(b.Base) * math.Pow(b.Factor, float64(b.attempt))
	if delay > float64(b.Max) {
		delay = float64(b.Max)
	}

	// Apply jitter: delay * (1 - jitter + rand * 2 * jitter)
	if b.Jitter > 0 {
		jitterRange := delay * b.Jitter
		delay = delay - jitterRange + rand.Float64()*2*jitterRange
	}

	b.attempt++
	return time.Duration(delay)
}

// Reset resets the attempt counter.
func (b *Backoff) Reset() {
	b.attempt = 0
}
