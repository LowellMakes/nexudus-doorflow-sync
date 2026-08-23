package events

import (
	"encoding/json"
	"regexp"
	"strconv"
)

// coworkerPattern matches a coworker id in an HTTP path, e.g.
// /v1/coworkers/42 -> 42, /api/spaces/coworkers/7 -> 7.
var coworkerPattern = regexp.MustCompile(`(?i)/coworkers?/(\d+)`)

// ExtractCoworkerID determines the affected coworker id from the HTTP path
// (authoritative for member-scoped events) or, failing that, from the JSON body
// (fields: coworker_id / coworker-id / actor.coworker_id / id). It returns ok=false
// when no coworker can be determined (the event is then no-op ack'd, task 4.2).
func ExtractCoworkerID(httpPath string, body []byte) (int, bool) {
	if httpPath != "" {
		if m := coworkerPattern.FindStringSubmatch(httpPath); m != nil {
			if id, err := strconv.Atoi(m[1]); err == nil {
				return id, true
			}
		}
	}

	if len(body) > 0 {
		var envelope map[string]any
		if err := json.Unmarshal(body, &envelope); err == nil {
			// Try well-known keys directly.
			for _, key := range []string{"coworker_id", "coworker-id", "coworkerid", "id"} {
				if v, ok := intValue(envelope[key]); ok {
					return v, true
				}
			}
			// Try a nested actor object.
			if actor, ok := envelope["actor"].(map[string]any); ok {
				for _, key := range []string{"coworker_id", "coworker-id", "id"} {
					if v, ok := intValue(actor[key]); ok {
						return v, true
					}
				}
			}
		}
	}
	return 0, false
}

// intValue coerces a JSON value into a non-zero positive int.
func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n >= 1 {
			return int(n), true
		}
	case int:
		if n >= 1 {
			return n, true
		}
	case string:
		if id, err := strconv.Atoi(n); err == nil && id >= 1 {
			return id, true
		}
	}
	return 0, false
}
