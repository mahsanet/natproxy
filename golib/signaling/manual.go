package signaling

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	ManualOfferPrefix  = "M1:"
	ManualAnswerPrefix = "M1A:"
)

// ManualOffer contains the server's SDP offer plus connection metadata,
// encoded into a copy-paste-friendly string.
type ManualOffer struct {
	Version              int    `json:"v"`
	ObfsKey              string `json:"ok"`
	RelayAddr            string `json:"ra,omitempty"`
	NumChannels          int    `json:"nc"`
	SmuxStreamBuffer     int    `json:"ssb,omitempty"`
	SmuxSessionBuffer    int    `json:"srb,omitempty"`
	SmuxFrameSize        int    `json:"sfr,omitempty"`
	DCMaxBuffered        int    `json:"dcb,omitempty"`
	DCLowMark            int    `json:"dcl,omitempty"`
	PaddingMax           int    `json:"pm,omitempty"`
	Padding              bool   `json:"p,omitempty"`
	TransportV           int    `json:"tv,omitempty"`
	SmuxKeepAlive        int    `json:"ska,omitempty"`
	SmuxKeepAliveTimeout int    `json:"skt,omitempty"`
	CompressedSDP        string `json:"sdp"`
}

// ManualAnswer contains the client's SDP answer, encoded into a
// copy-paste-friendly string.
type ManualAnswer struct {
	Version       int    `json:"v"`
	CompressedSDP string `json:"sdp"`
}

// EncodeManualOffer serializes a ManualOffer to "M1:<base64url(zlib(json))>".
func EncodeManualOffer(offer *ManualOffer) (string, error) {
	data, err := json.Marshal(offer)
	if err != nil {
		return "", fmt.Errorf("marshal offer: %w", err)
	}

	compressed, err := deflateBytes(data)
	if err != nil {
		return "", err
	}

	return ManualOfferPrefix + base64.RawURLEncoding.EncodeToString(compressed), nil
}

// DecodeManualOffer parses a "M1:..." code into a ManualOffer.
func DecodeManualOffer(code string) (*ManualOffer, error) {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, ManualOfferPrefix) {
		return nil, fmt.Errorf("invalid manual offer: missing %s prefix", ManualOfferPrefix)
	}
	payload := code[len(ManualOfferPrefix):]

	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	jsonData, err := inflateBytes(data)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}

	var offer ManualOffer
	if err := json.Unmarshal(jsonData, &offer); err != nil {
		return nil, fmt.Errorf("unmarshal offer: %w", err)
	}

	return &offer, nil
}

// EncodeManualAnswer serializes a ManualAnswer to "M1A:<base64url(zlib(json))>".
func EncodeManualAnswer(answer *ManualAnswer) (string, error) {
	data, err := json.Marshal(answer)
	if err != nil {
		return "", fmt.Errorf("marshal answer: %w", err)
	}

	compressed, err := deflateBytes(data)
	if err != nil {
		return "", err
	}

	return ManualAnswerPrefix + base64.RawURLEncoding.EncodeToString(compressed), nil
}

// DecodeManualAnswer parses a "M1A:..." code into a ManualAnswer.
func DecodeManualAnswer(code string) (*ManualAnswer, error) {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, ManualAnswerPrefix) {
		return nil, fmt.Errorf("invalid manual answer: missing %s prefix", ManualAnswerPrefix)
	}
	payload := code[len(ManualAnswerPrefix):]

	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	jsonData, err := inflateBytes(data)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}

	var answer ManualAnswer
	if err := json.Unmarshal(jsonData, &answer); err != nil {
		return nil, fmt.Errorf("unmarshal answer: %w", err)
	}

	return &answer, nil
}

// deflateBytes compresses data with flate level 6.
func deflateBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, 6)
	if err != nil {
		return nil, fmt.Errorf("create flate writer: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("flate write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("flate close: %w", err)
	}
	return buf.Bytes(), nil
}

// inflateBytes decompresses flate-compressed data.
func inflateBytes(data []byte) ([]byte, error) {
	r := flate.NewReader(bytes.NewReader(data))
	defer r.Close()
	return io.ReadAll(r)
}
