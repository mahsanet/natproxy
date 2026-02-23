package webrtc

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	mrand "math/rand"
	"sync"

	"golang.org/x/crypto/hkdf"

	"github.com/pion/dtls/v3/pkg/protocol/extension"
	"github.com/pion/dtls/v3/pkg/protocol/handshake"

	"natproxy/golib/applog"
)

// --- Phase 3: Seeded DTLS randomization with truncation ---

var (
	dtlsRandomSeed []byte
	dtlsSeedMu     sync.Mutex
)

// SetDTLSRandomSeed derives a deterministic DTLS randomization seed from the
// shared obfuscation key. Both peers derive the same seed (using role-specific
// salt) so truncation choices are compatible.
func SetDTLSRandomSeed(obfsKey []byte, isServer bool) {
	salt := "natproxy-dtls-seed-client"
	if isServer {
		salt = "natproxy-dtls-seed-server"
	}

	hkdfReader := hkdf.New(sha256.New, obfsKey, []byte(salt), []byte("dtls-randomize-v3"))
	seed := make([]byte, 32)
	if _, err := hkdfReader.Read(seed); err != nil {
		applog.Warnf("webrtc: HKDF for DTLS seed failed: %v", err)
		return
	}

	dtlsSeedMu.Lock()
	dtlsRandomSeed = seed
	dtlsSeedMu.Unlock()
}

// newSeededRand builds a deterministic PRNG from the DTLS seed.
// Returns nil when no seed is set — callers fall back to crypto/rand.
func newSeededRand() *mrand.Rand {
	dtlsSeedMu.Lock()
	seed := dtlsRandomSeed
	dtlsSeedMu.Unlock()

	if len(seed) == 0 {
		return nil
	}

	// Derive int64 seed from the first 8 bytes.
	seedInt := int64(binary.BigEndian.Uint64(seed[:8]))
	return mrand.New(mrand.NewSource(seedInt))
}

// TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256 must always be kept for compatibility.
const requiredCipherSuiteID = 0xc02b

// randomizeDTLSClientHello applies seeded truncation + shuffle to defeat
// fingerprinting. When a seed is available, cipher suites, curves, and sig
// algs are truncated (not just shuffled) to vary the list lengths across
// connections with different keys.
func randomizeDTLSClientHello(hello handshake.MessageClientHello) handshake.Message {
	rng := newSeededRand()

	if rng != nil {
		// Truncate cipher suites: keep 3 to N, always preserving the required suite.
		hello.CipherSuiteIDs = truncateCipherSuites(hello.CipherSuiteIDs, rng)
	}

	// Shuffle cipher suites.
	seededShuffleUint16s(hello.CipherSuiteIDs, rng)

	// Truncate and shuffle extensions' internal lists.
	for _, ext := range hello.Extensions {
		randomizeExtensionFieldsSeeded(ext, rng)
	}

	// Shuffle top-level extension order.
	seededShuffleExtensions(hello.Extensions, rng)

	applog.Infof("webrtc: randomized DTLS ClientHello (%d suites, %d extensions, seeded=%v)",
		len(hello.CipherSuiteIDs), len(hello.Extensions), rng != nil)

	return &hello
}

// randomizeDTLSServerHello shuffles the DTLS ServerHello extension order.
func randomizeDTLSServerHello(hello handshake.MessageServerHello) handshake.Message {
	rng := newSeededRand()
	seededShuffleExtensions(hello.Extensions, rng)

	applog.Infof("webrtc: randomized DTLS ServerHello (%d extensions)", len(hello.Extensions))
	return &hello
}

// truncateCipherSuites reduces the cipher suite list to a random subset (3 to N),
// always keeping the required AES-128-GCM suite for compatibility.
func truncateCipherSuites(suites []uint16, rng *mrand.Rand) []uint16 {
	if len(suites) <= 3 {
		return suites
	}

	// Keep between 3 and len(suites) suites.
	keep := 3 + rng.Intn(len(suites)-2)
	if keep > len(suites) {
		keep = len(suites)
	}

	// Ensure required suite is included.
	hasRequired := false
	for _, s := range suites[:keep] {
		if s == requiredCipherSuiteID {
			hasRequired = true
			break
		}
	}

	if !hasRequired {
		// Find and swap the required suite into the kept range.
		for i := keep; i < len(suites); i++ {
			if suites[i] == requiredCipherSuiteID {
				suites[keep-1], suites[i] = suites[i], suites[keep-1]
				break
			}
		}
	}

	return suites[:keep]
}

// randomizeExtensionFieldsSeeded truncates and shuffles extension internal lists.
func randomizeExtensionFieldsSeeded(ext extension.Extension, rng *mrand.Rand) {
	switch e := ext.(type) {
	case *extension.SupportedEllipticCurves:
		if rng != nil && len(e.EllipticCurves) > 1 {
			// Truncate curves: keep at least 1 (X25519 should be first).
			keep := 1 + rng.Intn(len(e.EllipticCurves))
			if keep > len(e.EllipticCurves) {
				keep = len(e.EllipticCurves)
			}
			e.EllipticCurves = e.EllipticCurves[:keep]
		}
		seededShuffle(len(e.EllipticCurves), func(i, j int) {
			e.EllipticCurves[i], e.EllipticCurves[j] = e.EllipticCurves[j], e.EllipticCurves[i]
		}, rng)

	case *extension.SupportedSignatureAlgorithms:
		if rng != nil && len(e.SignatureHashAlgorithms) > 2 {
			// Truncate sig algs: keep first (for ECDSA P-256 compat) + random subset.
			keep := 1 + rng.Intn(len(e.SignatureHashAlgorithms))
			if keep > len(e.SignatureHashAlgorithms) {
				keep = len(e.SignatureHashAlgorithms)
			}
			e.SignatureHashAlgorithms = e.SignatureHashAlgorithms[:keep]
		}
		if len(e.SignatureHashAlgorithms) > 1 {
			rest := e.SignatureHashAlgorithms[1:]
			seededShuffle(len(rest), func(i, j int) {
				rest[i], rest[j] = rest[j], rest[i]
			}, rng)
		}

	case *extension.UseSRTP:
		seededShuffle(len(e.ProtectionProfiles), func(i, j int) {
			e.ProtectionProfiles[i], e.ProtectionProfiles[j] = e.ProtectionProfiles[j], e.ProtectionProfiles[i]
		}, rng)

	case *extension.SupportedPointFormats:
		seededShuffle(len(e.PointFormats), func(i, j int) {
			e.PointFormats[i], e.PointFormats[j] = e.PointFormats[j], e.PointFormats[i]
		}, rng)
	}
}

// seededShuffle performs Fisher-Yates shuffle using either a seeded PRNG or crypto/rand.
func seededShuffle(n int, swap func(i, j int), rng *mrand.Rand) {
	if rng != nil {
		for i := n - 1; i > 0; i-- {
			j := rng.Intn(i + 1)
			swap(i, j)
		}
		return
	}
	cryptoShuffle(n, swap)
}

// seededShuffleUint16s shuffles a uint16 slice.
func seededShuffleUint16s(s []uint16, rng *mrand.Rand) {
	seededShuffle(len(s), func(i, j int) {
		s[i], s[j] = s[j], s[i]
	}, rng)
}

// seededShuffleExtensions shuffles an Extension slice.
func seededShuffleExtensions(s []extension.Extension, rng *mrand.Rand) {
	seededShuffle(len(s), func(i, j int) {
		s[i], s[j] = s[j], s[i]
	}, rng)
}

// cryptoShuffle performs a Fisher-Yates shuffle using crypto/rand.
func cryptoShuffle(n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		jBig, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return
		}
		j := int(jBig.Int64())
		swap(i, j)
	}
}
