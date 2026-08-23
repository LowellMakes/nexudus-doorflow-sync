package store

import (
	"path/filepath"
	"testing"
)

func TestMemoryGuardSetGetClear(t *testing.T) {
	s := NewMemory()
	ctx := t.Context()
	if got, _ := s.Pending(ctx, 1); got != nil {
		t.Fatalf("expected no pending for 1, got %+v", got)
	}
	if err := s.SetPending(ctx, Guard{MemberID: 1, DoorflowPin: "GEN-42", Attempts: 1}); err != nil {
		t.Fatal(err)
	}
	g, err := s.Pending(ctx, 1)
	if err != nil || g == nil {
		t.Fatalf("pending = %+v err=%v", g, err)
	}
	if g.DoorflowPin != "GEN-42" {
		t.Errorf("pin = %q", g.DoorflowPin)
	}
	if err := s.Clear(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Pending(ctx, 1); got != nil {
		t.Fatalf("still pending after clear: %+v", got)
	}
}

func TestFileStoreSurvivesRestart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "guard.json")

	s1, err := NewFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.SetPending(t.Context(), Guard{MemberID: 7, DoorflowPin: "GEN-9", Attempts: 2}); err != nil {
		t.Fatal(err)
	}
	// Simulate a process restart: open a fresh store from the same path.
	s2, err := NewFile(p)
	if err != nil {
		t.Fatal(err)
	}
	g, err := s2.Pending(t.Context(), 7)
	if err != nil || g == nil {
		t.Fatalf("guard lost on restart: %+v err=%v", g, err)
	}
	if g.DoorflowPin != "GEN-9" || g.Attempts != 2 {
		t.Errorf("guard not preserved: %+v", g)
	}
}
