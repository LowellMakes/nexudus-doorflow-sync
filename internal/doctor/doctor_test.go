package doctor_test

import (
	"strings"
	"testing"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/doctor"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/fakes"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/mapping"
)

func setup() (*doctor.Doctor, *fakes.FakeNexudus, *fakes.FakeDoorflow) {
	fN := fakes.NewFakeNexudus()
	fD := fakes.NewFakeDoorflow()
	m := mapping.Load(map[int32]int32{10: 20}, 50)
	d := doctor.New(fN, fD, m)
	return d, fN, fD
}

func member(id int, email string, pin string) *domain.Coworker {
	return &domain.Coworker{ID: id, UserID: id, FirstName: "A", LastName: "B", Email: email, TeamIDs: []int32{10}, ContractCount: 1, AccessPin: pin}
}

// 7.1: doctor walks the membership and prints desired-vs-current diffs with ONLY
// reads — zero Doorflow/Nexudus writes.
func TestDoctor_ReadOnlyReportNoWrites(t *testing.T) {
	d, fN, fD := setup()

	// A: eligible, no person yet -> will create + add pin.
	fN.Coworkers[1] = member(1, "a@y.c", "1234")
	fN.Contracts[1] = 1
	// B: eligible, fully in sync -> already in sync (no-op).
	fN.Coworkers[2] = member(2, "b@y.c", "4321")
	fN.Contracts[2] = 1
	p2, _ := fD.CreatePerson(t.Context(), &domain.Person{Email: "b@y.c", Enabled: true, GroupIDs: []int32{20, 50}, FirstName: "A", LastName: "B"})
	_, _ = fD.CreateCredential(t.Context(), p2.ID, domain.KindPin, "4321")
	// C: ineligible (no contract) -> no person, no access.
	fN.Coworkers[3] = member(3, "c@y.c", "")

	rep, err := d.Run(t.Context(), 100)
	if err != nil {
		t.Fatalf("doctor run: %v", err)
	}
	if rep.Members != 3 {
		t.Errorf("expected 3 members, got %d", rep.Members)
	}
	if !strings.Contains(rep.String, "WILL CREATE") {
		t.Errorf("expected a create diff, got:\n%s", rep.String)
	}
	if !strings.Contains(rep.String, "already in sync") {
		t.Errorf("expected an in-sync line, got:\n%s", rep.String)
	}
	if !strings.Contains(rep.String, "credential") {
		t.Errorf("expected a credential diff, got:\n%s", rep.String)
	}
	// Zero writes: no person/credential mutations, no commit, no PIN writeback/notify.
	if fD.Written > 2 || fD.Syncs != 0 || len(fN.PinWrites) != 0 || len(fN.Reminders) != 0 {
		t.Errorf("doctor must be read-only: written=%d syncs=%d pinWrites=%d reminders=%d",
			fD.Written, fD.Syncs, len(fN.PinWrites), len(fN.Reminders))
	}
	// The create diff should mask the PIN value, not print it in full.
	if strings.Contains(rep.String, "1234") {
		t.Errorf("doctor report must not print the full PIN, got:\n%s", rep.String)
	}
}
