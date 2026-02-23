// Signaling server for NATProxy peer endpoint exchange.
//
// Simple HTTP-based signaling:
//
//	POST /session/:id/offer   - store peer A's endpoint
//	GET  /session/:id/offer   - peer B retrieves peer A's endpoint
//	POST /session/:id/answer  - store peer B's endpoint
//	GET  /session/:id/answer  - peer A retrieves peer B's endpoint
//
// Server discovery:
//
//	POST   /discovery/register       - register a server listing
//	DELETE /discovery/{id}           - remove a server listing
//	POST   /discovery/{id}/heartbeat - refresh heartbeat
//	GET    /discovery/servers        - list active servers (?room=X optional)
//
// Sessions expire after 5 minutes. Listings expire after 30 seconds without heartbeat.
// No authentication for PoC.
//
// Deploy on Fly.io / Railway or run locally for testing:
//
//	go run . -addr :8080
//
// Run with separate signaling and discovery ports:
//
//	go run . -signaling-addr :5601 -discovery-addr :5602
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
)

var (
	Version = "dev"
	Commit  = "unknown"
)

func main() {
	showVersion   := flag.Bool("version", false, "print version and exit")
	addr          := flag.String("addr", "", "listen address for both signaling and discovery (overrides -signaling-addr and -discovery-addr)")
	signalingAddr := flag.String("signaling-addr", ":5601", "signaling listen address (empty to disable)")
	discoveryAddr := flag.String("discovery-addr", ":5602", "discovery listen address (empty to disable)")
	relayAddr     := flag.String("relay", ":3478", "UDP relay listen address (empty to disable)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("signaling-server %s (commit: %s)\n", Version, Commit)
		return
	}

	// -addr overrides both for backward compatibility.
	if *addr != "" {
		*signalingAddr = *addr
		*discoveryAddr = *addr
	}

	// Start UDP relay for NAT traversal fallback.
	var relay *UDPRelay
	if *relayAddr != "" {
		var err error
		relay, err = NewUDPRelay(*relayAddr)
		if err != nil {
			log.Printf("WARNING: UDP relay failed to start on %s: %v", *relayAddr, err)
		} else {
			go relay.Run()
			log.Printf("UDP relay listening on %s", *relayAddr)
		}
	}

	// Expire old sessions every minute.
	go cleanupLoop(relay)

	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}

	if *signalingAddr == *discoveryAddr && *signalingAddr != "" {
		// Combined mode: single mux, same behavior as before.
		mux := http.NewServeMux()
		mux.HandleFunc("/session/", handleSession)
		mux.HandleFunc("/discovery/", handleDiscovery)
		mux.HandleFunc("/health", healthHandler)
		log.Printf("Signaling+Discovery server listening on %s", *signalingAddr)
		log.Fatal(http.ListenAndServe(*signalingAddr, mux))
		return
	}

	// Separate mode: start each listener in its own goroutine.
	errCh := make(chan error, 2)

	if *signalingAddr != "" {
		sigMux := http.NewServeMux()
		sigMux.HandleFunc("/session/", handleSession)
		sigMux.HandleFunc("/health", healthHandler)
		log.Printf("Signaling server listening on %s", *signalingAddr)
		go func() {
			errCh <- http.ListenAndServe(*signalingAddr, sigMux)
		}()
	}

	if *discoveryAddr != "" {
		discMux := http.NewServeMux()
		discMux.HandleFunc("/discovery/", handleDiscovery)
		discMux.HandleFunc("/health", healthHandler)
		log.Printf("Discovery server listening on %s", *discoveryAddr)
		go func() {
			errCh <- http.ListenAndServe(*discoveryAddr, discMux)
		}()
	}

	if *signalingAddr == "" && *discoveryAddr == "" {
		log.Fatal("Both signaling-addr and discovery-addr are empty — nothing to serve")
	}

	log.Fatal(<-errCh)
}
