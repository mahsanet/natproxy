package config

import (
	"github.com/spf13/viper"
)

func Init() {
	viper.SetConfigName(".natproxy-cli")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("$HOME")
	viper.AddConfigPath(".")

	// Global defaults
	viper.SetDefault("stun_server", DefaultStunServer)
	viper.SetDefault("signaling_url", DefaultSignalingURL)
	viper.SetDefault("discovery_url", DefaultDiscoveryURL)

	// Server defaults
	viper.SetDefault("server.port", DefaultListenPort)
	viper.SetDefault("server.nat_method", "auto")
	viper.SetDefault("server.use_relay", false)
	viper.SetDefault("server.protocol", "vless")
	viper.SetDefault("server.transport", "xhttp")
	viper.SetDefault("server.upnp_lease_duration", 3600)
	viper.SetDefault("server.upnp_retries", 3)
	viper.SetDefault("server.ssdp_timeout", 3)
	viper.SetDefault("server.socks_auth", "noauth")
	viper.SetDefault("server.socks_udp", true)
	viper.SetDefault("server.kcp.mtu", 1350)
	viper.SetDefault("server.kcp.tti", 20)
	viper.SetDefault("server.kcp.uplink_capacity", 12)
	viper.SetDefault("server.kcp.downlink_capacity", 100)
	viper.SetDefault("server.kcp.congestion", true)
	viper.SetDefault("server.kcp.read_buffer", 4)
	viper.SetDefault("server.kcp.write_buffer", 4)
	viper.SetDefault("server.finalmask.type", "header-dtls")
	viper.SetDefault("server.xhttp.path", "/")
	viper.SetDefault("server.xhttp.mode", "auto")
	viper.SetDefault("server.transport_mode", "datachannel")
	viper.SetDefault("server.disable_ipv6", false)
	viper.SetDefault("server.rate_limit_up", 0)
	viper.SetDefault("server.rate_limit_down", 0)
	viper.SetDefault("server.discovery.enabled", true)
	// Server - Padding
	viper.SetDefault("server.padding.enabled", false)
	viper.SetDefault("server.padding.max", 256)
	viper.SetDefault("server.padding.version", 2)
	// Server - WebRTC channels & smux
	viper.SetDefault("server.num_channels", 6)
	viper.SetDefault("server.smux.stream_buffer", 2048)
	viper.SetDefault("server.smux.session_buffer", 8192)
	viper.SetDefault("server.smux.frame_size", 32768)
	viper.SetDefault("server.smux.keep_alive", 10)
	viper.SetDefault("server.smux.keep_alive_timeout", 300)
	// Server - Data channel
	viper.SetDefault("server.dc.max_buffered", 512)
	viper.SetDefault("server.dc.low_mark", 128)
	// Server - SCTP
	viper.SetDefault("server.sctp.recv_buffer", 8192)
	viper.SetDefault("server.sctp.rto_max", 2500)
	viper.SetDefault("server.sctp.zero_checksum", true)
	// Server - DTLS
	viper.SetDefault("server.dtls.retransmit", 100)
	viper.SetDefault("server.dtls.skip_verify", true)
	viper.SetDefault("server.dtls.disable_close", true)
	// Server - ICE
	viper.SetDefault("server.ice.disconn_timeout", 15000)
	viper.SetDefault("server.ice.failed_timeout", 25000)
	viper.SetDefault("server.ice.keepalive", 2000)
	// Server - UDP
	viper.SetDefault("server.udp.read_buffer", 8192)
	viper.SetDefault("server.udp.write_buffer", 8192)
	// Server - Logging
	viper.SetDefault("server.mask_ips", false)

	// Client defaults
	viper.SetDefault("client.socks_port", DefaultSocksPort)
	// Client - SCTP
	viper.SetDefault("client.sctp.recv_buffer", 8192)
	viper.SetDefault("client.sctp.rto_max", 2500)
	viper.SetDefault("client.sctp.zero_checksum", true)
	// Client - DTLS
	viper.SetDefault("client.dtls.retransmit", 100)
	viper.SetDefault("client.dtls.skip_verify", true)
	viper.SetDefault("client.dtls.disable_close", true)
	// Client - ICE
	viper.SetDefault("client.ice.disconn_timeout", 15000)
	viper.SetDefault("client.ice.failed_timeout", 25000)
	viper.SetDefault("client.ice.keepalive", 2000)
	// Client - UDP
	viper.SetDefault("client.udp.read_buffer", 8192)
	viper.SetDefault("client.udp.write_buffer", 8192)
	// Client - Logging
	viper.SetDefault("client.mask_ips", false)

	_ = viper.ReadInConfig()
}
