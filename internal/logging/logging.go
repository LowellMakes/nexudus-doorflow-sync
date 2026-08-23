// Package logging provides structured logging used across the service. It
// wraps log/slog and provides a single place to render API request outcomes so
// that failures always carry the response status and body — the direct fix for
// the legacy "stopped at 4am, no logged reason" failure (§3.7).
package logging

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"time"
)

// Field key names used throughout the service so callers and log shippers can
// rely on a stable schema.
const (
	FieldMemberID   = "member_id"
	FieldRequestID  = "request_id"
	FieldOutcome    = "outcome"
	FieldStatus     = "status"
	FieldBody       = "body"
	FieldDuration   = "duration"
	FieldMethod     = "method"
	FieldPath       = "path"
	FieldRedelivery = "redelivery"
)

// Outcome is a small closed vocabulary for what a unit of work ended up doing.
type Outcome string

const (
	OutcomeNoop       Outcome = "noop"
	OutcomeApplied    Outcome = "applied"
	OutcomeFailed     Outcome = "failed"
	OutcomeDead       Outcome = "dead_lettered"
	OutcomeSkipped    Outcome = "skipped"
	OutcomeRemediated Outcome = "pin_remediated"
)

// ParseLevel converts a level string into a slog.Level.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info", "":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
}

// New builds a *slog.Logger at the given level writing to w.
func New(w io.Writer, level string) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: ParseLevel(level)}))
}

// WithMember returns a child logger with the stable member-id field attached.
func WithMember(l *slog.Logger, id int) *slog.Logger {
	return l.With(FieldMemberID, id)
}

// LogRequest renders an HTTP call outcome. Failed calls (status >= 400) log at
// error level and MUST include the response body, so an operator can see why a
// credential/person write was rejected. Successful calls log at debug level.
func LogRequest(l *slog.Logger, method, path string, status int, body []byte, dur time.Duration) {
	attrs := append([]any{
		FieldMethod, method, FieldPath, path, FieldStatus, status, FieldDuration, dur.String(),
	}, withTruncatedBody(body)...)
	if status >= 400 {
		l.Error("api request failed", attrs...)
		return
	}
	l.Debug("api request ok", attrs...)
}

// LogOutcome renders the terminal outcome of a unit of work for one member.
func LogOutcome(l *slog.Logger, memberID int, outcome Outcome, err error, attrs ...any) {
	a := append([]any{FieldMemberID, memberID, FieldOutcome, string(outcome)}, attrs...)
	if err != nil {
		a = append(a, "error", err.Error())
	}
	switch outcome {
	case OutcomeFailed, OutcomeDead:
		l.Error("sync outcome", a...)
	case OutcomeSkipped:
		l.Warn("sync outcome", a...)
	case OutcomeRemediated:
		l.Warn("sync outcome (PIN changed — member must be notified)", a...)
	default:
		l.Info("sync outcome", a...)
	}
}

// ctxKeys are the string keys used to carry request/member context on a context.
type ctxKey string

const (
	CtxRequestID ctxKey = "request_id"
	CtxMemberID  ctxKey = "member_id"
)

// WithRequestID attaches a request id to ctx.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, CtxRequestID, id)
}

// RequestIDFrom returns the request id carried on ctx, if any.
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(CtxRequestID).(string); ok {
		return v
	}
	return ""
}

func withTruncatedBody(body []byte) []any {
	s := strings.TrimSpace(string(body))
	if len(s) > 2048 {
		s = s[:2048] + "…(truncated)"
	}
	return []any{FieldBody, s}
}

// Capture is an in-memory io.Writer used by tests to assert on log output.
type Capture struct{ buf bytes.Buffer }

func (c *Capture) Write(p []byte) (int, error) { return c.buf.Write(p) }
func (c *Capture) String() string              { return c.buf.String() }

// Contains reports whether the captured text contains the given substring.
func (c *Capture) Contains(s string) bool { return strings.Contains(c.String(), s) }
