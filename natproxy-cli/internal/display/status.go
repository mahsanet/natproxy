package display

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// ServerMetrics contains all server-side metrics for status display.
type ServerMetrics struct {
	ClientCount  int
	Method       string
	Uptime       time.Duration
	BytesUp      int64
	BytesDown    int64
	DataChannels int
	PeerConns    int
	SmuxStreams  int
	StreamDist   []int
	Goroutines   int
	HeapMB       float64
}

// ClientMetrics contains all client-side metrics for status display.
type ClientMetrics struct {
	Connected    bool
	SocksAddr    string
	LatencyMs    int
	DataChannels int
	PeerConns    int
	SmuxStreams  int
}

// rateTracker computes bytes/sec from cumulative counters via delta/dt.
type rateTracker struct {
	mu       sync.Mutex
	lastUp   int64
	lastDown int64
	lastTime time.Time
	rateUp   float64
	rateDown float64
}

func (rt *rateTracker) update(bytesUp, bytesDown int64) (rateUp, rateDown float64) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	now := time.Now()
	if !rt.lastTime.IsZero() {
		dt := now.Sub(rt.lastTime).Seconds()
		if dt > 0 {
			rt.rateUp = float64(bytesUp-rt.lastUp) / dt
			rt.rateDown = float64(bytesDown-rt.lastDown) / dt
		}
	}
	rt.lastUp = bytesUp
	rt.lastDown = bytesDown
	rt.lastTime = now
	return rt.rateUp, rt.rateDown
}

func formatRate(bytesPerSec float64) string {
	return FormatBytes(int64(bytesPerSec)) + "/s"
}

// ServerStatusFunc is called periodically to get server metrics.
type ServerStatusFunc func() ServerMetrics

// ClientStatusFunc is called periodically to get client metrics.
type ClientStatusFunc func() ClientMetrics

// ShowServerStatus refreshes a status line on stderr every 2 seconds.
func ShowServerStatus(done <-chan struct{}, fn ServerStatusFunc) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	rt := &rateTracker{}

	for {
		select {
		case <-done:
			fmt.Fprint(os.Stderr, "\r\033[K")
			return
		case <-ticker.C:
			m := fn()
			rateUp, rateDown := rt.update(m.BytesUp, m.BytesDown)

			method := m.Method
			if method == "" {
				method = "starting"
			}

			line := fmt.Sprintf("\r\033[KClients: %d | %s | Up: %s (%s) | Down: %s (%s)",
				m.ClientCount,
				method,
				FormatBytes(m.BytesUp), formatRate(rateUp),
				FormatBytes(m.BytesDown), formatRate(rateDown),
			)

			if m.PeerConns > 0 {
				line += fmt.Sprintf(" | PCs: %d", m.PeerConns)
			}
			if m.DataChannels > 0 {
				line += fmt.Sprintf(" | DC: %d", m.DataChannels)
			}
			if m.SmuxStreams > 0 {
				line += fmt.Sprintf(" | Streams: %d", m.SmuxStreams)
			}

			line += fmt.Sprintf(" | %s", FormatDuration(int(m.Uptime.Seconds())))

			fmt.Fprint(os.Stderr, line)
		}
	}
}

// ShowClientStatus refreshes a status line on stderr every second.
func ShowClientStatus(done <-chan struct{}, fn ClientStatusFunc) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			fmt.Fprint(os.Stderr, "\r\033[K")
			return
		case <-ticker.C:
			m := fn()
			if !m.Connected {
				continue
			}

			latencyStr := "N/A"
			if m.LatencyMs > 0 {
				latencyStr = fmt.Sprintf("%dms", m.LatencyMs)
			}

			line := fmt.Sprintf("\r\033[KConnected | SOCKS5: %s | Latency: %s",
				m.SocksAddr, latencyStr)

			if m.PeerConns > 0 {
				line += fmt.Sprintf(" | PCs: %d", m.PeerConns)
			}
			if m.DataChannels > 0 {
				line += fmt.Sprintf(" | DC: %d", m.DataChannels)
			}
			if m.SmuxStreams > 0 {
				line += fmt.Sprintf(" | Streams: %d", m.SmuxStreams)
			}

			fmt.Fprint(os.Stderr, line)
		}
	}
}
