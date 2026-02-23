package webrtc

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"time"

	"github.com/pion/rtp"
	pionwebrtc "github.com/pion/webrtc/v4"

	"natproxy/golib/applog"
)

// TransportMode selects between data channel and media stream transport.
type TransportMode int

const (
	// TransportDataChannels uses SCTP data channels (v1, default).
	TransportDataChannels TransportMode = iota
	// TransportMediaStream uses RTP media tracks to look like a video call (v2).
	TransportMediaStream
)

// MediaTrackSetup holds pre-prepared media track state.
// Created by PrepareMediaTrack before SDP negotiation so that tracks
// are included in the SDP offer/answer.
type MediaTrackSetup struct {
	localTrack    *pionwebrtc.TrackLocalStaticRTP
	codec         mediaCodecInfo
	frameRate     int
	ssrc          uint32
	remoteTrackCh chan *pionwebrtc.TrackRemote
}

// PrepareMediaTrack adds a video track to the PeerConnection and registers
// the OnTrack handler. Must be called BEFORE SDP offer/answer creation so
// the track is included in the SDP.
func PrepareMediaTrack(pc *pionwebrtc.PeerConnection, seed []byte) (*MediaTrackSetup, error) {
	codec := selectCodec(seed)
	frameRate := selectFrameRate(seed)

	var ssrc uint32
	if len(seed) >= 12 {
		ssrc = binary.BigEndian.Uint32(seed[8:12])
	} else {
		ssrc = rand.Uint32()
	}

	trackCodec := pionwebrtc.RTPCodecCapability{
		MimeType:  fmt.Sprintf("video/%s", codec.Name),
		ClockRate: codec.ClockRate,
	}

	localTrack, err := pionwebrtc.NewTrackLocalStaticRTP(trackCodec, "video", fmt.Sprintf("natproxy-%d", ssrc))
	if err != nil {
		return nil, fmt.Errorf("create local track: %w", err)
	}

	if _, err := pc.AddTrack(localTrack); err != nil {
		return nil, fmt.Errorf("add track: %w", err)
	}

	remoteTrackCh := make(chan *pionwebrtc.TrackRemote, 1)
	pc.OnTrack(func(track *pionwebrtc.TrackRemote, receiver *pionwebrtc.RTPReceiver) {
		applog.Infof("webrtc media: remote track received: %s ssrc=%d", track.Codec().MimeType, track.SSRC())
		select {
		case remoteTrackCh <- track:
		default:
		}
	})

	applog.Infof("webrtc media: track prepared (codec=%s, fps=%d, ssrc=%d)", codec.Name, frameRate, ssrc)
	return &MediaTrackSetup{
		localTrack:    localTrack,
		codec:         codec,
		frameRate:     frameRate,
		ssrc:          ssrc,
		remoteTrackCh: remoteTrackCh,
	}, nil
}

// MediaStreamTransport wraps a pion/webrtc RTP media track pair into an
// io.ReadWriteCloser. Data is embedded in RTP packets with realistic
// headers (codec, timestamps, SSRC) so traffic is indistinguishable from
// a real video call on the wire. A reliableRTP layer provides ordered,
// reliable delivery on top of the unreliable RTP stream.
type MediaStreamTransport struct {
	pc         *pionwebrtc.PeerConnection
	localTrack *pionwebrtc.TrackLocalStaticRTP
	codec      mediaCodecInfo
	clockRate  uint32
	frameRate  int
	ssrc       uint32

	// RTP sequencing
	seqNum    uint16
	timestamp uint32
	writeMu   sync.Mutex

	// Read side: receives from remote track
	readCh   chan []byte
	readBuf  []byte // leftover from previous read
	readDone chan struct{}

	// RTCP senders
	rtcpDone chan struct{}

	done chan struct{}
}

// NewMediaStreamTransport creates a media stream transport using a
// pre-prepared MediaTrackSetup. The write side is immediately available.
// The read side becomes available asynchronously when the remote track
// arrives (triggered by the remote peer's first RTP write).
func NewMediaStreamTransport(pc *pionwebrtc.PeerConnection, setup *MediaTrackSetup) (*MediaStreamTransport, error) {
	mt := &MediaStreamTransport{
		pc:         pc,
		localTrack: setup.localTrack,
		codec:      setup.codec,
		clockRate:  setup.codec.ClockRate,
		frameRate:  setup.frameRate,
		ssrc:       setup.ssrc,
		seqNum:     uint16(rand.Intn(65536)),
		timestamp:  rand.Uint32(),
		readCh:     make(chan []byte, 4096),
		readDone:   make(chan struct{}),
		rtcpDone:   make(chan struct{}),
		done:       make(chan struct{}),
	}

	// Wait for the remote track asynchronously. The read side blocks on
	// readCh until the remote track arrives and readTrackLoop starts
	// feeding data. The write side works immediately, which is critical:
	// the smux handshake writes trigger OnTrack on the remote peer,
	// breaking the would-be deadlock.
	go func() {
		select {
		case track := <-setup.remoteTrackCh:
			applog.Infof("webrtc media: remote track ready: %s", track.Codec().MimeType)
			go startRTCPReceiver(pc, uint32(track.SSRC()), mt.rtcpDone)
			go mt.readTrackLoop(track)
		case <-time.After(30 * time.Second):
			applog.Warn("webrtc media: timeout waiting for remote track (30s)")
		case <-mt.done:
			return
		}
	}()

	// Start RTCP sender reports for our local track
	go startRTCPSender(pc, setup.ssrc, setup.codec.ClockRate, mt.rtcpDone)

	applog.Infof("webrtc media: transport created (codec=%s, fps=%d, ssrc=%d)",
		setup.codec.Name, setup.frameRate, setup.ssrc)

	return mt, nil
}

// writeRTP sends data as an RTP packet with realistic headers.
func (mt *MediaStreamTransport) writeRTP(payload []byte) error {
	mt.writeMu.Lock()
	mt.seqNum++
	seq := mt.seqNum
	// Advance timestamp by clock_rate/frame_rate per "frame"
	mt.timestamp += mt.clockRate / uint32(mt.frameRate)
	ts := mt.timestamp
	mt.writeMu.Unlock()

	pkt := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			Padding:        false,
			Extension:      false,
			Marker:         true, // end of frame
			PayloadType:    mt.codec.PayloadType,
			SequenceNumber: seq,
			Timestamp:      ts,
			SSRC:           mt.ssrc,
		},
		Payload: payload,
	}

	return mt.localTrack.WriteRTP(pkt)
}

// readTrackLoop reads RTP packets from the remote track and extracts payloads.
func (mt *MediaStreamTransport) readTrackLoop(track *pionwebrtc.TrackRemote) {
	defer close(mt.readDone)

	for {
		select {
		case <-mt.done:
			return
		default:
		}

		pkt, _, err := track.ReadRTP()
		if err != nil {
			select {
			case <-mt.done:
			default:
				applog.Warnf("webrtc media: read RTP: %v", err)
			}
			return
		}

		if len(pkt.Payload) == 0 {
			continue
		}

		// Copy payload to avoid data races with pion's buffer reuse
		payload := make([]byte, len(pkt.Payload))
		copy(payload, pkt.Payload)

		select {
		case mt.readCh <- payload:
		case <-mt.done:
			return
		}
	}
}

// Write sends data over the media stream as RTP packets.
func (mt *MediaStreamTransport) Write(p []byte) (int, error) {
	select {
	case <-mt.done:
		return 0, io.ErrClosedPipe
	default:
	}

	if err := mt.writeRTP(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Read receives data from the remote media stream.
func (mt *MediaStreamTransport) Read(p []byte) (int, error) {
	// Drain leftover from previous read
	if len(mt.readBuf) > 0 {
		n := copy(p, mt.readBuf)
		mt.readBuf = mt.readBuf[n:]
		return n, nil
	}

	select {
	case data, ok := <-mt.readCh:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, data)
		if n < len(data) {
			mt.readBuf = data[n:]
		}
		return n, nil
	case <-mt.done:
		return 0, io.EOF
	}
}

// Close shuts down the media stream transport.
func (mt *MediaStreamTransport) Close() error {
	select {
	case <-mt.done:
		return nil
	default:
		close(mt.done)
	}

	// Stop RTCP goroutines
	select {
	case <-mt.rtcpDone:
	default:
		close(mt.rtcpDone)
	}

	return nil
}

// NewMediaReliableTransport creates a media stream transport wrapped with
// a reliable delivery layer, suitable for use with smux.
func NewMediaReliableTransport(pc *pionwebrtc.PeerConnection, setup *MediaTrackSetup) (io.ReadWriteCloser, error) {
	mt, err := NewMediaStreamTransport(pc, setup)
	if err != nil {
		return nil, err
	}
	return newReliableRTP(mt), nil
}
