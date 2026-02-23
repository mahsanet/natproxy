package nat

import (
	"fmt"
	"math/rand"
	"net"
)

// Pre-parsed CIDR ranges to avoid per-call net.ParseCIDR overhead.
var (
	privateNets []*net.IPNet
	bogonNetsNat []*net.IPNet
)

func init() {
	for _, cidr := range []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"100.64.0.0/10",
	} {
		_, network, _ := net.ParseCIDR(cidr)
		privateNets = append(privateNets, network)
	}
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
		bogonNetsNat = append(bogonNetsNat, network)
	}
}

// getLocalIP returns the non-loopback local IPv4 address by dialing a
// well-known address. This avoids net.Interfaces()/net.InterfaceAddrs()
// which fail on Android with "netlinkrib: permission denied".
func getLocalIP() (string, error) {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return "", fmt.Errorf("dial for local IP: %w", err)
	}
	defer conn.Close()

	addr := conn.LocalAddr().(*net.UDPAddr)
	if addr.IP.IsLoopback() || addr.IP.To4() == nil {
		return "", net.InvalidAddrError("no local IP found")
	}
	return addr.IP.String(), nil
}

// isPrivateIP checks if an IP address is in a private range.
func isPrivateIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	for _, network := range privateNets {
		if network.Contains(parsedIP) {
			return true
		}
	}
	return false
}

// randomPort returns a random port in the 10000-60000 range.
func randomPort() int {
	return 10000 + rand.Intn(50000)
}

// isBogonIP checks if an IP address is a bogon (non-routable) address.
// Used to validate STUN OTHER-ADDRESS responses.
func isBogonIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return true // unparseable = bogon
	}
	if parsedIP.IsLoopback() || parsedIP.IsMulticast() || parsedIP.IsUnspecified() ||
		parsedIP.IsLinkLocalUnicast() || parsedIP.IsLinkLocalMulticast() {
		return true
	}
	for _, network := range bogonNetsNat {
		if network.Contains(parsedIP) {
			return true
		}
	}
	return false
}
