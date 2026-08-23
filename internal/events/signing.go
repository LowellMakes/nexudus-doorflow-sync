package events

import (
	"crypto/subtle"
	"strings"
)

// Auth schemes (Nexudus webhook signature format) are an Open Question to resolve
// against the live API (task 7.4). We implement them behind AuthVerifier so the
// concrete scheme is a drop-in and the default posture is fail-closed (reject)
// until a non-empty verifier is configured (task 4.5).

// StaticSecret authenticates events by a shared-secret header (a common webhook
// auth scheme: the provider sends X-Nexudus-Signature: <secret>). It is
// constant-time and rejects when the secret is unset, the header is missing, or
// the value does not match.
type StaticSecret struct {
	Secret string
	// Header names accepted (case-insensitive). Defaults to the Nexudus header.
	HeaderNames []string
}

func (s StaticSecret) names() []string {
	if len(s.HeaderNames) == 0 {
		return []string{signatureHeaderPrefix}
	}
	return s.HeaderNames
}

func (s StaticSecret) Verify(headers map[string]string) bool {
	if s.Secret == "" || headers == nil {
		return false
	}
	for _, h := range s.names() {
		v, ok := fetchHeader(headers, h)
		if !ok || v == "" {
			continue
		}
		return subtle.ConstantTimeCompare([]byte(v), []byte(s.Secret)) == 1
	}
	return false
}

// RejectAll rejects every event. Used when no verifier is configured — fail-closed.
type RejectAll struct{}

func (RejectAll) Verify(map[string]string) bool { return false }

// fetchHeader does a case-insensitive lookup.
func fetchHeader(headers map[string]string, name string) (string, bool) {
	if v, ok := headers[name]; ok {
		return v, true
	}
	want := strings.ToLower(name)
	for k, v := range headers {
		if strings.ToLower(k) == want {
			return v, true
		}
	}
	return "", false
}
