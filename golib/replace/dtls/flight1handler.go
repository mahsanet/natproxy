// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package dtls

import (
	"context"
	"encoding/binary"
	mrand "math/rand"

	"github.com/pion/dtls/v3/pkg/crypto/elliptic"
	"github.com/pion/dtls/v3/pkg/protocol"
	"github.com/pion/dtls/v3/pkg/protocol/alert"
	"github.com/pion/dtls/v3/pkg/protocol/extension"
	"github.com/pion/dtls/v3/pkg/protocol/handshake"
	"github.com/pion/dtls/v3/pkg/protocol/recordlayer"
)

func flight1Parse(
	ctx context.Context,
	conn flightConn,
	state *State,
	cache *handshakeCache,
	cfg *handshakeConfig,
) (flightVal, *alert.Alert, error) {
	// HelloVerifyRequest can be skipped by the server,
	// so allow ServerHello during flight1 also
	seq, msgs, ok := cache.fullPullMap(state.handshakeRecvSequence, state.cipherSuite,
		handshakeCachePullRule{handshake.TypeHelloVerifyRequest, cfg.initialEpoch, false, true},
		handshakeCachePullRule{handshake.TypeServerHello, cfg.initialEpoch, false, true},
	)
	if !ok {
		// No valid message received. Keep reading
		return 0, nil, nil
	}

	if _, ok := msgs[handshake.TypeServerHello]; ok {
		// Flight1 and flight2 were skipped.
		// Parse as flight3.
		return flight3Parse(ctx, conn, state, cache, cfg)
	}

	if h, ok := msgs[handshake.TypeHelloVerifyRequest].(*handshake.MessageHelloVerifyRequest); ok {
		// DTLS 1.2 clients must not assume that the server will use the protocol version
		// specified in HelloVerifyRequest message. RFC 6347 Section 4.2.1
		if !h.Version.Equal(protocol.Version1_0) && !h.Version.Equal(protocol.Version1_2) {
			return 0, &alert.Alert{Level: alert.Fatal, Description: alert.ProtocolVersion}, errUnsupportedProtocolVersion
		}
		state.cookie = append([]byte{}, h.Cookie...)
		state.handshakeRecvSequence = seq

		return flight3, nil, nil
	}

	return 0, &alert.Alert{Level: alert.Fatal, Description: alert.InternalError}, nil
}

//nolint:cyclop
func flight1Generate(
	conn flightConn,
	state *State,
	_ *handshakeCache,
	cfg *handshakeConfig,
) ([]*packet, *alert.Alert, error) {
	var zeroEpoch uint16
	state.localEpoch.Store(zeroEpoch)
	state.remoteEpoch.Store(zeroEpoch)
	state.namedCurve = defaultNamedCurve
	state.cookie = nil

	if err := state.localRandom.Populate(); err != nil {
		return nil, nil, err
	}

	if cfg.helloRandomBytesGenerator != nil {
		state.localRandom.RandomBytes = cfg.helloRandomBytesGenerator()
	}

	extensions := []extension.Extension{
		&extension.SupportedSignatureAlgorithms{
			SignatureHashAlgorithms: cfg.localSignatureSchemes,
		},
		&extension.RenegotiationInfo{
			RenegotiatedConnection: 0,
		},
	}

	var setEllipticCurveCryptographyClientHelloExtensions bool
	for _, c := range cfg.localCipherSuites {
		if c.ECC() {
			setEllipticCurveCryptographyClientHelloExtensions = true

			break
		}
	}

	if setEllipticCurveCryptographyClientHelloExtensions {
		extensions = append(extensions, []extension.Extension{
			&extension.SupportedEllipticCurves{
				EllipticCurves: cfg.ellipticCurves,
			},
			&extension.SupportedPointFormats{
				PointFormats: []elliptic.CurvePointFormat{elliptic.CurvePointFormatUncompressed},
			},
		}...)
	}

	if len(cfg.localSRTPProtectionProfiles) > 0 {
		extensions = append(extensions, &extension.UseSRTP{
			ProtectionProfiles:  cfg.localSRTPProtectionProfiles,
			MasterKeyIdentifier: cfg.localSRTPMasterKeyIdentifier,
		})
	}

	if cfg.extendedMasterSecret == RequestExtendedMasterSecret ||
		cfg.extendedMasterSecret == RequireExtendedMasterSecret {
		extensions = append(extensions, &extension.UseExtendedMasterSecret{
			Supported: true,
		})
	}

	if len(cfg.serverName) > 0 {
		extensions = append(extensions, &extension.ServerName{ServerName: cfg.serverName})
	}

	if len(cfg.supportedProtocols) > 0 {
		extensions = append(extensions, &extension.ALPN{ProtocolNameList: cfg.supportedProtocols})
	}

	if cfg.sessionStore != nil {
		cfg.log.Tracef("[handshake] try to resume session")
		if s, err := cfg.sessionStore.Get(conn.sessionKey()); err != nil {
			return nil, &alert.Alert{Level: alert.Fatal, Description: alert.InternalError}, err
		} else if s.ID != nil {
			cfg.log.Tracef("[handshake] get saved session: %x", s.ID)

			state.SessionID = s.ID
			state.masterSecret = s.Secret
		}
	}

	// If we have a connection ID generator, use it. The CID may be zero length,
	// in which case we are just requesting that the server send us a CID to
	// use.
	if cfg.connectionIDGenerator != nil {
		state.setLocalConnectionID(cfg.connectionIDGenerator())
		// The presence of a generator indicates support for connection IDs. We
		// use the presence of a non-nil local CID in flight 3 to determine
		// whether we send a CID in the second ClientHello, so we convert any
		// nil CID returned by a generator to []byte{}.
		if state.getLocalConnectionID() == nil {
			state.setLocalConnectionID([]byte{})
		}
		extensions = append(extensions, &extension.ConnectionID{CID: state.getLocalConnectionID()})
	}

	cipherSuites := cipherSuiteIDs(cfg.localCipherSuites)

	// Phase 10: If randomization seed is set, apply truncation and shuffle
	// at the flight handler level BEFORE the hook runs.
	if len(cfg.randomizationSeed) >= 8 {
		seedInt := int64(binary.BigEndian.Uint64(cfg.randomizationSeed[:8]))
		rng := mrand.New(mrand.NewSource(seedInt)) //nolint:gosec

		// Truncate cipher suites: keep 3 to N, always keep AES-128-GCM-SHA256 (0xc02b)
		if len(cipherSuites) > 3 {
			keep := 3 + rng.Intn(len(cipherSuites)-2)
			if keep > len(cipherSuites) {
				keep = len(cipherSuites)
			}
			// Ensure 0xc02b is in kept range
			hasRequired := false
			for _, s := range cipherSuites[:keep] {
				if s == 0xc02b {
					hasRequired = true
					break
				}
			}
			if !hasRequired {
				for i := keep; i < len(cipherSuites); i++ {
					if cipherSuites[i] == 0xc02b {
						cipherSuites[keep-1], cipherSuites[i] = cipherSuites[i], cipherSuites[keep-1]
						break
					}
				}
			}
			cipherSuites = cipherSuites[:keep]
		}

		// Shuffle cipher suites
		rng.Shuffle(len(cipherSuites), func(i, j int) {
			cipherSuites[i], cipherSuites[j] = cipherSuites[j], cipherSuites[i]
		})

		// Truncate and shuffle extension internal fields
		for _, ext := range extensions {
			switch e := ext.(type) {
			case *extension.SupportedEllipticCurves:
				if len(e.EllipticCurves) > 1 {
					keep := 1 + rng.Intn(len(e.EllipticCurves))
					if keep > len(e.EllipticCurves) {
						keep = len(e.EllipticCurves)
					}
					e.EllipticCurves = e.EllipticCurves[:keep]
				}
			case *extension.SupportedSignatureAlgorithms:
				if len(e.SignatureHashAlgorithms) > 2 {
					keep := 1 + rng.Intn(len(e.SignatureHashAlgorithms))
					if keep > len(e.SignatureHashAlgorithms) {
						keep = len(e.SignatureHashAlgorithms)
					}
					e.SignatureHashAlgorithms = e.SignatureHashAlgorithms[:keep]
				}
			case *extension.UseSRTP:
				rng.Shuffle(len(e.ProtectionProfiles), func(i, j int) {
					e.ProtectionProfiles[i], e.ProtectionProfiles[j] = e.ProtectionProfiles[j], e.ProtectionProfiles[i]
				})
			}
		}

		// Shuffle extension order
		rng.Shuffle(len(extensions), func(i, j int) {
			extensions[i], extensions[j] = extensions[j], extensions[i]
		})
	}

	clientHello := &handshake.MessageClientHello{
		Version:            protocol.Version1_2,
		SessionID:          state.SessionID,
		Cookie:             state.cookie,
		Random:             state.localRandom,
		CipherSuiteIDs:     cipherSuites,
		CompressionMethods: defaultCompressionMethods(),
		Extensions:         extensions,
	}

	var content handshake.Handshake

	if cfg.clientHelloMessageHook != nil {
		content = handshake.Handshake{Message: cfg.clientHelloMessageHook(*clientHello)}
	} else {
		content = handshake.Handshake{Message: clientHello}
	}

	return []*packet{
		{
			record: &recordlayer.RecordLayer{
				Header: recordlayer.Header{
					Version: protocol.Version1_2,
				},
				Content: &content,
			},
		},
	}, nil, nil
}
