package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// SSE subscriber infrastructure for live discovery updates.

type sseSubscriber struct {
	ch   chan []byte
	room string
	done chan struct{}
}

var (
	ssesMu      sync.Mutex
	subscribers = make(map[*sseSubscriber]struct{})
)

func addSubscriber(sub *sseSubscriber) {
	ssesMu.Lock()
	subscribers[sub] = struct{}{}
	ssesMu.Unlock()
}

func removeSubscriber(sub *sseSubscriber) {
	ssesMu.Lock()
	delete(subscribers, sub)
	ssesMu.Unlock()
	close(sub.done)
}

func broadcastListings() {
	ssesMu.Lock()
	subs := make([]*sseSubscriber, 0, len(subscribers))
	for s := range subscribers {
		subs = append(subs, s)
	}
	ssesMu.Unlock()

	if len(subs) == 0 {
		return
	}

	// Build per-room filtered payloads.
	lmu.RLock()
	// Collect all listings once.
	all := make([]*serverListing, 0, len(listings))
	for _, l := range listings {
		all = append(all, l)
	}
	lmu.RUnlock()

	// Cache marshalled data per room key to avoid repeated work.
	cache := make(map[string][]byte)

	for _, sub := range subs {
		data, ok := cache[sub.room]
		if !ok {
			var filtered []*serverListing
			for _, l := range all {
				if sub.room == "" || l.Room == sub.room || l.Room == "" {
					filtered = append(filtered, l)
				}
			}
			if filtered == nil {
				filtered = []*serverListing{}
			}
			data, _ = json.Marshal(filtered)
			data = formatSSEEvent("servers", string(data))
			cache[sub.room] = data
		}
		select {
		case sub.ch <- data:
		default:
			// Slow subscriber, drop event.
		}
	}
}

func formatSSEEvent(eventType, data string) []byte {
	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, data))
}

type serverListing struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Room           string    `json:"room,omitempty"`
	ConnectionCode string    `json:"code"`
	Method         string    `json:"method"`
	Transport      string    `json:"transport"`
	Protocol       string    `json:"protocol"`
	NATMapping     string    `json:"nat_mapping,omitempty"`
	NATFiltering   string    `json:"nat_filtering,omitempty"`
	CreatedAt      time.Time `json:"-"`
	LastHeartbeat  time.Time `json:"-"`
}

var (
	lmu      sync.RWMutex
	listings = make(map[string]*serverListing)
)

func handleDiscovery(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/discovery/")
	path = strings.TrimSuffix(path, "/")

	switch {
	case path == "register" && r.Method == http.MethodPost:
		handleDiscoveryRegister(w, r)
	case path == "servers" && r.Method == http.MethodGet:
		handleDiscoveryList(w, r)
	case path == "stream" && r.Method == http.MethodGet:
		handleDiscoveryStream(w, r)
	default:
		// /discovery/{id} or /discovery/{id}/heartbeat
		parts := strings.Split(path, "/")
		if len(parts) == 1 && r.Method == http.MethodDelete {
			handleDiscoveryDelete(w, parts[0])
		} else if len(parts) == 2 && parts[1] == "heartbeat" && r.Method == http.MethodPost {
			handleDiscoveryHeartbeat(w, parts[0])
		} else {
			http.Error(w, "not found", http.StatusNotFound)
		}
	}
}

func handleDiscoveryRegister(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}

	var req struct {
		Name         string `json:"name"`
		Room         string `json:"room"`
		Code         string `json:"code"`
		Method       string `json:"method"`
		Transport    string `json:"transport"`
		Protocol     string `json:"protocol"`
		NATMapping   string `json:"nat_mapping"`
		NATFiltering string `json:"nat_filtering"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Name == "" || len(req.Name) > maxNameLength {
		http.Error(w, fmt.Sprintf("name is required (max %d chars)", maxNameLength), http.StatusBadRequest)
		return
	}
	if req.Code == "" {
		http.Error(w, "code is required", http.StatusBadRequest)
		return
	}

	id := generateListingID()
	now := time.Now()
	listing := &serverListing{
		ID:             id,
		Name:           req.Name,
		Room:           req.Room,
		ConnectionCode: req.Code,
		Method:         req.Method,
		Transport:      req.Transport,
		Protocol:       req.Protocol,
		NATMapping:     req.NATMapping,
		NATFiltering:   req.NATFiltering,
		CreatedAt:      now,
		LastHeartbeat:  now,
	}

	lmu.Lock()
	// Remove existing listings with the same name to prevent duplicates
	// (e.g., when app restarts before the old listing expires)
	for existingID, existing := range listings {
		if existing.Name == req.Name {
			delete(listings, existingID)
		}
	}
	listings[id] = listing
	lmu.Unlock()
	broadcastListings()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func handleDiscoveryDelete(w http.ResponseWriter, id string) {
	lmu.Lock()
	_, exists := listings[id]
	if exists {
		delete(listings, id)
	}
	lmu.Unlock()
	if exists {
		broadcastListings()
	}

	if !exists {
		http.Error(w, "listing not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

func handleDiscoveryHeartbeat(w http.ResponseWriter, id string) {
	lmu.Lock()
	l, exists := listings[id]
	if exists {
		l.LastHeartbeat = time.Now()
	}
	lmu.Unlock()

	if !exists {
		http.Error(w, "listing not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

func handleDiscoveryList(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	clientMapping := r.URL.Query().Get("nat_mapping")
	clientFiltering := r.URL.Query().Get("nat_filtering")

	lmu.RLock()
	var result []*serverListing
	for _, l := range listings {
		if room == "" || l.Room == room || l.Room == "" {
			result = append(result, l)
		}
	}
	lmu.RUnlock()

	if result == nil {
		result = []*serverListing{}
	}

	// Sort by NAT compatibility if client provides its NAT type.
	if clientMapping != "" {
		sort.Slice(result, func(i, j int) bool {
			scoreI := natCompatibilityScore(result[i].NATMapping, result[i].NATFiltering, clientMapping, clientFiltering)
			scoreJ := natCompatibilityScore(result[j].NATMapping, result[j].NATFiltering, clientMapping, clientFiltering)
			return scoreI > scoreJ // highest compatibility first
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func handleDiscoveryStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	room := r.URL.Query().Get("room")

	sub := &sseSubscriber{
		ch:   make(chan []byte, 16),
		room: room,
		done: make(chan struct{}),
	}
	addSubscriber(sub)
	defer removeSubscriber(sub)

	// Send initial full server list.
	lmu.RLock()
	var initial []*serverListing
	for _, l := range listings {
		if room == "" || l.Room == room || l.Room == "" {
			initial = append(initial, l)
		}
	}
	lmu.RUnlock()
	if initial == nil {
		initial = []*serverListing{}
	}
	data, _ := json.Marshal(initial)
	w.Write(formatSSEEvent("servers", string(data)))
	flusher.Flush()

	keepalive := time.NewTicker(30 * time.Second)
	defer keepalive.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-sub.ch:
			w.Write(event)
			flusher.Flush()
		case <-keepalive.C:
			w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}

func generateListingID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
