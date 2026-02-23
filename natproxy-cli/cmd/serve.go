package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"natproxy/cli/internal/display"
	"natproxy/cli/internal/orchestrator"
	"natproxy/cli/internal/types"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the proxy server (share internet)",
	Long:  "Start the NATProxy server. Attempts UPnP port mapping first, then falls back to WebRTC hole punching.\nRuns until Ctrl+C.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := buildServerConfig(cmd)
		manual, _ := cmd.Flags().GetBool("manual")

		orch := orchestrator.NewServerOrchestrator(cfg)

		// Start log streaming early so startup logs are visible.
		done := make(chan struct{})
		go display.StreamLogs(done)

		if manual {
			err := runManualServer(orch, done)
			if err != nil {
				display.FlushLogs()
				close(done)
			}
			return err
		}

		connectionCode, err := orch.StartServer()
		if err != nil {
			display.FlushLogs()
			close(done)
			return fmt.Errorf("start server: %w", err)
		}

		// Connection code to stdout (for piping)
		fmt.Println(connectionCode)

		// VLESS link to stderr (human display, not piped)
		if vl := orch.GetVLESSLink(); vl != "" {
			fmt.Fprintf(os.Stderr, "\nVLESS Link (import in v2rayNG/Nekoray):\n%s\n", vl)
		}

		// Register discovery
		noDiscovery, _ := cmd.Flags().GetBool("no-discovery")
		if cfg.DiscoveryEnabled && !noDiscovery {
			if err := orch.RegisterDiscovery(connectionCode); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: discovery registration failed: %v\n", err)
			}
		}

		// Start status display (logs already streaming)
		go display.ShowServerStatus(done, func() display.ServerMetrics {
			return orch.GetMetrics()
		})

		// Wait for signal
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig

		fmt.Fprintln(os.Stderr, "\nShutting down...")
		close(done)

		if err := orch.StopServer(); err != nil {
			return fmt.Errorf("stop server: %w", err)
		}

		return nil
	},
}

func runManualServer(orch *orchestrator.ServerOrchestrator, done chan struct{}) error {
	offerCode, err := orch.StartServerManual()
	if err != nil {
		return fmt.Errorf("start manual server: %w", err)
	}

	// Offer code to stdout (for piping / copying)
	fmt.Println(offerCode)

	fmt.Fprintln(os.Stderr, "\nShare the offer code above with the client.")
	fmt.Fprint(os.Stderr, "Paste the answer code (M1A:...): ")

	reader := bufio.NewReader(os.Stdin)
	answerCode, _ := reader.ReadString('\n')
	answerCode = strings.TrimSpace(answerCode)

	if answerCode == "" {
		orch.StopServer()
		display.FlushLogs()
		close(done)
		return fmt.Errorf("no answer code provided")
	}

	if err := orch.AcceptManualAnswer(answerCode); err != nil {
		orch.StopServer()
		display.FlushLogs()
		close(done)
		return fmt.Errorf("accept manual answer: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Connected! Server running. Press Ctrl+C to stop.")

	// Start status display (logs already streaming)
	go display.ShowServerStatus(done, func() display.ServerMetrics {
		return orch.GetMetrics()
	})

	// Wait for signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Fprintln(os.Stderr, "\nShutting down...")
	close(done)

	if err := orch.StopServer(); err != nil {
		return fmt.Errorf("stop server: %w", err)
	}

	return nil
}

func buildServerConfig(cmd *cobra.Command) types.ServerConfig {
	return types.ServerConfig{
		ListenPort:        viper.GetInt("server.port"),
		StunServer:        viper.GetString("stun_server"),
		SignalingURL:      viper.GetString("signaling_url"),
		DiscoveryURL:     viper.GetString("discovery_url"),
		NatMethod:         viper.GetString("server.nat_method"),
		UseRelay:          viper.GetBool("server.use_relay"),
		Protocol:          viper.GetString("server.protocol"),
		Transport:         viper.GetString("server.transport"),
		SocksAuth:         viper.GetString("server.socks_auth"),
		SocksUsername:     viper.GetString("server.socks_username"),
		SocksPassword:     viper.GetString("server.socks_password"),
		SocksUDP:          viper.GetBool("server.socks_udp"),
		KcpMTU:            viper.GetInt("server.kcp.mtu"),
		KcpTTI:            viper.GetInt("server.kcp.tti"),
		KcpUplinkCapacity: viper.GetInt("server.kcp.uplink_capacity"),
		KcpDownlinkCapacity: viper.GetInt("server.kcp.downlink_capacity"),
		KcpCongestion:       viper.GetBool("server.kcp.congestion"),
		KcpReadBufferSize:   viper.GetInt("server.kcp.read_buffer"),
		KcpWriteBufferSize:  viper.GetInt("server.kcp.write_buffer"),
		FinalMaskType:       viper.GetString("server.finalmask.type"),
		FinalMaskPassword:   viper.GetString("server.finalmask.password"),
		FinalMaskDomain:     viper.GetString("server.finalmask.domain"),
		XhttpPath:           viper.GetString("server.xhttp.path"),
		XhttpHost:           viper.GetString("server.xhttp.host"),
		XhttpMode:           viper.GetString("server.xhttp.mode"),
		UpnpLeaseDuration:   viper.GetInt("server.upnp_lease_duration"),
		UpnpRetries:         viper.GetInt("server.upnp_retries"),
		SsdpTimeout:         viper.GetInt("server.ssdp_timeout"),
		DiscoveryEnabled:    viper.GetBool("server.discovery.enabled"),
		DiscoveryName:       viper.GetString("server.discovery.name"),
		DiscoveryRoom:       viper.GetString("server.discovery.room"),
		TransportMode:       viper.GetString("server.transport_mode"),
		DisableIPv6:         viper.GetBool("server.disable_ipv6"),
		RateLimitUp:          viper.GetInt64("server.rate_limit_up") * 1024, // KB/s → bytes/s
		RateLimitDown:        viper.GetInt64("server.rate_limit_down") * 1024,
		PaddingEnabled:       viper.GetBool("server.padding.enabled"),
		PaddingMax:           viper.GetInt("server.padding.max"),
		NumPeerConnections:   viper.GetInt("server.num_peer_connections"),
		NumChannels:          viper.GetInt("server.num_channels"),
		SmuxStreamBuffer:     viper.GetInt("server.smux.stream_buffer"),
		SmuxSessionBuffer:    viper.GetInt("server.smux.session_buffer"),
		SmuxFrameSize:        viper.GetInt("server.smux.frame_size"),
		SmuxKeepAlive:        viper.GetInt("server.smux.keep_alive"),
		SmuxKeepAliveTimeout: viper.GetInt("server.smux.keep_alive_timeout"),
		DCMaxBuffered:        viper.GetInt("server.dc.max_buffered"),
		DCLowMark:            viper.GetInt("server.dc.low_mark"),
		SCTPRecvBuffer:       viper.GetInt("server.sctp.recv_buffer"),
		SCTPRTOMax:           viper.GetInt("server.sctp.rto_max"),
		SCTPZeroChecksum:     viper.GetBool("server.sctp.zero_checksum"),
		DTLSRetransmit:       viper.GetInt("server.dtls.retransmit"),
		DTLSSkipVerify:       viper.GetBool("server.dtls.skip_verify"),
		DisableCloseByDTLS:   viper.GetBool("server.dtls.disable_close"),
		ICEDisconnTimeout:    viper.GetInt("server.ice.disconn_timeout"),
		ICEFailedTimeout:     viper.GetInt("server.ice.failed_timeout"),
		ICEKeepalive:         viper.GetInt("server.ice.keepalive"),
		UDPReadBuffer:        viper.GetInt("server.udp.read_buffer"),
		UDPWriteBuffer:       viper.GetInt("server.udp.write_buffer"),
		MaskIPs:              viper.GetBool("server.mask_ips"),
		UUID:                 viper.GetString("server.uuid"),
	}
}

func init() {
	f := serveCmd.Flags()

	// Network
	f.Int("port", 0, "Listen port (default 10853)")
	f.String("stun-server", "", "STUN server host:port")
	f.String("signaling-url", "", "Signaling server URL")
	f.String("discovery-url", "", "Discovery server URL (default: signaling host on port 5602)")

	// NAT Traversal
	f.String("nat-method", "", "auto|upnp|holepunch (default auto)")
	f.Bool("use-relay", false, "UDP relay fallback for WebRTC")
	f.Int("upnp-lease-duration", 0, "UPnP lease seconds, 0=indefinite (default 3600)")
	f.Int("upnp-retries", 0, "UPnP mapping retries (default 3)")
	f.Int("ssdp-timeout", 0, "SSDP timeout seconds (default 3)")

	// Protocol & Transport
	f.String("protocol", "", "vless|socks (default vless)")
	f.String("transport", "", "kcp|xhttp (default xhttp)")
	f.String("uuid", "", "VLESS UUID (default: random on each start)")

	// SOCKS
	f.String("socks-auth", "", "noauth|password (default noauth)")
	f.String("socks-username", "", "SOCKS username")
	f.String("socks-password", "", "SOCKS password")
	f.Bool("socks-udp", true, "SOCKS UDP support")

	// KCP
	f.Int("kcp-mtu", 0, "MTU 576-1460 (default 1350)")
	f.Int("kcp-tti", 0, "TTI 10-100ms (default 20)")
	f.Int("kcp-uplink-capacity", 0, "Uplink MB/s (default 12)")
	f.Int("kcp-downlink-capacity", 0, "Downlink MB/s (default 100)")
	f.Bool("kcp-congestion", true, "Congestion control")
	f.Int("kcp-read-buffer", 0, "Read buffer MB (default 4)")
	f.Int("kcp-write-buffer", 0, "Write buffer MB (default 4)")

	// FinalMask
	f.String("finalmask-type", "", "Obfuscation type (default header-dtls)")
	f.String("finalmask-password", "", "Password for mkcp-aes128gcm")
	f.String("finalmask-domain", "", "Domain for header-dns")

	// xHTTP
	f.String("xhttp-path", "", "Path (default /)")
	f.String("xhttp-host", "", "Host header")
	f.String("xhttp-mode", "", "auto|packet-up|stream-up|stream-one (default auto)")

	// WebRTC Transport
	f.String("transport-mode", "", "datachannel|media (default datachannel)")
	f.Bool("disable-ipv6", false, "Disable IPv6 ICE candidates")
	f.Int64("rate-limit-up", 0, "Upload rate limit in KB/s (0=unlimited)")
	f.Int64("rate-limit-down", 0, "Download rate limit in KB/s (0=unlimited)")

	// Padding
	f.Bool("padding", false, "Enable traffic padding")
	f.Int("padding-max", 256, "Max padding bytes per write")
	f.Int("padding-version", 2, "Padding version (0=v1, 2=v2 decoy+burst)")
	// WebRTC channels & smux
	f.Int("num-peer-connections", 6, "Parallel PeerConnections (1-8)")
	f.Int("num-channels", 6, "Parallel data channels")
	f.Int("smux-stream-buffer", 2048, "Per-stream receive buffer KB")
	f.Int("smux-session-buffer", 8192, "Session receive buffer KB")
	f.Int("smux-frame-size", 32768, "Max smux frame size bytes")
	f.Int("smux-keep-alive", 10, "smux keepalive interval sec")
	f.Int("smux-keep-alive-timeout", 300, "smux keepalive timeout sec")
	// Data channel
	f.Int("dc-max-buffered", 2048, "DC backpressure high water KB")
	f.Int("dc-low-mark", 512, "DC backpressure low water KB")
	// SCTP
	f.Int("sctp-recv-buffer", 8192, "SCTP receive buffer KB")
	f.Int("sctp-rto-max", 2500, "SCTP max retransmit timeout ms")
	f.Bool("sctp-zero-checksum", true, "SCTP zero checksum optimization")
	// DTLS
	f.Int("dtls-retransmit", 100, "DTLS retransmission interval ms")
	f.Bool("dtls-skip-verify", true, "Skip DTLS HelloVerify")
	f.Bool("dtls-disable-close", true, "Prevent DTLS close → PC close")
	// ICE
	f.Int("ice-disconn-timeout", 15000, "ICE disconnected timeout ms")
	f.Int("ice-failed-timeout", 25000, "ICE failed timeout ms")
	f.Int("ice-keepalive", 2000, "ICE keepalive interval ms")
	// UDP
	f.Int("udp-read-buffer", 8192, "Kernel UDP read buffer KB")
	f.Int("udp-write-buffer", 8192, "Kernel UDP write buffer KB")
	// Logging
	f.Bool("mask-ips", false, "Mask IP addresses in logs")

	// Discovery
	f.String("discovery-name", "", "Display name")
	f.String("discovery-room", "", "Room name")
	f.Bool("no-discovery", false, "Disable discovery registration")

	// Manual signaling
	f.Bool("manual", false, "Manual signaling mode (no signaling server needed)")

	// Bind flags to viper
	viper.BindPFlag("server.port", f.Lookup("port"))
	viper.BindPFlag("stun_server", f.Lookup("stun-server"))
	viper.BindPFlag("signaling_url", f.Lookup("signaling-url"))
	viper.BindPFlag("discovery_url", f.Lookup("discovery-url"))
	viper.BindPFlag("server.nat_method", f.Lookup("nat-method"))
	viper.BindPFlag("server.use_relay", f.Lookup("use-relay"))
	viper.BindPFlag("server.upnp_lease_duration", f.Lookup("upnp-lease-duration"))
	viper.BindPFlag("server.upnp_retries", f.Lookup("upnp-retries"))
	viper.BindPFlag("server.ssdp_timeout", f.Lookup("ssdp-timeout"))
	viper.BindPFlag("server.protocol", f.Lookup("protocol"))
	viper.BindPFlag("server.transport", f.Lookup("transport"))
	viper.BindPFlag("server.uuid", f.Lookup("uuid"))
	viper.BindPFlag("server.socks_auth", f.Lookup("socks-auth"))
	viper.BindPFlag("server.socks_username", f.Lookup("socks-username"))
	viper.BindPFlag("server.socks_password", f.Lookup("socks-password"))
	viper.BindPFlag("server.socks_udp", f.Lookup("socks-udp"))
	viper.BindPFlag("server.kcp.mtu", f.Lookup("kcp-mtu"))
	viper.BindPFlag("server.kcp.tti", f.Lookup("kcp-tti"))
	viper.BindPFlag("server.kcp.uplink_capacity", f.Lookup("kcp-uplink-capacity"))
	viper.BindPFlag("server.kcp.downlink_capacity", f.Lookup("kcp-downlink-capacity"))
	viper.BindPFlag("server.kcp.congestion", f.Lookup("kcp-congestion"))
	viper.BindPFlag("server.kcp.read_buffer", f.Lookup("kcp-read-buffer"))
	viper.BindPFlag("server.kcp.write_buffer", f.Lookup("kcp-write-buffer"))
	viper.BindPFlag("server.finalmask.type", f.Lookup("finalmask-type"))
	viper.BindPFlag("server.finalmask.password", f.Lookup("finalmask-password"))
	viper.BindPFlag("server.finalmask.domain", f.Lookup("finalmask-domain"))
	viper.BindPFlag("server.xhttp.path", f.Lookup("xhttp-path"))
	viper.BindPFlag("server.xhttp.host", f.Lookup("xhttp-host"))
	viper.BindPFlag("server.xhttp.mode", f.Lookup("xhttp-mode"))
	viper.BindPFlag("server.transport_mode", f.Lookup("transport-mode"))
	viper.BindPFlag("server.disable_ipv6", f.Lookup("disable-ipv6"))
	viper.BindPFlag("server.rate_limit_up", f.Lookup("rate-limit-up"))
	viper.BindPFlag("server.rate_limit_down", f.Lookup("rate-limit-down"))
	viper.BindPFlag("server.discovery.name", f.Lookup("discovery-name"))
	viper.BindPFlag("server.discovery.room", f.Lookup("discovery-room"))
	viper.BindPFlag("server.padding.enabled", f.Lookup("padding"))
	viper.BindPFlag("server.padding.max", f.Lookup("padding-max"))
	viper.BindPFlag("server.padding.version", f.Lookup("padding-version"))
	viper.BindPFlag("server.num_peer_connections", f.Lookup("num-peer-connections"))
	viper.BindPFlag("server.num_channels", f.Lookup("num-channels"))
	viper.BindPFlag("server.smux.stream_buffer", f.Lookup("smux-stream-buffer"))
	viper.BindPFlag("server.smux.session_buffer", f.Lookup("smux-session-buffer"))
	viper.BindPFlag("server.smux.frame_size", f.Lookup("smux-frame-size"))
	viper.BindPFlag("server.smux.keep_alive", f.Lookup("smux-keep-alive"))
	viper.BindPFlag("server.smux.keep_alive_timeout", f.Lookup("smux-keep-alive-timeout"))
	viper.BindPFlag("server.dc.max_buffered", f.Lookup("dc-max-buffered"))
	viper.BindPFlag("server.dc.low_mark", f.Lookup("dc-low-mark"))
	viper.BindPFlag("server.sctp.recv_buffer", f.Lookup("sctp-recv-buffer"))
	viper.BindPFlag("server.sctp.rto_max", f.Lookup("sctp-rto-max"))
	viper.BindPFlag("server.sctp.zero_checksum", f.Lookup("sctp-zero-checksum"))
	viper.BindPFlag("server.dtls.retransmit", f.Lookup("dtls-retransmit"))
	viper.BindPFlag("server.dtls.skip_verify", f.Lookup("dtls-skip-verify"))
	viper.BindPFlag("server.dtls.disable_close", f.Lookup("dtls-disable-close"))
	viper.BindPFlag("server.ice.disconn_timeout", f.Lookup("ice-disconn-timeout"))
	viper.BindPFlag("server.ice.failed_timeout", f.Lookup("ice-failed-timeout"))
	viper.BindPFlag("server.ice.keepalive", f.Lookup("ice-keepalive"))
	viper.BindPFlag("server.udp.read_buffer", f.Lookup("udp-read-buffer"))
	viper.BindPFlag("server.udp.write_buffer", f.Lookup("udp-write-buffer"))
	viper.BindPFlag("server.mask_ips", f.Lookup("mask-ips"))

	rootCmd.AddCommand(serveCmd)
}
