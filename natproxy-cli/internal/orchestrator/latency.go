package orchestrator

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/proxy"
)

const (
	latencyTestURL        = "http://cp.cloudflare.com"
	latencyDialTimeout    = 5 * time.Second
	latencyRequestTimeout = 10 * time.Second
)

// TestLatency measures round-trip time through the local SOCKS5 proxy.
// Returns latency in milliseconds and any error.
func TestLatency(socksPort int) (int64, error) {
	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, &net.Dialer{Timeout: latencyDialTimeout})
	if err != nil {
		return 0, fmt.Errorf("socks5 dialer: %w", err)
	}

	transport := &http.Transport{
		Dial: dialer.Dial,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   latencyRequestTimeout,
	}

	start := time.Now()
	resp, err := client.Get(latencyTestURL)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	resp.Body.Close()
	return time.Since(start).Milliseconds(), nil
}
