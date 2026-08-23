package remediation

import (
	"context"
	"errors"
	"log/slog"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/store"
)

// Local outcome errors for remediation.
var (
	// ErrNoPin is returned when Doorflow's auto-generation returned no value.
	ErrNoPin = errors.New("doorflow did not return a generated pin value")
	// ErrWriteBackMismatch is returned when a Nexudus read-back disagrees with the value we just wrote.
	ErrWriteBackMismatch = errors.New("nexudus pin write-back not confirmed")
)

// Remediator resolves one member's PIN conflict (spec: pin-remediation).
type Remediator struct {
	N        domain.Nexudus
	D        domain.Doorflow
	S        store.Store
	Notifier domain.Notifier
	Logger   *slog.Logger
}

// New builds a Remediator.
func New(n domain.Nexudus, d domain.Doorflow, s store.Store, notif domain.Notifier, log *slog.Logger) *Remediator {
	if log == nil {
		log = slog.Default()
	}
	return &Remediator{N: n, D: d, S: s, Notifier: notif, Logger: log}
}

// ResolvePin gives the member a working, unique PIN in Doorflow and durably
// writes it back to Nexudus. On the first pass it generates a unique value; on
// later passes it honors the persisted one and retries the write-back rather
// than regenerating (spec: "Guard against regeneration churn"). Returns the
// resulting Doorflow PIN and whether the member has converged.
func (r *Remediator) ResolvePin(ctx context.Context, memberID, personID int, email string) (pin string, converged bool, err error) {
	g, gerr := r.pending(memberID)
	if gerr != nil {
		return "", false, gerr
	}

	if g != nil {
		return r.confirmPending(ctx, memberID, g.DoorflowPin, email)
	}
	return r.generate(ctx, memberID, personID, email)
}

// pending reads the durable guard, if any.
func (r *Remediator) pending(memberID int) (*store.Guard, error) {
	if r.S == nil {
		return nil, nil
	}
	return r.S.Pending(context.Background(), memberID)
}

// confirmPending retries the Nexudus write-back of the already-set PIN and, once
// confirmed, clears the guard and notifies. It never regenerates.
func (r *Remediator) confirmPending(ctx context.Context, memberID int, pin, email string) (string, bool, error) {
	log := r.Logger
	if err := backWrite(ctx, r.N, memberID, pin, email); err != nil {
		log.Log(ctx, slog.LevelWarn, "pin write-back still pending (guarded)", "member", memberID)
		return pin, false, err
	}
	if r.S != nil {
		_ = r.S.Clear(context.Background(), memberID)
	}
	notify(r, ctx, memberID, email)
	log.Info("pin converged after pending write-back", "member", memberID, "pin_set", true)
	return pin, true, nil
}

// generate auto-generates a unique PIN in Doorflow and durably writes it back to
// Nexudus. If the write-back is not confirmed yet, the guard holds it for retry.
func (r *Remediator) generate(ctx context.Context, memberID, personID int, email string) (string, bool, error) {
	log := r.Logger
	gen, err := autogenPin(ctx, r.D, personID, log)
	if err != nil {
		return "", false, err
	}
	if r.S != nil {
		if serr := r.S.SetPending(ctx, store.Guard{MemberID: memberID, DoorflowPin: gen, Attempts: 1}); serr != nil {
			return "", false, serr
		}
	}
	if err := backWrite(ctx, r.N, memberID, gen, email); err != nil {
		log.Warn("pin set; Nexudus write-back pending (guarded)", "member", memberID)
		return gen, false, err
	}
	if r.S != nil {
		_ = r.S.Clear(context.Background(), memberID)
	}
	notify(r, ctx, memberID, email)
	log.Info("pin auto-remediated", "member", memberID, "pin_set", true)
	return gen, true, nil
}

// autogenPin sets a unique PIN on the person using Doorflow's value "*" feature.
func autogenPin(ctx context.Context, d domain.Doorflow, personID int, log *slog.Logger) (string, error) {
	creds, err := d.ListCredentials(ctx, personID)
	if err != nil {
		return "", err
	}
	if existing, ok := pinCredential(creds); ok {
		if upd, uerr := d.UpdateCredential(ctx, existing.ID, "*"); uerr == nil && upd.Value != "" {
			return upd.Value, nil
		}
	}
	created, cerr := d.CreateCredential(ctx, personID, domain.KindPin, "*")
	if cerr != nil {
		if iu, ok := cerr.(*domain.InUseError); ok {
			log.Error("autogen-pin failed (still in use)", "person", personID, "detail", iu.Detail)
		}
		return "", cerr
	}
	if created.Value == "" {
		return "", ErrNoPin
	}
	return created.Value, nil
}

// pinCredential finds the PIN credential in a list.
func pinCredential(creds []*domain.Credential) (*domain.Credential, bool) {
	for _, c := range creds {
		if c.Kind == domain.KindPin {
			return c, true
		}
	}
	return nil, false
}

// backWrite sets the member's PIN on Nexudus and confirms via read-back.
func backWrite(ctx context.Context, n domain.Nexudus, memberID int, pin, email string) error {
	if err := n.UpdateCoworkerPin(ctx, memberID, pin); err != nil {
		return err
	}
	cw, err := n.GetCoworker(ctx, memberID)
	if err != nil {
		return err
	}
	if cw.AccessPin != pin {
		return ErrWriteBackMismatch
	}
	_ = email
	return nil
}

// notify sends the member a "your PIN changed" notice (spec: notification).
func notify(r *Remediator, ctx context.Context, memberID int, email string) {
	if r.Notifier == nil {
		return
	}
	if err := r.Notifier.NotifyPinChanged(ctx, memberID, email); err != nil {
		r.Logger.Warn("pin-changed notice failed", "member", memberID, "email", email, "err", err)
	}
}
