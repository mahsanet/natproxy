package webrtc

import (
	"net"
	"time"

	"github.com/pion/stun/v2"

	"natproxy/golib/applog"
)

// StartSTUNKeepalive periodically pings a STUN server to keep the NAT hole open.
// Close the returned channel to stop it.
//
// ~20s interval works well since NAT mappings typically expire at 30-60s.
func StartSTUNKeepalive(conn net.PacketConn, stunServers []string, interval time.Duration) chan struct{} {
	stop := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Resolve STUN server addresses once upfront.
		var addrs []*net.UDPAddr
		for _, s := range stunServers {
			addr, err := net.ResolveUDPAddr("udp4", s)
			if err != nil {
				applog.Warnf("stun-keepalive: resolve %s: %v", s, err)
				continue
			}
			addrs = append(addrs, addr)
		}
		if len(addrs) == 0 {
			applog.Warn("stun-keepalive: no reachable STUN servers, exiting")
			return
		}

		applog.Infof("stun-keepalive: started (interval=%v, servers=%d)", interval, len(addrs))

		for {
			select {
			case <-stop:
				applog.Info("stun-keepalive: stopped")
				return
			case <-ticker.C:
				// Send a binding request to the first server.
				msg := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
				if _, err := conn.WriteTo(msg.Raw, addrs[0]); err != nil {
					applog.Warnf("stun-keepalive: send to %s: %v", addrs[0], err)
				}
			}
		}
	}()

	return stop
}
