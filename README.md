**:gb: English** | **[:iran: فارسی](README.fa.md)**

# NATProxy

**P2P Internet Sharing for Android + Desktop**

Share a device's internet connection with other devices over the internet using NAT traversal — no port forwarding setup required. A "server" device shares its connection, and "client" devices route their traffic through it via a WebRTC data channel or xray-core proxy tunnel.

Three components:
- **Android App** — Flutter App with VPN (TUN interface)
- **Desktop CLI** — Go CLI tool with local SOCKS5 proxy
- **Signaling Server** — Lightweight Go server for peer discovery and SDP exchange

## Features

- **NAT traversal cascade** — tries UPnP port mapping first, falls back to WebRTC ICE hole punching, then UDP relay
- **Server discovery** — browse available servers with NAT compatibility scoring (best hole-punch candidates ranked first)
- **Anti-DPI obfuscation** — DTLS fingerprint randomization (forked pion/dtls), FinalMask traffic obfuscation, configurable traffic padding (off by default)
- **SDP compression** — connection codes compressed with zlib + base64 for easy sharing
- **Parallel PeerConnections** — multiple WebRTC connections for increased throughput (configurable 1-8)
- **Manual signaling** — exchange offer/answer codes without a signaling server
- **DNS caching** — built-in DNS cache to reduce latency
- **xray-core selective imports** — only VLESS (XHTTP, KCP, SOCKS) are linked
- **VLESS link generation** — when UPnP succeeds with VLESS protocol, generates a standard `vless://` link importable by v2rayNG, v2rayN, Nekoray, and other xray compatible clients
- **Desktop CLI** — full featured CLI tool with SOCKS5 proxy, YAML config, and JSON output for scripting

## Quick Start

### Prerequisites

| Tool           | Version   | Notes                                      |
|----------------|-----------|--------------------------------------------|
| Go             | 1.25+     | For golib and signaling server             |
| gomobile       | latest    | `go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init` |
| Dart SDK       | ^3.10.8   | Comes with Flutter                         |
| Flutter        | latest    | For the Android app                        |
| Android SDK    | API 24+   | minSdk 24                                  |
| Java           | 17        | Required by Android Gradle plugin          |

### 1. Environment Setup (optional)

The app ships with default values in `lib/config/env_config.dart`, so this step is optional. To customize:

```bash
cp .env.example .env
# Edit .env with your signaling server URL, STUN server, etc.
dart scripts/apply_env.dart
```

### 2. Build the Go Library

```bash
./build-android.sh arm          # arm64 + arm (most devices)
./build-android.sh x86          # x86_64 + x86 (emulators)
./build-android.sh universal    # All four ABIs
./build-android.sh arm split    # Per-ABI APKs (smaller downloads)
```

Or manually:

```bash
cd golib
go mod tidy
gomobile bind -v \
  -ldflags="-checklinkname=0" \
  -target=android/arm64,android/arm \
  -androidapi=24 \
  -o ../android/app/libs/golib.aar \
  ./
```

> **Note:** `-checklinkname=0` is required because pion/webrtc's transitive dependency `wlynxg/anet` uses `//go:linkname` (restricted since Go 1.23).

### 3. Build the Android App

```bash
flutter build apk
# or
flutter run -d android
```

### 4. Run the Signaling Server

```bash
cd signaling-server
go run . -addr :8080
```

See [signaling-server/README.md](signaling-server/README.md) for deployment options.

### 5. Run the Desktop CLI (optional)

```bash
cd natproxy-cli
go build -o natproxy-cli .
./natproxy-cli serve                # Share internet
./natproxy-cli connect <code>       # Connect via SOCKS5
```

See [natproxy-cli/README.md](natproxy-cli/README.md) for full command reference.

## Configuration

### Environment Variables (`.env`)

The `.env` file is transformed into Dart constants at build time via `dart scripts/apply_env.dart`. The generated file `lib/config/env_config.dart` is committed with defaults so the app builds without a local `.env`.

**Infrastructure:**

| Variable          | Default                        | Description                          |
|-------------------|--------------------------------|--------------------------------------|
| `SIGNALING_URL`   | `http://[IP]:5601`    | WebRTC signaling server URL          |
| `DISCOVERY_URL`   | `http://[IP]:5602`    | Server discovery registry URL        |
| `STUN_SERVER`     | `stun.l.google.com:19302`      | STUN server for NAT detection + ICE  |

**Server Defaults:**

| Variable                     | Default   | Description                      |
|------------------------------|-----------|----------------------------------|
| `SERVER_LISTEN_PORT`         | `10853`   | Proxy listen port                |
| `SERVER_NAT_METHOD`          | `auto`    | `auto`, `upnp`, or `holepunch`  |
| `SERVER_PROTOCOL`            | `vless`   | `vless` or `socks`              |
| `SERVER_TRANSPORT`           | `xhttp`   | `kcp` or `xhttp`                |
| `SERVER_DISCOVERY_ENABLED`   | `true`    | Register in discovery list       |
| `SERVER_USE_RELAY`           | `false`   | UDP relay fallback               |

**Client Defaults:**

| Variable                     | Default     | Description                     |
|------------------------------|-------------|---------------------------------|
| `CLIENT_SOCKS_PORT`         | `10808`     | Local SOCKS5 port for tun2socks |
| `CLIENT_TUN_ADDRESS`        | `10.0.0.2`  | TUN interface IP                |
| `CLIENT_MTU`                | `1500`      | TUN MTU (1280-9000)            |
| `CLIENT_DNS1`               | `8.8.8.8`   | Primary DNS                     |
| `CLIENT_DNS2`               | `1.1.1.1`   | Secondary DNS                   |
| `CLIENT_ALLOW_DIRECT_DNS`   | `false`     | Allow ISP DNS fallback (privacy risk) |
| `CLIENT_DISCOVERY_ENABLED`  | `true`      | Show discovery browser          |

**VPN:**

| Variable           | Default     | Description                        |
|--------------------|-------------|------------------------------------|
| `VPN_SESSION_NAME` | `NATProxy`  | Android VPN settings label         |

### CLI Configuration

The desktop CLI uses YAML config files and command-line flags instead of `.env`. See [natproxy-cli/README.md](natproxy-cli/README.md) for the full configuration reference.

## How It Works

### Connection Code

When a server starts, it generates a connection code — a base64-encoded, zlib-compressed JSON blob containing:

- Server endpoint (IP:port or relay info)
- UUID for authentication
- NAT traversal method used (UPnP / holepunch / relay)
- Protocol and transport settings

Clients paste this code to connect. The code is designed to be short enough to share via messaging apps.

When the UPnP path succeeds with the VLESS protocol, a standard `vless://` link is also generated. This link can be imported directly into v2rayNG, Nekoray, and other Xray-compatible clients — no NATProxy app required on the client side.

### Client VPN (Android)

```
App Traffic → Android TUN interface → tun2socks → SOCKS5 (127.0.0.1:10808)
    → xray-core outbound (UPnP path)
    OR
    → WebRTC data channel → smux → remote SOCKS5 (hole punch path)
        → Internet
```

All proxy and WebRTC sockets are protected via `VpnService.protect(fd)` to prevent routing loops through the TUN interface.

### Client SOCKS5 (Desktop CLI)

```
curl --proxy socks5://127.0.0.1:10808 → SOCKS5 listener
    → xray-core outbound (UPnP path)
    OR
    → WebRTC data channel → smux → remote SOCKS5 (hole punch path)
        → Internet
```

No TUN/VPN — applications must be configured to use the SOCKS5 proxy individually.

## Anti-DPI & Privacy

| Technique                        | Description                                                    |
|----------------------------------|----------------------------------------------------------------|
| **DTLS fingerprint randomization** | Forked `pion/dtls` randomizes ClientHello fields to avoid fingerprinting |
| **FinalMask obfuscation (xray-core KCP)**        | Configurable traffic obfuscation modes (`header-dtls`, `mkcp-aes128gcm`, `header-dns`) |
| **Traffic padding**              | Adds random padding bytes to writes (v2: decoy + burst patterns) |
| **SDP compression**              | Connection codes are zlib-compressed to reduce size and obscure structure |
| **IP masking in logs**           | Optional flag to mask IP addresses in all log output           |

## Security Considerations

> **This is a proof-of-concept.** It demonstrates NAT traversal and P2P proxying but **lacks production security hardening**.

| Aspect                | PoC Approach                              | Production Recommendation               |
|-----------------------|-------------------------------------------|-----------------------------------------|
| Signaling transport   | Plain HTTP                                | TLS (HTTPS)                             |
| Authentication        | UUID in connection code                   | Mutual TLS or token-based auth          |
| Signaling server auth | None — anyone can create sessions         | API keys or OAuth                       |
| Discovery             | Open registration                         | Authenticated registration + rate limits |
| Relay                 | Opaque forwarding, no auth                | Authenticated relay with quotas         |