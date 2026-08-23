// Package engine is the single sync spine that both the event path and the
// reconcile path share. It reads authoritative state from Nexudus, diffs it
// against Doorflow, and applies the delta (person + credentials + one commit per
// batch). On a PIN in-use conflict it delegates to the Remediator (spec:
// doorflow-sync). Writes are gated by the DryRun flag (dry-run: compute only,
// no writes — doctor/read mode).
package engine

import (
	"context"
	"log/slog"
	"sync"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/mapping"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/remediation"
)

// Report records the outcome for one member in a sync pass.
type Report struct {
	MemberID   int
	Mutated    bool // any Doorflow write was performed
	Remediated bool // a PIN conflict was auto-remediated
	Err        error
}

// Engine is the single sync spine.
type Engine struct {
	N         domain.Nexudus
	D         domain.Doorflow
	Map       *mapping.Mapping
	Remediate *remediation.Remediator
	Logger    *slog.Logger
	DryRun    bool // when true, compute desired state but skip all writes

	locks map[int]*sync.Mutex
	mu    sync.Mutex
}

// New builds an Engine.
func New(n domain.Nexudus, d domain.Doorflow, m *mapping.Mapping, rem *remediation.Remediator, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.Default()
	}
	return &Engine{N: n, D: d, Map: m, Remediate: rem, Logger: log, locks: map[int]*sync.Mutex{}}
}

// lock gets (or creates) the per-member mutex, preventing concurrent runs on the
// same member (event + reconcile, spec: doorflow-sync/concurrency).
func (e *Engine) lock(id int) func() {
	e.mu.Lock()
	l, ok := e.locks[id]
	if !ok {
		l = &sync.Mutex{}
		e.locks[id] = l
	}
	e.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// SyncBatch processes a set of coworker IDs in one pass. A single Doorflow
// commit (POST /sync) is issued at the end iff at least one member was mutated.
// If none mutated, no sync is issued (spec: idempotent/no-op gate, task 3.5).
func (e *Engine) SyncBatch(ctx context.Context, ids []int) []Report {
	results := make([]Report, 0, len(ids))
	mutated := false
	for _, id := range ids {
		unlock := e.lock(id)
		r := e.syncOne(ctx, id)
		unlock()
		results = append(results, r)
		if r.Mutated {
			mutated = true
		}
	}
	// Single commit at the end iff any member mutated (and not in dry-run).
	if mutated && !e.DryRun {
		if err := e.D.Sync(ctx); err != nil {
			e.Logger.Error("batch sync commit failed", "err", err)
		}
	}
	return results
}

// SyncOne processes a single coworker (event-path entry). It issues one commit
// after the member's changes (spec: after any person/credential change → commit).
func (e *Engine) SyncOne(ctx context.Context, coworkerID int) Report {
	unlock := e.lock(coworkerID)
	defer unlock()
	r := e.syncOne(ctx, coworkerID)
	if r.Mutated && !e.DryRun {
		if err := e.D.Sync(ctx); err != nil {
			e.Logger.Error("single sync commit failed", "member", coworkerID, "err", err)
		}
	}
	return r
}

// ProcessMember converges one coworker WITHOUT committing. It is the shared spine
// used by the reconcile sweep (which issues a single commit for the whole set,
// spec: reconcile 5.5 / doorflow-sync 5.4). Returns whether a mutation occurred.
func (e *Engine) ProcessMember(ctx context.Context, coworkerID int) bool {
	unlock := e.lock(coworkerID)
	defer unlock()
	r := e.syncOne(ctx, coworkerID)
	if r.Err != nil {
		e.Logger.Error("reconcile member failed", "member", coworkerID, "err", r.Err)
	}
	return r.Mutated
}

// Commit issues the single reader commit for a set of mutations.
func (e *Engine) Commit(ctx context.Context) error { return e.D.Sync(ctx) }
