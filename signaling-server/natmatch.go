package main

// natCompatibilityScore guesses how likely a hole punch will work between
// a server and client given their RFC 5780 NAT types. Higher = better odds.
//
// Based on the Snowflake/Tor compatibility matrix:
//
//	EI+EI  = 100 (both endpoint-independent → easy hole punch)
//	EI+AD  = 70  (one side is address-dependent → usually works)
//	EI+APD = 40  (one side is address+port-dependent → often works with ICE)
//	AD+AD  = 30  (both address-dependent → sometimes works)
//	AD+APD = 15  (mixed restrictive → unlikely without relay)
//	APD+APD = 5  (both symmetric → almost never works without relay)
func natCompatibilityScore(sMapping, sFiltering, cMapping, cFiltering string) int {
	if sMapping == "" || cMapping == "" {
		// Unknown NAT type — return a neutral score
		return 50
	}

	mappingScore := mappingPairScore(sMapping, cMapping)
	filteringScore := filteringPairScore(sFiltering, cFiltering)

	// Combined score: mapping is more important for hole-punching success
	return (mappingScore*70 + filteringScore*30) / 100
}

func mappingPairScore(a, b string) int {
	aRank := mappingRank(a)
	bRank := mappingRank(b)

	// Sort so aRank <= bRank (lower is better)
	if aRank > bRank {
		aRank, bRank = bRank, aRank
	}

	switch {
	case aRank == 0 && bRank == 0:
		return 100 // EI + EI
	case aRank == 0 && bRank == 1:
		return 70 // EI + AD
	case aRank == 0 && bRank == 2:
		return 40 // EI + APD
	case aRank == 1 && bRank == 1:
		return 30 // AD + AD
	case aRank == 1 && bRank == 2:
		return 15 // AD + APD
	case aRank == 2 && bRank == 2:
		return 5 // APD + APD (symmetric + symmetric)
	default:
		return 25
	}
}

func filteringPairScore(a, b string) int {
	aRank := filteringRank(a)
	bRank := filteringRank(b)

	if aRank > bRank {
		aRank, bRank = bRank, aRank
	}

	switch {
	case aRank == 0 && bRank == 0:
		return 100 // EI + EI
	case aRank == 0 && bRank == 1:
		return 80 // EI + AD
	case aRank == 0 && bRank == 2:
		return 50 // EI + APD
	case aRank == 1 && bRank == 1:
		return 40 // AD + AD
	case aRank == 1 && bRank == 2:
		return 20 // AD + APD
	case aRank == 2 && bRank == 2:
		return 10 // APD + APD
	default:
		return 30
	}
}

// mappingRank turns a NAT mapping string into a rank — 0 is best (EI), 2 is worst (APD).
func mappingRank(m string) int {
	switch m {
	case "endpoint_independent", "EndpointIndependent":
		return 0
	case "address_dependent", "AddressDependent":
		return 1
	case "address_and_port_dependent", "AddressAndPortDependent":
		return 2
	default:
		return 1 // unknown → assume mid-range
	}
}

// filteringRank is the same idea as mappingRank but for filtering behavior.
func filteringRank(f string) int {
	switch f {
	case "endpoint_independent", "EndpointIndependent":
		return 0
	case "address_dependent", "AddressDependent":
		return 1
	case "address_and_port_dependent", "AddressAndPortDependent":
		return 2
	default:
		return 1
	}
}
