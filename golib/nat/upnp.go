package nat

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sync"
	"time"

	"natproxy/golib/applog"

	"github.com/huin/goupnp"
	"github.com/huin/goupnp/dcps/internetgateway2"
	"github.com/huin/goupnp/httpu"
	"github.com/huin/goupnp/ssdp"
)

const (
	upnpMappingRetries   = 3
	upnpLeaseDuration    = 3600 // seconds (1 hour)
	upnpLeaseRenewEvery  = 50 * time.Minute
	ssdpSearchTimeout    = 3 * time.Second
	ssdpExpectedReplies  = 2
	bruteForceDialTimout = 300 * time.Millisecond
)

// igdClient abstracts over WANIPConnection1 and WANIPConnection2.
type igdClient interface {
	GetExternalIPAddress() (string, error)
	AddPortMapping(remoteHost string, externalPort uint16, protocol string,
		internalPort uint16, internalClient string, enabled bool,
		description string, leaseDuration uint32) error
	DeletePortMapping(remoteHost string, externalPort uint16, protocol string) error
}

var (
	upnpMu       sync.Mutex
	activeLeases []upnpLease
)

type upnpLease struct {
	port     int
	protocol string
	client   igdClient
	stopCh   chan struct{}
}

// UPnPOptions configures UPnP port mapping behavior.
type UPnPOptions struct {
	LeaseDuration  int           // seconds (0 = indefinite)
	MappingRetries int
	SSDPTimeout    time.Duration
}

// TryUPnP maps a port through the router via UPnP IGD.
func TryUPnP(internalPort, externalPort int, protocol string, opts UPnPOptions) (string, int, error) {
	// Apply defaults for zero values
	if opts.LeaseDuration < 0 {
		opts.LeaseDuration = upnpLeaseDuration
	}
	if opts.MappingRetries <= 0 {
		opts.MappingRetries = upnpMappingRetries
	}
	if opts.SSDPTimeout <= 0 {
		opts.SSDPTimeout = ssdpSearchTimeout
	}
	upnpMu.Lock()
	defer upnpMu.Unlock()

	applog.Info("Discovering UPnP gateway...")
	clients, err := discoverUPnPClients(opts.SSDPTimeout)
	if err != nil {
		return "", 0, fmt.Errorf("UPnP discovery failed: %w", err)
	}
	if len(clients) == 0 {
		applog.Warn("No UPnP IGD devices found")
		return "", 0, fmt.Errorf("no UPnP IGD devices found")
	}
	applog.Infof("Found %d UPnP device(s)", len(clients))

	localIP, err := getLocalIP()
	if err != nil {
		return "", 0, fmt.Errorf("get local IP: %w", err)
	}
	applog.Infof("Local IP for UPnP: %s", localIP)

	var lastErr error
	for i, client := range clients {
		applog.Infof("Trying UPnP client %d/%d...", i+1, len(clients))
		externalIP, err := client.GetExternalIPAddress()
		if err != nil {
			applog.Warnf("UPnP client %d: GetExternalIPAddress failed: %v", i+1, err)
			lastErr = err
			continue
		}
		applog.Infof("UPnP client %d: external IP = %s", i+1, externalIP)

		if isPrivateIP(externalIP) {
			applog.Warnf("UPnP client %d: double NAT detected (external IP %s is private)", i+1, externalIP)
			lastErr = fmt.Errorf("double NAT detected: external IP %s is private", externalIP)
			continue
		}

		for attempts := 0; attempts < opts.MappingRetries; attempts++ {
			port := externalPort
			if attempts > 0 {
				port = randomPort()
			}

			applog.Infof("UPnP client %d: AddPortMapping(%s, ext=%d, %s, int=%d, %s, lease=%d)...",
				i+1, protocol, port, protocol, internalPort, localIP, opts.LeaseDuration)
			err = client.AddPortMapping(
				"",           // NewRemoteHost (empty = any)
				uint16(port), // NewExternalPort
				protocol,     // NewProtocol ("TCP" or "UDP")
				uint16(internalPort),
				localIP, // NewInternalClient
				true,    // NewEnabled
				"NATProxy",
				uint32(opts.LeaseDuration),
			)
			if err != nil {
				applog.Warnf("UPnP client %d: AddPortMapping failed (attempt %d, port %d): %v", i+1, attempts+1, port, err)
				lastErr = err

				// Some routers reject non-zero lease durations; retry with 0 (indefinite)
				if attempts == 0 {
					applog.Info("Retrying with leaseDuration=0 (indefinite)...")
					err = client.AddPortMapping("", uint16(port), protocol,
						uint16(internalPort), localIP, true, "NATProxy", 0)
					if err == nil {
						applog.Successf("UPnP port mapping added (indefinite lease): %s:%d", externalIP, port)
						lease := upnpLease{
							port:     port,
							protocol: protocol,
							client:   client,
							stopCh:   make(chan struct{}),
						}
						activeLeases = append(activeLeases, lease)
						go renewLease(lease, internalPort, localIP, 0)
						return externalIP, port, nil
					}
					applog.Warnf("UPnP client %d: AddPortMapping with lease=0 also failed: %v", i+1, err)
					lastErr = err
				}
				continue
			}

			lease := upnpLease{
				port:     port,
				protocol: protocol,
				client:   client,
				stopCh:   make(chan struct{}),
			}
			activeLeases = append(activeLeases, lease)
			go renewLease(lease, internalPort, localIP, uint32(opts.LeaseDuration))

			applog.Successf("UPnP port mapping added: %s:%d", externalIP, port)
			return externalIP, port, nil
		}
	}

	applog.Errorf("UPnP port mapping failed after trying %d client(s): %v", len(clients), lastErr)
	return "", 0, fmt.Errorf("UPnP port mapping failed: %w", lastErr)
}

// discoverUPnPClients finds IGD clients using Android-safe methods.
// Avoids goupnp's default discovery which calls net.Interfaces() (blocked on Android).
func discoverUPnPClients(ssdpTimeout time.Duration) ([]igdClient, error) {
	// Method 1: Manual SSDP with explicit local address binding
	clients, err := discoverViaSSDP(ssdpTimeout)
	if err == nil && len(clients) > 0 {
		return clients, nil
	}
	if err != nil {
		applog.Warnf("SSDP discovery failed: %v", err)
	}

	// Method 2: Brute-force common gateway UPnP URLs
	applog.Info("Trying brute-force gateway discovery...")
	clients, err = discoverBruteForce()
	if err == nil && len(clients) > 0 {
		return clients, nil
	}
	if err != nil {
		applog.Warnf("Brute-force discovery failed: %v", err)
	}

	return nil, fmt.Errorf("no UPnP devices found via SSDP or brute-force")
}

// discoverViaSSDP performs manual SSDP multicast using httpu.NewHTTPUClientAddr
// which binds directly to a local address without calling net.Interfaces().
func discoverViaSSDP(ssdpTimeout time.Duration) ([]igdClient, error) {
	localIP, err := getLocalIP()
	if err != nil {
		return nil, fmt.Errorf("get local IP: %w", err)
	}

	applog.Infof("SSDP search from %s...", localIP)
	httpuClient, err := httpu.NewHTTPUClientAddr(localIP)
	if err != nil {
		return nil, fmt.Errorf("create HTTPU client: %w", err)
	}
	defer httpuClient.Close()

	// Search targets, each with its own fresh context/timeout
	searchTargets := []string{
		"urn:schemas-upnp-org:service:WANIPConnection:2",
		"urn:schemas-upnp-org:service:WANIPConnection:1",
		"urn:schemas-upnp-org:service:WANPPPConnection:1",
		"urn:schemas-upnp-org:device:InternetGatewayDevice:1",
		"urn:schemas-upnp-org:device:InternetGatewayDevice:2",
		ssdp.SSDPAll,
	}

	// Deduplicate device locations
	seen := map[string]bool{}
	var locations []*url.URL

	for _, target := range searchTargets {
		// Fresh context per target to avoid timeout exhaustion
		ctx, cancel := context.WithTimeout(context.Background(), ssdpTimeout)
		responses, err := ssdp.RawSearch(ctx, httpuClient, target, ssdpExpectedReplies)
		cancel()

		if err != nil {
			applog.Warnf("SSDP search for %s failed: %v", target, err)
			continue
		}
		applog.Infof("SSDP got %d responses for %s", len(responses), target)

		for _, resp := range responses {
			loc, err := resp.Location()
			if err != nil {
				continue
			}
			locStr := loc.String()
			if !seen[locStr] {
				seen[locStr] = true
				locations = append(locations, loc)
				applog.Infof("Found device at %s", locStr)
			}
		}
	}

	return clientsFromLocations(locations)
}

// clientsFromLocations fetches device descriptions and extracts IGD clients.
// Tries WANIPConnection2, WANIPConnection1, and WANPPPConnection1.
// WANPPPConnection1 is used by PPPoE routers (Bell Canada, AT&T, BT, etc.).
func clientsFromLocations(locations []*url.URL) ([]igdClient, error) {
	var allClients []igdClient

	for _, loc := range locations {
		// Fetch the root device description once
		applog.Infof("Fetching device description at %s...", loc)
		root, err := goupnp.DeviceByURL(loc)
		if err != nil {
			applog.Warnf("Failed to fetch device at %s: %v", loc, err)
			continue
		}

		// Try WANIPConnection2
		v2clients, err := internetgateway2.NewWANIPConnection2ClientsFromRootDevice(root, loc)
		if err != nil {
			applog.Infof("WANIPConnection2 not available at %s: %v", loc, err)
		} else if len(v2clients) == 0 {
			applog.Infof("WANIPConnection2 returned 0 clients at %s", loc)
		} else {
			for _, c := range v2clients {
				applog.Successf("Found WANIPConnection2 service at %s", loc)
				allClients = append(allClients, c)
			}
		}

		// Try WANIPConnection1
		v1clients, err := internetgateway2.NewWANIPConnection1ClientsFromRootDevice(root, loc)
		if err != nil {
			applog.Infof("WANIPConnection1 not available at %s: %v", loc, err)
		} else if len(v1clients) == 0 {
			applog.Infof("WANIPConnection1 returned 0 clients at %s", loc)
		} else {
			for _, c := range v1clients {
				applog.Successf("Found WANIPConnection1 service at %s", loc)
				allClients = append(allClients, c)
			}
		}

		// Try WANPPPConnection1 (PPPoE routers: Bell Canada, AT&T, BT, Deutsche Telekom, etc.)
		pppClients, err := internetgateway2.NewWANPPPConnection1ClientsFromRootDevice(root, loc)
		if err != nil {
			applog.Infof("WANPPPConnection1 not available at %s: %v", loc, err)
		} else if len(pppClients) == 0 {
			applog.Infof("WANPPPConnection1 returned 0 clients at %s", loc)
		} else {
			for _, c := range pppClients {
				applog.Successf("Found WANPPPConnection1 (PPPoE) service at %s", loc)
				allClients = append(allClients, c)
			}
		}
	}

	applog.Infof("clientsFromLocations: found %d IGD client(s) total from %d location(s)", len(allClients), len(locations))
	return allClients, nil
}

// discoverBruteForce tries common gateway IPs and UPnP description URLs.
func discoverBruteForce() ([]igdClient, error) {
	localIP, err := getLocalIP()
	if err != nil {
		return nil, fmt.Errorf("get local IP: %w", err)
	}

	gatewayIPs := deriveGatewayIPs(localIP)
	applog.Infof("Trying %d candidate gateway IPs...", len(gatewayIPs))

	// Common UPnP description paths (ordered by likelihood)
	descPaths := []string{
		"/rootDesc.xml",
		"/description.xml",
		"/upnp/IGD.xml",
		"/igd.xml",
		"/IGatewayDeviceDescDoc",
		"/gatedesc.xml",
		"/DeviceDescription.xml",
	}
	// Common UPnP ports (ordered by likelihood)
	descPorts := []int{49152, 5000, 49153, 1900, 2869, 5431, 49000, 8080, 80}

	// First pass: find which gateways have ANY open UPnP port (fast scan)
	type openTarget struct {
		ip   string
		port int
	}
	var openTargets []openTarget

	for _, gw := range gatewayIPs {
		for _, port := range descPorts {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", gw, port), bruteForceDialTimout)
			if err != nil {
				continue
			}
			conn.Close()
			applog.Infof("Port %s:%d is open", gw, port)
			openTargets = append(openTargets, openTarget{gw, port})
		}
	}

	if len(openTargets) == 0 {
		return nil, fmt.Errorf("no open UPnP ports found on candidate gateways")
	}

	// Second pass: try description paths on open ports only
	var locations []*url.URL
	for _, target := range openTargets {
		for _, path := range descPaths {
			descURL := fmt.Sprintf("http://%s:%d%s", target.ip, target.port, path)
			loc, _ := url.Parse(descURL)

			// Quick check: can we actually fetch the XML?
			root, err := goupnp.DeviceByURL(loc)
			if err != nil {
				continue
			}
			_ = root
			applog.Successf("Brute-force found UPnP device at %s", descURL)
			locations = append(locations, loc)
			// Found a valid device on this port, no need to try more paths
			break
		}
	}

	return clientsFromLocations(locations)
}

// deriveGatewayIPs returns probable gateway IPs based on the local IP.
func deriveGatewayIPs(localIP string) []string {
	ip := net.ParseIP(localIP).To4()
	if ip == nil {
		return []string{"192.168.1.1", "192.168.0.1", "10.0.0.1"}
	}

	// Most likely: .1 of the same subnet
	subnet := fmt.Sprintf("%d.%d.%d", ip[0], ip[1], ip[2])
	gateways := []string{
		subnet + ".1",
		subnet + ".254",
	}

	wellKnown := []string{
		"192.168.1.1", "192.168.0.1", "192.168.2.1",
		"10.0.0.1", "10.0.0.138", "10.0.1.1", "172.16.0.1",
	}
	seen := map[string]bool{}
	for _, g := range gateways {
		seen[g] = true
	}
	for _, g := range wellKnown {
		if !seen[g] {
			gateways = append(gateways, g)
			seen[g] = true
		}
	}

	return gateways
}

// RemoveUPnPMapping removes an active UPnP port mapping.
func RemoveUPnPMapping(externalPort int, protocol string) {
	upnpMu.Lock()
	defer upnpMu.Unlock()

	for i, lease := range activeLeases {
		if lease.port == externalPort && lease.protocol == protocol {
			close(lease.stopCh)
			lease.client.DeletePortMapping("", uint16(externalPort), protocol)
			activeLeases = append(activeLeases[:i], activeLeases[i+1:]...)
			return
		}
	}
}

// PortMapOptions combines UPnP + NAT-PMP/PCP configuration.
type PortMapOptions struct {
	UPnPOptions
	PMPLifetime int // NAT-PMP/PCP lease lifetime in seconds (default 3600)
}

// TryPortMapping tries UPnP first, then NAT-PMP, then PCP.
func TryPortMapping(internalPort, externalPort int, protocol string, opts PortMapOptions) (string, int, string, error) {
	// Try UPnP first
	extIP, extPort, err := TryUPnP(internalPort, externalPort, protocol, opts.UPnPOptions)
	if err == nil {
		return extIP, extPort, "upnp", nil
	}
	applog.Warnf("UPnP failed: %v, trying NAT-PMP...", err)

	// Try NAT-PMP
	lifetime := opts.PMPLifetime
	if lifetime <= 0 {
		lifetime = 3600
	}
	extIP, extPort, err = tryNATPMP(internalPort, externalPort, protocol, lifetime)
	if err == nil {
		return extIP, extPort, "natpmp", nil
	}
	applog.Warnf("NAT-PMP failed: %v, trying PCP...", err)

	// Try PCP
	extIP, extPort, err = tryPCP(internalPort, externalPort, protocol, lifetime)
	if err == nil {
		return extIP, extPort, "pcp", nil
	}
	applog.Warnf("PCP failed: %v", err)

	return "", 0, "", fmt.Errorf("all port mapping methods failed (UPnP, NAT-PMP, PCP)")
}

// renewLease periodically renews a UPnP port mapping before it expires.
func renewLease(lease upnpLease, internalPort int, localIP string, leaseDuration uint32) {
	ticker := time.NewTicker(upnpLeaseRenewEvery)
	defer ticker.Stop()

	for {
		select {
		case <-lease.stopCh:
			return
		case <-ticker.C:
			lease.client.AddPortMapping(
				"",
				uint16(lease.port),
				lease.protocol,
				uint16(internalPort),
				localIP,
				true,
				"NATProxy",
				leaseDuration,
			)
		}
	}
}
