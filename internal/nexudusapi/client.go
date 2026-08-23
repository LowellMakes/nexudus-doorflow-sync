// Package nexudusapi is a small, typed REST client over the documented Nexudus
// endpoints used by door-access sync. It is the concrete domain.Nexudus
// adapter. It deliberately covers only the reads for the sync path plus the two
// documented write paths (PIN write-back and PIN reminder), and logs every
// non-2xx response with its body (fix for the legacy "no logged reason").
package nexudusapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Client talks to Nexudus.
type Client struct {
	baseURL string
	user    string
	pass    string
	hc      *http.Client
	log     *slog.Logger
}

// New builds a Client from explicit connection parameters.
func New(baseURL, user, pass string, log *slog.Logger, timeout time.Duration) *Client {
	if log == nil {
		log = slog.Default()
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		user:    user,
		pass:    pass,
		hc:      &http.Client{Timeout: timeout},
		log:     log,
	}
}

// apiError is returned for non-2xx responses; it carries the status and body so
// callers can log / classify precisely.
type apiError struct {
	status int
	body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("nexudus HTTP %d: %s", e.status, e.body)
}

// Status returns the HTTP status of the failure.
func (e *apiError) Status() int { return e.status }

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
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	start := time.Now()
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		logRequest(c.log, method, path, resp.StatusCode, raw, time.Since(start))
		if out != nil && len(raw) > 0 {
			if rerr := json.Unmarshal(raw, out); rerr != nil {
				return fmt.Errorf("nexudus: decode %s: %w", path, rerr)
			}
		}
		return nil
	}
	logRequest(c.log, method, path, resp.StatusCode, raw, time.Since(start))
	return &apiError{status: resp.StatusCode, body: strings.TrimSpace(string(raw))}
}

// logRequest mirrors logging.LogRequest but lives here to avoid an import cycle
// with packages that import both. It keeps the same stable field names.
func logRequest(l *slog.Logger, method, path string, status int, body []byte, dur time.Duration) {
	msg := "nexudus request"
	lvl := slog.LevelDebug
	if status >= 400 {
		msg = "nexudus request failed"
		lvl = slog.LevelError
	}
	s := string(body)
	if len(s) > 2048 {
		s = s[:2048] + "…(truncated)"
	}
	l.Log(context.Background(), lvl, msg,
		"method", method, "path", path, "status", status,
		"duration", dur.String(), "body", strings.TrimSpace(s))
}
