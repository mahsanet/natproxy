package webrtc

import (
	"math/rand"
	"time"

	"github.com/pion/rtcp"
	pionwebrtc "github.com/pion/webrtc/v4"

	"natproxy/golib/applog"
)

// startRTCPSender periodically sends RTCP Sender Reports to make the
// media stream look realistic. Real video calls send SR every ~5 seconds.
func startRTCPSender(pc *pionwebrtc.PeerConnection, ssrc uint32, clockRate uint32, done <-chan struct{}) {
	ticker := time.NewTicker(jitter(5*time.Second, 0.20))
	defer ticker.Stop()

	var packetCount uint32
	var octetCount uint32

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			// Simulate realistic packet/octet counts for a video call.
			packetCount += uint32(150 + rand.Intn(50)) // ~150-200 packets/5s
			octetCount += uint32(750000 + rand.Intn(250000)) // ~750KB-1MB/5s

			now := time.Now()
			ntpSec := uint32(now.Unix()) + 2208988800 // NTP epoch offset
			ntpFrac := uint32(now.Nanosecond()) * 4 // approximate fractional

			sr := &rtcp.SenderReport{
				SSRC:        ssrc,
				NTPTime:     uint64(ntpSec)<<32 | uint64(ntpFrac),
				RTPTime:     uint32(now.UnixNano()/int64(time.Second/time.Duration(clockRate))),
				PacketCount: packetCount,
				OctetCount:  octetCount,
			}

			if err := pc.WriteRTCP([]rtcp.Packet{sr}); err != nil {
				applog.Warnf("webrtc: RTCP SR send failed: %v", err)
				return
			}
		}
	}
}

// startRTCPReceiver periodically sends RTCP Receiver Reports.
func startRTCPReceiver(pc *pionwebrtc.PeerConnection, ssrc uint32, done <-chan struct{}) {
	ticker := time.NewTicker(jitter(5*time.Second, 0.20))
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			rr := &rtcp.ReceiverReport{
				Reports: []rtcp.ReceptionReport{
					{
						SSRC:               ssrc,
						FractionLost:       0,
						TotalLost:          0,
						LastSequenceNumber: 0,
						Jitter:             uint32(rand.Intn(100)),
					},
				},
			}

			if err := pc.WriteRTCP([]rtcp.Packet{rr}); err != nil {
				applog.Warnf("webrtc: RTCP RR send failed: %v", err)
				return
			}
		}
	}
}
