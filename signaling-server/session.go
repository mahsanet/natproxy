package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type session struct {
	offer      []byte
	answer     []byte
	createdAt  time.Time
	lastActive time.Time
}

// offerSubscriber is a persistent SSE subscriber for offer updates.
// Unlike answerWaiters (one-shot), these stay registered until the client disconnects.
type offerSubscriber struct {
	ch   chan []byte  // capacity 1
	done chan struct{}
}

var (
	mu       sync.RWMutex
	sessions = make(map[string]*session)

	// SSE waiters for answer delivery: session ID → list of waiting channels.
	answerWaitersMu sync.Mutex
	answerWaiters   = make(map[string][]chan []byte)

	// Persistent SSE subscribers for offer updates.
	offerSubsMu sync.Mutex
	offerSubs   = make(map[string][]*offerSubscriber)
)

func handleSession(w http.ResponseWriter, r *http.Request) {
	// Parse path: /session/{id}/{type} or /session/{id}/answer/stream
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/session/"), "/")

	// Handle SSE streams: /session/{id}/{answer|offer}/stream
	if len(parts) == 3 && parts[2] == "stream" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch parts[1] {
		case "answer":
			handleAnswerStream(w, r, parts[0])
		case "offer":
			handleOfferStream(w, r, parts[0])
		default:
			http.Error(w, "invalid stream type: expected 'offer' or 'answer'", http.StatusBadRequest)
		}
		return
	}

	if len(parts) != 2 {
		http.Error(w, "invalid path: expected /session/{id}/{offer|answer}", http.StatusBadRequest)
		return
	}

	sessionID := parts[0]
	infoType := parts[1]

	if infoType != "offer" && infoType != "answer" {
		http.Error(w, "invalid type: expected 'offer' or 'answer'", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPost:
		handlePost(w, r, sessionID, infoType)
	case http.MethodGet:
		handleGet(w, r, sessionID, infoType)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handlePost(w http.ResponseWriter, r *http.Request, sessionID, infoType string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}

	// Validate JSON
	if !json.Valid(body) {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	s, exists := sessions[sessionID]
	now := time.Now()
	if !exists {
		s = &session{createdAt: now}
		sessions[sessionID] = s
	}
	s.lastActive = now

	switch infoType {
	case "offer":
		s.offer = body
		notifyOfferSubscribers(sessionID, body)
	case "answer":
		// An empty SDP ({"sdp":""}) is a "clear" — nil out the slot so SSE
		// waiters and GET polls treat it as "not yet available".
		var check struct{ SDP string `json:"sdp"` }
		if json.Unmarshal(body, &check) == nil && check.SDP == "" {
			s.answer = nil
		} else {
			s.answer = body
			notifyAnswerWaiters(sessionID, body)
		}
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

// notifyAnswerWaiters wakes up any SSE clients waiting on this session's answer.
func notifyAnswerWaiters(sessionID string, data []byte) {
	answerWaitersMu.Lock()
	waiters := answerWaiters[sessionID]
	delete(answerWaiters, sessionID)
	answerWaitersMu.Unlock()

	for _, ch := range waiters {
		select {
		case ch <- data:
		default:
		}
	}
}

func addOfferSubscriber(sessionID string, sub *offerSubscriber) {
	offerSubsMu.Lock()
	offerSubs[sessionID] = append(offerSubs[sessionID], sub)
	offerSubsMu.Unlock()
}

func removeOfferSubscriber(sessionID string, sub *offerSubscriber) {
	offerSubsMu.Lock()
	subs := offerSubs[sessionID]
	for i, s := range subs {
		if s == sub {
			offerSubs[sessionID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(offerSubs[sessionID]) == 0 {
		delete(offerSubs, sessionID)
	}
	offerSubsMu.Unlock()
	close(sub.done)
}

// notifyOfferSubscribers pushes a new offer to all watching clients.
// Drains any queued value first so they always get the latest, not a stale one.
func notifyOfferSubscribers(sessionID string, data []byte) {
	offerSubsMu.Lock()
	subs := make([]*offerSubscriber, len(offerSubs[sessionID]))
	copy(subs, offerSubs[sessionID])
	offerSubsMu.Unlock()

	for _, sub := range subs {
		// Drain any old value so subscriber always gets the latest.
		select {
		case <-sub.ch:
		default:
		}
		select {
		case sub.ch <- data:
		default:
		}
	}
}

// handleOfferStream keeps an SSE connection open and pushes each new offer as it arrives.
// Unlike the answer stream, this doesn't close after the first event.
func handleOfferStream(w http.ResponseWriter, r *http.Request, sessionID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Register subscriber BEFORE checking existing offer (race safety).
	sub := &offerSubscriber{
		ch:   make(chan []byte, 1),
		done: make(chan struct{}),
	}
	addOfferSubscriber(sessionID, sub)
	defer removeOfferSubscriber(sessionID, sub)

	// If offer already exists, send it immediately.
	mu.RLock()
	s, exists := sessions[sessionID]
	var existing []byte
	if exists && s.offer != nil {
		existing = make([]byte, len(s.offer))
		copy(existing, s.offer)
	}
	mu.RUnlock()

	if existing != nil {
		w.Write(formatSSEEvent("offer", string(existing)))
		flusher.Flush()
	}

	keepalive := time.NewTicker(90 * time.Second)
	defer keepalive.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-sub.ch:
			w.Write(formatSSEEvent("offer", string(data)))
			flusher.Flush()
		case <-keepalive.C:
			w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}

// handleAnswerStream waits for the SDP answer and delivers it over SSE, then closes.
// If the answer is already there, it fires immediately.
func handleAnswerStream(w http.ResponseWriter, r *http.Request, sessionID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Register waiter BEFORE checking for existing answer to avoid a race
	// where an answer arrives between the check and registration.
	ch := make(chan []byte, 1)
	answerWaitersMu.Lock()
	answerWaiters[sessionID] = append(answerWaiters[sessionID], ch)
	answerWaitersMu.Unlock()

	// Clean up waiter on exit.
	defer func() {
		answerWaitersMu.Lock()
		waiters := answerWaiters[sessionID]
		for i, w := range waiters {
			if w == ch {
				answerWaiters[sessionID] = append(waiters[:i], waiters[i+1:]...)
				break
			}
		}
		if len(answerWaiters[sessionID]) == 0 {
			delete(answerWaiters, sessionID)
		}
		answerWaitersMu.Unlock()
	}()

	// Check if answer already exists.
	mu.RLock()
	s, exists := sessions[sessionID]
	var existing []byte
	if exists && s.answer != nil {
		existing = make([]byte, len(s.answer))
		copy(existing, s.answer)
	}
	mu.RUnlock()

	if existing != nil {
		w.Write(formatSSEEvent("answer", string(existing)))
		flusher.Flush()
		return
	}

	keepalive := time.NewTicker(90 * time.Second)
	defer keepalive.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-ch:
			w.Write(formatSSEEvent("answer", string(data)))
			flusher.Flush()
			return
		case <-keepalive.C:
			w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}

func handleGet(w http.ResponseWriter, r *http.Request, sessionID, infoType string) {
	mu.RLock()
	defer mu.RUnlock()

	s, exists := sessions[sessionID]
	if !exists {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var data []byte
	switch infoType {
	case "offer":
		data = s.offer
	case "answer":
		data = s.answer
	}

	if data == nil {
		http.Error(w, "data not yet available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
