// Package reconcile implements the scheduled/start-up reconciliation sweeps
// (spec: reconciliation). Both the incremental (updated-since / paid-since) and
// the full-membership sweep feed their target coworker set through the SAME
// engine spine (ProcessMember) — there is no second Nexudus→Doorflow translation
// (spec: reconcile 5.4). The sweep is batched and rate-limited (bounded
// concurrency) and issues one reader commit at the end when ≥1 member mutated
// (spec: reconcile 5.5).
package reconcile

import (
	"context"
	"log/slog"
	"sync"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

// Processer converges one coworker (engine.ProcessMember) and reports mutation.
type Processer func(ctx context.Context, coworkerID int) (mutated bool, err error)

// Committer issues the single reader commit for a reconcile set.
type Committer func(ctx context.Context) error

// Reconciler drives the sweeps.
type Reconciler struct {
	// N provides the paging sweeps (updated-since / paid-since / all).
	N domain.Nexudus
	// Process converges one member without committing (engine.ProcessMember).
	Process Processer
	// Commit issues one reader commit for the whole set (engine.Commit).
	Commit Committer
	// PageSize is the Nexudus page size for sweeps (kept under provider limits).
	PageSize int
	// MaxConcurrent bounds how many members are processed concurrently, so the
	// sweep stays within Nexudus/Doorflow API rate limits (spec: reconcile 5.3).
	MaxConcurrent int
	Logger        *slog.Logger
}

// New builds a Reconciler over the provided spine.
func New(n domain.Nexudus, p Processer, c Committer, pageSize, maxConcurrent int, log *slog.Logger) *Reconciler {
	if pageSize <= 0 {
		pageSize = 100
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	if log == nil {
		log = slog.Default()
	}
	return &Reconciler{N: n, Process: p, Commit: c, PageSize: pageSize, MaxConcurrent: maxConcurrent, Logger: log}
}

// Sweep converges the given coworker set with bounded concurrency and issues at
// most one reader commit at the end (spec: reconcile 5.5). It returns the number
// of members processed and whether any mutated.
func (r *Reconciler) Sweep(ctx context.Context, ids []int) (processed int, mutated bool) {
	if len(ids) == 0 {
		return 0, false
	}
	sem := make(chan struct{}, r.MaxConcurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	anyMutated := false
	processedCount := 0
	for _, id := range ids {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			mut, err := r.Process(ctx, id)
			mu.Lock()
			processedCount++
			if mut {
				anyMutated = true
			}
			mu.Unlock()
			if err != nil {
				r.Logger.Error("sweep: member failed", "member", id, "err", err)
			}
		}(id)
	}
	wg.Wait()
	if anyMutated && r.Commit != nil {
		if err := r.Commit(ctx); err != nil {
			r.Logger.Error("sweep commit failed", "err", err)
		}
	}
	return processedCount, anyMutated
}
