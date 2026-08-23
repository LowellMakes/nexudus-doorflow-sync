package fakes

import (
	"context"
	"strings"
	"sync"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

// FakeDoorflow is an in-memory domain.Doorflow for tests.
type FakeDoorflow struct {
	mu sync.Mutex

	Persons   map[int]*domain.Person
	Creds     map[int][]*domain.Credential // personID -> credentials
	PersonSeq int
	CredSeq   int

	// TakenPins are PIN values already in use (trigger *InUseError on set).
	TakenPins map[string]bool
	// Written counts credential + person mutations (assert no-op idempotency).
	Written int
	// Syncs counts Commit(Sync) calls.
	Syncs int
}

// NewFakeDoorflow returns an empty FakeDoorflow.
func NewFakeDoorflow() *FakeDoorflow {
	return &FakeDoorflow{
		Persons:   map[int]*domain.Person{},
		Creds:     map[int][]*domain.Credential{},
		TakenPins: map[string]bool{},
	}
}

// MarkPinTaken records a pin value as already in use.
func (f *FakeDoorflow) MarkPinTaken(v string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.TakenPins[v] = true
}

func (f *FakeDoorflow) nextPersonID() int {
	f.PersonSeq++
	return f.PersonSeq
}

func (f *FakeDoorflow) nextCredID() int {
	f.CredSeq++
	return f.CredSeq
}

func (f *FakeDoorflow) findCred(personID int, kind domain.CredentialKind) *domain.Credential {
	for _, c := range f.Creds[personID] {
		if c.Kind == kind {
			return c
		}
	}
	return nil
}

func (f *FakeDoorflow) findCredByID(id int) *domain.Credential {
	for _, list := range f.Creds {
		for _, c := range list {
			if c.ID == id {
				return c
			}
		}
	}
	return nil
}

// CredentialsFor returns the current set of credentials on a person (a copy),
// for test assertions.
func (f *FakeDoorflow) CredentialsFor(personID int) []*domain.Credential {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*domain.Credential, 0, len(f.Creds[personID]))
	for _, c := range f.Creds[personID] {
		cp := *c
		out = append(out, &cp)
	}
	return out
}

func (f *FakeDoorflow) FindPersonByEmail(ctx context.Context, email string) (*domain.Person, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.Persons {
		if strings.EqualFold(p.Email, email) {
			cp := *p
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *FakeDoorflow) FindPersonBySystemID(ctx context.Context, systemID string) (*domain.Person, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.Persons {
		if p.SystemID == systemID {
			cp := *p
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *FakeDoorflow) CreatePerson(ctx context.Context, p *domain.Person) (*domain.Person, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nextPersonID()
	cp := *p
	cp.ID = n
	f.Persons[n] = &cp
	f.Written++
	return &cp, nil
}

func (f *FakeDoorflow) UpdatePerson(ctx context.Context, id int, p *domain.Person) (*domain.Person, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.Persons[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *p
	cp.ID = id
	cp.Email = cur.Email // identity anchor; do not reassign
	cp.SystemID = cur.SystemID
	f.Persons[id] = &cp
	f.Written++
	return &cp, nil
}
