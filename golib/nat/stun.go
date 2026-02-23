package nat

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"natproxy/golib/applog"

	"github.com/pion/stun/v2"
)

const (
	stunReadTimeout = 5 * time.Second
	stunBufSize     = 1500
)

// RFC 3489/5780 attribute types for NAT type detection.
const (
	attrChangeRequest  stun.AttrType = 0x0003 // CHANGE-REQUEST
	attrChangedAddress stun.AttrType = 0x0005 // CHANGED-ADDRESS

	changeIPFlag   = uint32(0x04)
	changePortFlag = uint32(0x02)

	changeReqTimeout = 2 * time.Second // timeout for CHANGE-REQUEST tests (expect no response)
)

// --- RFC 5780 NAT Classification Types ---

// NATMapping describes the NAT's address mapping behavior (RFC 5780 §4.3).
type NATMapping int

const (
	MappingUnknown              NATMapping = iota
	MappingEndpointIndependent             // same mapping regardless of destination
	MappingAddressDependent                // mapping changes with destination IP
	MappingAddressPortDependent            // mapping changes with destination IP:port
)

func (m NATMapping) String() string {
	switch m {
	case MappingEndpointIndependent:
		return "endpoint_independent"
	case MappingAddressDependent:
		return "address_dependent"
	case MappingAddressPortDependent:
		return "address_port_dependent"
	default:
		return "unknown"
	}
}

// NATFiltering describes the NAT's filtering behavior (RFC 5780 §4.4).
type NATFiltering int

const (
	FilteringUnknown              NATFiltering = iota
	FilteringEndpointIndependent               // accepts from any source
	FilteringAddressDependent                  // accepts only from contacted IPs
	FilteringAddressPortDependent              // accepts only from contacted IP:port
)

func (f NATFiltering) String() string {
	switch f {
	case FilteringEndpointIndependent:
		return "endpoint_independent"
	case FilteringAddressDependent:
		return "address_dependent"
	case FilteringAddressPortDependent:
		return "address_port_dependent"
	default:
		return "unknown"
	}
}

// NATTraversal predicts hole-punching success based on NAT types.
type NATTraversal int

const (
	TraversalUnknown         NATTraversal = iota
	TraversalUnlimited                    // both sides endpoint-independent mapping
	TraversalPartiallyLimited             // one side address-dependent
	TraversalStrictlyLimited              // at least one side address-port-dependent
)

func (t NATTraversal) String() string {
	switch t {
	case TraversalUnlimited:
		return "unlimited"
	case TraversalPartiallyLimited:
		return "partially_limited"
	case TraversalStrictlyLimited:
		return "strictly_limited"
	default:
		return "unknown"
	}
}

// NATType holds the full RFC 5780 NAT classification.
type NATType struct {
	Mapping    NATMapping
	Filtering  NATFiltering
	MappedIP   string
	MappedPort int
}

// PredictTraversal predicts hole-punching success between two NAT types.
func PredictTraversal(local, remote NATType) NATTraversal {
	if local.Mapping == MappingEndpointIndependent && remote.Mapping == MappingEndpointIndependent {
		return TraversalUnlimited
	}
	if local.Mapping == MappingAddressPortDependent || remote.Mapping == MappingAddressPortDependent {
		return TraversalStrictlyLimited
	}
	if local.Mapping == MappingAddressDependent || remote.Mapping == MappingAddressDependent {
		return TraversalPartiallyLimited
	}
	return TraversalUnknown
}

// --- Public API ---

// DiscoverPublicAddr sends a STUN Binding Request to discover the public IP and port
// using an ephemeral local port.
func DiscoverPublicAddr(stunServer string) (string, int, error) {
	applog.Infof("STUN binding request to %s (ephemeral port)...", stunServer)
	conn, err := net.Dial("udp4", stunServer)
	if err != nil {
		return "", 0, fmt.Errorf("dial STUN server %s: %w", stunServer, err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	applog.Infof("STUN local socket: %s", localAddr.String())

	return doSTUN(conn, stunServer)
}

// DiscoverPublicAddrFromPort sends a STUN Binding Request from a specific local port.
func DiscoverPublicAddrFromPort(stunServer string, localPort int) (string, int, error) {
	applog.Infof("STUN binding request to %s from local port %d...", stunServer, localPort)

	localAddr := &net.UDPAddr{Port: localPort}
	remoteAddr, err := net.ResolveUDPAddr("udp4", stunServer)
	if err != nil {
		return "", 0, fmt.Errorf("resolve STUN server %s: %w", stunServer, err)
	}

	conn, err := net.DialUDP("udp4", localAddr, remoteAddr)
	if err != nil {
		return "", 0, fmt.Errorf("dial STUN from port %d: %w", localPort, err)
	}
	defer conn.Close()

	applog.Infof("STUN local socket: %s → %s", conn.LocalAddr().String(), conn.RemoteAddr().String())

	return doSTUN(conn, stunServer)
}

// doSTUN performs the actual STUN binding request on an existing connection.
func doSTUN(conn net.Conn, _ string) (string, int, error) {
	c, err := stun.NewClient(conn)
	if err != nil {
		return "", 0, fmt.Errorf("create STUN client: %w", err)
	}
	defer c.Close()

	var publicIP string
	var publicPort int
	var stunErr error

	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	err = c.Do(message, func(res stun.Event) {
		if res.Error != nil {
			stunErr = res.Error
			return
		}

		var xorAddr stun.XORMappedAddress
		if err := xorAddr.GetFrom(res.Message); err != nil {
			stunErr = fmt.Errorf("parse XOR-MAPPED-ADDRESS: %w", err)
			return
		}

		publicIP = xorAddr.IP.String()
		publicPort = xorAddr.Port
	})

	if err != nil {
		return "", 0, fmt.Errorf("STUN request: %w", err)
	}
	if stunErr != nil {
		return "", 0, stunErr
	}

	applog.Infof("STUN result: public address %s:%d", publicIP, publicPort)
	return publicIP, publicPort, nil
}

// DiscoverPublicAddrVia sends a STUN Binding Request on an existing unconnected
// UDP socket.
func DiscoverPublicAddrVia(conn *net.UDPConn, stunServer string) (string, int, error) {
	remoteAddr, err := net.ResolveUDPAddr("udp4", stunServer)
	if err != nil {
		return "", 0, fmt.Errorf("resolve STUN server %s: %w", stunServer, err)
	}

	msg := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	raw := msg.Raw

	applog.Infof("STUN (via shared socket) binding request to %s...", stunServer)

	if _, err := conn.WriteToUDP(raw, remoteAddr); err != nil {
		return "", 0, fmt.Errorf("send STUN to %s: %w", stunServer, err)
	}

	conn.SetReadDeadline(time.Now().Add(stunReadTimeout))
	buf := make([]byte, stunBufSize)
	n, _, err := conn.ReadFromUDP(buf)
	conn.SetReadDeadline(time.Time{})
	if err != nil {
		return "", 0, fmt.Errorf("read STUN response from %s: %w", stunServer, err)
	}

	resp := new(stun.Message)
	resp.Raw = buf[:n]
	if err := resp.Decode(); err != nil {
		return "", 0, fmt.Errorf("decode STUN response from %s: %w", stunServer, err)
	}

	var xorAddr stun.XORMappedAddress
	if err := xorAddr.GetFrom(resp); err != nil {
		return "", 0, fmt.Errorf("parse XOR-MAPPED-ADDRESS from %s: %w", stunServer, err)
	}

	publicIP := xorAddr.IP.String()
	publicPort := xorAddr.Port
	applog.Infof("STUN (via shared socket) result from %s: %s:%d", stunServer, publicIP, publicPort)
	return publicIP, publicPort, nil
}

// --- NAT Detection ---

// DetectNATTypeFull does RFC 5780 NAT detection, falling back to RFC 3489 +
// two-server comparison when the primary server doesn't support CHANGE-REQUEST.
func DetectNATTypeFull(stunServer1, stunServer2 string) (*NATType, error) {
	// Try RFC 5780 / RFC 3489 detection with servers that support CHANGE-REQUEST
	rfc3489Servers := []string{
		"stun.stunprotocol.org:3478",
		"stun.voipbuster.com:3478",
		"stun.voipstunt.com:3478",
		"stun.sipnet.net:3478",
	}
	for _, server := range rfc3489Servers {
		result, err := detectNATTypeRFC5780(server)
		if err != nil {
			applog.Warnf("RFC 5780 NAT detection via %s failed: %v", server, err)
			continue
		}
		applog.Infof("NAT type (RFC 5780 via %s): mapping=%s, filtering=%s",
			server, result.Mapping, result.Filtering)
		return result, nil
	}

	// Fallback to two-server port comparison
	applog.Info("RFC 5780 servers unavailable, using two-server fallback...")
	return detectNATTypeTwoServerFull(stunServer1, stunServer2)
}

// detectNATTypeRFC5780 implements the RFC 5780 NAT classification:
//
//	Mapping test (§4.3):
//	  Step 1: Binding to server1, get XOR-MAPPED-ADDRESS + OTHER-ADDRESS
//	  Step 2: Binding to OTHER-ADDRESS IP (same port), compare mapped addresses
//	  Step 3: If different, binding to OTHER-ADDRESS IP:OTHER-PORT, compare again
//
//	Filtering test (§4.4):
//	  Step 1: CHANGE-REQUEST with change-IP+port
//	  Step 2: If no response, CHANGE-REQUEST with change-port only
func detectNATTypeRFC5780(server string) (*NATType, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 0})
	if err != nil {
		return nil, fmt.Errorf("bind: %w", err)
	}
	defer conn.Close()

	serverAddr, err := net.ResolveUDPAddr("udp4", server)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", server, err)
	}

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	applog.Infof("NAT detection (RFC 5780) via %s from %s", server, localAddr)

	// Step 1: Basic binding to primary server
	res1, err := stunBindReq(conn, serverAddr, 0, stunReadTimeout)
	if err != nil {
		return nil, fmt.Errorf("test I: %w", err)
	}

	altIP, altPort := getChangedAddr(res1.msg)
	if altIP == "" {
		return nil, fmt.Errorf("server %s doesn't provide OTHER-ADDRESS/CHANGED-ADDRESS", server)
	}

	// Validate OTHER-ADDRESS
	if isBogonIP(altIP) {
		return nil, fmt.Errorf("server %s returned bogon OTHER-ADDRESS: %s", server, altIP)
	}

	applog.Infof("Step 1: mapped=%s:%d, alternate=%s:%d",
		res1.mappedIP, res1.mappedPort, altIP, altPort)

	// Check for open internet
	localIP := localAddr.IP.String()
	if (localIP == res1.mappedIP || localIP == "0.0.0.0") && localAddr.Port == res1.mappedPort {
		applog.Info("Local address matches mapped address → no NAT")
		_, err = stunBindReq(conn, serverAddr, changeIPFlag|changePortFlag, changeReqTimeout)
		if err == nil {
			return &NATType{
				Mapping: MappingEndpointIndependent, Filtering: FilteringEndpointIndependent,
				MappedIP: res1.mappedIP, MappedPort: res1.mappedPort,
			}, nil
		}
		return &NATType{
			Mapping: MappingEndpointIndependent, Filtering: FilteringAddressPortDependent,
			MappedIP: res1.mappedIP, MappedPort: res1.mappedPort,
		}, nil
	}

	// --- Mapping test (RFC 5780 §4.3) ---
	// Step 2: Binding to OTHER-ADDRESS IP (same port as primary)
	altAddr2 := &net.UDPAddr{IP: net.ParseIP(altIP), Port: serverAddr.Port}
	res2, err := stunBindReq(conn, altAddr2, 0, stunReadTimeout)

	var mapping NATMapping
	if err != nil {
		// Can't reach alternate IP, fall back to alternate IP:port
		altAddr3 := &net.UDPAddr{IP: net.ParseIP(altIP), Port: altPort}
		res3, err3 := stunBindReq(conn, altAddr3, 0, stunReadTimeout)
		if err3 != nil {
			return nil, fmt.Errorf("mapping test failed (neither alt addr reachable): %v / %v", err, err3)
		}
		if res1.mappedIP == res3.mappedIP && res1.mappedPort == res3.mappedPort {
			mapping = MappingEndpointIndependent
		} else {
			mapping = MappingAddressPortDependent
		}
	} else {
		if res1.mappedIP == res2.mappedIP && res1.mappedPort == res2.mappedPort {
			mapping = MappingEndpointIndependent
		} else {
			// Step 3: Different mapping → address-dependent or address-port-dependent?
			altAddr3 := &net.UDPAddr{IP: net.ParseIP(altIP), Port: altPort}
			res3, err3 := stunBindReq(conn, altAddr3, 0, stunReadTimeout)
			if err3 != nil {
				mapping = MappingAddressPortDependent // can't determine, assume worst case
			} else if res2.mappedIP == res3.mappedIP && res2.mappedPort == res3.mappedPort {
				mapping = MappingAddressDependent
			} else {
				mapping = MappingAddressPortDependent
			}
		}
	}
	applog.Infof("Mapping: %s", mapping)

	// --- Filtering test (RFC 5780 §4.4) ---
	var filtering NATFiltering

	// Test: CHANGE-REQUEST with change-IP+port
	_, err = stunBindReq(conn, serverAddr, changeIPFlag|changePortFlag, changeReqTimeout)
	if err == nil {
		filtering = FilteringEndpointIndependent
	} else {
		// Test: CHANGE-REQUEST with change-port only
		_, err = stunBindReq(conn, serverAddr, changePortFlag, changeReqTimeout)
		if err == nil {
			filtering = FilteringAddressDependent
		} else {
			filtering = FilteringAddressPortDependent
		}
	}
	applog.Infof("Filtering: %s", filtering)

	return &NATType{
		Mapping: mapping, Filtering: filtering,
		MappedIP: res1.mappedIP, MappedPort: res1.mappedPort,
	}, nil
}

// detectNATTypeTwoServerFull falls back to comparing mapped ports across two
// different STUN servers.
func detectNATTypeTwoServerFull(stunServer1, stunServer2 string) (*NATType, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 0})
	if err != nil {
		return nil, fmt.Errorf("bind ephemeral UDP: %w", err)
	}
	defer conn.Close()

	applog.Infof("NAT detection: using single socket %s for both STUN queries", conn.LocalAddr().String())

	stunServers := []string{
		stunServer1,
		stunServer2,
		"stun2.l.google.com:19302",
		"stun3.l.google.com:19302",
		"stun4.l.google.com:19302",
	}

	type result struct {
		ip   string
		port int
	}
	var results []result
	used := map[string]bool{}

	for _, s := range stunServers {
		if len(results) >= 2 {
			break
		}
		if used[s] {
			continue
		}
		ip, port, err := DiscoverPublicAddrVia(conn, s)
		if err != nil {
			applog.Warnf("STUN server %s failed: %v, trying next...", s, err)
			continue
		}
		used[s] = true
		results = append(results, result{ip, port})
	}

	if len(results) < 2 {
		return nil, fmt.Errorf("fewer than 2 STUN servers responded")
	}

	ip1, port1 := results[0].ip, results[0].port
	ip2, port2 := results[1].ip, results[1].port

	if isPrivateIP(ip1) {
		return &NATType{
			Mapping: MappingUnknown, Filtering: FilteringUnknown,
			MappedIP: ip1, MappedPort: port1,
		}, nil
	}

	if ip1 != ip2 {
		return &NATType{
			Mapping: MappingUnknown, Filtering: FilteringUnknown,
			MappedIP: ip1, MappedPort: port1,
		}, nil
	}

	if port1 != port2 {
		applog.Infof("NAT detection: ports differ (%d vs %d) → symmetric", port1, port2)
		return &NATType{
			Mapping: MappingAddressPortDependent, Filtering: FilteringUnknown,
			MappedIP: ip1, MappedPort: port1,
		}, nil
	}

	applog.Infof("NAT detection: ports match (%d) → cone (endpoint-independent)", port1)
	return &NATType{
		Mapping: MappingEndpointIndependent, Filtering: FilteringUnknown,
		MappedIP: ip1, MappedPort: port1,
	}, nil
}

// --- STUN helpers ---

// stunBindResult holds the result of a STUN binding request.
type stunBindResult struct {
	mappedIP   string
	mappedPort int
	msg        *stun.Message
}

// stunBindReq sends a STUN binding request with optional CHANGE-REQUEST flags
// and returns the mapped address. Retries up to 3 times to handle UDP packet loss.
func stunBindReq(conn *net.UDPConn, serverAddr *net.UDPAddr, changeFlags uint32, timeout time.Duration) (*stunBindResult, error) {
	const maxRetries = 3
	const retryDelay = 500 * time.Millisecond

	setters := []stun.Setter{stun.TransactionID, stun.BindingRequest}
	if changeFlags != 0 {
		val := make([]byte, 4)
		binary.BigEndian.PutUint32(val, changeFlags)
		setters = append(setters, stun.RawAttribute{
			Type:  attrChangeRequest,
			Value: val,
		})
	}

	msg, err := stun.Build(setters...)
	if err != nil {
		return nil, fmt.Errorf("build: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}

		if _, err := conn.WriteToUDP(msg.Raw, serverAddr); err != nil {
			lastErr = fmt.Errorf("send: %w", err)
			continue
		}

		deadline := time.Now().Add(timeout)
		buf := make([]byte, stunBufSize)
		for {
			conn.SetReadDeadline(deadline)
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				conn.SetReadDeadline(time.Time{})
				lastErr = fmt.Errorf("recv: %w", err)
				break
			}

			resp := new(stun.Message)
			resp.Raw = make([]byte, n)
			copy(resp.Raw, buf[:n])
			if err := resp.Decode(); err != nil {
				continue
			}
			if resp.TransactionID != msg.TransactionID {
				continue
			}

			conn.SetReadDeadline(time.Time{})

			var mappedIP string
			var mappedPort int
			var xorAddr stun.XORMappedAddress
			if err := xorAddr.GetFrom(resp); err == nil {
				mappedIP = xorAddr.IP.String()
				mappedPort = xorAddr.Port
			} else {
				var mapAddr stun.MappedAddress
				if err := mapAddr.GetFrom(resp); err == nil {
					mappedIP = mapAddr.IP.String()
					mappedPort = mapAddr.Port
				} else {
					return nil, fmt.Errorf("no mapped address in response")
				}
			}

			return &stunBindResult{mappedIP: mappedIP, mappedPort: mappedPort, msg: resp}, nil
		}
	}
	return nil, lastErr
}

// getChangedAddr extracts the alternate server address from a STUN response.
// Tries CHANGED-ADDRESS (RFC 3489) then OTHER-ADDRESS (RFC 5780).
func getChangedAddr(msg *stun.Message) (string, int) {
	var addr stun.MappedAddress
	if err := addr.GetFromAs(msg, attrChangedAddress); err == nil {
		return addr.IP.String(), addr.Port
	}
	if err := addr.GetFromAs(msg, stun.AttrOtherAddress); err == nil {
		return addr.IP.String(), addr.Port
	}
	return "", 0
}
