package events_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	events "github.com/LowellMakes/nexudus-doorflow-sync/internal/events"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fakeDispatcher struct{ calls []int }

func (f *fakeDispatcher) Dispatch(ctx context.Context, coworkerID int) error {
	f.calls = append(f.calls, coworkerID)
	return nil
}

type fakeConsumer struct {
	msgs  []events.Message
	i     int
	acked int
}

func (c *fakeConsumer) Next(ctx context.Context) (events.Message, error) {
	if c.i >= len(c.msgs) {
		return events.Message{}, nil // done
	}
	m := c.msgs[c.i]
	c.i++
	return m, nil
}
func (c *fakeConsumer) Ack(events.Message, uint64) error  { c.acked++; return nil }
func (c *fakeConsumer) Term(events.Message, uint64) error { c.acked++; return nil }

func hdr(sig, path string) map[string]string {
	return map[string]string{"X-Nexudus-Signature": sig, "X-HTTP-Path": path}
}

// 4.2: a member event dispatches the affected coworker; a non-member event is a
// no-op (no dispatch, no error).
func TestMemberEventDispatchesNonMemberNoop(t *testing.T) {
	fd := &fakeDispatcher{}
	p := events.New(fd, &events.StaticSecret{Secret: "s3cr3t"}, discardLog())
	ctx := t.Context()

	r := p.Process(ctx, "http.host.post.v1.coworkers.42", hdr("s3cr3t", "/v1/coworkers/42"), nil)
	if r.Kind != events.OutcomeDispatched || r.CoworkerID != 42 {
		t.Fatalf("member event not dispatched: %+v", r)
	}
	if len(fd.calls) != 1 || fd.calls[0] != 42 {
		t.Errorf("dispatcher calls = %v; want [42]", fd.calls)
	}

	r2 := p.Process(ctx, "http.host.post.v1.rooms.opened", hdr("s3cr3t", "/v1/rooms/opened"), nil)
	if r2.Kind != events.OutcomeNoCoworker {
		t.Fatalf("non-member event should be no-op, got %+v", r2)
	}
	if len(fd.calls) != 1 {
		t.Errorf("non-member event must not dispatch: %v", fd.calls)
	}
}

// 4.5: unsigned / tampered events are rejected (no dispatch, no door write);
// with no verifier configured the service fails closed.
func TestAuthRejectsUnsignedTamperedAndFailClosed(t *testing.T) {
	ctx := t.Context()
	// Tampered secret
	fd := &fakeDispatcher{}
	p := events.New(fd, &events.StaticSecret{Secret: "s3cr3t"}, discardLog())
	if r := p.Process(ctx, "s", hdr("wrong", "/v1/coworkers/5"), nil); r.Kind != events.OutcomeRejected {
		t.Errorf("tampered event should be rejected, got %+v", r)
	}
	// Unsigned
	fd2 := &fakeDispatcher{}
	p2 := events.New(fd2, &events.StaticSecret{Secret: "s3cr3t"}, discardLog())
	if r := p2.Process(ctx, "s", map[string]string{"X-HTTP-Path": "/v1/coworkers/5"}, nil); r.Kind != events.OutcomeRejected {
		t.Errorf("unsigned event should be rejected, got %+v", r)
	}
	if len(fd.calls) != 0 || len(fd2.calls) != 0 {
		t.Errorf("rejected events must not dispatch: %v / %v", fd.calls, fd2.calls)
	}
	// Fail-closed: no verifier configured.
	fd3 := &fakeDispatcher{}
	p3 := events.New(fd3, nil, discardLog())
	if r := p3.Process(ctx, "s", hdr("s3cr3t", "/v1/coworkers/5"), nil); r.Kind != events.OutcomeRejected {
		t.Errorf("no verifier => fail-closed, got %+v", r)
	}
	if len(fd3.calls) != 0 {
		t.Errorf("fail-closed must not dispatch: %v", fd3.calls)
	}
}

// 4.3: a malformed/rejected event then a good event: the consumer advances, the
// good event dispatches, the consumer never blocks, and both are resolved (acked).
func TestBadThenGoodAdvances(t *testing.T) {
	fd := &fakeDispatcher{}
	p := events.New(fd, &events.StaticSecret{Secret: "s3cr3t"}, discardLog())
	fc := &fakeConsumer{msgs: []events.Message{
		{Subject: "s", Headers: hdr("wrongsecret", "/v1/coworkers/9"), Data: []byte(`{`)}, // rejected + malformed body
		{Subject: "s", Headers: hdr("s3cr3t", "/v1/coworkers/9"), Data: []byte(`{}`)},     // dispatched
	}}
	if err := p.Run(t.Context(), fc); err != nil {
		t.Fatalf("Run errored: %v", err)
	}
	if len(fd.calls) != 1 || fd.calls[0] != 9 {
		t.Errorf("only the good event should dispatch: %v", fd.calls)
	}
	if fc.acked != 2 {
		t.Errorf("both messages must be resolved (acked), got %d", fc.acked)
	}
}
