package signaling

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// SDP line prefixes to keep for data-channel ICE negotiation.
var sdpKeepPrefixes = []string{
	"v=",
	"o=",
	"s=",
	"t=",
	"m=",
	"c=",
	"a=ice-ufrag:",
	"a=ice-pwd:",
	"a=fingerprint:",
	"a=setup:",
	"a=mid:",
	"a=sctp-port:",
	"a=max-message-size:",
	"a=candidate:",
	"a=end-of-candidates",
}

// MinifySDP strips SDP down to just what's needed for ICE/data-channel negotiation.
// Usually cuts the size by 60-70%.
func MinifySDP(sdp string) string {
	var kept []string
	for _, line := range strings.Split(sdp, "\r\n") {
		if line == "" {
			continue
		}
		for _, prefix := range sdpKeepPrefixes {
			if strings.HasPrefix(line, prefix) {
				kept = append(kept, line)
				break
			}
		}
	}
	if len(kept) == 0 {
		return sdp
	}
	return strings.Join(kept, "\r\n") + "\r\n"
}

// CompressSDP minifies, then zlib-compresses, then base64url-encodes an SDP.
func CompressSDP(sdp string) (string, error) {
	minified := MinifySDP(sdp)

	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, 6)
	if err != nil {
		return "", fmt.Errorf("create flate writer: %w", err)
	}
	if _, err := w.Write([]byte(minified)); err != nil {
		return "", fmt.Errorf("flate write: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("flate close: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}

// DecompressSDP undoes CompressSDP — note it gives back the minified SDP, not the original.
func DecompressSDP(compressed string) (string, error) {
	data, err := base64.RawURLEncoding.DecodeString(compressed)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	r := flate.NewReader(bytes.NewReader(data))
	defer r.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("flate decompress: %w", err)
	}

	sdp := string(out)
	if !strings.Contains(sdp, "v=") {
		return "", fmt.Errorf("decompressed data does not look like SDP")
	}

	return sdp, nil
}
