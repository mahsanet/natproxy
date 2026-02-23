**:gb: English** | **[:iran: فارسی](README.fa.md)**

# NATProxy

**P2P Internet Sharing for Android + Desktop**

Share a device's internet connection with other devices over the internet using NAT traversal — no port forwarding setup required. A "server" device shares its connection, and "client" devices route their traffic through it via a WebRTC data channel or xray-core proxy tunnel.

Three components:
- **Android App** — Flutter UI with system-wide VPN (TUN interface)
- **Desktop CLI** — Go command-line tool with local SOCKS5 proxy
- **Signaling Server** — Lightweight Go server for peer discovery and SDP exchange

## Features

- **NAT traversal cascade** — tries UPnP port mapping first, falls back to WebRTC ICE hole punching, then UDP relay
- **Server discovery** — browse available servers with NAT compatibility scoring (best hole-punch candidates ranked first)
- **Anti-DPI obfuscation** — DTLS fingerprint randomization (forked pion/dtls), FinalMask traffic obfuscation, configurable traffic padding
- **SDP compression** — connection codes compressed with zlib + base64 for easy sharing
- **Parallel PeerConnections** — multiple WebRTC connections for increased throughput (configurable 1-8)
- **Manual signaling** — exchange offer/answer codes without a signaling server
- **DNS caching** — built-in DNS cache to reduce latency
- **xray-core selective imports** — only VLESS (freedom, SOCKS, TCP, and KCP) are linked (~30-50 MB binary savings vs full xray)
- **VLESS link generation** — when UPnP succeeds with VLESS protocol, generates a standard `vless://` link importable by v2rayNG, Nekoray, and other Xray-compatible clients
- **Desktop CLI** — full-featured command-line tool with SOCKS5 proxy, YAML config, and JSON output for scripting

## Architecture

### Full Stack

```
Flutter UI (Dart)
  ├── HomeScreen (role selector: Server / Client)
  ├── ServerScreen (start/stop, connection code, client count)
  └── ClientScreen (enter code, connect/disconnect, traffic stats)
        │
        │  MethodChannel "com.p2pshare/vpn"
        │  EventChannel  "com.p2pshare/status"
        ▼
Kotlin Platform Layer (Android)
  ├── MainActivity (MethodChannel handler)
  ├── ProxyVpnService (Android TUN VPN, foreground service)
  └── GoBridge (calls into gomobile-generated Golib class)
        │
        ▼
Go Mobile Library (golib/ → .aar)
  ├── api.go           — exported gomobile API (simple types only)
  ├── nat/             — UPnP IGD + STUN NAT detection
  ├── signaling/       — HTTP signaling client + SDP compression
  ├── webrtc/          — PeerConnection, ICE, data channels, smux
  ├── xray/            — xray-core lifecycle (VLESS/SOCKS/KCP/xHTTP)
  └── tunnel/          — TUN fd → tun2socks → SOCKS proxy
```

### NAT Traversal Decision Tree

```
Start Server
    │
    ├─► Try UPnP port mapping
    │     │
    │     ├─ Success → xray-core (VLESS over TCP/KCP/xHTTP) + VLESS link
    │     │
    │     └─ Fail ──► WebRTC hole punch (ICE/DTLS/SCTP)
    │                   │
    │                   ├─ ICE succeeds → pion/webrtc + smux data channels
    │                   │
    │                   └─ ICE fails ──► UDP relay (opaque forwarding via signaling server :3478)
    │
    └─► Connection code generated (base64 JSON with endpoint + UUID + method + settings)
```

### Transport Paths

| Path         | When Used                | Transport Stack                              |
|--------------|--------------------------|----------------------------------------------|
| UPnP         | Router supports UPnP IGD | xray-core: VLESS inbound → freedom outbound  |
| Hole Punch   | UPnP fails, ICE succeeds | pion/webrtc: data channel → smux → SOCKS5    |
| UDP Relay    | Both above fail          | Same as hole punch, relayed through signaling server |

## Project Structure

```
natproxy/
├── lib/                    # Flutter UI (Dart)
│   ├── config/             # Build-time env constants
│   ├── models/             # Data models
│   ├── screens/            # Server, Client, Settings screens
│   ├── services/           # Platform bridge, settings service
│   └── widgets/            # Shared UI components
├── android/                # Android platform layer
│   └── app/src/main/kotlin/com/example/natproxy/
│       ├── MainActivity.kt
│       ├── ProxyVpnService.kt
│       └── GoBridge.kt
├── golib/                  # Go mobile library (→ .aar)
│   ├── api.go              # Exported gomobile API
│   ├── nat/                # UPnP + STUN
│   ├── signaling/          # Signaling client + SDP compression
│   ├── webrtc/             # WebRTC peer connections
│   ├── xray/               # xray-core engine
│   ├── tunnel/             # TUN → tun2socks
│   ├── applog/             # Logging infrastructure
│   ├── util/               # Shared utilities
│   └── replace/dtls/       # Forked pion/dtls (fingerprint randomization)
├── signaling-server/       # Signaling + discovery + UDP relay
├── natproxy-cli/           # Desktop CLI tool
│   ├── cmd/                # Cobra commands (serve, connect, discover, nat)
│   └── internal/           # Config, orchestrator, display, types
├── scripts/
│   └── apply_env.dart      # .env → env_config.dart generator
├── build-android.sh        # Build golib .aar + Flutter APK
├── .env.example            # Environment variable template
└── CLAUDE.md               # AI assistant instructions
```

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
curl --proxy socks5h://127.0.0.1:10808 → SOCKS5 listener
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
| **FinalMask obfuscation**        | Configurable traffic obfuscation modes (`header-dtls`, `mkcp-aes128gcm`, `header-dns`) |
| **Traffic padding**              | Adds random padding bytes to writes (v2: decoy + burst patterns) |
| **SDP compression**              | Connection codes are zlib-compressed to reduce size and obscure structure |
| **IP masking in logs**           | Optional flag to mask IP addresses in all log output           |

## Security Considerations

> **This is a proof-of-concept.** It demonstrates NAT traversal and P2P proxying but lacks production security hardening.

| Aspect                | PoC Approach                              | Production Recommendation               |
|-----------------------|-------------------------------------------|-----------------------------------------|
| Signaling transport   | Plain HTTP                                | TLS (HTTPS)                             |
| Authentication        | UUID in connection code                   | Mutual TLS or token-based auth          |
| Signaling server auth | None — anyone can create sessions         | API keys or OAuth                       |
| Discovery             | Open registration                         | Authenticated registration + rate limits |
| Relay                 | Opaque forwarding, no auth                | Authenticated relay with quotas         |

### Socket Protection

On Android, all outbound sockets (xray-core, WebRTC ICE/DTLS, STUN) must be protected via `VpnService.protect(fd)` to avoid routing loops. The WebRTC path uses `UDPMux` with a single pre-protected socket shared across all PeerConnections.

## Development

### Linting & Formatting

```bash
dart analyze lib/          # Flutter static analysis
dart format .              # Format Dart code
```

### Testing

```bash
flutter test                        # All Flutter tests
flutter test test/widget_test.dart  # Single test file
```

### Go Tests

```bash
cd golib && go test ./...
cd signaling-server && go test ./...
```

## SDK Requirements

| Component      | Version / Requirement                    |
|----------------|------------------------------------------|
| Dart SDK       | ^3.10.8                                  |
| Go             | 1.25+                                    |
| Android minSdk | 24                                       |
| Java           | 17                                       |
| Kotlin         | 2.2.20                                   |
| Gradle         | 8.11.1                                   |
| gomobile       | latest                                   |
| Android namespace | `com.example.natproxy`                |

### Key Go Dependencies

| Package                  | Version    | Purpose                         |
|--------------------------|------------|---------------------------------|
| `xtls/xray-core`        | v1.260204  | Proxy engine (VLESS/SOCKS)      |
| `pion/webrtc/v4`        | v4.1.2     | WebRTC peer connections         |
| `pion/dtls/v3`          | v3.0.6     | DTLS (forked for fingerprint randomization) |
| `pion/stun/v2`          | v2.0.0     | STUN NAT detection              |
| `pion/ice/v4`           | v4.0.10    | ICE connectivity                |
| `huin/goupnp`           | v1.3.0     | UPnP IGD port mapping           |
| `xtaci/smux`            | v1.5.33    | Stream multiplexer over data channels |
| `sagernet/sing-tun`     | v0.7.11    | tun2socks (TUN → SOCKS proxy)  |
| `golang.org/x/mobile`   | latest     | gomobile binding generation     |

## Key Design Decisions

- **gomobile type restrictions** — exported Go functions use only `int`, `float64`, `bool`, `string`, `[]byte`. All complex data is JSON-serialized strings passed across the FFI boundary.
- **xray-core selective imports** — only VLESS (freedom, SOCKS, TCP, and KCP protocol) handlers are registered, saving ~30-50 MB vs the full xray distribution.
- **Socket protection** — xray-core outbound sockets and WebRTC ICE UDP sockets must be `protect()`ed via `VpnService.protect(fd)` to avoid routing loops through the TUN interface. The WebRTC path uses `UDPMux` with a single pre-protected socket.
- **Forked pion/dtls** — `golib/replace/dtls/` contains a fork of `pion/dtls` with randomized DTLS ClientHello fields to resist fingerprinting.
- **Android 14+** — the VPN uses `foregroundServiceType="specialUse"` and displays a persistent foreground notification.
- **NAT traversal cascade** — UPnP (direct TCP) is preferred because it's faster and more reliable. WebRTC hole punching is the fallback. UDP relay is the last resort.

## Sub-project Documentation

- **[Signaling Server](signaling-server/README.md)** — API reference, relay protocol, NAT scoring, deployment
- **[Desktop CLI](natproxy-cli/README.md)** — Commands, flags, YAML config, manual signaling, scripting
