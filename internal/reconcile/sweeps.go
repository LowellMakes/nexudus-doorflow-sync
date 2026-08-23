package reconcile

import (
	"context"
	"time"
)

// Incremental converges the members touched since the reference time: coworkers
// updated-since and coworker-invoices paid-since (the "dues cleared → re-enable"
// leg, spec: reconcile 5.1/5.2). It converges, not just logs (spec: 5.4).
func (r *Reconciler) Incremental(ctx context.Context, since time.Time) (processed int, mutated bool) {
	ids := r.idsUpdatedSince(ctx, since)
	r.Logger.Info("incremental reconcile", "since", since.Format(time.RFC3339), "targets", len(ids))
	return r.Sweep(ctx, ids)
}

// Full converges the entire membership by paging all cows (no `size=50` cap) and
// driving each through the shared spine (spec: reconcile 5.3). Batched +
// rate-limited within the API limits by the bounded sweep concurrency.
func (r *Reconciler) Full(ctx context.Context) (processed int, mutated bool) {
	ids := r.allIDs(ctx)
	r.Logger.Info("full reconcile", "targets", len(ids))
	return r.Sweep(ctx, ids)
}

// idsUpdatedSince collects coworker ids from both the updated-since and
// paid-since sweeps (the latter covers dues-clearing re-enables that may not bump
// the coworker's row timestamp), deduplicated.
func (r *Reconciler) idsUpdatedSince(ctx context.Context, since time.Time) []int {
	seen := map[int]bool{}
	ids := make([]int, 0)
	add := func(id int) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for page := 0; ; page++ {
		cows, more, err := r.N.UpdatedCoworkersSince(ctx, since, page, r.PageSize)
		if err == nil {
			for _, c := range cows {
				add(c.ID)
			}
		}
		if err != nil || !more {
			break
		}
	}
	for page := 0; ; page++ {
		paid, more, err := r.N.PaidMembersSince(ctx, since, page, r.PageSize)
		if err == nil {
			for _, id := range paid {
				add(id)
			}
		}
		if err != nil || !more {
			break
		}
	}
	return ids
}

// allIDs pages the entire coworker population (no legacy size cap).
func (r *Reconciler) allIDs(ctx context.Context) []int {
	ids := make([]int, 0)
	for page := 0; ; page++ {
		cows, more, err := r.N.AllCoworkers(ctx, page, r.PageSize)
		if err == nil {
			for _, c := range cows {
				ids = append(ids, c.ID)
			}
		}
		if err != nil || !more {
			break
		}
	}
	return ids
}
