# P2P Internet Sharing PoC — Architecture & Implementation Plan

## Flutter + Go Mobile + xray-core on Android

---

## 1. High-Level Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     Flutter UI Layer                     │
│  ┌──────────────────┐       ┌─────────────────────────┐ │
│  │   Server Screen   │       │     Client Screen       │ │
│  │  - Start/Stop     │       │  - Enter connection info│ │
│  │  - Show public IP │       │  - Connect/Disconnect   │ │
│  │  - Show conn code │       │  - Traffic stats        │ │
│  │  - Client count   │       │  - Connection status    │ │
│  └────────┬─────────┘       └───────────┬─────────────┘ │
│           │          Method Channel       │               │
├───────────┴──────────────────────────────┴───────────────┤
│              Platform Channel Bridge (Kotlin)             │
│         (Android VPN Service + Go Library Calls)          │
├──────────────────────────────────────────────────────────┤
│                  Go Mobile Library (.aar)                 │
│  ┌────────────┐ ┌────────────┐ ┌───────────────────────┐│
│  │ NAT Module │ │ Signaling  │ │   xray-core wrapper   ││
│  │ - UPnP     │ │ - STUN     │ │   - Inbound config    ││
│  │ - UDP Hole │ │ - Exchange │ │   - Outbound config   ││
│  │   Punch    │ │   Info     │ │   - Start/Stop        ││
│  └────────────┘ └────────────┘ └───────────────────────┘│
├──────────────────────────────────────────────────────────┤
│                 Android OS (VPN Service / TUN)            │
└──────────────────────────────────────────────────────────┘
```

---

## 2. Component Breakdown

### 2.1 Project Structure

```
project-root/
├── flutter_app/
│   ├── lib/
│   │   ├── main.dart
│   │   ├── screens/
│   │   │   ├── home_screen.dart          # Role selector
│   │   │   ├── server_screen.dart        # Server UI
│   │   │   └── client_screen.dart        # Client UI
│   │   ├── services/
│   │   │   ├── platform_bridge.dart      # MethodChannel to Kotlin
│   │   │   └── connection_model.dart     # Shared connection info model
│   │   └── widgets/
│   │       ├── status_card.dart
│   │       └── connection_code.dart
│   └── android/
│       └── app/src/main/
│           ├── kotlin/.../
│           │   ├── MainActivity.kt
│           │   ├── VpnService.kt         # Android TUN VPN Service
│           │   └── GoBridge.kt           # Calls into Go .aar
│           └── AndroidManifest.xml
├── golib/                                # Go Mobile library
│   ├── go.mod
│   ├── go.sum
│   ├── api.go                            # Exported API (gomobile bind)
│   ├── nat/
│   │   ├── upnp.go                       # UPnP port mapping
│   │   ├── stun.go                       # STUN client
│   │   └── holepunch.go                  # UDP hole punching logic
│   ├── signaling/
│   │   └── signaling.go                  # Peer info exchange
│   ├── xray/
│   │   ├── server.go                     # xray-core inbound config builder
│   │   ├── client.go                     # xray-core outbound config builder
│   │   └── engine.go                     # xray-core lifecycle management
│   └── tunnel/
│       └── tun.go                        # TUN fd handling from Android
└── signaling-server/                     # Lightweight relay (optional)
    └── main.go                           # WebSocket or HTTP matchmaker
```

### 2.2 Dependencies

| Component | Library / Tool | Purpose |
|-----------|---------------|---------|
| Go Mobile | `golang.org/x/mobile/cmd/gomobile` | Build Go → Android .aar |
| xray-core | `github.com/xtls/xray-core` | Proxy engine (inbound + outbound) |
| UPnP | `github.com/huin/goupnp` | UPnP IGD port mapping |
| STUN | `github.com/pion/stun` | Discover public IP + port |
| UDP | `net` stdlib | UDP hole punching |
| Signaling | `nhooyr.io/websocket` or plain HTTP | Exchange peer info |
| Flutter | `flutter` SDK | UI |
| VPN Service | Android SDK (`android.net.VpnService`) | TUN interface |

---

## 3. Detailed Flow — Server Mode ("Share Internet")

### Step 1: Discover Local Network Info
```
Server presses "Start Sharing"
  → Go: get local IP (net.InterfaceAddrs)
  → Go: get gateway IP (parse /proc/net/route on Android)
```

### Step 2: Attempt UPnP Port Mapping
```go
// nat/upnp.go
func TryUPnP(internalPort, externalPort int, protocol string) (externalIP string, err error) {
    // 1. Discover IGD devices via SSDP (M-SEARCH on 239.255.255.250:1900)
    // 2. Find WANIPConnection or WANPPPConnection service
    // 3. Call AddPortMapping(
    //        NewRemoteHost: "",
    //        NewExternalPort: externalPort,
    //        NewProtocol: "TCP",       // xray needs TCP (or "UDP" for QUIC)
    //        NewInternalPort: internalPort,
    //        NewInternalClient: localIP,
    //        NewEnabled: true,
    //        NewPortMappingDescription: "P2PShare",
    //        NewLeaseDuration: 3600,    // 1 hour, renew periodically
    //    )
    // 4. Call GetExternalIPAddress() to get the public IP
    // 5. Return public IP + mapped external port
}
```

**Edge cases for UPnP:**
- Router doesn't support UPnP → fall through to hole punching
- UPnP disabled in router settings → same
- Multiple IGD devices (double NAT) → try each, warn user
- Lease expiry → background goroutine renews every 50 minutes
- Port conflict → retry with random port in 10000-60000 range

### Step 3: Fallback — STUN + UDP Hole Punching
```
If UPnP fails:
  → Go: Use STUN to discover public IP:port mapping
  → Go: Exchange this mapping with peer via signaling server
  → Go: Both sides send UDP packets to each other's public IP:port
  → Go: Once UDP path is established, upgrade to a tunnel
```

```go
// nat/stun.go
func DiscoverPublicAddr(stunServer string) (publicIP string, publicPort int, err error) {
    // 1. Send STUN Binding Request to stunServer (e.g., stun.l.google.com:19302)
    // 2. Parse XOR-MAPPED-ADDRESS from response
    // 3. Return public IP and port
}

// nat/holepunch.go
func PunchHole(localPort int, remotePublicIP string, remotePublicPort int) (net.Conn, error) {
    // 1. Bind local UDP socket to localPort (same one used for STUN)
    // 2. Send 10 UDP packets to remotePublicIP:remotePublicPort at 500ms intervals
    //    (this creates NAT mapping and hopefully the return path)
    // 3. Simultaneously listen for incoming UDP packets
    // 4. When bidirectional communication is confirmed, return the conn
    // 5. Timeout after 15 seconds → return error
}
```

**Critical edge cases for UDP hole punching:**
- **Symmetric NAT**: Different external port for every destination → hole punching FAILS. Detection strategy: do STUN against two different servers; if ports differ, NAT is symmetric. Inform user: "Your NAT type is incompatible. Try enabling UPnP or use a relay."
- **CGNAT (Carrier-Grade NAT)**: Public IP returned by STUN is still private (e.g., 100.64.x.x) → hole punching likely fails. Detect and warn.
- **Port-restricted cone NAT**: Hole punching works but requires precise timing.
- **Firewall blocking UDP**: Fall back to TCP-based relay as last resort.

### Step 4: Start xray-core Inbound

Once we have a reachable address (via UPnP mapped port or hole-punched UDP path):

```go
// xray/server.go
func BuildServerConfig(listenAddr string, listenPort int, uuid string) ([]byte, error) {
    // Generate xray-core JSON config:
    config := map[string]interface{}{
        "inbounds": []map[string]interface{}{
            {
                "tag":      "proxy-in",
                "port":     listenPort,
                "listen":   listenAddr,   // "0.0.0.0" for UPnP, specific for holepunch
                "protocol": "vless",
                "settings": map[string]interface{}{
                    "clients": []map[string]interface{}{
                        {
                            "id":   uuid,  // generated UUID for auth
                            "flow": "",
                        },
                    },
                    "decryption": "none",
                },
                "streamSettings": map[string]interface{}{
                    "network":  "tcp",      // TCP for UPnP path
                    // OR "kcp" for UDP hole-punched path (mKCP over UDP)
                    "security": "none",     // PoC; add TLS for production
                },
            },
        },
        "outbounds": []map[string]interface{}{
            {
                "tag":      "direct",
                "protocol": "freedom",   // direct internet access
                "settings": map[string]interface{}{},
            },
        },
    }
    return json.Marshal(config)
}
```

**Key design decision — UPnP vs Hole Punch determines transport:**

| NAT Traversal Method | xray Transport | Why |
|---------------------|---------------|-----|
| UPnP (TCP port open) | `tcp` or `ws` | Full TCP available, most efficient |
| UPnP (UDP port open) | `kcp` (mKCP) | Can use UDP-native transport |
| UDP hole punch | `kcp` (mKCP) | MUST use UDP since only UDP path exists |
| Both fail → relay | `tcp` via relay | Fallback through TURN-like relay |

### Step 5: Generate Connection Code

```go
type ConnectionInfo struct {
    PublicIP   string `json:"ip"`
    Port       int    `json:"port"`
    UUID       string `json:"uuid"`
    Transport  string `json:"transport"`  // "tcp", "kcp"
    Method     string `json:"method"`     // "upnp", "holepunch", "relay"
    // For hole punching, extra fields:
    StunPort   int    `json:"stun_port,omitempty"`
    SessionID  string `json:"session_id,omitempty"`
}
// Encode to base64 or short alphanumeric code for easy sharing
```

---

## 4. Detailed Flow — Client Mode ("Connect to Shared Internet")

### Step 1: Receive Connection Code
```
Client enters/scans the connection code
  → Decode ConnectionInfo struct
  → Determine transport method
```

### Step 2: If Hole Punching — Perform Client-Side Punch
```
If method == "holepunch":
  → Go: STUN discover own public IP:port
  → Go: Send own public address to signaling server with session_id
  → Go: Receive server's punched endpoint
  → Go: Execute hole punch from client side
  → Go: Establish UDP bidirectional path
```

### Step 3: Configure xray-core Outbound
```go
// xray/client.go
func BuildClientConfig(serverIP string, serverPort int, uuid string, transport string, socksPort int) ([]byte, error) {
    config := map[string]interface{}{
        "inbounds": []map[string]interface{}{
            {
                "tag":      "socks-in",
                "port":     socksPort,      // e.g., 10808
                "listen":   "127.0.0.1",
                "protocol": "socks",
                "settings": map[string]interface{}{
                    "udp": true,
                },
            },
        },
        "outbounds": []map[string]interface{}{
            {
                "tag":      "proxy-out",
                "protocol": "vless",
                "settings": map[string]interface{}{
                    "vnext": []map[string]interface{}{
                        {
                            "address": serverIP,
                            "port":    serverPort,
                            "users": []map[string]interface{}{
                                {
                                    "id":         uuid,
                                    "encryption": "none",
                                },
                            },
                        },
                    },
                },
                "streamSettings": map[string]interface{}{
                    "network": transport,   // "tcp" or "kcp"
                },
            },
        },
    }
    return json.Marshal(config)
}
```

### Step 4: Start Android VPN Service (TUN)
```
Flutter → MethodChannel → Kotlin VpnService
  → Create TUN interface via VpnService.Builder
  → Route all traffic (0.0.0.0/0) through TUN
  → Exclude the xray-core server IP from TUN (to avoid loop!)
  → Forward TUN fd to Go layer for tun2socks
```

```kotlin
// VpnService.kt
class ProxyVpnService : VpnService() {
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val serverIP = intent?.getStringExtra("server_ip") ?: return START_NOT_STICKY

        val builder = Builder()
            .setSession("P2PShare")
            .addAddress("10.0.0.2", 32)         // TUN interface address
            .addRoute("0.0.0.0", 0)              // Capture all IPv4 traffic
            .addRoute("::", 0)                    // Capture all IPv6 traffic
            .addDnsServer("8.8.8.8")
            .addDnsServer("1.1.1.1")
            .setMtu(1500)
            .setBlocking(false)

        // CRITICAL: Protect the xray-core socket from the VPN
        // This prevents a routing loop
        // Done via protect(socket_fd) in Go code

        val tunFd = builder.establish()
            ?: throw IllegalStateException("VPN permission not granted")

        // Pass TUN fd to Go library
        val fd = tunFd.detachFd()
        GoBridge.startTunnel(fd, "127.0.0.1:10808")  // xray SOCKS port

        return START_STICKY
    }
}
```

### Step 5: tun2socks — Route TUN Traffic Through xray

```go
// tunnel/tun.go
// Use a tun2socks implementation to read packets from TUN fd
// and forward them through the local SOCKS5 proxy (xray inbound)

// Option A: Use github.com/xjasonlyu/tun2socks/v2
// Option B: Use hev-socks5-tunnel (C library with Go bindings)
// Option C: Write lightweight gVisor-based TCP/IP stack

func StartTunnel(tunFd int, socksAddr string) error {
    // 1. Open TUN device from fd
    // 2. Create gVisor or lwIP-based network stack
    // 3. Intercept TCP → dial SOCKS5 at socksAddr
    // 4. Intercept UDP → use SOCKS5 UDP ASSOCIATE
    // 5. Handle DNS specially if needed (UDP port 53)
}
```

**Socket protection (anti-loop):**
```go
// The xray-core outbound socket MUST be "protected" from the Android VPN
// so its traffic goes directly to the real network interface, not back into TUN.
//
// Flow:
//   Go xray-core creates socket → gets fd
//   Go calls back to Kotlin via JNI → VpnService.protect(fd)
//   Socket is now excluded from TUN routing
//
// Implementation: register a DialerController in xray-core config or
// use the xray internet.RegisterDialerController API
```

---

## 5. xray-core Integration Details

### 5.1 Building xray-core with gomobile

```bash
# In golib/go.mod
module p2pshare/golib

go 1.22

require (
    github.com/xtls/xray-core v1.8.x
    github.com/huin/goupnp v1.3.x
    github.com/pion/stun/v2 v2.x.x
    golang.org/x/mobile v0.x.x
)
```

```go
// xray/engine.go
package xray

import (
    "context"
    "fmt"

    xcore "github.com/xtls/xray-core/core"
    _ "github.com/xtls/xray-core/main/distro/all"  // Register all features
)

var instance *xcore.Instance

func StartXray(configJSON []byte) error {
    config, err := xcore.LoadConfig("json", bytes.NewReader(configJSON))
    if err != nil {
        return fmt.Errorf("load config: %w", err)
    }

    inst, err := xcore.New(config)
    if err != nil {
        return fmt.Errorf("create instance: %w", err)
    }

    if err := inst.Start(); err != nil {
        return fmt.Errorf("start: %w", err)
    }

    instance = inst
    return nil
}

func StopXray() error {
    if instance != nil {
        return instance.Close()
    }
    return nil
}

// RegisterProtectFunc registers the Android VPN socket protection callback
func RegisterProtectFunc(protectFn func(fd int) bool) {
    // Hook into xray-core's dialer system so every outbound socket
    // gets protected before connecting
    // Use: internet.RegisterDialerController(func(network, addr string, fd uintptr) error { ... })
}
```

### 5.2 gomobile bind command

```bash
cd golib
gomobile bind -v \
  -target=android/arm64,android/arm \
  -androidapi=24 \
  -o ../flutter_app/android/app/libs/golib.aar \
  ./
```

### 5.3 Exported Go API (what Kotlin calls)

```go
// api.go — all exported functions must use simple types (gomobile restriction)
package golib

// === Server API ===
func StartServer() (string, error)
// Returns: base64-encoded ConnectionInfo JSON
// Internally: tries UPnP → STUN → starts xray

func StopServer() error

func GetServerStatus() string
// Returns JSON: {"running": true, "clients": 2, "upnp": true, "publicIP": "..."}

// === Client API ===
func StartClient(connectionCode string, tunFd int, protectFd func(int) bool) error
// connectionCode: base64 ConnectionInfo from server
// tunFd: TUN file descriptor from Android VpnService
// protectFd: callback to VpnService.protect()

func StopClient() error

func GetClientStatus() string
// Returns JSON: {"connected": true, "bytesUp": 1024, "bytesDown": 4096}

// === Utility ===
func DetectNATType() string
// Returns: "full_cone", "restricted_cone", "port_restricted", "symmetric", "unknown"

func GetPublicIP() string
```

---

## 6. Signaling Server (Required for Hole Punching)

A lightweight server that both peers contact to exchange their public endpoints.

```go
// signaling-server/main.go
package main

// Simple HTTP-based signaling:
// POST /session/:id/offer   → store peer A's endpoint
// GET  /session/:id/offer   → peer B retrieves peer A's endpoint
// POST /session/:id/answer  → store peer B's endpoint
// GET  /session/:id/answer  → peer A retrieves peer B's endpoint
//
// Sessions expire after 5 minutes.
// No authentication for PoC; add HMAC tokens for production.

// For PoC, can be deployed on a free Fly.io / Railway instance
// OR use a Firebase Realtime Database as signaling (no server needed)
```

**Flow:**
```
Server App                    Signaling                    Client App
    |                             |                             |
    |-- POST /session/abc/offer --|                             |
    |   {ip, port, uuid, ...}    |                             |
    |                             |                             |
    |                             |-- GET /session/abc/offer ---|
    |                             |   → returns server info     |
    |                             |                             |
    |                             |-- POST /session/abc/answer -|
    |                             |   {ip, port}                |
    |                             |                             |
    |-- GET /session/abc/answer --|                             |
    |   → returns client info     |                             |
    |                             |                             |
    |<========= UDP Hole Punch =========>|                     |
    |<========= xray traffic ==========>|                      |
```

---

## 7. Android-Specific Implementation

### 7.1 AndroidManifest.xml Permissions

```xml
<uses-permission android:name="android.permission.INTERNET" />
<uses-permission android:name="android.permission.ACCESS_NETWORK_STATE" />
<uses-permission android:name="android.permission.ACCESS_WIFI_STATE" />
<uses-permission android:name="android.permission.CHANGE_NETWORK_STATE" />
<uses-permission android:name="android.permission.FOREGROUND_SERVICE" />
<uses-permission android:name="android.permission.FOREGROUND_SERVICE_SPECIAL_USE" />
<uses-permission android:name="android.permission.RECEIVE_BOOT_COMPLETED" />

<!-- VPN Service Declaration -->
<service
    android:name=".ProxyVpnService"
    android:permission="android.permission.BIND_VPN_SERVICE"
    android:exported="false"
    android:foregroundServiceType="specialUse">
    <intent-filter>
        <action android:name="android.net.VpnService" />
    </intent-filter>
</service>
```

### 7.2 VPN Permission Request (Flutter Side)

```dart
// services/platform_bridge.dart
class PlatformBridge {
  static const _channel = MethodChannel('com.p2pshare/vpn');

  static Future<bool> requestVpnPermission() async {
    return await _channel.invokeMethod('requestVpnPermission');
  }

  static Future<void> startServer() async {
    final connectionCode = await _channel.invokeMethod('startServer');
    return connectionCode;
  }

  static Future<void> startClient(String connectionCode) async {
    await _channel.invokeMethod('startClient', {'code': connectionCode});
  }

  static Future<void> stop() async {
    await _channel.invokeMethod('stop');
  }

  static Stream<Map<String, dynamic>> get statusStream {
    return const EventChannel('com.p2pshare/status')
        .receiveBroadcastStream()
        .map((e) => Map<String, dynamic>.from(e));
  }
}
```

### 7.3 Kotlin Bridge

```kotlin
// GoBridge.kt
class GoBridge {
    companion object {
        init {
            // golib.aar is automatically loaded
        }

        fun startServer(): String {
            return Golib.startServer()  // calls exported Go func
        }

        fun startClient(code: String, tunFd: Int) {
            Golib.startClient(code, tunFd.toLong()) { fd ->
                // protect callback — must run on VpnService
                ProxyVpnService.instance?.protect(fd) ?: false
            }
        }

        fun stopAll() {
            Golib.stopServer()
            Golib.stopClient()
        }
    }
}
```

---

## 8. Step-by-Step Implementation Order

### Phase 1: Skeleton (Day 1–2)
1. Create Flutter project with two screens (Server / Client)
2. Set up `golib/` Go module with empty exported functions
3. Build `.aar` with `gomobile bind`, verify it loads in Android
4. Set up MethodChannel communication Flutter ↔ Kotlin ↔ Go
5. Verify round-trip: button press → Go function → return value → Flutter UI

### Phase 2: UPnP + xray Server (Day 3–5)
1. Implement `nat/upnp.go` — discover IGD, add port mapping
2. Implement `xray/engine.go` — start/stop xray-core
3. Implement `xray/server.go` — build VLESS inbound config
4. Wire together: press "Start Sharing" → UPnP map → start xray → show connection code
5. **Test**: connect from a desktop xray client to verify the server works

### Phase 3: xray Client + TUN (Day 6–8)
1. Implement `xray/client.go` — build VLESS outbound + SOCKS inbound config
2. Implement Android `VpnService` in Kotlin
3. Implement `tunnel/tun.go` — tun2socks (use existing library)
4. Implement socket protection (protect fd callback)
5. Wire together: enter code → start xray client → start VPN → all traffic proxied
6. **Test**: client device should browse internet through server device

### Phase 4: NAT Traversal Fallback (Day 9–12)
1. Implement `nat/stun.go` — STUN binding
2. Implement NAT type detection (test against 2 STUN servers)
3. Deploy signaling server (or use Firebase)
4. Implement `nat/holepunch.go` — UDP hole punching
5. Implement mKCP transport path in xray configs
6. Wire together: UPnP fail → detect NAT → STUN → signal → punch → xray over mKCP
7. **Test**: two devices on different networks, both behind NAT

### Phase 5: Polish + Edge Cases (Day 13–15)
1. Connection retry logic with exponential backoff
2. UPnP lease renewal background task
3. Handle VPN revocation (user turns off VPN from system settings)
4. Battery optimization: request exemption, use foreground service notification
5. Graceful shutdown: cleanup port mappings, close xray, remove TUN
6. Connection quality indicator (ping/bandwidth estimation)
7. Error messages for all failure modes

---

## 9. Full Edge Case Matrix

| # | Edge Case | Detection | Handling |
|---|-----------|-----------|----------|
| 1 | UPnP not supported | `goupnp.Discover` returns 0 devices | Fall through to STUN + hole punch |
| 2 | UPnP disabled | Discovery succeeds but `AddPortMapping` fails | Same as above |
| 3 | Double NAT | UPnP returns private IP as "external" | Warn user; try hole punch |
| 4 | Symmetric NAT | STUN to 2 servers returns different ports | Warn user: "Cannot connect directly. Need UPnP or relay." |
| 5 | CGNAT | STUN returns `100.64.x.x` or ISP-shared IP | Warn user; hole punch unlikely to work |
| 6 | Port already in use | `bind()` fails with EADDRINUSE | Retry with random port (3 attempts) |
| 7 | xray-core crash | Monitor goroutine / process exit | Auto-restart with backoff; notify user |
| 8 | TUN fd invalid | `establish()` returns null | User denied VPN permission → re-request |
| 9 | Routing loop | Traffic to server goes back into TUN | `VpnService.protect(fd)` on xray socket |
| 10 | DNS leak | DNS goes through real interface | Add DNS server to TUN builder; route UDP 53 through tunnel |
| 11 | IPv6 leak | IPv6 traffic bypasses IPv4 tunnel | Add `::0/0` route to TUN or block IPv6 |
| 12 | Battery kill | Android kills background service | `START_STICKY` + foreground notification + battery optimization exemption |
| 13 | Network switch | WiFi → mobile data mid-session | Detect with `ConnectivityManager`; restart hole punch or re-verify UPnP |
| 14 | Multiple clients | Second client connects to same server | xray-core handles natively (VLESS supports multiple clients with same UUID) |
| 15 | Server goes offline | Client loses connection | Detect via heartbeat/ping; show "Reconnecting..." with retry |
| 16 | Signaling server down | HTTP request to signaling fails | Retry 3x; show error "Cannot establish connection" |
| 17 | Clock skew | STUN transaction timeout | Use NTP or generous timeouts (5s for STUN) |
| 18 | MTU issues | Large packets dropped | Set TUN MTU to 1400 (safe for most paths); mKCP has own MTU negotiation |
| 19 | Half-open hole punch | Only one direction works | Send keepalive packets every 15s to maintain NAT mapping |
| 20 | Firewall blocks all UDP | Hole punch and STUN fail entirely | Detect timeout; suggest "Try TCP relay" or "Check firewall" |

---

## 10. Protocol Selection Decision Tree

```
START
  │
  ├─ Try UPnP TCP port mapping
  │   ├─ Success → Use VLESS + TCP stream
  │   │            (most efficient, lowest overhead)
  │   │
  │   └─ Fail → Detect NAT type via STUN
  │              │
  │              ├─ Full Cone / Restricted Cone / Port-Restricted
  │              │   → UDP Hole Punch
  │              │   │
  │              │   ├─ Success → Use VLESS + mKCP stream
  │              │   │            (UDP-based, works through punched hole)
  │              │   │
  │              │   └─ Fail → Relay fallback
  │              │
  │              └─ Symmetric NAT
  │                  → Skip hole punch (won't work)
  │                  → Relay fallback
  │
  └─ Relay fallback
      → Connect via TURN-like relay server
      → Use VLESS + WebSocket stream through relay
      → Highest latency but always works
```

---

## 11. Security Considerations (PoC Level)

For PoC, these are noted but minimal:

| Concern | PoC Approach | Production Approach |
|---------|-------------|-------------------|
| Traffic encryption | xray VLESS (no TLS) | VLESS + XTLS / Reality |
| Auth | Random UUID per session | Pre-shared key + device binding |
| Signaling tampering | Plain HTTP | HTTPS + signed payloads |
| Connection code theft | Base64 encoded | Encrypted + time-limited + OTP |
| Man-in-the-middle | Not addressed in PoC | TLS certificate pinning |
| Resource abuse | Server has no limits | Rate limiting + bandwidth cap |

---

## 12. Key Implementation Gotchas

### 12.1 gomobile type restrictions
gomobile `bind` only supports: `int`, `float64`, `bool`, `string`, `[]byte`, and interfaces with methods using those types. You CANNOT export structs, maps, slices of structs, or channels. All complex data must be serialized to JSON strings or `[]byte`.

### 12.2 xray-core binary size
xray-core with all protocols is ~30–50 MB. For PoC strip unused protocols:
```go
// Instead of importing all:
//   _ "github.com/xtls/xray-core/main/distro/all"
// Import only what you need:
import (
    _ "github.com/xtls/xray-core/app/proxyman/inbound"
    _ "github.com/xtls/xray-core/app/proxyman/outbound"
    _ "github.com/xtls/xray-core/proxy/vless/inbound"
    _ "github.com/xtls/xray-core/proxy/vless/outbound"
    _ "github.com/xtls/xray-core/proxy/freedom"
    _ "github.com/xtls/xray-core/proxy/socks"
    _ "github.com/xtls/xray-core/transport/internet/tcp"
    _ "github.com/xtls/xray-core/transport/internet/kcp"
)
```

### 12.3 tun2socks choice
Recommended: **[hev-socks5-tunnel](https://github.com/hevlib/hev-socks5-tunnel)** — lightweight C library with Go bindings, used in production by several Android VPN apps. Alternative: **[tun2socks](https://github.com/xjasonlyu/tun2socks)** — pure Go, uses gVisor netstack, heavier but easier to integrate.

### 12.4 Android 14+ restrictions
- `FOREGROUND_SERVICE_SPECIAL_USE` type required for VPN
- Must show foreground notification while VPN is active
- Must handle `onRevoke()` in VpnService

### 12.5 UDP hole punch keepalive
NAT mappings typically expire in 30–120 seconds. Send a keepalive UDP packet every 15 seconds to maintain the punched hole. If using mKCP, its built-in keepalive handles this.

---

## 13. Testing Strategy

| Test | Method |
|------|--------|
| UPnP on real router | Two physical Android devices on same network |
| Hole punch | Two devices on different WiFi networks (or one on mobile data) |
| Symmetric NAT simulation | Mobile carrier data (most are symmetric) |
| VPN routing | Verify `whatismyip.com` shows server's public IP on client |
| DNS leak | Use `dnsleaktest.com` on client device |
| Reconnection | Kill server app mid-transfer, verify client retries |
| Battery | Let both run for 1 hour, check battery usage |
| Concurrent clients | Connect 2-3 clients to one server simultaneously |