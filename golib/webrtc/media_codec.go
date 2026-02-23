package webrtc

import (
	"encoding/binary"
)

// Codec definitions for media stream transport.
type mediaCodecInfo struct {
	Name        string
	PayloadType uint8
	ClockRate   uint32
}

var mediaCodecs = []mediaCodecInfo{
	{Name: "H264", PayloadType: 96, ClockRate: 90000},
	{Name: "VP8", PayloadType: 97, ClockRate: 90000},
	{Name: "VP9", PayloadType: 98, ClockRate: 90000},
	{Name: "AV1", PayloadType: 35, ClockRate: 90000},
}

var frameRates = []int{25, 30, 60}

// selectCodec deterministically selects a codec based on the seed.
func selectCodec(seed []byte) mediaCodecInfo {
	if len(seed) < 4 {
		return mediaCodecs[0]
	}
	idx := int(binary.BigEndian.Uint32(seed[:4])) % len(mediaCodecs)
	return mediaCodecs[idx]
}

// selectFrameRate deterministically selects a frame rate based on the seed.
func selectFrameRate(seed []byte) int {
	if len(seed) < 8 {
		return 30
	}
	idx := int(binary.BigEndian.Uint32(seed[4:8])) % len(frameRates)
	return frameRates[idx]
}
