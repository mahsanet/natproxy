package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"natproxy/cli/internal/display"
	"natproxy/cli/internal/orchestrator"
	"natproxy/cli/internal/types"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var connectCmd = &cobra.Command{
	Use:   "connect [connection-code]",
	Short: "Connect to a server and expose a local SOCKS5 proxy",
	Long:  "Connect to a NATProxy server using a connection code or interactive discovery.\nExposes a local SOCKS5 proxy. Runs until Ctrl+C.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := types.ClientConfig{
			SocksPort:          viper.GetInt("client.socks_port"),
			StunServer:         viper.GetString("stun_server"),
			SignalingURL:       viper.GetString("signaling_url"),
			SCTPRecvBuffer:     viper.GetInt("client.sctp.recv_buffer"),
			SCTPRTOMax:         viper.GetInt("client.sctp.rto_max"),
			SCTPZeroChecksum:   viper.GetBool("client.sctp.zero_checksum"),
			DTLSRetransmit:     viper.GetInt("client.dtls.retransmit"),
			DTLSSkipVerify:     viper.GetBool("client.dtls.skip_verify"),
			DisableCloseByDTLS: viper.GetBool("client.dtls.disable_close"),
			ICEDisconnTimeout:  viper.GetInt("client.ice.disconn_timeout"),
			ICEFailedTimeout:   viper.GetInt("client.ice.failed_timeout"),
			ICEKeepalive:       viper.GetInt("client.ice.keepalive"),
			UDPReadBuffer:      viper.GetInt("client.udp.read_buffer"),
			UDPWriteBuffer:     viper.GetInt("client.udp.write_buffer"),
			MaskIPs:            viper.GetBool("client.mask_ips"),
		}

		discoverMode, _ := cmd.Flags().GetBool("discover")

		var connectionCode string
		if len(args) > 0 {
			connectionCode = args[0]
		}

		if connectionCode == "" && !discoverMode {
			return fmt.Errorf("provide a connection code or use --discover")
		}

		// Interactive discovery mode
		if discoverMode || connectionCode == "" {
			room, _ := cmd.Flags().GetString("room")
			code, err := interactiveDiscover(viper.GetString("discovery_url"), room)
			if err != nil {
				return err
			}
			connectionCode = code
		}

		orch := orchestrator.NewClientOrchestrator(cfg)

		// Auto-detect manual offer codes (M1:...)
		if strings.HasPrefix(connectionCode, "M1:") {
			return runManualClient(orch, cfg, connectionCode)
		}

		if err := orch.StartClient(connectionCode); err != nil {
			return fmt.Errorf("connect: %w", err)
		}

		socksAddr := fmt.Sprintf("127.0.0.1:%d", cfg.SocksPort)
		fmt.Fprintf(os.Stderr, "SOCKS5 proxy ready at %s\n", socksAddr)
		fmt.Println(socksAddr) // structured output to stdout

		// Start log streaming and status display
		done := make(chan struct{})
		go display.StreamLogs(done)
		go display.ShowClientStatus(done, func() display.ClientMetrics {
			return orch.GetMetrics(socksAddr)
		})

		// Wait for signal
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig

		fmt.Fprintln(os.Stderr, "\nDisconnecting...")
		close(done)

		if err := orch.StopClient(); err != nil {
			return fmt.Errorf("disconnect: %w", err)
		}

		return nil
	},
}

func runManualClient(orch *orchestrator.ClientOrchestrator, cfg types.ClientConfig, offerCode string) error {
	answerCode, err := orch.StartClientManual(offerCode)
	if err != nil {
		return fmt.Errorf("process manual offer: %w", err)
	}

	// Answer code to stdout (for piping / copying)
	fmt.Println(answerCode)

	fmt.Fprintln(os.Stderr, "\nShare the answer code above with the server.")
	fmt.Fprint(os.Stderr, "Press Enter after the server accepts it...")

	reader := bufio.NewReader(os.Stdin)
	reader.ReadString('\n')

	if err := orch.WaitManualConnection(65 * time.Second); err != nil {
		return fmt.Errorf("manual connection: %w", err)
	}

	socksAddr := fmt.Sprintf("127.0.0.1:%d", cfg.SocksPort)
	fmt.Fprintf(os.Stderr, "SOCKS5 proxy ready at %s\n", socksAddr)
	fmt.Println(socksAddr)

	// Start log streaming and status display
	done := make(chan struct{})
	go display.StreamLogs(done)
	go display.ShowClientStatus(done, func() display.ClientMetrics {
		return orch.GetMetrics(socksAddr)
	})

	// Wait for signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Fprintln(os.Stderr, "\nDisconnecting...")
	close(done)

	if err := orch.StopClient(); err != nil {
		return fmt.Errorf("disconnect: %w", err)
	}

	return nil
}

func interactiveDiscover(sigURL, room string) (string, error) {
	raw, err := orchestrator.ListServers(sigURL, room)
	if err != nil {
		return "", fmt.Errorf("list servers: %w", err)
	}

	var servers []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &servers); err != nil {
		return "", fmt.Errorf("parse servers: %w", err)
	}

	if len(servers) == 0 {
		return "", fmt.Errorf("no servers found")
	}

	fmt.Fprintln(os.Stderr, "Available Servers:")
	for i, s := range servers {
		name, _ := s["name"].(string)
		method, _ := s["method"].(string)
		transport, _ := s["transport"].(string)
		protocol, _ := s["protocol"].(string)
		sRoom, _ := s["room"].(string)

		desc := method
		if protocol != "" && protocol != method {
			desc += " / " + protocol
		}
		if transport != "" && transport != method {
			desc += " / " + transport
		}

		roomStr := ""
		if sRoom != "" {
			roomStr = " | Room: " + sRoom
		}
		fmt.Fprintf(os.Stderr, "  [%d] %-20s | %-30s%s\n", i+1, name, desc, roomStr)
	}

	fmt.Fprintf(os.Stderr, "Select server (1-%d): ", len(servers))
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	idx, err := strconv.Atoi(input)
	if err != nil || idx < 1 || idx > len(servers) {
		return "", fmt.Errorf("invalid selection: %s", input)
	}

	selected := servers[idx-1]
	code, ok := selected["code"].(string)
	if !ok || code == "" {
		return "", fmt.Errorf("server has no connection code")
	}

	return code, nil
}

func init() {
	f := connectCmd.Flags()

	f.Int("socks-port", 0, "Local SOCKS5 port (default 10808)")
	f.String("stun-server", "", "STUN server")
	f.String("signaling-url", "", "Signaling URL")
	f.String("discovery-url", "", "Discovery server URL")
	f.Bool("discover", false, "Browse servers interactively")
	f.String("room", "", "Filter discovery by room")
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

	viper.BindPFlag("client.socks_port", f.Lookup("socks-port"))
	viper.BindPFlag("stun_server", f.Lookup("stun-server"))
	viper.BindPFlag("signaling_url", f.Lookup("signaling-url"))
	viper.BindPFlag("discovery_url", f.Lookup("discovery-url"))
	viper.BindPFlag("client.sctp.recv_buffer", f.Lookup("sctp-recv-buffer"))
	viper.BindPFlag("client.sctp.rto_max", f.Lookup("sctp-rto-max"))
	viper.BindPFlag("client.sctp.zero_checksum", f.Lookup("sctp-zero-checksum"))
	viper.BindPFlag("client.dtls.retransmit", f.Lookup("dtls-retransmit"))
	viper.BindPFlag("client.dtls.skip_verify", f.Lookup("dtls-skip-verify"))
	viper.BindPFlag("client.dtls.disable_close", f.Lookup("dtls-disable-close"))
	viper.BindPFlag("client.ice.disconn_timeout", f.Lookup("ice-disconn-timeout"))
	viper.BindPFlag("client.ice.failed_timeout", f.Lookup("ice-failed-timeout"))
	viper.BindPFlag("client.ice.keepalive", f.Lookup("ice-keepalive"))
	viper.BindPFlag("client.udp.read_buffer", f.Lookup("udp-read-buffer"))
	viper.BindPFlag("client.udp.write_buffer", f.Lookup("udp-write-buffer"))
	viper.BindPFlag("client.mask_ips", f.Lookup("mask-ips"))

	rootCmd.AddCommand(connectCmd)
}
