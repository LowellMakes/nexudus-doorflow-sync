package logging

import (
	"testing"
	"time"
)

func TestFailedRequestLogsResponseBody(t *testing.T) {
	c := &Capture{}
	l := New(c, "debug")
	body := []byte(`{"error":"unauthorized","code":"fob_401"}`)
	LogRequest(l, "POST", "/api/2/person/88", 401, body, 12*time.Millisecond)

	if !c.Contains("fob_401") || !c.Contains("unauthorized") {
		t.Fatalf("failed request log must include the response body.\ngot:\n%s", c.String())
	}
	if !c.Contains("status=401") {
		t.Fatalf("failed request log must include the status.\ngot:\n%s", c.String())
	}
}

func TestSuccessfulRequestDoesNotLogBodyAtInfo(t *testing.T) {
	c := &Capture{}
	l := New(c, "info") // info level: debug lines are suppressed
	LogRequest(l, "GET", "/api/2/people", 200, []byte(`{"secret":"dont-leak"}`), time.Millisecond)
	if c.Contains("dont-leak") {
		t.Fatalf("successful (debug-level) request should not be rendered at info level:\n%s", c.String())
	}
	// But at debug level it is rendered (and is not an error line).
	c2 := &Capture{}
	l2 := New(c2, "debug")
	LogRequest(l2, "GET", "/api/2/people", 200, []byte(`ok`), time.Millisecond)
	if !c2.Contains("api request ok") {
		t.Fatalf("expected a debug 'api request ok' line:\n%s", c2.String())
	}
}

func TestLogOutcomeRendersStableFields(t *testing.T) {
	c := &Capture{}
	l := New(c, "debug")
	LogOutcome(l, 555, OutcomeApplied, nil, "groups_changed", "2")
	out := c.String()
	if !contains(out, "member_id=555") || !contains(out, "outcome=applied") {
		t.Fatalf("LogOutcome must carry stable member_id/outcome fields.\ngot:\n%s", out)
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
