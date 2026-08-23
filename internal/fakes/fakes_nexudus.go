// Package fakes provides in-memory implementations of the domain client
// interfaces, used to test the engine, events, reconcile, and remediation logic
// without any real HTTP or NATS.
package fakes

import (
	"context"
	"sync"
	"time"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

// PaidEvent records that a member's dues cleared at a given time.
type PaidEvent struct {
	CoworkerID int
	At         time.Time
}

// FakeNexudus is an in-memory domain.Nexudus for tests.
type FakeNexudus struct {
	mu sync.Mutex

	Coworkers map[int]*domain.Coworker
	// Contracts and Overdue drive ContractsAndInvoices.
	Contracts map[int]int
	Overdue   map[int]bool
	// Paid records dues-clearing events for PaidMembersSince.
	Paid []PaidEvent

	// Write failures you can script for the PIN write-back / reminder calls.
	UpdatePinErr    error
	SendReminderErr error

	// PinWrites records every (id,pin) from UpdateCoworkerPin (for assertions).
	PinWrites []PINWrite
	// Reminders records every id from SendPinReminder.
	Reminders []int
	// PinWritebacks are the read-modify-write results; tests can pre-seed the
	// current PIN per member to verify read-modify-write preserves other fields.
	CoworkerPin map[int]string
}

// PINWrite is one observed UpdateCoworkerPin call.
type PINWrite struct {
	CoworkerID int
	Pin        string
}

// NewFakeNexudus returns an empty FakeNexudus.
func NewFakeNexudus() *FakeNexudus {
	return &FakeNexudus{
		Coworkers:   map[int]*domain.Coworker{},
		Contracts:   map[int]int{},
		Overdue:     map[int]bool{},
		CoworkerPin: map[int]string{},
	}
}

// Upsert adds/updates a coworker in the fake's state.
func (f *FakeNexudus) Upsert(c *domain.Coworker) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Coworkers[c.ID] = c
}

// SetOverdue marks a coworker as having an overdue invoice.
func (f *FakeNexudus) SetOverdue(id int, overdue bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Overdue[id] = overdue
}

// RecordPaid records a dues-clearing event.
func (f *FakeNexudus) RecordPaid(id int, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Paid = append(f.Paid, PaidEvent{CoworkerID: id, At: at})
}

func (f *FakeNexudus) GetCoworker(ctx context.Context, id int) (*domain.Coworker, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.Coworkers[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *c
	// Preserve the current PIN so read-modify-write asserts hold even if the
	// coworker record itself doesn't carry it.
	if p, ok := f.CoworkerPin[id]; ok {
		cp.AccessPin = p
	}
	return &cp, nil
}

func (f *FakeNexudus) ContractsAndInvoices(ctx context.Context, id int) (int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Contracts[id], f.Overdue[id], nil
}

func (f *FakeNexudus) UpdatedCoworkersSince(ctx context.Context, since time.Time, page, size int) ([]*domain.Coworker, bool, error) {
	return f.paginateAll(page, size)
}

func (f *FakeNexudus) AllCoworkers(ctx context.Context, page, size int) ([]*domain.Coworker, bool, error) {
	return f.paginateAll(page, size)
}

func (f *FakeNexudus) paginateAll(page, size int) ([]*domain.Coworker, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := make([]*domain.Coworker, 0, len(f.Coworkers))
	for _, c := range f.Coworkers {
		all = append(all, c)
	}
	// deterministic order by id
	sortCoworkers(all)
	if page*size >= len(all) {
		return nil, false, nil
	}
	end := (page + 1) * size
	if end > len(all) {
		end = len(all)
	}
	out := make([]*domain.Coworker, end-page*size)
	for i := page * size; i < end; i++ {
		cp := *all[i]
		out[i-page*size] = &cp
	}
	return out, end < len(all), nil
}

func (f *FakeNexudus) PaidMembersSince(ctx context.Context, since time.Time, page, size int) ([]int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]int, 0, len(f.Paid))
	seen := map[int]bool{}
	for _, e := range f.Paid {
		if !e.At.Before(since) && !seen[e.CoworkerID] {
			seen[e.CoworkerID] = true
			ids = append(ids, e.CoworkerID)
		}
	}
	sortInts(ids)
	if page*size >= len(ids) {
		return nil, false, nil
	}
	end := (page + 1) * size
	if end > len(ids) {
		end = len(ids)
	}
	return ids[page*size : end], end < len(ids), nil
}

func (f *FakeNexudus) UpdateCoworkerPin(ctx context.Context, id int, pin string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.UpdatePinErr != nil {
		return f.UpdatePinErr
	}
	if _, ok := f.Coworkers[id]; !ok {
		return domain.ErrNotFound
	}
	f.CoworkerPin[id] = pin
	if c, ok := f.Coworkers[id]; ok {
		c.AccessPin = pin
	}
	f.PinWrites = append(f.PinWrites, PINWrite{CoworkerID: id, Pin: pin})
	return nil
}

func (f *FakeNexudus) SendPinReminder(ctx context.Context, id int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SendReminderErr != nil {
		return f.SendReminderErr
	}
	f.Reminders = append(f.Reminders, id)
	return nil
}
