**:gb: English** | **[:iran: فارسی](README.fa.md)**

# NATProxy CLI

Desktop command-line tool for P2P internet sharing. Replicates every feature of the NATProxy Android app for Linux, macOS, and Windows. On desktop, the client exposes a local **SOCKS5 proxy** instead of a TUN/VPN — point any application at the proxy to route traffic through the remote server.

Shares the same Go library (`golib/`) as the Android app, so CLI servers can serve Android clients and vice versa.

## Installation

```bash
cd natproxy-cli
go build -o natproxy-cli .
```

The CLI imports `golib` via a `replace` directive in `go.mod`:

```
replace natproxy/golib => ../golib
```

This means the `golib/` directory must be present alongside `natproxy-cli/`.

## Quick Start

**Server** (device sharing internet):

```bash
./natproxy-cli serve
# Outputs a connection code to stdout
```

**Client** (device using shared internet):

```bash
./natproxy-cli connect <connection-code>
# SOCKS5 proxy ready at 127.0.0.1:10808
```

**Use the proxy:**

```bash
curl --proxy socks5h://127.0.0.1:10808 https://ifconfig.me
```

## Commands

### `serve`

Start the proxy server and share your internet connection. Attempts UPnP port mapping first, then falls back to WebRTC hole punching.

- Connection code is printed to **stdout** (for piping).
- When UPnP succeeds with VLESS protocol, a standard `vless://` link is printed to **stderr** (importable by v2rayNG, Nekoray, etc.).
- Logs and status are printed to **stderr**.
- Runs until `Ctrl+C` (SIGINT/SIGTERM).

```bash
./natproxy-cli serve
./natproxy-cli serve --nat-method holepunch --use-relay
./natproxy-cli serve --manual  # No signaling server needed
```

#### Server Flags

**Network:**

| Flag               | Default                           | Description                    |
|--------------------|-----------------------------------|--------------------------------|
| `--port`           | `10853`                           | Listen port                    |
| `--stun-server`    | `stun.l.google.com:19302`         | STUN server host:port          |
| `--signaling-url`  | `http://[IP]:5601`       | Signaling server URL           |
| `--discovery-url`  | `http://[IP]:5602`       | Discovery server URL           |

**NAT Traversal:**

| Flag                    | Default | Description                              |
|-------------------------|---------|------------------------------------------|
| `--nat-method`          | `auto`  | `auto`, `upnp`, or `holepunch`           |
| `--use-relay`           | `false` | Enable UDP relay fallback for WebRTC     |
| `--upnp-lease-duration` | `3600`  | UPnP lease duration in seconds (0 = indefinite) |
| `--upnp-retries`        | `3`     | UPnP mapping retry attempts              |
| `--ssdp-timeout`        | `3`     | SSDP discovery timeout in seconds        |

**Protocol & Transport (UPnP path / xray-core):**

| Flag            | Default | Description                              |
|-----------------|---------|------------------------------------------|
| `--protocol`    | `vless` | `vless` or `socks`                       |
| `--transport`   | `xhttp` | `kcp` or `xhttp`                         |
| `--uuid`        | *(random)* | VLESS UUID (empty = random on each start) |

**SOCKS (server-side outbound):**

| Flag               | Default   | Description                   |
|--------------------|-----------|-------------------------------|
| `--socks-auth`     | `noauth`  | `noauth` or `password`        |
| `--socks-username` | *(empty)* | SOCKS username                |
| `--socks-password` | *(empty)* | SOCKS password                |
| `--socks-udp`      | `true`    | Enable SOCKS UDP support      |

**KCP (when `--transport kcp`):**

| Flag                     | Default | Description             |
|--------------------------|---------|-------------------------|
| `--kcp-mtu`              | `1350`  | MTU (576-1460)          |
| `--kcp-tti`              | `20`    | TTI in ms (10-100)      |
| `--kcp-uplink-capacity`  | `12`    | Uplink capacity MB/s    |
| `--kcp-downlink-capacity`| `100`   | Downlink capacity MB/s  |
| `--kcp-congestion`       | `true`  | Congestion control      |
| `--kcp-read-buffer`      | `4`     | Read buffer MB          |
| `--kcp-write-buffer`     | `4`     | Write buffer MB         |

**FinalMask (anti-DPI obfuscation):**

| Flag                  | Default        | Description                       |
|-----------------------|----------------|-----------------------------------|
| `--finalmask-type`    | `header-dtls`  | Obfuscation type                  |
| `--finalmask-password`| *(empty)*      | Password for `mkcp-aes128gcm`    |
| `--finalmask-domain`  | *(empty)*      | Domain for `header-dns`           |

**xHTTP (when `--transport xhttp`):**

| Flag           | Default | Description                                 |
|----------------|---------|---------------------------------------------|
| `--xhttp-path` | `/`     | URL path                                    |
| `--xhttp-host` | *(empty)* | Host header                               |
| `--xhttp-mode` | `auto`  | `auto`, `packet-up`, `stream-up`, `stream-one` |

**WebRTC (hole punch path):**

| Flag                      | Default        | Description                        |
|---------------------------|----------------|------------------------------------|
| `--transport-mode`        | `datachannel`  | `datachannel` or `media`           |
| `--num-peer-connections`  | `6`            | Parallel PeerConnections (1-8)     |
| `--num-channels`          | `6`            | Parallel data channels             |
| `--disable-ipv6`          | `false`        | Disable IPv6 ICE candidates        |

**Rate Limiting:**

| Flag               | Default | Description                         |
|--------------------|---------|-------------------------------------|
| `--rate-limit-up`  | `0`     | Upload rate limit in KB/s (0 = unlimited) |
| `--rate-limit-down`| `0`     | Download rate limit in KB/s (0 = unlimited) |

**Traffic Padding:**

| Flag              | Default | Description                    |
|-------------------|---------|--------------------------------|
| `--padding`       | `false` | Enable traffic padding         |
| `--padding-max`   | `256`   | Max padding bytes per write    |
| `--padding-version`| `2`    | Padding version (0=v1, 2=v2 decoy+burst) |

**Smux (stream multiplexer):**

| Flag                       | Default | Description                      |
|----------------------------|---------|----------------------------------|
| `--smux-stream-buffer`     | `2048`  | Per-stream receive buffer KB     |
| `--smux-session-buffer`    | `8192`  | Session receive buffer KB        |
| `--smux-frame-size`        | `32768` | Max frame size bytes             |
| `--smux-keep-alive`        | `10`    | Keepalive interval seconds       |
| `--smux-keep-alive-timeout`| `300`   | Keepalive timeout seconds        |

**Low-Level Tuning:**

| Flag                    | Default | Description                           |
|-------------------------|---------|---------------------------------------|
| `--dc-max-buffered`     | `2048`  | Data channel backpressure high water KB |
| `--dc-low-mark`         | `512`   | Data channel backpressure low water KB  |
| `--sctp-recv-buffer`    | `8192`  | SCTP receive buffer KB                |
| `--sctp-rto-max`        | `2500`  | SCTP max retransmit timeout ms        |
| `--sctp-zero-checksum`  | `true`  | SCTP zero checksum optimization       |
| `--dtls-retransmit`     | `100`   | DTLS retransmission interval ms       |
| `--dtls-skip-verify`    | `true`  | Skip DTLS HelloVerify                 |
| `--dtls-disable-close`  | `true`  | Prevent DTLS close cascading to PeerConnection |
| `--ice-disconn-timeout` | `15000` | ICE disconnected timeout ms           |
| `--ice-failed-timeout`  | `25000` | ICE failed timeout ms                 |
| `--ice-keepalive`       | `2000`  | ICE keepalive interval ms             |
| `--udp-read-buffer`     | `8192`  | Kernel UDP read buffer KB             |
| `--udp-write-buffer`    | `8192`  | Kernel UDP write buffer KB            |

**Discovery:**

| Flag               | Default   | Description                         |
|--------------------|-----------|-------------------------------------|
| `--discovery-name` | *(empty)* | Display name in discovery listing   |
| `--discovery-room` | *(empty)* | Room name for filtering             |
| `--no-discovery`   | `false`   | Disable discovery registration      |

**Other:**

| Flag         | Default | Description                            |
|--------------|---------|----------------------------------------|
| `--manual`   | `false` | Manual signaling mode (no signaling server) |
| `--mask-ips` | `false` | Mask IP addresses in logs              |

---

### `connect [code]`

Connect to a NATProxy server and expose a local SOCKS5 proxy.

- SOCKS5 address is printed to **stdout**.
- Logs and status are printed to **stderr**.
- Auto-detects manual offer codes (codes starting with `M1:`).
- Runs until `Ctrl+C`.

```bash
./natproxy-cli connect <connection-code>
./natproxy-cli connect --discover              # Browse servers interactively
./natproxy-cli connect --discover --room home   # Filter by room
./natproxy-cli connect M1:<offer>               # Manual signaling
```

#### Client Flags

| Flag                    | Default                           | Description                           |
|-------------------------|-----------------------------------|---------------------------------------|
| `--socks-port`          | `10808`                           | Local SOCKS5 port                     |
| `--stun-server`         | `stun.l.google.com:19302`         | STUN server                           |
| `--signaling-url`       | `http://[IP]:5601`       | Signaling server URL                  |
| `--discovery-url`       | `http://[IP]:5602`       | Discovery server URL                  |
| `--discover`            | `false`                           | Browse servers interactively          |
| `--room`                | *(empty)*                         | Filter discovery by room              |
| `--sctp-recv-buffer`    | `8192`                            | SCTP receive buffer KB                |
| `--sctp-rto-max`        | `2500`                            | SCTP max retransmit timeout ms        |
| `--sctp-zero-checksum`  | `true`                            | SCTP zero checksum optimization       |
| `--dtls-retransmit`     | `100`                             | DTLS retransmission interval ms       |
| `--dtls-skip-verify`    | `true`                            | Skip DTLS HelloVerify                 |
| `--dtls-disable-close`  | `true`                            | Prevent DTLS close cascading          |
| `--ice-disconn-timeout` | `15000`                           | ICE disconnected timeout ms           |
| `--ice-failed-timeout`  | `25000`                           | ICE failed timeout ms                 |
| `--ice-keepalive`       | `2000`                            | ICE keepalive interval ms             |
| `--udp-read-buffer`     | `8192`                            | Kernel UDP read buffer KB             |
| `--udp-write-buffer`    | `8192`                            | Kernel UDP write buffer KB            |
| `--mask-ips`            | `false`                           | Mask IP addresses in logs             |

---

### `discover`

List available servers from the discovery service.

```bash
./natproxy-cli discover
./natproxy-cli discover --room office
./natproxy-cli discover --json
```

| Flag               | Default | Description             |
|--------------------|---------|-------------------------|
| `--discovery-url`  | *(config)* | Discovery server URL |
| `--room`           | *(empty)*  | Filter by room       |
| `--json`           | `false`    | Output as JSON array |

**Human-readable output:**

```
Available Servers:
  [1] alice-phone          | holepunch / vless / webrtc   | Room: home
  [2] bob-laptop           | upnp / vless / xhttp
```

**JSON output (`--json`):**

```json
[
  {"id":"a1b2...","name":"alice-phone","room":"home","code":"...","method":"holepunch",...}
]
```

---

### `nat`

Detect your NAT type and public IP using RFC 5780 STUN probes.

```bash
./natproxy-cli nat
./natproxy-cli nat --json
```

| Flag            | Default                     | Description           |
|-----------------|-----------------------------|-----------------------|
| `--stun-server` | `stun.l.google.com:19302`   | STUN server host:port |
| `--json`        | `false`                     | Output as JSON        |

**Human-readable output:**

```
NAT Type:    Endpoint Independent
Public IP:   203.0.113.42
Public Port: 54321
```

**JSON output:**

```json
{
  "nat_type": "EndpointIndependent",
  "mapping": "EndpointIndependent",
  "filtering": "AddressDependent",
  "public_ip": "203.0.113.42",
  "public_port": 54321
}
```

---

### `version`

Print version and commit SHA.

```bash
./natproxy-cli version
# natproxy-cli dev (commit: unknown)
```

Set at build time with ldflags:

```bash
go build -ldflags "-X natproxy/cli/cmd.Version=1.0.0 -X natproxy/cli/cmd.Commit=$(git rev-parse --short HEAD)" .
```

## Manual Signaling

For environments without a signaling server, use manual offer/answer exchange.

### Step-by-Step

**1. Server starts in manual mode:**

```bash
./natproxy-cli serve --manual
# Prints M1:<offer-code> to stdout
```

**2. Copy the offer code to the client:**

```bash
./natproxy-cli connect M1:<offer-code>
# Prints M1A:<answer-code> to stdout
```

**3. Copy the answer code back to the server:**

The server prompts for the answer code on stderr. Paste the `M1A:...` code and press Enter.

**4. Connection established.**

Both sides continue running. The client's SOCKS5 proxy is ready.

### Notes

- No signaling server is needed — codes are exchanged out-of-band (chat, email, etc.)
- The `M1:` prefix identifies offer codes; `M1A:` identifies answer codes
- The `connect` command auto-detects manual offer codes by the `M1:` prefix

## Configuration File

The CLI reads `~/.natproxy-cli` (YAML) for persistent configuration. Command-line flags take precedence over config file values, which take precedence over built-in defaults.

**Priority:** CLI flags > config file > defaults

### Example `~/.natproxy-cli`

```yaml
# Global
stun_server: stun.l.google.com:19302
signaling_url: http://your-server:5601
discovery_url: http://your-server:5602

# Server settings
server:
  port: 10853
  nat_method: auto        # auto | upnp | holepunch
  use_relay: false
  protocol: vless          # vless | socks
  transport: xhttp         # kcp | xhttp
  uuid: ""                 # VLESS UUID (empty = random on each start)
  socks_auth: noauth       # noauth | password
  socks_username: ""
  socks_password: ""
  socks_udp: true
  transport_mode: datachannel  # datachannel | media
  disable_ipv6: false
  num_peer_connections: 6
  num_channels: 6
  rate_limit_up: 0         # KB/s, 0 = unlimited
  rate_limit_down: 0
  mask_ips: false

  # UPnP
  upnp_lease_duration: 3600
  upnp_retries: 3
  ssdp_timeout: 3

  # Discovery
  discovery:
    enabled: true
    name: ""
    room: ""

  # KCP
  kcp:
    mtu: 1350
    tti: 20
    uplink_capacity: 12
    downlink_capacity: 100
    congestion: true
    read_buffer: 4
    write_buffer: 4

  # FinalMask
  finalmask:
    type: header-dtls
    password: ""
    domain: ""

  # xHTTP
  xhttp:
    path: /
    host: ""
    mode: auto

  # Padding
  padding:
    enabled: false
    max: 256
    version: 2

  # Smux
  smux:
    stream_buffer: 2048
    session_buffer: 8192
    frame_size: 32768
    keep_alive: 10
    keep_alive_timeout: 300

  # Data channel
  dc:
    max_buffered: 512
    low_mark: 128

  # SCTP
  sctp:
    recv_buffer: 8192
    rto_max: 2500
    zero_checksum: true

  # DTLS
  dtls:
    retransmit: 100
    skip_verify: true
    disable_close: true

  # ICE
  ice:
    disconn_timeout: 15000
    failed_timeout: 25000
    keepalive: 2000

  # UDP
  udp:
    read_buffer: 8192
    write_buffer: 8192

# Client settings
client:
  socks_port: 10808
  mask_ips: false
  sctp:
    recv_buffer: 8192
    rto_max: 2500
    zero_checksum: true
  dtls:
    retransmit: 100
    skip_verify: true
    disable_close: true
  ice:
    disconn_timeout: 15000
    failed_timeout: 25000
    keepalive: 2000
  udp:
    read_buffer: 8192
    write_buffer: 8192
```

## Scripting

### stdout/stderr Separation

The CLI separates structured output from human-readable output:

| Stream   | Content                                          |
|----------|--------------------------------------------------|
| `stdout` | Connection code, SOCKS address, JSON output      |
| `stderr` | Logs, status updates, VLESS link, interactive prompts |

This makes it easy to capture output in scripts:

```bash
# Capture connection code
CODE=$(./natproxy-cli serve 2>/dev/null)
echo "$CODE" | xclip -selection clipboard

# Capture SOCKS address
SOCKS=$(./natproxy-cli connect "$CODE" 2>/dev/null)
```

### JSON Output

Use `--json` with `discover` and `nat` commands for machine-readable output:

```bash
./natproxy-cli discover --json | jq '.[0].code'
./natproxy-cli nat --json | jq '.nat_type'
```

### Signal Handling

Both `serve` and `connect` handle SIGINT and SIGTERM for clean shutdown. Resources (UPnP mappings, discovery listings, WebRTC connections) are cleaned up before exit.

## Interoperability

Connection codes are compatible between the CLI and the Android app. Both share the same `golib/` Go library:

- A CLI server can serve Android clients
- An Android server can serve CLI clients
- Connection codes work interchangeably

## Differences from Android App

| Feature              | Android App                | CLI                        |
|----------------------|----------------------------|----------------------------|
| Traffic routing      | TUN/VPN (system-wide)      | SOCKS5 proxy (per-app)     |
| Platform             | Android only               | Linux, macOS, Windows      |
| Configuration        | Settings UI                | CLI flags + YAML config    |
| Discovery            | In-app browser             | `discover` command / `--discover` flag |
| Manual signaling     | QR code / paste            | `--manual` flag + stdin    |
| Background operation | Foreground service         | Terminal process           |

See the [root README](../README.md) for architecture details and build instructions.
