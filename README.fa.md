<div dir="rtl">

**[:gb: English](README.md)** | **:iran: فارسی**

# NATProxy

**اشتراک‌گذاری اینترنت نظیر-به-نظیر برای اندروید + دسکتاپ**

اتصال اینترنت یک دستگاه را با دستگاه‌های دیگر از طریق اینترنت با استفاده از عبور از NAT به اشتراک بگذارید — بدون نیاز به تنظیم port forwarding. یک دستگاه "سرور" اتصال خود را به اشتراک می‌گذارد و دستگاه‌های "کلاینت" ترافیک خود را از طریق کانال داده WebRTC یا تونل پروکسی xray-core مسیریابی می‌کنند.

سه مؤلفه:
- **اپلیکیشن اندروید** — رابط کاربری Flutter با VPN سیستم‌گستر (رابط TUN)
- **CLI دسکتاپ** — ابزار خط فرمان Go با پروکسی SOCKS5 محلی
- **سرور سیگنالینگ** — سرور سبک Go برای کشف نظیرها و تبادل SDP

## قابلیت‌ها

- **آبشار عبور از NAT** — ابتدا مپینگ پورت UPnP، سپس WebRTC ICE hole punching، و در نهایت رله UDP
- **کشف سرور** — مرور سرورهای موجود با امتیازدهی سازگاری NAT (بهترین کاندیداهای hole-punch اول رتبه‌بندی می‌شوند)
- **مبهم‌سازی ضد DPI** — تصادفی‌سازی اثر انگشت DTLS (فورک pion/dtls)، مبهم‌سازی ترافیک FinalMask، پدینگ ترافیک قابل تنظیم
- **فشرده‌سازی SDP** — کدهای اتصال با zlib + base64 فشرده شده برای اشتراک‌گذاری آسان
- **PeerConnection‌های موازی** — چندین اتصال WebRTC برای افزایش توان عملیاتی (قابل تنظیم 1-8)
- **سیگنالینگ دستی** — تبادل کدهای offer/answer بدون سرور سیگنالینگ
- **کش DNS** — کش DNS داخلی برای کاهش تأخیر
- **واردات انتخابی xray-core** — فقط VLESS (freedom، SOCKS، TCP و KCP) لینک شده (صرفه‌جویی 30-50 مگابایت نسبت به xray کامل)
- **تولید لینک VLESS** — هنگام موفقیت UPnP با پروتکل VLESS، یک لینک استاندارد `vless://` تولید می‌کند که در v2rayNG، Nekoray و سایر کلاینت‌های سازگار با Xray قابل وارد کردن است
- **CLI دسکتاپ** — ابزار خط فرمان کامل با پروکسی SOCKS5، پیکربندی YAML و خروجی JSON برای اسکریپت‌نویسی

## معماری

### پشته کامل

```
Flutter UI (Dart)
  ├── HomeScreen (انتخاب نقش: سرور / کلاینت)
  ├── ServerScreen (شروع/توقف، کد اتصال، تعداد کلاینت‌ها)
  └── ClientScreen (وارد کردن کد، اتصال/قطع، آمار ترافیک)
        │
        │  MethodChannel "com.p2pshare/vpn"
        │  EventChannel  "com.p2pshare/status"
        ▼
لایه پلتفرم Kotlin (اندروید)
  ├── MainActivity (مدیریت MethodChannel)
  ├── ProxyVpnService (VPN TUN اندروید، سرویس پیش‌زمینه)
  └── GoBridge (فراخوانی کلاس Golib تولیدشده gomobile)
        │
        ▼
کتابخانه Go Mobile (golib/ → .aar)
  ├── api.go           — API صادرشده gomobile (فقط نوع‌های ساده)
  ├── nat/             — UPnP IGD + تشخیص NAT از STUN
  ├── signaling/       — کلاینت HTTP سیگنالینگ + فشرده‌سازی SDP
  ├── webrtc/          — PeerConnection، ICE، کانال‌های داده، smux
  ├── xray/            — چرخه حیات xray-core (VLESS/SOCKS/KCP/xHTTP)
  └── tunnel/          — TUN fd → tun2socks → پروکسی SOCKS
```

### درخت تصمیم عبور از NAT

```
شروع سرور
    │
    ├─► تلاش مپینگ پورت UPnP
    │     │
    │     ├─ موفق → xray-core (VLESS روی TCP/KCP/xHTTP) + لینک VLESS
    │     │
    │     └─ شکست ──► WebRTC hole punch (ICE/DTLS/SCTP)
    │                   │
    │                   ├─ ICE موفق → pion/webrtc + کانال‌های داده smux
    │                   │
    │                   └─ ICE شکست ──► رله UDP (ارسال غیرشفاف از طریق سرور سیگنالینگ :3478)
    │
    └─► تولید کد اتصال (base64 JSON با endpoint + UUID + روش + تنظیمات)
```

### مسیرهای انتقال

| مسیر        | زمان استفاده               | پشته انتقال                                  |
|-------------|---------------------------|----------------------------------------------|
| UPnP        | روتر از UPnP IGD پشتیبانی می‌کند | xray-core: VLESS ورودی → freedom خروجی     |
| Hole Punch  | UPnP شکست، ICE موفق      | pion/webrtc: کانال داده → smux → SOCKS5      |
| رله UDP     | هر دو بالا شکست          | مشابه hole punch، رله شده از طریق سرور سیگنالینگ |

## ساختار پروژه

```
natproxy/
├── lib/                    # رابط کاربری Flutter (Dart)
│   ├── config/             # ثابت‌های محیطی زمان ساخت
│   ├── models/             # مدل‌های داده
│   ├── screens/            # صفحات سرور، کلاینت، تنظیمات
│   ├── services/           # پل پلتفرم، سرویس تنظیمات
│   └── widgets/            # مؤلفه‌های مشترک UI
├── android/                # لایه پلتفرم اندروید
│   └── app/src/main/kotlin/com/example/natproxy/
│       ├── MainActivity.kt
│       ├── ProxyVpnService.kt
│       └── GoBridge.kt
├── golib/                  # کتابخانه Go Mobile (→ .aar)
│   ├── api.go              # API صادرشده gomobile
│   ├── nat/                # UPnP + STUN
│   ├── signaling/          # کلاینت سیگنالینگ + فشرده‌سازی SDP
│   ├── webrtc/             # اتصالات WebRTC
│   ├── xray/               # موتور xray-core
│   ├── tunnel/             # TUN → tun2socks
│   ├── applog/             # زیرساخت لاگ‌گیری
│   ├── util/               # ابزارهای مشترک
│   └── replace/dtls/       # فورک pion/dtls (تصادفی‌سازی اثر انگشت)
├── signaling-server/       # سیگنالینگ + کشف + رله UDP
├── natproxy-cli/           # ابزار CLI دسکتاپ
│   ├── cmd/                # دستورات Cobra (serve، connect، discover، nat)
│   └── internal/           # پیکربندی، ارکستراتور، نمایش، نوع‌ها
├── scripts/
│   └── apply_env.dart      # مولد .env → env_config.dart
├── build-android.sh        # ساخت golib .aar + Flutter APK
├── .env.example            # قالب متغیرهای محیطی
└── CLAUDE.md               # دستورالعمل‌های دستیار AI
```

## شروع سریع

### پیش‌نیازها

| ابزار          | نسخه      | نکات                                       |
|----------------|-----------|---------------------------------------------|
| Go             | 1.25+     | برای golib و سرور سیگنالینگ                |
| gomobile       | آخرین    | `go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init` |
| Dart SDK       | ^3.10.8   | همراه Flutter ارائه می‌شود                  |
| Flutter        | آخرین    | برای اپلیکیشن اندروید                      |
| Android SDK    | API 24+   | minSdk 24                                   |
| Java           | 17        | مورد نیاز پلاگین Gradle اندروید           |

### 1. راه‌اندازی محیط (اختیاری)

اپلیکیشن با مقادیر پیش‌فرض در `lib/config/env_config.dart` ارسال می‌شود، بنابراین این مرحله اختیاری است. برای سفارشی‌سازی:

```bash
cp .env.example .env
# فایل .env را با آدرس سرور سیگنالینگ، سرور STUN و غیره ویرایش کنید
dart scripts/apply_env.dart
```

### 2. ساخت کتابخانه Go

```bash
./build-android.sh arm          # arm64 + arm (اکثر دستگاه‌ها)
./build-android.sh x86          # x86_64 + x86 (شبیه‌سازها)
./build-android.sh universal    # هر چهار ABI
./build-android.sh arm split    # APK جداگانه برای هر ABI (دانلود کوچکتر)
```

یا به صورت دستی:

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

> **نکته:** `-checklinkname=0` ضروری است زیرا وابستگی غیرمستقیم pion/webrtc یعنی `wlynxg/anet` از `//go:linkname` استفاده می‌کند (از Go 1.23 محدود شده).

### 3. ساخت اپلیکیشن اندروید

```bash
flutter build apk
# یا
flutter run -d android
```

### 4. اجرای سرور سیگنالینگ

```bash
cd signaling-server
go run . -addr :8080
```

برای گزینه‌های استقرار [signaling-server/README.fa.md](signaling-server/README.fa.md) را ببینید.

### 5. اجرای CLI دسکتاپ (اختیاری)

```bash
cd natproxy-cli
go build -o natproxy-cli .
./natproxy-cli serve                # اشتراک‌گذاری اینترنت
./natproxy-cli connect <code>       # اتصال از طریق SOCKS5
```

برای مرجع کامل دستورات [natproxy-cli/README.fa.md](natproxy-cli/README.fa.md) را ببینید.

## پیکربندی

### متغیرهای محیطی (`.env`)

فایل `.env` در زمان ساخت توسط `dart scripts/apply_env.dart` به ثابت‌های Dart تبدیل می‌شود. فایل تولیدشده `lib/config/env_config.dart` با مقادیر پیش‌فرض commit شده تا اپلیکیشن بدون فایل محلی `.env` ساخته شود.

**زیرساخت:**

| متغیر             | پیش‌فرض                        | توضیحات                             |
|-------------------|--------------------------------|--------------------------------------|
| `SIGNALING_URL`   | `http://[IP]:5601`    | آدرس سرور سیگنالینگ WebRTC          |
| `DISCOVERY_URL`   | `http://[IP]:5602`    | آدرس رجیستری کشف سرور              |
| `STUN_SERVER`     | `stun.l.google.com:19302`      | سرور STUN برای تشخیص NAT + ICE     |

**پیش‌فرض‌های سرور:**

| متغیر                        | پیش‌فرض   | توضیحات                      |
|------------------------------|-----------|-------------------------------|
| `SERVER_LISTEN_PORT`         | `10853`   | پورت شنود پروکسی             |
| `SERVER_NAT_METHOD`          | `auto`    | `auto`، `upnp` یا `holepunch` |
| `SERVER_PROTOCOL`            | `vless`   | `vless` یا `socks`            |
| `SERVER_TRANSPORT`           | `xhttp`   | `kcp` یا `xhttp`              |
| `SERVER_DISCOVERY_ENABLED`   | `true`    | ثبت در لیست کشف               |
| `SERVER_USE_RELAY`           | `false`   | رله UDP پشتیبان               |

**پیش‌فرض‌های کلاینت:**

| متغیر                        | پیش‌فرض     | توضیحات                       |
|------------------------------|-------------|-------------------------------|
| `CLIENT_SOCKS_PORT`         | `10808`     | پورت SOCKS5 محلی برای tun2socks |
| `CLIENT_TUN_ADDRESS`        | `10.0.0.2`  | IP رابط TUN                   |
| `CLIENT_MTU`                | `1500`      | MTU رابط TUN (1280-9000)      |
| `CLIENT_DNS1`               | `8.8.8.8`   | DNS اصلی                      |
| `CLIENT_DNS2`               | `1.1.1.1`   | DNS ثانویه                    |
| `CLIENT_ALLOW_DIRECT_DNS`   | `false`     | اجازه DNS ISP (خطر حریم خصوصی) |
| `CLIENT_DISCOVERY_ENABLED`  | `true`      | نمایش مرورگر کشف             |

**VPN:**

| متغیر              | پیش‌فرض     | توضیحات                          |
|--------------------|-------------|----------------------------------|
| `VPN_SESSION_NAME` | `NATProxy`  | برچسب تنظیمات VPN اندروید       |

### پیکربندی CLI

CLI دسکتاپ از فایل‌های پیکربندی YAML و فلگ‌های خط فرمان به جای `.env` استفاده می‌کند. برای مرجع کامل پیکربندی [natproxy-cli/README.fa.md](natproxy-cli/README.fa.md) را ببینید.

## نحوه کار

### کد اتصال

هنگام شروع سرور، یک کد اتصال تولید می‌شود — یک بلوب JSON کدشده با base64 و فشرده‌شده با zlib که شامل:

- نقطه پایانی سرور (IP:port یا اطلاعات رله)
- UUID برای احراز هویت
- روش عبور از NAT استفاده‌شده (UPnP / holepunch / relay)
- تنظیمات پروتکل و انتقال

کلاینت‌ها این کد را برای اتصال paste می‌کنند. کد به اندازه‌ای کوتاه طراحی شده که از طریق پیام‌رسان‌ها قابل اشتراک‌گذاری باشد.

وقتی مسیر UPnP با پروتکل VLESS موفق شود، یک لینک استاندارد `vless://` نیز تولید می‌شود. این لینک مستقیماً در v2rayNG، Nekoray و سایر کلاینت‌های سازگار با Xray قابل وارد کردن است — بدون نیاز به اپلیکیشن NATProxy در سمت کلاینت.

### VPN کلاینت (اندروید)

```
ترافیک اپلیکیشن → رابط TUN اندروید → tun2socks → SOCKS5 (127.0.0.1:10808)
    → خروجی xray-core (مسیر UPnP)
    یا
    → کانال داده WebRTC → smux → SOCKS5 ریموت (مسیر hole punch)
        → اینترنت
```

تمام سوکت‌های پروکسی و WebRTC از طریق `VpnService.protect(fd)` محافظت می‌شوند تا از حلقه مسیریابی در رابط TUN جلوگیری شود.

### SOCKS5 کلاینت (CLI دسکتاپ)

```
curl --proxy socks5h://127.0.0.1:10808 → شنونده SOCKS5
    → خروجی xray-core (مسیر UPnP)
    یا
    → کانال داده WebRTC → smux → SOCKS5 ریموت (مسیر hole punch)
        → اینترنت
```

بدون TUN/VPN — اپلیکیشن‌ها باید به صورت جداگانه برای استفاده از پروکسی SOCKS5 پیکربندی شوند.

## ضد DPI و حریم خصوصی

| تکنیک                          | توضیحات                                                        |
|--------------------------------|----------------------------------------------------------------|
| **تصادفی‌سازی اثر انگشت DTLS** | فورک `pion/dtls` فیلدهای ClientHello را تصادفی می‌کند تا از شناسایی جلوگیری شود |
| **مبهم‌سازی FinalMask**        | حالت‌های مبهم‌سازی ترافیک قابل تنظیم (`header-dtls`، `mkcp-aes128gcm`، `header-dns`) |
| **پدینگ ترافیک**              | بایت‌های پدینگ تصادفی به نوشتن‌ها اضافه می‌کند (v2: الگوهای decoy + burst) |
| **فشرده‌سازی SDP**             | کدهای اتصال با zlib فشرده شده تا اندازه کاهش یابد و ساختار مبهم شود |
| **پنهان‌سازی IP در لاگ‌ها**   | فلگ اختیاری برای پنهان‌سازی آدرس‌های IP در تمام خروجی لاگ     |

## ملاحظات امنیتی

> **این یک اثبات مفهوم (PoC) است.** عبور از NAT و پروکسی P2P را نشان می‌دهد اما فاقد تقویت امنیتی تولید است.

| جنبه                | رویکرد PoC                               | پیشنهاد تولید                           |
|--------------------|-------------------------------------------|-----------------------------------------|
| انتقال سیگنالینگ   | HTTP ساده                                | TLS (HTTPS)                             |
| احراز هویت         | UUID در کد اتصال                          | TLS متقابل یا احراز هویت مبتنی بر توکن |
| احراز هویت سرور سیگنالینگ | بدون — هر کسی می‌تواند جلسه ایجاد کند | کلیدهای API یا OAuth                    |
| کشف                | ثبت‌نام آزاد                              | ثبت‌نام احراز هویت‌شده + محدودسازی نرخ  |
| رله                | ارسال غیرشفاف، بدون احراز هویت           | رله احراز هویت‌شده با سهمیه            |

### محافظت سوکت

در اندروید، تمام سوکت‌های خروجی (xray-core، WebRTC ICE/DTLS، STUN) باید از طریق `VpnService.protect(fd)` محافظت شوند تا از حلقه مسیریابی جلوگیری شود. مسیر WebRTC از `UDPMux` با یک سوکت محافظت‌شده مشترک بین تمام PeerConnectionها استفاده می‌کند.

## توسعه

### لینتینگ و فرمت‌بندی

```bash
dart analyze lib/          # آنالیز استاتیک Flutter
dart format .              # فرمت‌بندی کد Dart
```

### تست

```bash
flutter test                        # تمام تست‌های Flutter
flutter test test/widget_test.dart  # یک فایل تست
```

### تست‌های Go

```bash
cd golib && go test ./...
cd signaling-server && go test ./...
```

## نیازمندی‌های SDK

| مؤلفه           | نسخه / نیازمندی                          |
|-----------------|------------------------------------------|
| Dart SDK        | ^3.10.8                                  |
| Go              | 1.25+                                    |
| Android minSdk  | 24                                       |
| Java            | 17                                       |
| Kotlin          | 2.2.20                                   |
| Gradle          | 8.11.1                                   |
| gomobile        | آخرین                                   |
| فضای نام اندروید | `com.example.natproxy`                 |

### وابستگی‌های کلیدی Go

| پکیج                    | نسخه       | هدف                              |
|--------------------------|------------|---------------------------------|
| `xtls/xray-core`        | v1.260204  | موتور پروکسی (VLESS/SOCKS)     |
| `pion/webrtc/v4`        | v4.1.2     | اتصالات WebRTC                  |
| `pion/dtls/v3`          | v3.0.6     | DTLS (فورک‌شده برای تصادفی‌سازی اثر انگشت) |
| `pion/stun/v2`          | v2.0.0     | تشخیص NAT از STUN               |
| `pion/ice/v4`           | v4.0.10    | اتصال ICE                       |
| `huin/goupnp`           | v1.3.0     | مپینگ پورت UPnP IGD             |
| `xtaci/smux`            | v1.5.33    | مالتی‌پلکسر استریم روی کانال‌های داده |
| `sagernet/sing-tun`     | v0.7.11    | tun2socks (TUN → پروکسی SOCKS) |
| `golang.org/x/mobile`   | آخرین     | تولید binding با gomobile       |

## تصمیمات طراحی کلیدی

- **محدودیت‌های نوع gomobile** — توابع Go صادرشده فقط از `int`، `float64`، `bool`، `string`، `[]byte` استفاده می‌کنند. تمام داده‌های پیچیده به صورت رشته‌های JSON-serialized از مرز FFI عبور می‌کنند.
- **واردات انتخابی xray-core** — فقط VLESS (freedom، SOCKS، TCP و KCP) ثبت شده و حدود 30-50 مگابایت نسبت به توزیع کامل xray صرفه‌جویی می‌شود.
- **محافظت سوکت** — سوکت‌های خروجی xray-core و سوکت‌های ICE UDP باید از طریق `VpnService.protect(fd)` `protect()` شوند تا از حلقه مسیریابی در رابط TUN جلوگیری شود. مسیر WebRTC از `UDPMux` با یک سوکت محافظت‌شده استفاده می‌کند.
- **فورک pion/dtls** — `golib/replace/dtls/` شامل یک فورک از `pion/dtls` با فیلدهای ClientHello تصادفی‌شده DTLS برای مقاومت در برابر شناسایی است.
- **اندروید 14+** — VPN از `foregroundServiceType="specialUse"` استفاده کرده و یک نوتیفیکیشن پیش‌زمینه دائمی نمایش می‌دهد.
- **آبشار عبور از NAT** — UPnP (TCP مستقیم) ترجیح داده می‌شود زیرا سریع‌تر و قابل‌اطمینان‌تر است. WebRTC hole punching پشتیبان است. رله UDP آخرین راه‌حل است.

## مستندات زیرپروژه‌ها

- **[سرور سیگنالینگ](signaling-server/README.fa.md)** — مرجع API، پروتکل رله، امتیازدهی NAT، استقرار
- **[CLI دسکتاپ](natproxy-cli/README.fa.md)** — دستورات، فلگ‌ها، پیکربندی YAML، سیگنالینگ دستی، اسکریپت‌نویسی

</div>
