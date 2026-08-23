package engine

import (
	"context"
	"log/slog"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

// syncOne computes the desired state for one coworker from Nexudus and applies
// the delta to Doorflow. It is the single code path shared by the event and
// reconcile flows (spec: doorflow-sync).
func (e *Engine) syncOne(ctx context.Context, id int) (r Report) {
	r.MemberID = id

	cw, err := e.N.GetCoworker(ctx, id)
	if err != nil {
		if err == domain.ErrNotFound {
			e.Logger.Debug("coworker not found; skipping", "member", id)
			return r
		}
		r.Err = err
		return r
	}

	// Contacts (no membership user id) never get access: ensure none.
	if isContact(cw) {
		r.Mutated = e.ensureNoAccess(ctx, id, cw.Email)
		return r
	}

	// Eligibility is contract + payment driven (spec: membership).
	contracts, overdue, err := e.N.ContractsAndInvoices(ctx, id)
	if err != nil {
		r.Err = err
		return r
	}
	eligible := contracts > 0 && !overdue

	// Ensure the person exists and is at the desired enabled state + exact-set groups.
	person, personMutated, pErr := e.ensurePerson(ctx, cw, eligible)
	if pErr != nil {
		r.Err = pErr
		r.Mutated = personMutated
		return r
	}
	r.Mutated = personMutated

	// Sync credentials only for eligible members (conservative: never delete).
	// A nil person occurs for non-eligible members and for the dry-run create
	// path (no person id is materialized when writes are gated).
	if eligible && person != nil {
		credMutated, remediated, cErr := e.syncCredentials(ctx, cw, person)
		if cErr != nil {
			r.Err = cErr
		}
		r.Remediated = remediated
		if credMutated {
			r.Mutated = true
		}
	}
	return r
}

// isContact reports whether the coworker has no membership user id.
func isContact(cw *domain.Coworker) bool { return cw == nil || cw.UserID == 0 }

// logInfo is a small helper to avoid nil-deref noise at call sites.
func (e *Engine) logInfo(ctx context.Context, msg string, kv ...any) {
	if e.Logger != nil {
		e.Logger.Log(ctx, slog.LevelInfo, msg, kv...)
	}
}
