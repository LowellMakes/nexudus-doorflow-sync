// Package domain defines the value types and client interfaces the sync engine
// operates on. Both Nexudus (source of truth) and Doorflow (target) are accessed
// through these interfaces, so the engine is decoupled from concrete transports
// and can be tested with fakes.
package domain

import (
	"errors"
	"time"
)

// CredentialKind identifies which physical credential we are managing.
type CredentialKind int

const (
	// KindPin is the numeric keypad PIN.
	KindPin CredentialKind = iota
	// KindKeyFob is the RF key fob.
	KindKeyFob
	// KindCard is the contactless access card.
	KindCard
)

// String returns the human name of the kind (used in logs).
func (k CredentialKind) String() string {
	switch k {
	case KindPin:
		return "pin"
	case KindKeyFob:
		return "keyfob"
	case KindCard:
		return "card"
	default:
		return "unknown"
	}
}

// ErrNotFound indicates the requested entity does not exist.
var ErrNotFound = errors.New("not found")

// ErrAlreadyExists indicates a create collided with an existing entity. It is a
// benign, idempotency-friendly outcome, distinct from an in-use credential value.
var ErrAlreadyExists = errors.New("credential of this type already exists")

// InUseError is the distinct, actionable outcome for a credential **value** that
// is already in use (e.g. a member-chosen PIN that collides). It is the trigger
// for automatic PIN remediation (spec: pin-remediation, "Detect an in-use PIN
// as a distinct outcome").
type InUseError struct {
	Kind CredentialKind
	// Detail carries the provider's message for logging.
	Detail string
}

func (e *InUseError) Error() string {
	return "credential value of type " + e.Kind.String() + " is already in use"
}
func (e *InUseError) Is(target error) bool { return target == ErrInUse }

// ErrInUse is the sentinel used to test for any in-use conflict.
var ErrInUse = errors.New("credential value in use")

func (e *InUseError) Unwrap() error { return ErrInUse }

// Coworker is the authoritative Nexudus state we reconcile, projected to the
// fields Doorflow sync actually needs. It is the "desired state" input.
type Coworker struct {
	ID int
	// UserID is 0 when the coworker is a contact (no Nexudus user record) —
	// such records are skipped entirely (spec: doorflow-sync, "Contacts").
	UserID    int
	FirstName string
	LastName  string
	Email     string
	// TeamIDs are the coworker's Nexudus team ids (used only for group mapping).
	TeamIDs []int32
	// ContractCount is the number of active contracts (drives enablement).
	ContractCount int
	// Access credentials as set in Nexudus (ground truth for member intent).
	AccessPin  string
	KeyFob     string
	AccessCard string
}

// Contact reports whether the coworker is a non-member contact to skip.
func (c *Coworker) Contact() bool { return c.UserID == 0 }

// Person is a Doorflow person projection used by the engine and clients.
type Person struct {
	ID        int
	Email     string
	FirstName string
	LastName  string
	Enabled   bool
	// GroupIDs is the exact set of doorflow group ids the member should be in.
	GroupIDs []int32
	// SystemID is our stable marker ("NX<coworker-id>") for cross-system matching.
	SystemID string
}

// Credential is a Doorflow credential projection.
type Credential struct {
	ID    int
	Kind  CredentialKind
	Value string
}

// Desired is the full desired Doorflow footprint for one coworker, computed by
// the engine from authoritative Nexudus state.
type Desired struct {
	Person Person
	// Credentials holds the set of credentials that should exist. Absence of a
	// kind here means the member has no such value in Nexudus.
	Credentials map[CredentialKind]string
	// Disabled reports whether the person should be disabled (ineligible) but
	// retained (disable-not-delete).
	Disabled bool
}

// Clock abstracts time for testable scheduling.
type Clock interface {
	Now() time.Time
}

// SystemClockNow returns the real wall-clock time.
func SystemClockNow() time.Time { return time.Now() }
