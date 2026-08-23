package engine

import (
	"context"
	"errors"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

// syncCredentials reconciles the desired credential set (from Nexudus) against
// what Doorflow reports. It adds/changes values and, on a PIN in-use conflict,
// routes to the Remediator. It never deletes (conservative, spec: credential
// removal is log-only). Returns whether a credential write happened, whether a
// PIN was remediated, and any non-conflict error.
func (e *Engine) syncCredentials(ctx context.Context, cw *domain.Coworker, person *domain.Person) (bool, bool, error) {
	cur, err := e.D.ListCredentials(ctx, person.ID)
	if err != nil {
		return false, false, err
	}
	byKind := map[domain.CredentialKind]*domain.Credential{}
	for _, c := range cur {
		byKind[c.Kind] = c
	}

	// Desired credential values from the authoritative Nexudus fields.
	type kv struct {
		kind  domain.CredentialKind
		value string
	}
	want := []kv{}
	if cw.AccessPin != "" {
		want = append(want, kv{domain.KindPin, cw.AccessPin})
	}
	if cw.KeyFob != "" {
		want = append(want, kv{domain.KindKeyFob, cw.KeyFob})
	}
	if cw.AccessCard != "" {
		want = append(want, kv{domain.KindCard, cw.AccessCard})
	}

	mutated, remediated := false, false
	var firstErr error
	for _, w := range want {
		m, rem, werr := e.setCredential(ctx, cw.ID, person.ID, w.kind, w.value, byKind[w.kind], cw.Email)
		if m {
			mutated = true
		}
		if rem {
			remediated = true
		}
		if werr != nil && firstErr == nil {
			firstErr = werr
		}
	}
	return mutated, remediated, firstErr
}

// setCredential reconciles one credential kind to the desired value.
func (e *Engine) setCredential(ctx context.Context, memberID, personID int, kind domain.CredentialKind, value string, cur *domain.Credential, email string) (bool, bool, error) {
	// No-op gate: already at the desired value → zero writes (spec: idempotency).
	if cur != nil && cur.Value == value {
		return false, false, nil
	}

	// Dry-run (write flag off): report the intended change but issue NO write
	// (spec: 7.2 flag-off → zero mutations).
	if e.DryRun {
		return true, false, nil
	}

	var opErr error
	if cur == nil {
		_, opErr = e.D.CreateCredential(ctx, personID, kind, value)
		if errors.Is(opErr, domain.ErrAlreadyExists) {
			return false, false, nil // benign: already present (idempotency)
		}
	} else {
		_, opErr = e.D.UpdateCredential(ctx, cur.ID, value)
	}
	if opErr == nil {
		return true, false, nil
	}

	// PIN in-use conflict: distinguish and auto-remediate rather than drop.
	if kind == domain.KindPin && errors.Is(opErr, domain.ErrInUse) {
		if _, _, rerr := e.Remediate.ResolvePin(ctx, memberID, personID, email); rerr != nil {
			return false, true, rerr
		}
		return true, true, nil // PIN is now set (guarded if write-back still pending)
	}
	return false, false, opErr
}
