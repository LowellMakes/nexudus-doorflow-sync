package engine

import (
	"context"
	"strconv"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

// desiredGroups resolves the exact set a member should belong to: the groups
// mapped from the teams the member is currently in, plus the always-included
// BasicMember fallback when configured (spec: exact-set groups, D3).
func (e *Engine) desiredGroups(cw *domain.Coworker) []int32 {
	// Resolve returns mapped teams ∪ BasicMember, deduped + sorted (spec: exact-set, D3).
	return e.Map.Resolve(cw.TeamIDs)
}

// ensurePerson ensures a person exists at the desired enabled state and exact-set
// group membership. It returns the person (for its id), whether a person write
// occurred, and any error. A non-eligible member with no existing person yields
// (nil, false, nil) — a disabled person is not created (disable-not-delete model).
func (e *Engine) ensurePerson(ctx context.Context, cw *domain.Coworker, eligible bool) (*domain.Person, bool, error) {
	email := cw.Email
	cur, err := e.D.FindPersonByEmail(ctx, email)
	if err != nil {
		if err != domain.ErrNotFound {
			return cur, false, err
		}
		if !eligible {
			// No access for a non-eligible member who has no person yet.
			return nil, false, nil
		}
		// Write flag off: report the intended create but issue no write (7.2).
		if e.DryRun {
			return nil, true, nil
		}
		p, cerr := e.D.CreatePerson(ctx, &domain.Person{
			Email:     email,
			FirstName: cw.FirstName,
			LastName:  cw.LastName,
			Enabled:   true,
			GroupIDs:  e.desiredGroups(cw),
			SystemID:  "NX" + strconv.Itoa(cw.ID),
		})
		return p, true, cerr
	}

	// Person exists: is it already at the desired state (no-op gate)?
	changed := false
	if eligible {
		if !cur.Enabled || !sameGroups(cur.GroupIDs, e.desiredGroups(cw)) {
			changed = true
		}
	} else if cur.Enabled {
		changed = true
	}
	if !changed || e.DryRun {
		return cur, changed, nil
	}
	up := *cur
	up.Enabled = eligible
	if eligible {
		up.GroupIDs = e.desiredGroups(cw)
	}
	if _, uerr := e.D.UpdatePerson(ctx, cur.ID, &up); uerr != nil {
		return cur, true, uerr
	}
	return cur, true, nil
}

// ensureNoAccess makes sure a contact has no enabled access: disables any
// existing person. It reports whether a write occurred.
func (e *Engine) ensureNoAccess(ctx context.Context, id int, email string) bool {
	p, err := e.D.FindPersonByEmail(ctx, email)
	if err != nil || !p.Enabled {
		return false
	}
	if !e.DryRun {
		if _, uerr := e.D.UpdatePerson(ctx, p.ID, disablePerson(p)); uerr != nil {
			e.logInfo(ctx, "contact disable failed", "member", id, "email", email, "err", uerr)
			return false
		}
	}
	return true
}

// disablePerson returns a copy with access disabled.
func disablePerson(p *domain.Person) *domain.Person { q := *p; q.Enabled = false; return &q }

// sameGroups reports unordered set equality of two group id lists.
func sameGroups(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	am := map[int32]int{}
	for _, x := range a {
		am[x]++
	}
	seen := map[int32]bool{}
	for _, y := range b {
		if am[y] == 0 || seen[y] {
			return false
		}
		seen[y] = true
	}
	return true
}
