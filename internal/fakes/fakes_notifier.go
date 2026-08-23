package fakes

import (
	"context"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

// PINCall is one observed NotifyPinChanged call.
type PINCall struct {
	CoworkerID int
	Email      string
}

// FakeNotifier is an in-memory domain.Notifier for tests.
type FakeNotifier struct {
	PIN []PINCall
}

// NewFakeNotifier returns an empty FakeNotifier.
func NewFakeNotifier() *FakeNotifier { return &FakeNotifier{} }

func (f *FakeNotifier) NotifyPinChanged(ctx context.Context, coworkerID int, email string) error {
	f.PIN = append(f.PIN, PINCall{CoworkerID: coworkerID, Email: email})
	return nil
}

var _ domain.Notifier = (*FakeNotifier)(nil)
