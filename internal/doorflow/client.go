// Package doorflow is a small, typed REST client over the documented Doorflow
// person (v2) and credential (v3) endpoints, plus the reader commit. It is the
// concrete domain.Doorflow adapter. It logs the response status and body on any
// non-2xx so a rejected person/credential write is never silent.
package doorflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

// Client talks to Doorflow.
type Client struct {
	baseURL  string // api.doorflow.com (person + credentials + sync)
	authType string // "basic" | "bearer"
	apiKey   string
	apiKey2  string
	hc       *http.Client
	log      *slog.Logger

	mu         sync.RWMutex
	kindToID   map[domain.CredentialKind]int
	idToKind   map[int]domain.CredentialKind
	typesReady bool
}

// New builds a Doorflow client.
func New(baseURL, authType, apiKey, apiKey2 string, log *slog.Logger, timeout time.Duration) *Client {
	if log == nil {
		log = slog.Default()
	}
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		authType: strings.ToLower(authType),
		apiKey:   apiKey,
		apiKey2:  apiKey2,
		hc:       &http.Client{Timeout: timeout},
		log:      log,
		kindToID: map[domain.CredentialKind]int{},
		idToKind: map[int]domain.CredentialKind{},
	}
}

// apiError carries provider status + body; InUse reports a value-in-use 409.
type apiError struct {
	status int
	body   string
	inUse  bool
}

func (e *apiError) Error() string { return fmt.Sprintf("doorflow HTTP %d: %s", e.status, e.body) }
func (e *apiError) Status() int   { return e.status }
func (e *apiError) IsInUse() bool { return e.inUse }

// isValueInUse heuristically classifies a conflict body as "value already in
// use" (vs a plain "type already exists"). Driven by the provider's message.
func isValueInUse(body string) bool {
	b := strings.ToLower(body)
	return strings.Contains(b, "in_use") ||
		strings.Contains(b, "in use") ||
		strings.Contains(b, "already used") ||
		strings.Contains(b, "conflict")
}

func (c *Client) do(ctx context.Context, method, path string, encodable any, out any) error {
	var body io.Reader
	if encodable != nil {
		b, err := json.Marshal(encodable)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if encodable != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	switch c.authType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	case "basic", "":
		req.SetBasicAuth(c.apiKey, c.apiKey2)
	}
	start := time.Now()
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("doorflow: decode %s %s: %w", method, path, err)
			}
		}
		c.debugLog(method, path, resp.StatusCode, raw, time.Since(start))
		return nil
	}
	bodyStr := strings.TrimSpace(string(raw))
	c.errorLog(method, path, resp.StatusCode, raw, time.Since(start))
	return &apiError{status: resp.StatusCode, body: bodyStr, inUse: resp.StatusCode == http.StatusConflict && isValueInUse(bodyStr)}
}

func (c *Client) get(ctx context.Context, path string, out any, q url.Values) error {
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return c.do(ctx, "GET", path, nil, out)
}

func (c *Client) debugLog(method, path string, status int, body []byte, dur time.Duration) {
	s := string(body)
	if len(s) > 1024 {
		s = s[:1024] + "…(truncated)"
	}
	c.log.Log(context.Background(), slog.LevelDebug, "doorflow request ok",
		"method", method, "path", path, "status", status, "duration", dur.String(), "body", strings.TrimSpace(s))
}

func (c *Client) errorLog(method, path string, status int, body []byte, dur time.Duration) {
	s := string(body)
	if len(s) > 2048 {
		s = s[:2048] + "…(truncated)"
	}
	c.log.Log(context.Background(), slog.LevelError, "doorflow request failed",
		"method", method, "path", path, "status", status, "duration", dur.String(), "body", strings.TrimSpace(s))
}

var _ domain.Doorflow = (*Client)(nil)
