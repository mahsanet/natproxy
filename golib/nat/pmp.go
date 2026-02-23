package nat

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"natproxy/golib/applog"
)

// NAT-PMP (RFC 6886) and PCP (RFC 6887) port mapping implementations.

const (
	pmpPort         = 5351
	pmpVersion      = 0
	pmpOpcodeUDP    = 1
	pmpOpcodeTCP    = 2
	pmpTimeout      = 3 * time.Second
	pmpDefaultLife  = 3600 // 1 hour
	pcpVersion      = 2
	pcpOpcodeMAP    = 1
	pcpProtoTCP     = 6
	pcpProtoUDP     = 17
	pcpTimeout      = 3 * time.Second
)

// --- NAT-PMP ---

// tryNATPMP maps a port via NAT-PMP (RFC 6886).
func tryNATPMP(internalPort, externalPort int, protocol string, lifetime int) (string, int, error) {
	gatewayIP, err := discoverGatewayForPMP()
	if err != nil {
		return "", 0, fmt.Errorf("discover gateway: %w", err)
	}
	applog.Infof("NAT-PMP: gateway = %s", gatewayIP)

	addr := &net.UDPAddr{IP: net.ParseIP(gatewayIP), Port: pmpPort}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return "", 0, fmt.Errorf("dial gateway: %w", err)
	}
	defer conn.Close()

	// Step 1: Get external address
	extIP, err := pmpGetExternalAddress(conn)
	if err != nil {
		return "", 0, fmt.Errorf("get external address: %w", err)
	}
	applog.Infof("NAT-PMP: external IP = %s", extIP)

	// Step 2: Request port mapping
	var opcode byte
	switch protocol {
	case "UDP":
		opcode = pmpOpcodeUDP
	case "TCP":
		opcode = pmpOpcodeTCP
	default:
		opcode = pmpOpcodeTCP
	}

	if lifetime <= 0 {
		lifetime = pmpDefaultLife
	}

	mappedPort, mappedLifetime, err := pmpRequestMapping(conn, opcode, uint16(internalPort), uint16(externalPort), uint32(lifetime))
	if err != nil {
		return "", 0, fmt.Errorf("request mapping: %w", err)
	}

	applog.Successf("NAT-PMP: mapped %s:%d → ext:%d (lifetime=%ds)", protocol, internalPort, mappedPort, mappedLifetime)
	return extIP, int(mappedPort), nil
}

func pmpGetExternalAddress(conn *net.UDPConn) (string, error) {
	// Request: [version(1)][opcode=0(1)]
	req := []byte{pmpVersion, 0}
	conn.SetWriteDeadline(time.Now().Add(pmpTimeout))
	if _, err := conn.Write(req); err != nil {
		return "", err
	}

	buf := make([]byte, 12)
	conn.SetReadDeadline(time.Now().Add(pmpTimeout))
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	if n < 12 {
		return "", fmt.Errorf("response too short: %d bytes", n)
	}

	// Response: [version(1)][opcode=128(1)][result(2)][epoch(4)][extIP(4)]
	resultCode := binary.BigEndian.Uint16(buf[2:4])
	if resultCode != 0 {
		return "", fmt.Errorf("result code %d", resultCode)
	}

	ip := net.IP(buf[8:12]).String()
	return ip, nil
}

func pmpRequestMapping(conn *net.UDPConn, opcode byte, internalPort, externalPort uint16, lifetime uint32) (uint16, uint32, error) {
	// Request: [version(1)][opcode(1)][reserved(2)][internalPort(2)][externalPort(2)][lifetime(4)]
	req := make([]byte, 12)
	req[0] = pmpVersion
	req[1] = opcode
	binary.BigEndian.PutUint16(req[4:6], internalPort)
	binary.BigEndian.PutUint16(req[6:8], externalPort)
	binary.BigEndian.PutUint32(req[8:12], lifetime)

	conn.SetWriteDeadline(time.Now().Add(pmpTimeout))
	if _, err := conn.Write(req); err != nil {
		return 0, 0, err
	}

	buf := make([]byte, 16)
	conn.SetReadDeadline(time.Now().Add(pmpTimeout))
	n, err := conn.Read(buf)
	if err != nil {
		return 0, 0, err
	}
	if n < 16 {
		return 0, 0, fmt.Errorf("response too short: %d bytes", n)
	}

	// Response: [version(1)][opcode+128(1)][result(2)][epoch(4)][internalPort(2)][mappedExtPort(2)][lifetime(4)]
	resultCode := binary.BigEndian.Uint16(buf[2:4])
	if resultCode != 0 {
		return 0, 0, fmt.Errorf("result code %d", resultCode)
	}

	mappedPort := binary.BigEndian.Uint16(buf[10:12])
	mappedLifetime := binary.BigEndian.Uint32(buf[12:16])
	return mappedPort, mappedLifetime, nil
}

// --- PCP ---

// tryPCP attempts port mapping via PCP (RFC 6887).
func tryPCP(internalPort, externalPort int, protocol string, lifetime int) (string, int, error) {
	gatewayIP, err := discoverGatewayForPMP()
	if err != nil {
		return "", 0, fmt.Errorf("discover gateway: %w", err)
	}
	applog.Infof("PCP: gateway = %s", gatewayIP)

	addr := &net.UDPAddr{IP: net.ParseIP(gatewayIP), Port: pmpPort}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return "", 0, fmt.Errorf("dial gateway: %w", err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	var proto byte
	switch protocol {
	case "UDP":
		proto = pcpProtoUDP
	default:
		proto = pcpProtoTCP
	}

	if lifetime <= 0 {
		lifetime = pmpDefaultLife
	}

	// Build PCP MAP request (60 bytes)
	// [version(1)][R=0+opcode(1)][reserved(2)][lifetime(4)][client-ip(16)][nonce(12)][protocol(1)][reserved(3)][internalPort(2)][externalPort(2)][externalIP(16)]
	req := make([]byte, 60)
	req[0] = pcpVersion
	req[1] = pcpOpcodeMAP // R=0 (request)
	binary.BigEndian.PutUint32(req[4:8], uint32(lifetime))

	// Client IP in IPv4-mapped IPv6 format
	clientIP := localAddr.IP.To4()
	if clientIP != nil {
		// ::ffff:a.b.c.d
		req[18] = 0xff
		req[19] = 0xff
		copy(req[20:24], clientIP)
	}

	// Mapping nonce (random 12 bytes)
	copy(req[24:36], []byte("natproxy-pcp")) // simple deterministic nonce

	req[36] = proto
	// reserved bytes 37-39 = 0
	binary.BigEndian.PutUint16(req[40:42], uint16(internalPort))
	binary.BigEndian.PutUint16(req[42:44], uint16(externalPort))
	// Suggested external IP = all zeros (let router choose)

	conn.SetWriteDeadline(time.Now().Add(pcpTimeout))
	if _, err := conn.Write(req); err != nil {
		return "", 0, err
	}

	buf := make([]byte, 60)
	conn.SetReadDeadline(time.Now().Add(pcpTimeout))
	n, err := conn.Read(buf)
	if err != nil {
		return "", 0, err
	}
	if n < 60 {
		return "", 0, fmt.Errorf("PCP response too short: %d bytes", n)
	}

	// Check R bit (response) and result code
	if buf[1]&0x80 == 0 {
		return "", 0, fmt.Errorf("not a PCP response")
	}
	resultCode := buf[3]
	if resultCode != 0 {
		return "", 0, fmt.Errorf("PCP result code %d", resultCode)
	}

	mappedPort := binary.BigEndian.Uint16(buf[42:44])

	// External IP from response bytes 44-59
	extIP := net.IP(buf[44:60])
	// Check if it's IPv4-mapped
	if extIP[10] == 0xff && extIP[11] == 0xff {
		extIP = extIP[12:16]
	}

	applog.Successf("PCP: mapped %s:%d → %s:%d", protocol, internalPort, extIP.String(), mappedPort)
	return extIP.String(), int(mappedPort), nil
}

// --- Gateway discovery ---

func discoverGatewayForPMP() (string, error) {
	localIP, err := getLocalIP()
	if err != nil {
		return "", err
	}
	gateways := deriveGatewayIPs(localIP)
	if len(gateways) == 0 {
		return "", fmt.Errorf("no gateway candidates")
	}

	// Try each gateway — first one that responds to NAT-PMP wins
	for _, gw := range gateways {
		addr := &net.UDPAddr{IP: net.ParseIP(gw), Port: pmpPort}
		conn, err := net.DialUDP("udp4", nil, addr)
		if err != nil {
			continue
		}

		// Send external address request and check for response
		req := []byte{pmpVersion, 0}
		conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
		conn.Write(req)

		buf := make([]byte, 12)
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, err := conn.Read(buf)
		conn.Close()
		if err == nil && n >= 12 {
			return gw, nil
		}
	}

	// Default to .1 of subnet
	return gateways[0], nil
}
