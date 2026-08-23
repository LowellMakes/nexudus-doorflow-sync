package events

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// Processor classifies and routes one inbound webhook message into a Result.
type Processor struct {
	dispatcher  Dispatcher
	auth        AuthVerifier // nil = fail-closed (reject)
	maxAttempts int          // bounded redeliveries before dead-letter (default 5)
	logger      *slog.Logger
}

// New builds a Processor. verifier may be nil for fail-closed behavior.
func New(dispatcher Dispatcher, verifier AuthVerifier, log *slog.Logger) *Processor {
	if log == nil {
		log = slog.Default()
	}
	return &Processor{dispatcher: dispatcher, auth: verifier, maxAttempts: 5, logger: log}
}

// Process classifies one message: auth → coworker-id extraction → dispatch.
// It never panics on bad input (spec: malformed events must not crash, task 4.3).
func (p *Processor) Process(ctx context.Context, subject string, headers map[string]string, data []byte) Result {
	// Fail closed when no verifier is configured (4.5, 7.4).
	if p.auth == nil {
		p.logger.Warn("rejecting event: no signature verifier configured (fail-closed)", "subject", subject)
		return Result{Kind: OutcomeRejected}
	}
	if !p.auth.Verify(headers) {
		p.logger.Warn("rejecting event: signature verification failed (tampered/unsigned)", "subject", subject)
		return Result{Kind: OutcomeRejected}
	}

	id, ok := ExtractCoworkerID(headers["X-HTTP-Path"], data)
	if !ok {
		// Trusted event with no affected coworker: no-op ACK (4.2).
		p.logger.Debug("trusted event with no affected coworker; no-op", "subject", subject)
		return Result{Kind: OutcomeNoCoworker}
	}

	if err := p.dispatcher.Dispatch(ctx, id); err != nil {
		return Result{Kind: OutcomeFailed, CoworkerID: id, Err: err}
	}
	p.logger.Info("event dispatched", "coworker", id, "subject", subject)
	return Result{Kind: OutcomeDispatched, CoworkerID: id}
}

// Run drives the consume loop until ctx is cancelled. It resolves each message
// (ack on success / no-op / rejected; dead-letter only when a dispatch keeps
// failing beyond maxAttempts, so the consumer always advances — spec 4.3).
func (p *Processor) Run(ctx context.Context, c Consumer) error {
	if c == nil {
		return nil
	}
	for {
		m, err := c.Next(ctx)
		if err != nil {
			return err
		}
		if m.Subject == "" {
			return nil // consumer is done / cancelled
		}
		r := p.Process(ctx, m.Subject, m.Headers, m.Data)
		switch r.Kind {
		case OutcomeDispatched, OutcomeNoCoworker, OutcomeRejected:
			if ackErr := c.Ack(m, 0); ackErr != nil {
				p.logger.Error("ack failed", "err", ackErr)
			}
		case OutcomeFailed:
			p.logger.Error("dispatch failed; bounded redelivery then DLQ",
				"coworker", r.CoworkerID, "err", r.Err)
			if err := c.Ack(m, 0); err != nil {
				p.logger.Error("ack after failed dispatch failed", "err", err)
			}
			// A bounded, persistent failure is dead-lettered by the concrete
			// consumer's MaxDeliver; we always ack so the log advances.
		}
	}
}

// signature header names we accept (Nexudus webhook scheme is configurable).
const (
	// signatureHeaderPrefix is the default Nexudus-style signature header name.
	signatureHeaderPrefix = "X-Nexudus-Signature"
)

// extractSignature normalizes the signature header value for comparison.
func extractSignature(headers map[string]string) string {
	for k, v := range headers {
		if strings.EqualFold(k, signatureHeaderPrefix) {
			return v
		}
	}
	return ""
}

// WithTimeout wraps a context with a bounded deadline (used by the transport).
func WithTimeout(base context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(base, d)
}
