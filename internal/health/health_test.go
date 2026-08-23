package health

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	b, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(b)
}

func TestNotReadyReturns503(t *testing.T) {
	s := NewState()
	code, body := get(t, s, "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("readyz before consumer connect = %d (%s), want 503", code, body)
	}
}

func TestReadyWhenConsumerConnected(t *testing.T) {
	s := NewState()
	s.MarkConsumerConnected("doorflow-sync")
	s.RecordProcessed(42, time.Now())
	code, body := get(t, s, "/readyz")
	if code != http.StatusOK {
		t.Fatalf("readyz = %d (%s), want 200", code, body)
	}
	for _, want := range []string{`"consumer_connected":true`, `"last_stream_seq":42`, "doorflow-sync"} {
		if !contains(body, want) {
			t.Fatalf("readyz body missing %q.\n got: %s", want, body)
		}
	}
}

func TestLivenessAlwaysAlive(t *testing.T) {
	s := NewState() // not ready
	code, _ := get(t, s, "/healthz")
	if code != http.StatusOK {
		t.Fatalf("healthz = %d, want 200", code)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
