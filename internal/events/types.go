// Package events implements the JetStream webhook ingress (spec:
// event-ingestion). Nexudus webhook events arrive over NATS JetStream (replayed
// from a durable log when we're down, at-least-once). This package splits the
// testable logic — auth verification, coworker-id extraction, dispatch, and
// graceful handling of malformed / persistently-failing messages — from the
// concrete JetStream transport (see jetstream.go), so the logic is unit-testable
// without a live NATS.
package events

import (
	"context"
)

// Message is one inbound webhook message from the JetStream proxy.
type Message struct {
	Subject string
	Headers map[string]string
	Data    []byte
}

// Consumer is the (injected) JetStream transport: pull a message, then resolve it
// (ack / reject) once handled. The concrete implementation is in jetstream.go.
type Consumer interface {
	// Next blocks for the next message. A nil (Message, nil) or a non-nil error
	// means stop / retry respectively.
	Next(ctx context.Context) (Message, error)
	// Ack finalizes a successfully-handled message (no redelivery).
	Ack(msg Message, sequence uint64) error
	// Term removes a message from redelivery and routes it to a dead-letter path.
	Term(msg Message, sequence uint64) error
}

// Sequence is the delivery attempt counter (from JetStream metadata). 1 = first.
// It is threaded through the loop so we can bound redeliveries.

// Dispatcher processes one affected coworker (wired to the engine).
type Dispatcher interface {
	Dispatch(ctx context.Context, coworkerID int) error
}

// AuthVerifier decides whether an inbound event is trusted. When no verifier is
// configured the service is fail-closed (reject), per the trust model (spec:
// event-ingestion / auth, task 4.5).
type AuthVerifier interface {
	Verify(headers map[string]string) bool
}

// Result describes how one message was handled so the loop can ack / dead-letter.
type Result struct {
	Kind       Outcome
	CoworkerID int
	Err        error
}

// Outcome is the classification of a processed message.
type Outcome int

const (
	// OutcomeDispatched: an affected coworker was processed (ACK).
	OutcomeDispatched Outcome = iota
	// OutcomeNoCoworker: trusted, but no coworker affected (ACK as no-op).
	OutcomeNoCoworker
	// OutcomeRejected: failed auth (ACK + log, never dispatch, never write).
	OutcomeRejected
	// OutcomeFailed: dispatch failed; bounded redelivery then dead-letter.
	OutcomeFailed
)
