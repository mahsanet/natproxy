// Package signaling implements the client side of the signaling protocol
// for exchanging peer endpoint information during NAT hole punching.
package signaling

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"natproxy/golib/util"
)

// ErrListingExpired is returned by HeartbeatServer when the signaling server
// responds with 404, indicating the listing was already expired/deleted.
// Callers should re-register when they receive this error.
var ErrListingExpired = errors.New("listing expired (404)")

// encodeBase64Signaling encodes bytes to base64 for signaling transport.
func encodeBase64Signaling(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// decodeBase64Signaling decodes base64 from signaling transport.
func decodeBase64Signaling(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

const (
	httpTimeout       = 20 * time.Second
	peerInfoRetries   = 30
	peerRetryInterval = 2 * time.Second
)

var httpClient = &http.Client{Timeout: httpTimeout}

type peerInfo struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

// PostOffer sends the server's public endpoint to the signaling server.
func PostOffer(signalingURL, sessionID string, ip string, port int) error {
	info := peerInfo{IP: ip, Port: port}
	body, _ := json.Marshal(info)

	url := fmt.Sprintf("%s/session/%s/offer", signalingURL, sessionID)
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post offer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("post offer: status %d", resp.StatusCode)
	}
	return nil
}

// GetOffer retrieves the server's public endpoint from the signaling server.
// Retries up to maxRetries times with retryInterval between attempts.
func GetOffer(signalingURL, sessionID string) (string, int, error) {
	return getPeerInfo(fmt.Sprintf("%s/session/%s/offer", signalingURL, sessionID))
}

// PostAnswer sends the client's public endpoint to the signaling server.
func PostAnswer(signalingURL, sessionID string, ip string, port int) error {
	info := peerInfo{IP: ip, Port: port}
	body, _ := json.Marshal(info)

	url := fmt.Sprintf("%s/session/%s/answer", signalingURL, sessionID)
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post answer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("post answer: status %d", resp.StatusCode)
	}
	return nil
}

// GetAnswer retrieves the client's public endpoint from the signaling server.
func GetAnswer(signalingURL, sessionID string) (string, int, error) {
	return getPeerInfo(fmt.Sprintf("%s/session/%s/answer", signalingURL, sessionID))
}

// --- SDP signaling (for WebRTC hole punch path) ---

type sdpPayload struct {
	SDP string `json:"sdp"`
}

// PostSDPOffer posts a WebRTC SDP offer string to the signaling server.
// If client is non-nil it is used instead of the default (e.g. for protected sockets).
// If obfsKey is non-nil, the SDP payload is encrypted before posting.
func PostSDPOffer(signalingURL, sessionID, sdp string, client *http.Client, obfsKey ...[]byte) error {
	if client == nil {
		client = httpClient
	}

	payload := sdp
	if len(obfsKey) > 0 && len(obfsKey[0]) > 0 {
		encrypted, err := EncryptPayload([]byte(sdp), obfsKey[0])
		if err != nil {
			return fmt.Errorf("encrypt SDP offer: %w", err)
		}
		payload = encodeBase64Signaling(encrypted)
	}

	body, _ := json.Marshal(sdpPayload{SDP: payload})
	url := fmt.Sprintf("%s/session/%s/offer", signalingURL, sessionID)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post SDP offer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("post SDP offer: status %d", resp.StatusCode)
	}
	return nil
}

// GetSDPOffer retrieves a WebRTC SDP offer string from the signaling server.
// If client is non-nil it is used instead of the default.
// If obfsKey is provided, the SDP payload is decrypted after retrieval.
func GetSDPOffer(signalingURL, sessionID string, client *http.Client, obfsKey ...[]byte) (string, error) {
	raw, err := getSDPInfo(fmt.Sprintf("%s/session/%s/offer", signalingURL, sessionID), client)
	if err != nil {
		return "", err
	}
	if len(obfsKey) > 0 && len(obfsKey[0]) > 0 {
		return decryptSDPPayload(raw, obfsKey[0])
	}
	return raw, nil
}

// PostSDPAnswer posts a WebRTC SDP answer string to the signaling server.
// If client is non-nil it is used instead of the default.
// If obfsKey is provided, the SDP payload is encrypted before posting.
func PostSDPAnswer(signalingURL, sessionID, sdp string, client *http.Client, obfsKey ...[]byte) error {
	if client == nil {
		client = httpClient
	}

	payload := sdp
	if len(obfsKey) > 0 && len(obfsKey[0]) > 0 && sdp != "" {
		encrypted, err := EncryptPayload([]byte(sdp), obfsKey[0])
		if err != nil {
			return fmt.Errorf("encrypt SDP answer: %w", err)
		}
		payload = encodeBase64Signaling(encrypted)
	}

	body, _ := json.Marshal(sdpPayload{SDP: payload})
	url := fmt.Sprintf("%s/session/%s/answer", signalingURL, sessionID)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post SDP answer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("post SDP answer: status %d", resp.StatusCode)
	}
	return nil
}

// GetSDPAnswer retrieves a WebRTC SDP answer string from the signaling server.
// If client is non-nil it is used instead of the default.
// If obfsKey is provided, the SDP payload is decrypted after retrieval.
func GetSDPAnswer(signalingURL, sessionID string, client *http.Client, obfsKey ...[]byte) (string, error) {
	raw, err := getSDPInfo(fmt.Sprintf("%s/session/%s/answer", signalingURL, sessionID), client)
	if err != nil {
		return "", err
	}
	if len(obfsKey) > 0 && len(obfsKey[0]) > 0 {
		return decryptSDPPayload(raw, obfsKey[0])
	}
	return raw, nil
}

// decryptSDPPayload decrypts a base64-encoded encrypted SDP payload.
func decryptSDPPayload(encoded string, obfsKey []byte) (string, error) {
	data, err := decodeBase64Signaling(encoded)
	if err != nil {
		return "", fmt.Errorf("decode encrypted SDP: %w", err)
	}

	// Anti-replay check
	if len(data) >= 12 && !globalReplayGuard.Check(data[:12]) {
		return "", fmt.Errorf("SDP replay detected")
	}

	plaintext, err := DecryptPayload(data, obfsKey)
	if err != nil {
		return "", fmt.Errorf("decrypt SDP: %w", err)
	}
	return string(plaintext), nil
}

func getSDPInfo(url string, client *http.Client) (string, error) {
	if client == nil {
		client = httpClient
	}
	backoff := util.NewBackoff(1*time.Second, 10*time.Second, 0.5)
	deadline := time.Now().Add(time.Duration(peerInfoRetries) * peerRetryInterval)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			time.Sleep(backoff.Next())
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			time.Sleep(backoff.Next())
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("read response: %w", err)
		}

		var payload sdpPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			return "", fmt.Errorf("parse SDP payload: %w", err)
		}

		if payload.SDP == "" {
			// Empty SDP means it was cleared (stale data removed);
			// treat like not-yet-available and keep retrying.
			time.Sleep(backoff.Next())
			continue
		}

		return payload.SDP, nil
	}

	return "", fmt.Errorf("SDP not available within timeout")
}

// --- Multi-SDP signaling (for multi-PeerConnection) ---
//
// The SDP field becomes a JSON array of SDP strings. The signaling server
// is unchanged — it stores opaque blobs. Always uses array format even for
// npc=1 to keep the wire protocol uniform.

// PostSDPOffers posts N SDP offers as a JSON array string.
func PostSDPOffers(signalingURL, sessionID string, sdps []string, client *http.Client, obfsKey ...[]byte) error {
	if client == nil {
		client = httpClient
	}

	// Marshal []string → JSON array string
	arrayJSON, err := json.Marshal(sdps)
	if err != nil {
		return fmt.Errorf("marshal SDP offers: %w", err)
	}
	payload := string(arrayJSON)

	if len(obfsKey) > 0 && len(obfsKey[0]) > 0 {
		encrypted, err := EncryptPayload([]byte(payload), obfsKey[0])
		if err != nil {
			return fmt.Errorf("encrypt SDP offers: %w", err)
		}
		payload = encodeBase64Signaling(encrypted)
	}

	body, _ := json.Marshal(sdpPayload{SDP: payload})
	url := fmt.Sprintf("%s/session/%s/offer", signalingURL, sessionID)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post SDP offers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("post SDP offers: status %d", resp.StatusCode)
	}
	return nil
}

// GetSDPOffers retrieves N SDP offers from the signaling server.
func GetSDPOffers(signalingURL, sessionID string, client *http.Client, obfsKey ...[]byte) ([]string, error) {
	raw, err := getSDPInfo(fmt.Sprintf("%s/session/%s/offer", signalingURL, sessionID), client)
	if err != nil {
		return nil, err
	}
	if len(obfsKey) > 0 && len(obfsKey[0]) > 0 {
		decrypted, err := decryptSDPPayload(raw, obfsKey[0])
		if err != nil {
			return nil, err
		}
		raw = decrypted
	}

	var sdps []string
	if err := json.Unmarshal([]byte(raw), &sdps); err != nil {
		// Backward compatibility: server posted a single raw SDP string
		sdps = []string{raw}
	}
	return sdps, nil
}

// PostSDPAnswers posts N SDP answers as a JSON array string.
// Pass nil sdps to clear the answer slot.
func PostSDPAnswers(signalingURL, sessionID string, sdps []string, client *http.Client, obfsKey ...[]byte) error {
	if client == nil {
		client = httpClient
	}

	// nil sdps = clear the slot (post empty SDP)
	payload := ""
	if sdps != nil {
		arrayJSON, err := json.Marshal(sdps)
		if err != nil {
			return fmt.Errorf("marshal SDP answers: %w", err)
		}
		payload = string(arrayJSON)
	}

	if len(obfsKey) > 0 && len(obfsKey[0]) > 0 && payload != "" {
		encrypted, err := EncryptPayload([]byte(payload), obfsKey[0])
		if err != nil {
			return fmt.Errorf("encrypt SDP answers: %w", err)
		}
		payload = encodeBase64Signaling(encrypted)
	}

	body, _ := json.Marshal(sdpPayload{SDP: payload})
	url := fmt.Sprintf("%s/session/%s/answer", signalingURL, sessionID)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post SDP answers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("post SDP answers: status %d", resp.StatusCode)
	}
	return nil
}

// GetSDPAnswers retrieves N SDP answers from the signaling server.
func GetSDPAnswers(signalingURL, sessionID string, client *http.Client, obfsKey ...[]byte) ([]string, error) {
	raw, err := getSDPInfo(fmt.Sprintf("%s/session/%s/answer", signalingURL, sessionID), client)
	if err != nil {
		return nil, err
	}
	if len(obfsKey) > 0 && len(obfsKey[0]) > 0 {
		decrypted, err := decryptSDPPayload(raw, obfsKey[0])
		if err != nil {
			return nil, err
		}
		raw = decrypted
	}

	var sdps []string
	if err := json.Unmarshal([]byte(raw), &sdps); err != nil {
		// Backward compatibility: client posted a single raw SDP string
		sdps = []string{raw}
	}
	return sdps, nil
}

// --- Context-aware SDP signaling ---

// getSDPInfoCtx is like getSDPInfo but supports context cancellation.
func getSDPInfoCtx(ctx context.Context, url string, client *http.Client) (string, error) {
	if client == nil {
		client = httpClient
	}
	backoff := util.NewBackoff(1*time.Second, 10*time.Second, 0.5)
	deadline := time.Now().Add(time.Duration(peerInfoRetries) * peerRetryInterval)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("cancelled: %w", ctx.Err())
		default:
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return "", fmt.Errorf("create request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return "", fmt.Errorf("cancelled: %w", ctx.Err())
			}
			time.Sleep(backoff.Next())
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			time.Sleep(backoff.Next())
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("read response: %w", err)
		}

		var payload sdpPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			return "", fmt.Errorf("parse SDP payload: %w", err)
		}

		if payload.SDP == "" {
			time.Sleep(backoff.Next())
			continue
		}

		return payload.SDP, nil
	}

	return "", fmt.Errorf("SDP not available within timeout")
}

// GetSDPOffersCtx retrieves N SDP offers with context cancellation support.
func GetSDPOffersCtx(ctx context.Context, signalingURL, sessionID string, client *http.Client, obfsKey ...[]byte) ([]string, error) {
	raw, err := getSDPInfoCtx(ctx, fmt.Sprintf("%s/session/%s/offer", signalingURL, sessionID), client)
	if err != nil {
		return nil, err
	}
	if len(obfsKey) > 0 && len(obfsKey[0]) > 0 {
		decrypted, err := decryptSDPPayload(raw, obfsKey[0])
		if err != nil {
			return nil, err
		}
		raw = decrypted
	}

	var sdps []string
	if err := json.Unmarshal([]byte(raw), &sdps); err != nil {
		sdps = []string{raw}
	}
	return sdps, nil
}

// PostSDPAnswersCtx posts N SDP answers with context cancellation support.
func PostSDPAnswersCtx(ctx context.Context, signalingURL, sessionID string, sdps []string, client *http.Client, obfsKey ...[]byte) error {
	if client == nil {
		client = httpClient
	}

	payload := ""
	if sdps != nil {
		arrayJSON, err := json.Marshal(sdps)
		if err != nil {
			return fmt.Errorf("marshal SDP answers: %w", err)
		}
		payload = string(arrayJSON)
	}

	if len(obfsKey) > 0 && len(obfsKey[0]) > 0 && payload != "" {
		encrypted, err := EncryptPayload([]byte(payload), obfsKey[0])
		if err != nil {
			return fmt.Errorf("encrypt SDP answers: %w", err)
		}
		payload = encodeBase64Signaling(encrypted)
	}

	body, _ := json.Marshal(sdpPayload{SDP: payload})
	url := fmt.Sprintf("%s/session/%s/answer", signalingURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post SDP answers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("post SDP answers: status %d", resp.StatusCode)
	}
	return nil
}

// --- SSE-based SDP answer retrieval ---

// WaitSDPAnswersSSE subscribes to the signaling server's SSE stream waiting for an SDP answer.
// Callers should fall back to pollForSDPAnswers if this errors.
func WaitSDPAnswersSSE(ctx context.Context, signalingURL, sessionID string, client *http.Client, obfsKey ...[]byte) ([]string, error) {
	if client == nil {
		client = httpClient
	}

	url := fmt.Sprintf("%s/session/%s/answer/stream", signalingURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SSE connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SSE stream: status %d", resp.StatusCode)
	}

	// Parse SSE stream line by line, looking for "data: " lines.
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		// Parse the outer JSON blob (the raw session answer body).
		var payload sdpPayload
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil, fmt.Errorf("parse SSE SDP payload: %w", err)
		}

		if payload.SDP == "" {
			continue
		}

		raw := payload.SDP
		if len(obfsKey) > 0 && len(obfsKey[0]) > 0 {
			decrypted, err := decryptSDPPayload(raw, obfsKey[0])
			if err != nil {
				return nil, err
			}
			raw = decrypted
		}

		var sdps []string
		if err := json.Unmarshal([]byte(raw), &sdps); err != nil {
			// Backward compatibility: single SDP string.
			sdps = []string{raw}
		}

		if len(sdps) == 0 {
			continue
		}

		return sdps, nil
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("SSE read: %w", err)
	}

	return nil, fmt.Errorf("SSE stream ended without answer")
}

// --- SSE-based SDP offer streaming ---

// StreamSDPOffers connects to the signaling server's offer SSE stream and
// returns a channel that delivers []string SDP offers. The first value is the
// initial offers (or the first to arrive); subsequent values are pushed when
// the server refreshes offers (e.g. NAT keepalive). The channel is closed when
// the stream ends or ctx is cancelled.
func StreamSDPOffers(ctx context.Context, signalingURL, sessionID string, client *http.Client, obfsKey ...[]byte) (<-chan []string, error) {
	if client == nil {
		client = httpClient
	}

	url := fmt.Sprintf("%s/session/%s/offer/stream", signalingURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create offer SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("offer SSE connect: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("offer SSE stream: status %d", resp.StatusCode)
	}

	ch := make(chan []string, 2)

	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			var payload sdpPayload
			if err := json.Unmarshal([]byte(data), &payload); err != nil {
				continue // transient error, don't kill stream
			}

			if payload.SDP == "" {
				continue
			}

			raw := payload.SDP
			if len(obfsKey) > 0 && len(obfsKey[0]) > 0 {
				decrypted, err := decryptSDPPayload(raw, obfsKey[0])
				if err != nil {
					continue // skip bad payload
				}
				raw = decrypted
			}

			var sdps []string
			if err := json.Unmarshal([]byte(raw), &sdps); err != nil {
				sdps = []string{raw}
			}

			if len(sdps) == 0 {
				continue
			}

			select {
			case ch <- sdps:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// --- Discovery API (unchanged) ---

// RegisterServer lists a server on the signaling server for discovery.
// natMapping and natFiltering are optional but help clients find compatible servers.
func RegisterServer(signalingURL, name, room, connectionCode, method, transport, protocol string, natInfo ...string) (string, error) {
	data := map[string]string{
		"name":      name,
		"room":      room,
		"code":      connectionCode,
		"method":    method,
		"transport": transport,
		"protocol":  protocol,
	}
	if len(natInfo) >= 1 && natInfo[0] != "" {
		data["nat_mapping"] = natInfo[0]
	}
	if len(natInfo) >= 2 && natInfo[1] != "" {
		data["nat_filtering"] = natInfo[1]
	}
	payload, _ := json.Marshal(data)

	url := fmt.Sprintf("%s/discovery/register", signalingURL)
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("register server: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 201 {
		return "", fmt.Errorf("register server: status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("register server: parse response: %w", err)
	}
	return result.ID, nil
}

// DeregisterServer removes a server listing from the signaling server.
func DeregisterServer(signalingURL, listingID string) error {
	url := fmt.Sprintf("%s/discovery/%s", signalingURL, listingID)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deregister server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("deregister server: status %d", resp.StatusCode)
	}
	return nil
}

// HeartbeatServer pings the signaling server to keep the listing alive.
// ErrListingExpired is returned when the server has already dropped it (404).
func HeartbeatServer(signalingURL, listingID string) error {
	url := fmt.Sprintf("%s/discovery/%s/heartbeat", signalingURL, listingID)
	resp, err := httpClient.Post(url, "application/json", nil)
	if err != nil {
		return fmt.Errorf("heartbeat server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrListingExpired
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("heartbeat server: status %d", resp.StatusCode)
	}
	return nil
}

// ListServers fetches available servers from the signaling server.
// Pass natInfo (mapping, filtering) so the server can sort by NAT compatibility.
// Returns raw JSON as a string — gomobile can't handle slices of structs.
func ListServers(signalingURL, room string, natInfo ...string) (string, error) {
	url := fmt.Sprintf("%s/discovery/servers", signalingURL)
	sep := "?"
	if room != "" {
		url += sep + "room=" + room
		sep = "&"
	}
	if len(natInfo) >= 1 && natInfo[0] != "" {
		url += sep + "nat_mapping=" + natInfo[0]
		sep = "&"
	}
	if len(natInfo) >= 2 && natInfo[1] != "" {
		url += sep + "nat_filtering=" + natInfo[1]
	}

	resp, err := httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("list servers: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("list servers: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("list servers: status %d", resp.StatusCode)
	}
	return string(body), nil
}

func getPeerInfo(url string) (string, int, error) {
	backoff := util.NewBackoff(1*time.Second, 10*time.Second, 0.5)
	deadline := time.Now().Add(time.Duration(peerInfoRetries) * peerRetryInterval)
	for time.Now().Before(deadline) {
		resp, err := httpClient.Get(url)
		if err != nil {
			time.Sleep(backoff.Next())
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			time.Sleep(backoff.Next())
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", 0, fmt.Errorf("read response: %w", err)
		}

		var info peerInfo
		if err := json.Unmarshal(body, &info); err != nil {
			return "", 0, fmt.Errorf("parse peer info: %w", err)
		}

		return info.IP, info.Port, nil
	}

	return "", 0, fmt.Errorf("peer info not available within timeout")
}
