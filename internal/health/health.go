// Package health exposes a liveness endpoint and a readiness endpoint that
// reports whether the JetStream consumer is connected and how far along the
// durable log the service has processed. This is what "the service is up and
// actually consuming" means to operators.
package health

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// State is the mutable health/readiness state, safe for concurrent use.
type State struct {
	consumerConnected atomic.Bool
	consumerName      atomic.Value // string
	lastStreamSeq     atomic.Int64
	lastProcessedAt   atomic.Value // time.Time
	started           time.Time
}

// NewState returns a State with ready=false (consumer not yet connected).
func NewState() *State {
	s := &State{started: time.Now()}
	s.consumerName.Store("")
	s.lastProcessedAt.Store(time.Time{})
	return s
}

// MarkConsumerConnected records that the durable consumer is connected.
func (s *State) MarkConsumerConnected(name string) {
	s.consumerName.Store(name)
	s.consumerConnected.Store(true)
}

// MarkConsumerDisconnected clears the ready flag (e.g. after a lost connection).
func (s *State) MarkConsumerDisconnected() {
	s.consumerConnected.Store(false)
}

// RecordProcessed advances the "last processed" watermark.
func (s *State) RecordProcessed(seq int64, at time.Time) {
	if s.lastStreamSeq.Load() < seq {
		s.lastStreamSeq.Store(seq)
	}
	s.lastProcessedAt.Store(at)
}

func (s *State) ready() bool { return s.consumerConnected.Load() }

func (s *State) snapshot() map[string]any {
	connected := s.ready()
	at := s.lastProcessedAt.Load()
	var processedAtStr string
	if t, ok := at.(time.Time); ok && !t.IsZero() {
		processedAtStr = t.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"status":             boolStatus(connected),
		"consumer_connected": connected,
		"consumer":           s.consumerName.Load(),
		"last_stream_seq":    s.lastStreamSeq.Load(),
		"last_processed_at":  processedAtStr,
		"started_at":         s.started.UTC().Format(time.RFC3339),
	}
}

func boolStatus(b bool) string {
	if b {
		return "ready"
	}
	return "not_ready"
}

// ServeHTTP implements GET /healthz (liveness) and GET /readyz (readiness).
// /readyz returns 200 only while the durable consumer is connected; otherwise 503.
func (s *State) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/healthz":
		writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
	case "/readyz":
		if s.ready() {
			writeJSON(w, http.StatusOK, s.snapshot())
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "consumer_connected": false})
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		_ = err
	}
}

// Run starts an http.Server serving s on addr until ctx signals or the server
// is stopped. It blocks until the server finishes.
func Run(addr string, s *State, srv *http.Server) error {
	srv.Handler = s
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("health: listen %s: %w", addr, err)
	}
	return srv.Serve(l)
}
