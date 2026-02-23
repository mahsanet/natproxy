package webrtc

import (
	"net"
	"strings"

	"natproxy/golib/applog"
)

// bogonNets are parsed once at init to avoid per-call net.ParseCIDR overhead.
var bogonNets []*net.IPNet

func init() {
	for _, cidr := range []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"100.64.0.0/10",
		"192.0.2.0/24",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"192.0.0.0/24",
		"198.18.0.0/15",
		"240.0.0.0/4",
	} {
		_, network, _ := net.ParseCIDR(cidr)
		bogonNets = append(bogonNets, network)
	}
}

// filterBogonCandidates removes ICE candidates with bogon (non-routable) IP
// addresses from the SDP. This prevents leaking private network addresses and
// avoids wasting time on unreachable candidates.
func filterBogonCandidates(sdp string) string {
	var filtered []string
	removed := 0
	for _, line := range strings.Split(sdp, "\r\n") {
		if strings.HasPrefix(line, "a=candidate:") {
			ip := extractCandidateIP(line)
			if ip != "" && isBogonIPLocal(ip) {
				removed++
				continue
			}
		}
		filtered = append(filtered, line)
	}
	if removed > 0 {
		applog.Infof("webrtc: filtered %d bogon ICE candidates from SDP", removed)
	}
	return strings.Join(filtered, "\r\n")
}

// filterIPv6Candidates removes IPv6 ICE candidates from the SDP when IPv6
// is disabled. This can improve connection speed in networks with broken
// IPv6 connectivity.
func filterIPv6Candidates(sdp string) string {
	var filtered []string
	removed := 0
	for _, line := range strings.Split(sdp, "\r\n") {
		if strings.HasPrefix(line, "a=candidate:") {
			ip := extractCandidateIP(line)
			if parsed := net.ParseIP(ip); ip != "" && parsed != nil && parsed.To4() == nil {
				removed++
				continue
			}
		}
		filtered = append(filtered, line)
	}
	if removed > 0 {
		applog.Infof("webrtc: filtered %d IPv6 ICE candidates from SDP", removed)
	}
	return strings.Join(filtered, "\r\n")
}

// extractCandidateIP extracts the IP address from an ICE candidate line.
// Format: a=candidate:<foundation> <component> <transport> <priority> <ip> <port> ...
func extractCandidateIP(line string) string {
	parts := strings.Fields(line)
	if len(parts) < 6 {
		return ""
	}
	// parts[0] = "a=candidate:<foundation>"
	// parts[4] = <ip>
	return parts[4]
}

// isBogonIPLocal checks if an IP address is a bogon (non-routable) address.
// Includes RFC 1918, loopback, link-local, documentation, benchmarking ranges.
func isBogonIPLocal(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return true // unparseable = bogon
	}
	if parsedIP.IsLoopback() || parsedIP.IsMulticast() || parsedIP.IsUnspecified() ||
		parsedIP.IsLinkLocalUnicast() || parsedIP.IsLinkLocalMulticast() {
		return true
	}
	for _, network := range bogonNets {
		if network.Contains(parsedIP) {
			return true
		}
	}
	return false
}
