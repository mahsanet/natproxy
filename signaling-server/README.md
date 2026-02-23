**:gb: English** | **[:iran: فارسی](README.fa.md)**

# NATProxy Signaling Server

Lightweight HTTP + UDP server that coordinates peer-to-peer connections for NATProxy. It handles SDP offer/answer exchange, server discovery with NAT compatibility scoring, and an opaque UDP relay for symmetric NAT fallback. Zero external dependencies — single static Go binary, in-memory state only.

## Quick Start

```bash
cd signaling-server
go build -o signaling-server .
./signaling-server -addr :8080
```

This starts both signaling and discovery on port 8080, plus the UDP relay on port 3478.

### Default Ports

| Service    | Default Address | Description                      |
|------------|-----------------|----------------------------------|
| Signaling  | `:5601`         | SDP offer/answer exchange        |
| Discovery  | `:5602`         | Server listing & browsing        |
| UDP Relay  | `:3478`         | NAT traversal relay fallback     |

## CLI Flags

| Flag               | Default  | Description                                                    |
|--------------------|----------|----------------------------------------------------------------|
| `-addr`            | *(none)* | Listen address for both signaling and discovery (overrides the two below) |
| `-signaling-addr`  | `:5601`  | Signaling listen address (empty to disable)                    |
| `-discovery-addr`  | `:5602`  | Discovery listen address (empty to disable)                    |
| `-relay`           | `:3478`  | UDP relay listen address (empty to disable)                    |

When `-addr` is set, it overrides both `-signaling-addr` and `-discovery-addr` for backward compatibility.

**Examples:**

```bash
# Combined mode — everything on one port
./signaling-server -addr :8080

# Split mode — signaling and discovery on separate ports
./signaling-server -signaling-addr :5601 -discovery-addr :5602

# Disable UDP relay
./signaling-server -addr :8080 -relay ""
```

## API Reference

All request/response bodies are JSON. Body size is limited to **16 KB**.

### Session Endpoints

Sessions store SDP offers and answers for peer connection setup.

#### `POST /session/{id}/offer`

Store peer A's SDP offer.

- **Body:** JSON object (e.g. `{"sdp": "...", "candidates": [...]}`)
- **Response:** `200 OK` — `ok`
- Creates the session if it doesn't exist.
- Notifies any SSE subscribers watching this session's offer stream.

#### `GET /session/{id}/offer`

Retrieve peer A's SDP offer.

- **Response:** `200 OK` — JSON body as stored
- **Error:** `404 Not Found` — session doesn't exist or offer not yet posted

#### `POST /session/{id}/answer`

Store peer B's SDP answer.

- **Body:** JSON object
- **Response:** `200 OK` — `ok`
- An empty SDP (`{"sdp":""}`) clears the answer slot (used for renegotiation).
- Notifies any SSE subscribers waiting for this session's answer.

#### `GET /session/{id}/answer`

Retrieve peer B's SDP answer.

- **Response:** `200 OK` — JSON body as stored
- **Error:** `404 Not Found` — session doesn't exist or answer not yet posted

#### `GET /session/{id}/offer/stream`

SSE stream for offer updates. Stays open and pushes each new offer as it arrives.

- **Content-Type:** `text/event-stream`
- **Event format:** `event: offer\ndata: <json>\n\n`
- Sends the current offer immediately if one exists.
- Sends `: keepalive` comments every 90 seconds.
- Remains open until the client disconnects.

#### `GET /session/{id}/answer/stream`

SSE stream for the answer. Delivers once and closes.

- **Content-Type:** `text/event-stream`
- **Event format:** `event: answer\ndata: <json>\n\n`
- If the answer already exists, fires immediately and closes.
- Otherwise waits for the answer to be posted, delivers it, then closes.
- Sends `: keepalive` comments every 90 seconds while waiting.

### Discovery Endpoints

Discovery allows servers to register themselves and clients to browse available servers.

#### `POST /discovery/register`

Register a server listing.

**Request body:**

```json
{
  "name": "my-server",
  "room": "home",
  "code": "<base64-connection-code>",
  "method": "holepunch",
  "transport": "webrtc",
  "protocol": "vless",
  "nat_mapping": "endpoint_independent",
  "nat_filtering": "address_dependent"
}
```

- `name` — required, max 50 characters
- `code` — required (the connection code clients will use)
- All other fields are optional metadata
- **Dedup:** If a listing with the same `name` already exists, it is replaced (handles app restarts)
- **Response:** `201 Created` — `{"id": "<hex-uuid>"}`
- Broadcasts updated listing to all SSE discovery subscribers

#### `DELETE /discovery/{id}`

Remove a server listing.

- **Response:** `200 OK` — `ok`
- **Error:** `404 Not Found` — listing doesn't exist

#### `POST /discovery/{id}/heartbeat`

Refresh a listing's heartbeat to prevent expiry.

- **Response:** `200 OK` — `ok`
- **Error:** `404 Not Found` — listing doesn't exist (expired or never registered)

#### `GET /discovery/servers`

List active servers.

**Query parameters:**

| Parameter       | Description                                    |
|-----------------|------------------------------------------------|
| `room`          | Filter by room name (empty = all rooms)        |
| `nat_mapping`   | Client's NAT mapping type (for sorting)        |
| `nat_filtering` | Client's NAT filtering type (for sorting)      |

- **Response:** `200 OK` — JSON array of server listings
- When `nat_mapping` is provided, results are sorted by NAT compatibility score (best first)

**Response example:**

```json
[
  {
    "id": "a1b2c3...",
    "name": "my-server",
    "room": "home",
    "code": "<base64>",
    "method": "holepunch",
    "transport": "webrtc",
    "protocol": "vless",
    "nat_mapping": "endpoint_independent",
    "nat_filtering": "address_dependent"
  }
]
```

#### `GET /discovery/stream`

SSE stream of server listing updates. Pushes the full server list whenever a listing is added, removed, or expires.

**Query parameters:**

| Parameter | Description                              |
|-----------|------------------------------------------|
| `room`    | Filter by room name (empty = all rooms)  |

- **Content-Type:** `text/event-stream`
- **Event format:** `event: servers\ndata: <json-array>\n\n`
- Sends the full current list immediately on connect.
- Sends `: keepalive` comments every 30 seconds.

### Health

#### `GET /health`

- **Response:** `200 OK` — `ok`

## UDP Relay

The relay provides NAT traversal fallback for symmetric NAT pairs where hole punching fails. It forwards opaque UDP packets between two peers — it never decrypts or inspects content.

### Registration Protocol

Peers register by sending a **21-byte** UDP packet to the relay:

```
[0xDEADBEEF (4 bytes)][SHA256(sessionID)[:16] (16 bytes)][role (1 byte)]
```

| Offset | Size | Field                              |
|--------|------|------------------------------------|
| 0      | 4    | Magic bytes: `0xDE 0xAD 0xBE 0xEF` |
| 4      | 16   | First 16 bytes of SHA-256 hash of the session ID |
| 20     | 1    | Role: `0x00` = server, `0x01` = client |

- Registration is fire-and-forget (no response sent).
- Subsequent registrations from the same role update the peer's address.

### Data Forwarding

Any UDP packet that is not exactly 21 bytes starting with the magic prefix is treated as data:

- Packets from the server peer are forwarded to the client peer's last-known address.
- Packets from the client peer are forwarded to the server peer's last-known address.
- Source-address routing — the relay maps `addr → (session, role)` on registration.

## NAT Compatibility Scoring

When clients provide their NAT type via query parameters, the server sorts discovery results by compatibility score. The scoring is based on RFC 5780 NAT behavior types.

### Score Matrix

Mapping pair scores (higher = better odds of hole punching):

| Server ↓ \ Client → | EI  | AD  | APD |
|----------------------|-----|-----|-----|
| **EI**               | 100 | 70  | 40  |
| **AD**               | 70  | 30  | 15  |
| **APD**              | 40  | 15  | 5   |

Filtering pair scores:

| Server ↓ \ Client → | EI  | AD  | APD |
|----------------------|-----|-----|-----|
| **EI**               | 100 | 80  | 50  |
| **AD**               | 80  | 40  | 20  |
| **APD**              | 50  | 20  | 10  |

**NAT type abbreviations:**
- **EI** — Endpoint Independent (most permissive, best for P2P)
- **AD** — Address Dependent
- **APD** — Address and Port Dependent (symmetric NAT, hardest for P2P)

### Combined Score Formula

```
score = (mapping_pair_score * 70 + filtering_pair_score * 30) / 100
```

Mapping behavior is weighted more heavily because it has a larger impact on hole-punching success. Unknown NAT types receive a neutral score of 50.

## Expiry & Cleanup

| Resource            | TTL / Expiry                      |
|---------------------|-----------------------------------|
| Sessions            | 5 minutes since last activity     |
| Discovery listings  | 90 seconds since last heartbeat   |
| Relay sessions      | 5 minutes since last packet       |
| Cleanup interval    | Every 1 minute                    |

The cleanup loop also garbage-collects disconnected SSE subscribers.

## Deployment

### Single Binary

```bash
go build -o signaling-server .
```

Produces a static binary with no runtime dependencies. Deploy anywhere that runs Go binaries.

### Cloud Platforms

The server works well on Fly.io, Railway, Render, or any VPS. Requirements:

- **TCP port** for HTTP signaling + discovery
- **UDP port 3478** for the relay (if enabled)

### Reverse Proxy

When behind nginx or Cloudflare, disable response buffering for SSE endpoints:

```nginx
location /session/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header X-Accel-Buffering no;
    proxy_buffering off;
    proxy_cache off;
}

location /discovery/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header X-Accel-Buffering no;
    proxy_buffering off;
    proxy_cache off;
}
```

### Firewall

Open the following ports:

| Port       | Protocol | Service                    |
|------------|----------|----------------------------|
| 5601       | TCP      | Signaling (or custom)      |
| 5602       | TCP      | Discovery (or custom)      |
| 3478       | UDP      | Relay                      |

## Security Notes

This is a **proof-of-concept** server:

- **No authentication** — any client can create sessions and register servers
- **In-memory only** — all state is lost on restart, no persistence layer
- **16 KB body limit** — prevents trivially large payloads (SDP offers/answers are typically ~4 KB each)
- **Ephemeral sessions** — 5-minute expiry limits abuse window
- **Opaque relay** — the relay never decrypts packet content, just forwards bytes
- **Scalability** — suitable for ~100-500 concurrent sessions; for production, add authentication, rate limiting, and persistent storage
