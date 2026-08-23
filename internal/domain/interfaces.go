package domain

import (
	"context"
	"time"
)

// Nexudus is the source-of-truth API the sync path reads from (and, for the
// documented PIN write-back exception, writes to). It is a projection onto the
// fields Doorflow sync consumes, so concrete adapters can back it with either
// the generated REST client or a fake.
type Nexudus interface {
	// GetCoworker returns one authoritative coworker by id.
	GetCoworker(ctx context.Context, id int) (*Coworker, error)

	// ContractsAndInvoices returns the enablement inputs for one coworker:
	// the number of active contracts and whether any invoice is overdue.
	ContractsAndInvoices(ctx context.Context, coworkerID int) (contracts int, hasOverdue bool, err error)

	// UpdatedCoworkersSince returns a page of coworkers updated at/after since
	// and whether more pages remain (cursor-style: pass the returned page+1).
	UpdatedCoworkersSince(ctx context.Context, since time.Time, page, size int) ([]*Coworker, bool, error)

	// AllCoworkers returns a page of all coworkers (full reconcile) and whether
	// more pages remain.
	AllCoworkers(ctx context.Context, page, size int) ([]*Coworker, bool, error)

	// PaidMembersSince returns coworker ids that had an invoice payment land at/
	// after since (dues cleared), for the "re-enable" reconcile leg.
	PaidMembersSince(ctx context.Context, since time.Time, page, size int) ([]int, bool, error)

	// UpdateCoworkerPin is the documented write-back exception used by PIN
	// remediation: it sets the member's Active Pincode to pin via a
	// read-modify-write, leaving all other coworker fields unchanged.
	UpdateCoworkerPin(ctx context.Context, coworkerID int, pin string) error

	// SendPinReminder notifies the member that their PIN is available to view.
	SendPinReminder(ctx context.Context, coworkerID int) error
}

// Doorflow is the target API the engine writes to. Implementations must surface
// a value-in-use collision as an error satisfying *InUseError / ErrInUse so the
// engine can distinguish it from other failures and route it to remediation.
type Doorflow interface {
	// FindPersonByEmail locates a person. Returns ErrNotFound if absent.
	FindPersonByEmail(ctx context.Context, email string) (*Person, error)
	// FindPersonBySystemID locates a person by our stable marker.
	FindPersonBySystemID(ctx context.Context, systemID string) (*Person, error)
	// CreatePerson creates a person and returns it (with a stable id).
	CreatePerson(ctx context.Context, p *Person) (*Person, error)
	// UpdatePerson updates a person and returns the new state.
	UpdatePerson(ctx context.Context, id int, p *Person) (*Person, error)

	// ListCredentials returns all credentials on a person.
	ListCredentials(ctx context.Context, personID int) ([]*Credential, error)
	// CreateCredential adds a credential of kind with value ("*" requests the
	// provider's auto-generation and the returned Value is the generated one).
	// A provider 409 where the credential already exists is benign and returns
	// the existing credential; a 409 where the value is in use returns *InUseError.
	CreateCredential(ctx context.Context, personID int, kind CredentialKind, value string) (*Credential, error)
	// UpdateCredential sets a credential's value and returns the resulting credential
	// (so an autogeneration with "*" yields the generated value). An in-use value
	// returns *InUseError.
	UpdateCredential(ctx context.Context, credID int, value string) (*Credential, error)
	// DeleteCredential removes a credential.
	DeleteCredential(ctx context.Context, credID int) error

	// Sync commits the batch of mutations so far to Doorflow's readers.
	Sync(ctx context.Context) error
}

// Notifier delivers member-facing notifications (PIN changed, etc.).
type Notifier interface {
	NotifyPinChanged(ctx context.Context, coworkerID int, email string) error
}
