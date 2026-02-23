<div dir="rtl">

**[:gb: English](README.md)** | **:iran: فارسی**

# NATProxy CLI

ابزار خط فرمان دسکتاپ برای اشتراک‌گذاری اینترنت نظیر-به-نظیر. تمام قابلیت‌های اپلیکیشن اندروید NATProxy را برای لینوکس، مک‌اواس و ویندوز تکرار می‌کند. در دسکتاپ، کلاینت یک **پروکسی SOCKS5 محلی** ارائه می‌دهد به جای TUN/VPN — هر اپلیکیشنی را به پروکسی اشاره دهید تا ترافیک از طریق سرور ریموت مسیریابی شود.

از همان کتابخانه Go (`golib/`) با اپلیکیشن اندروید استفاده می‌کند، بنابراین سرورهای CLI می‌توانند به کلاینت‌های اندروید سرویس دهند و بالعکس.

## نصب

```bash
cd natproxy-cli
go build -o natproxy-cli .
```

CLI از `golib` از طریق دستور `replace` در `go.mod` استفاده می‌کند:

```
replace natproxy/golib => ../golib
```

بنابراین دایرکتوری `golib/` باید در کنار `natproxy-cli/` وجود داشته باشد.

## شروع سریع

**سرور** (دستگاهی که اینترنت را به اشتراک می‌گذارد):

```bash
./natproxy-cli serve
# کد اتصال را در stdout چاپ می‌کند
```

**کلاینت** (دستگاهی که از اینترنت مشترک استفاده می‌کند):

```bash
./natproxy-cli connect <connection-code>
# پروکسی SOCKS5 در 127.0.0.1:10808 آماده است
```

**استفاده از پروکسی:**

```bash
curl --proxy socks5h://127.0.0.1:10808 https://ifconfig.me
```

## دستورات

### `serve`

سرور پروکسی را راه‌اندازی کرده و اتصال اینترنت شما را به اشتراک می‌گذارد. ابتدا مپینگ پورت UPnP را امتحان می‌کند، سپس به WebRTC hole punching بازمی‌گردد.

- کد اتصال در **stdout** چاپ می‌شود (برای piping).
- وقتی UPnP با پروتکل VLESS موفق شود، یک لینک استاندارد `vless://` در **stderr** چاپ می‌شود (قابل وارد کردن در v2rayNG، Nekoray و غیره).
- لاگ‌ها و وضعیت در **stderr** چاپ می‌شوند.
- تا `Ctrl+C` (SIGINT/SIGTERM) اجرا می‌شود.

```bash
./natproxy-cli serve
./natproxy-cli serve --nat-method holepunch --use-relay
./natproxy-cli serve --manual  # بدون نیاز به سرور سیگنالینگ
```

#### فلگ‌های سرور

**شبکه:**

| فلگ               | پیش‌فرض                           | توضیحات                    |
|--------------------|-----------------------------------|----------------------------|
| `--port`           | `10853`                           | پورت شنود                  |
| `--stun-server`    | `stun.l.google.com:19302`         | آدرس سرور STUN             |
| `--signaling-url`  | `http://[IP]:5601`       | آدرس سرور سیگنالینگ        |
| `--discovery-url`  | `http://[IP]:5602`       | آدرس سرور کشف              |

**عبور از NAT:**

| فلگ                     | پیش‌فرض | توضیحات                                 |
|-------------------------|---------|------------------------------------------|
| `--nat-method`          | `auto`  | `auto`، `upnp` یا `holepunch`           |
| `--use-relay`           | `false` | فعال‌سازی رله UDP پشتیبان برای WebRTC   |
| `--upnp-lease-duration` | `3600`  | مدت اجاره UPnP به ثانیه (0 = نامحدود)  |
| `--upnp-retries`        | `3`     | تعداد تلاش مجدد مپینگ UPnP              |
| `--ssdp-timeout`        | `3`     | مهلت کشف SSDP به ثانیه                  |

**پروتکل و انتقال (مسیر UPnP / xray-core):**

| فلگ            | پیش‌فرض | توضیحات                              |
|-----------------|---------|--------------------------------------|
| `--protocol`    | `vless` | `vless` یا `socks`                   |
| `--transport`   | `xhttp` | `kcp` یا `xhttp`                     |
| `--uuid`        | *(تصادفی)* | UUID پروتکل VLESS (خالی = تصادفی در هر شروع) |

**SOCKS (خروجی سمت سرور):**

| فلگ               | پیش‌فرض   | توضیحات                   |
|--------------------|-----------|---------------------------|
| `--socks-auth`     | `noauth`  | `noauth` یا `password`    |
| `--socks-username` | *(خالی)*  | نام کاربری SOCKS          |
| `--socks-password` | *(خالی)*  | رمز عبور SOCKS            |
| `--socks-udp`      | `true`    | فعال‌سازی پشتیبانی UDP    |

**KCP (وقتی `--transport kcp`):**

| فلگ                      | پیش‌فرض | توضیحات              |
|--------------------------|---------|----------------------|
| `--kcp-mtu`              | `1350`  | MTU (576-1460)       |
| `--kcp-tti`              | `20`    | TTI به میلی‌ثانیه (10-100) |
| `--kcp-uplink-capacity`  | `12`    | ظرفیت آپلینک MB/s   |
| `--kcp-downlink-capacity`| `100`   | ظرفیت دانلینک MB/s  |
| `--kcp-congestion`       | `true`  | کنترل ازدحام         |
| `--kcp-read-buffer`      | `4`     | بافر خواندن MB       |
| `--kcp-write-buffer`     | `4`     | بافر نوشتن MB        |

**FinalMask (مبهم‌سازی ضد DPI):**

| فلگ                  | پیش‌فرض        | توضیحات                         |
|-----------------------|----------------|----------------------------------|
| `--finalmask-type`    | `header-dtls`  | نوع مبهم‌سازی                   |
| `--finalmask-password`| *(خالی)*       | رمز عبور برای `mkcp-aes128gcm` |
| `--finalmask-domain`  | *(خالی)*       | دامنه برای `header-dns`         |

**xHTTP (وقتی `--transport xhttp`):**

| فلگ           | پیش‌فرض   | توضیحات                                     |
|----------------|---------|---------------------------------------------|
| `--xhttp-path` | `/`     | مسیر URL                                    |
| `--xhttp-host` | *(خالی)* | هدر Host                                   |
| `--xhttp-mode` | `auto`  | `auto`، `packet-up`، `stream-up`، `stream-one` |

**WebRTC (مسیر hole punch):**

| فلگ                       | پیش‌فرض        | توضیحات                           |
|---------------------------|----------------|-----------------------------------|
| `--transport-mode`        | `datachannel`  | `datachannel` یا `media`          |
| `--num-peer-connections`  | `6`            | PeerConnection های موازی (1-8)    |
| `--num-channels`          | `6`            | کانال‌های داده موازی              |
| `--disable-ipv6`          | `false`        | غیرفعال‌سازی کاندیداهای ICE IPv6   |

**محدودسازی نرخ:**

| فلگ               | پیش‌فرض | توضیحات                                |
|--------------------|---------|----------------------------------------|
| `--rate-limit-up`  | `0`     | محدودیت نرخ آپلود KB/s (0 = نامحدود)  |
| `--rate-limit-down`| `0`     | محدودیت نرخ دانلود KB/s (0 = نامحدود) |

**پدینگ ترافیک:**

| فلگ               | پیش‌فرض | توضیحات                       |
|-------------------|---------|-------------------------------|
| `--padding`       | `false` | فعال‌سازی پدینگ ترافیک        |
| `--padding-max`   | `256`   | حداکثر بایت پدینگ در هر نوشتن |
| `--padding-version`| `2`    | نسخه پدینگ (0=v1، 2=v2 decoy+burst) |

**Smux (مالتی‌پلکسر استریم):**

| فلگ                        | پیش‌فرض | توضیحات                         |
|----------------------------|---------|----------------------------------|
| `--smux-stream-buffer`     | `2048`  | بافر دریافت هر استریم KB        |
| `--smux-session-buffer`    | `8192`  | بافر دریافت جلسه KB             |
| `--smux-frame-size`        | `32768` | حداکثر اندازه فریم بایت         |
| `--smux-keep-alive`        | `10`    | فاصله keepalive ثانیه            |
| `--smux-keep-alive-timeout`| `300`   | مهلت keepalive ثانیه             |

**تنظیمات سطح پایین:**

| فلگ                     | پیش‌فرض | توضیحات                                 |
|-------------------------|---------|------------------------------------------|
| `--dc-max-buffered`     | `2048`  | سطح بالای backpressure کانال داده KB     |
| `--dc-low-mark`         | `512`   | سطح پایین backpressure کانال داده KB     |
| `--sctp-recv-buffer`    | `8192`  | بافر دریافت SCTP کیلوبایت               |
| `--sctp-rto-max`        | `2500`  | حداکثر مهلت ارسال مجدد SCTP میلی‌ثانیه  |
| `--sctp-zero-checksum`  | `true`  | بهینه‌سازی checksum صفر SCTP             |
| `--dtls-retransmit`     | `100`   | فاصله ارسال مجدد DTLS میلی‌ثانیه        |
| `--dtls-skip-verify`    | `true`  | رد شدن از HelloVerify DTLS              |
| `--dtls-disable-close`  | `true`  | جلوگیری از آبشاری شدن close DTLS به PeerConnection |
| `--ice-disconn-timeout` | `15000` | مهلت قطع ICE میلی‌ثانیه                 |
| `--ice-failed-timeout`  | `25000` | مهلت شکست ICE میلی‌ثانیه                |
| `--ice-keepalive`       | `2000`  | فاصله keepalive ICE میلی‌ثانیه          |
| `--udp-read-buffer`     | `8192`  | بافر خواندن UDP کرنل کیلوبایت           |
| `--udp-write-buffer`    | `8192`  | بافر نوشتن UDP کرنل کیلوبایت           |

**کشف:**

| فلگ               | پیش‌فرض   | توضیحات                            |
|--------------------|-----------|-------------------------------------|
| `--discovery-name` | *(خالی)*  | نام نمایشی در لیست کشف             |
| `--discovery-room` | *(خالی)*  | نام اتاق برای فیلتر               |
| `--no-discovery`   | `false`   | غیرفعال‌سازی ثبت‌نام کشف           |

**سایر:**

| فلگ         | پیش‌فرض | توضیحات                                |
|--------------|---------|----------------------------------------|
| `--manual`   | `false` | حالت سیگنالینگ دستی (بدون سرور سیگنالینگ) |
| `--mask-ips` | `false` | پنهان‌سازی آدرس‌های IP در لاگ‌ها      |

---

### `connect [code]`

اتصال به سرور NATProxy و ارائه پروکسی SOCKS5 محلی.

- آدرس SOCKS5 در **stdout** چاپ می‌شود.
- لاگ‌ها و وضعیت در **stderr** چاپ می‌شوند.
- کدهای offer دستی (شروع‌شونده با `M1:`) به طور خودکار شناسایی می‌شوند.
- تا `Ctrl+C` اجرا می‌شود.

```bash
./natproxy-cli connect <connection-code>
./natproxy-cli connect --discover              # مرور سرورها به صورت تعاملی
./natproxy-cli connect --discover --room home   # فیلتر بر اساس اتاق
./natproxy-cli connect M1:<offer>               # سیگنالینگ دستی
```

#### فلگ‌های کلاینت

| فلگ                     | پیش‌فرض                           | توضیحات                           |
|-------------------------|-----------------------------------|-----------------------------------|
| `--socks-port`          | `10808`                           | پورت SOCKS5 محلی                  |
| `--stun-server`         | `stun.l.google.com:19302`         | سرور STUN                         |
| `--signaling-url`       | `http://[IP]:5601`       | آدرس سرور سیگنالینگ              |
| `--discovery-url`       | `http://[IP]:5602`       | آدرس سرور کشف                    |
| `--discover`            | `false`                           | مرور سرورها به صورت تعاملی       |
| `--room`                | *(خالی)*                          | فیلتر کشف بر اساس اتاق          |
| `--sctp-recv-buffer`    | `8192`                            | بافر دریافت SCTP کیلوبایت        |
| `--sctp-rto-max`        | `2500`                            | حداکثر مهلت ارسال مجدد SCTP ms   |
| `--sctp-zero-checksum`  | `true`                            | بهینه‌سازی checksum صفر SCTP      |
| `--dtls-retransmit`     | `100`                             | فاصله ارسال مجدد DTLS ms         |
| `--dtls-skip-verify`    | `true`                            | رد شدن از HelloVerify DTLS       |
| `--dtls-disable-close`  | `true`                            | جلوگیری از آبشاری شدن close DTLS |
| `--ice-disconn-timeout` | `15000`                           | مهلت قطع ICE ms                  |
| `--ice-failed-timeout`  | `25000`                           | مهلت شکست ICE ms                 |
| `--ice-keepalive`       | `2000`                            | فاصله keepalive ICE ms           |
| `--udp-read-buffer`     | `8192`                            | بافر خواندن UDP کرنل KB          |
| `--udp-write-buffer`    | `8192`                            | بافر نوشتن UDP کرنل KB          |
| `--mask-ips`            | `false`                           | پنهان‌سازی آدرس‌های IP در لاگ‌ها |

---

### `discover`

لیست سرورهای موجود از سرویس کشف.

```bash
./natproxy-cli discover
./natproxy-cli discover --room office
./natproxy-cli discover --json
```

| فلگ               | پیش‌فرض     | توضیحات                |
|--------------------|------------|------------------------|
| `--discovery-url`  | *(تنظیمات)* | آدرس سرور کشف       |
| `--room`           | *(خالی)*    | فیلتر بر اساس اتاق  |
| `--json`           | `false`     | خروجی به صورت آرایه JSON |

**خروجی قابل خواندن:**

```
Available Servers:
  [1] alice-phone          | holepunch / vless / webrtc   | Room: home
  [2] bob-laptop           | upnp / vless / xhttp
```

**خروجی JSON (`--json`):**

```json
[
  {"id":"a1b2...","name":"alice-phone","room":"home","code":"...","method":"holepunch",...}
]
```

---

### `nat`

تشخیص نوع NAT و IP عمومی شما با استفاده از پروب‌های STUN بر اساس RFC 5780.

```bash
./natproxy-cli nat
./natproxy-cli nat --json
```

| فلگ            | پیش‌فرض                      | توضیحات              |
|-----------------|-----------------------------|-----------------------|
| `--stun-server` | `stun.l.google.com:19302`   | آدرس سرور STUN       |
| `--json`        | `false`                     | خروجی به صورت JSON   |

**خروجی قابل خواندن:**

```
NAT Type:    Endpoint Independent
Public IP:   203.0.113.42
Public Port: 54321
```

**خروجی JSON:**

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

چاپ نسخه و SHA کامیت.

```bash
./natproxy-cli version
# natproxy-cli dev (commit: unknown)
```

تنظیم در زمان ساخت با ldflags:

```bash
go build -ldflags "-X natproxy/cli/cmd.Version=1.0.0 -X natproxy/cli/cmd.Commit=$(git rev-parse --short HEAD)" .
```

## سیگنالینگ دستی

برای محیط‌هایی بدون سرور سیگنالینگ، از تبادل دستی offer/answer استفاده کنید.

### مرحله به مرحله

**1. سرور در حالت دستی شروع می‌شود:**

```bash
./natproxy-cli serve --manual
# M1:<offer-code> را در stdout چاپ می‌کند
```

**2. کد offer را به کلاینت کپی کنید:**

```bash
./natproxy-cli connect M1:<offer-code>
# M1A:<answer-code> را در stdout چاپ می‌کند
```

**3. کد answer را به سرور برگردانید:**

سرور در stderr درخواست کد answer می‌کند. کد `M1A:...` را paste کرده و Enter بزنید.

**4. اتصال برقرار شد.**

هر دو طرف به اجرا ادامه می‌دهند. پروکسی SOCKS5 کلاینت آماده است.

### نکات

- نیازی به سرور سیگنالینگ نیست — کدها از طریق کانال‌های خارجی (چت، ایمیل و غیره) تبادل می‌شوند
- پیشوند `M1:` کدهای offer را مشخص می‌کند؛ `M1A:` کدهای answer را
- دستور `connect` به طور خودکار کدهای offer دستی را از پیشوند `M1:` شناسایی می‌کند

## فایل پیکربندی

CLI فایل `~/.natproxy-cli` (YAML) را برای پیکربندی دائمی می‌خواند. فلگ‌های خط فرمان بر مقادیر فایل پیکربندی و مقادیر پیکربندی بر پیش‌فرض‌ها اولویت دارند.

**اولویت:** فلگ‌های CLI > فایل پیکربندی > پیش‌فرض‌ها

### مثال `~/.natproxy-cli`

```yaml
# عمومی
stun_server: stun.l.google.com:19302
signaling_url: http://your-server:5601
discovery_url: http://your-server:5602

# تنظیمات سرور
server:
  port: 10853
  nat_method: auto        # auto | upnp | holepunch
  use_relay: false
  protocol: vless          # vless | socks
  transport: xhttp         # kcp | xhttp
  uuid: ""                 # UUID پروتکل VLESS (خالی = تصادفی در هر شروع)
  socks_auth: noauth       # noauth | password
  socks_username: ""
  socks_password: ""
  socks_udp: true
  transport_mode: datachannel  # datachannel | media
  disable_ipv6: false
  num_peer_connections: 6
  num_channels: 6
  rate_limit_up: 0         # KB/s، 0 = نامحدود
  rate_limit_down: 0
  mask_ips: false

  # UPnP
  upnp_lease_duration: 3600
  upnp_retries: 3
  ssdp_timeout: 3

  # کشف
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

  # پدینگ
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

  # کانال داده
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

# تنظیمات کلاینت
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

## اسکریپت‌نویسی

### جداسازی stdout/stderr

CLI خروجی ساختاریافته را از خروجی قابل خواندن جدا می‌کند:

| استریم   | محتوا                                           |
|----------|--------------------------------------------------|
| `stdout` | کد اتصال، آدرس SOCKS، خروجی JSON               |
| `stderr` | لاگ‌ها، بروزرسانی وضعیت، لینک VLESS، اعلان‌های تعاملی |

این کار ثبت خروجی در اسکریپت‌ها را آسان می‌کند:

```bash
# ثبت کد اتصال
CODE=$(./natproxy-cli serve 2>/dev/null)
echo "$CODE" | xclip -selection clipboard

# ثبت آدرس SOCKS
SOCKS=$(./natproxy-cli connect "$CODE" 2>/dev/null)
```

### خروجی JSON

از `--json` با دستورات `discover` و `nat` برای خروجی قابل پردازش ماشینی استفاده کنید:

```bash
./natproxy-cli discover --json | jq '.[0].code'
./natproxy-cli nat --json | jq '.nat_type'
```

### مدیریت سیگنال

هر دو دستور `serve` و `connect` سیگنال‌های SIGINT و SIGTERM را برای خاموشی تمیز مدیریت می‌کنند. منابع (مپینگ‌های UPnP، لیست‌های کشف، اتصالات WebRTC) قبل از خروج پاکسازی می‌شوند.

## سازگاری متقابل

کدهای اتصال بین CLI و اپلیکیشن اندروید سازگار هستند. هر دو از همان کتابخانه Go (`golib/`) استفاده می‌کنند:

- سرور CLI می‌تواند به کلاینت‌های اندروید سرویس دهد
- سرور اندروید می‌تواند به کلاینت‌های CLI سرویس دهد
- کدهای اتصال به صورت متقابل قابل استفاده هستند

## تفاوت‌ها با اپلیکیشن اندروید

| قابلیت               | اپلیکیشن اندروید             | CLI                         |
|----------------------|------------------------------|------------------------------|
| مسیریابی ترافیک     | TUN/VPN (سیستم‌گستر)         | پروکسی SOCKS5 (هر اپلیکیشن) |
| پلتفرم              | فقط اندروید                  | لینوکس، مک‌اواس، ویندوز     |
| پیکربندی            | رابط کاربری تنظیمات          | فلگ‌های CLI + پیکربندی YAML  |
| کشف                 | مرورگر درون‌برنامه           | دستور `discover` / فلگ `--discover` |
| سیگنالینگ دستی      | QR code / paste              | فلگ `--manual` + stdin       |
| اجرای پس‌زمینه      | سرویس پیش‌زمینه              | پروسه ترمینال                |

برای جزئیات معماری و دستورالعمل‌های ساخت، [README اصلی](../README.fa.md) را ببینید.

</div>
