package fakes

import (
	"context"
	"strconv"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

func (f *FakeDoorflow) ListCredentials(ctx context.Context, personID int) ([]*domain.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*domain.Credential, 0, len(f.Creds[personID]))
	for _, c := range f.Creds[personID] {
		cp := *c
		out = append(out, &cp)
	}
	return out, nil
}

func (f *FakeDoorflow) CreateCredential(ctx context.Context, personID int, kind domain.CredentialKind, value string) (*domain.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.Persons[personID]; !ok {
		return nil, domain.ErrNotFound
	}
	// Benign create-collision: a credential of this kind already exists.
	if existing := f.findCred(personID, kind); existing != nil {
		return existing, domain.ErrAlreadyExists
	}
	final, err := f.resolveKindValue(kind, value)
	if err != nil {
		return nil, err
	}
	c := &domain.Credential{ID: f.nextCredID(), Kind: kind, Value: final}
	f.Creds[personID] = append(f.Creds[personID], c)
	f.Written++
	return c, nil
}

// resolveKindValue handles the "*" auto-generation contract and the in-use
// conflict for PINs. It assumes the caller holds the lock.
func (f *FakeDoorflow) resolveKindValue(kind domain.CredentialKind, value string) (string, error) {
	if value == "*" {
		return "GEN-" + strconv.Itoa(f.nextCredID()), nil
	}
	if kind == domain.KindPin && f.TakenPins[value] {
		return "", &domain.InUseError{Kind: kind}
	}
	return value, nil
}

func (f *FakeDoorflow) UpdateCredential(ctx context.Context, credID int, value string) (*domain.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.findCredByID(credID)
	if c == nil {
		return nil, domain.ErrNotFound
	}
	final, err := f.resolveKindValue(c.Kind, value)
	if err != nil {
		return nil, err
	}
	c.Value = final
	f.Written++
	cp := *c
	return &cp, nil
}

func (f *FakeDoorflow) DeleteCredential(ctx context.Context, credID int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for pid, list := range f.Creds {
		for i, c := range list {
			if c.ID == credID {
				f.Creds[pid] = append(list[:i], list[i+1:]...)
				f.Written++
				return nil
			}
		}
	}
	return domain.ErrNotFound
}

// Sync records a commit to Doorflow's readers.
func (f *FakeDoorflow) Sync(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Syncs++
	return nil
}
