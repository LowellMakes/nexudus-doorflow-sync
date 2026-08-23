package engine_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

// 3.2: create→no-op on repeat, then change→update (one write), then no-op again.
func TestEngineCredentialUpdateAndNoop(t *testing.T) {
	eng, nw, df, _ := setup(t)
	ctx := t.Context()
	nw.Upsert(&domain.Coworker{ID: 1, UserID: 5, Email: "a@b.c", TeamIDs: []int32{111}, AccessPin: "1000"})
	nw.Contracts[1] = 1
	p, _ := df.CreatePerson(ctx, &domain.Person{Email: "a@b.c", Enabled: true, GroupIDs: []int32{4482, 4483}})
	df.CreateCredential(ctx, p.ID, domain.KindPin, "1000")

	// create→no-op on repeat
	w0 := df.Written
	if r := eng.SyncBatch(ctx, []int{1}); r[0].Mutated || df.Written != w0 {
		t.Fatalf("repeat of an unchanged credential must be a no-op: mutated=%v written=%d->%d", r[0].Mutated, w0, df.Written)
	}
	// change→update is exactly one write
	nw.Coworkers[1].AccessPin = "2000"
	w1 := df.Written
	if r := eng.SyncBatch(ctx, []int{1}); !r[0].Mutated || df.Written != w1+1 {
		t.Fatalf("a changed credential must issue one update: delta=%d", df.Written-w1)
	}
	pinVal := ""
	for _, c := range df.CredentialsFor(p.ID) {
		if c.Kind == domain.KindPin {
			pinVal = c.Value
		}
	}
	if pinVal != "2000" {
		t.Errorf("credential value = %q; want 2000", pinVal)
	}
	// repeat the same value again → no-op
	w2 := df.Written
	if r := eng.SyncBatch(ctx, []int{1}); r[0].Mutated || df.Written != w2 {
		t.Fatalf("repeating an already-applied credential must be a no-op")
	}
}

// 3.3: a contact (no UserId) gets no person; an ineligible member with an
// existing person is disabled, not deleted.
func TestEngineContactNoAccessAndDisableNotDelete(t *testing.T) {
	eng, nw, df, _ := setup(t)
	ctx := t.Context()

	// A contact must not gain a Doorflow person.
	nw.Upsert(&domain.Coworker{ID: 0, UserID: 0, Email: "contact@b.c"})
	eng.SyncBatch(ctx, []int{0})
	if _, err := df.FindPersonByEmail(ctx, "contact@b.c"); err != domain.ErrNotFound {
		t.Errorf("contact should not have a Doorflow person")
	}

	// A member who loses eligibility but already has a person → disabled, retained.
	nw.Upsert(&domain.Coworker{ID: 2, UserID: 7, Email: "duped@b.c", TeamIDs: []int32{111}, AccessPin: "1"})
	nw.Contracts[2] = 0 // no active contract → ineligible
	p2, _ := df.CreatePerson(ctx, &domain.Person{Email: "duped@b.c", Enabled: true, GroupIDs: []int32{4482, 4483}})
	df.CreateCredential(ctx, p2.ID, domain.KindPin, "1")
	eng.SyncBatch(ctx, []int{2})
	got, err := df.FindPersonByEmail(ctx, "duped@b.c")
	if err != nil {
		t.Fatalf("ineligible member's person must be retained (disable-not-delete): %v", err)
	}
	if got.Enabled {
		t.Errorf("ineligible member must be disabled, not enabled")
	}
	if _, cerr := df.FindPersonByEmail(ctx, "contact@b.c"); cerr != domain.ErrNotFound {
		t.Errorf("contact still must not have a person")
	}
}

// 3.5: redelivering the same member in parallel must converge to a single
// person + single credential (no duplicate person/credential).
func TestEnginePerMemberLockNoDuplicate(t *testing.T) {
	eng, nw, df, _ := setup(t)
	ctx := t.Context()
	nw.Upsert(&domain.Coworker{ID: 1, UserID: 5, Email: "a@b.c", TeamIDs: []int32{111}, AccessPin: "1"})
	nw.Contracts[1] = 1

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			eng.SyncBatch(ctx, []int{1})
		}()
	}
	wg.Wait()

	persons := 0
	for _, p := range df.Persons {
		if strings.EqualFold(p.Email, "a@b.c") {
			persons++
		}
	}
	if persons != 1 {
		t.Errorf("duplicate Doorflow persons for the same member: %d", persons)
	}
	// A single pin credential on one person.
	pins, personID := 0, 0
	for pid, creds := range df.Creds {
		for _, c := range creds {
			if c.Kind == domain.KindPin {
				pins++
				personID = pid
			}
		}
	}
	if pins != 1 {
		t.Errorf("duplicate pin credentials: %d", pins)
	}
	_ = personID
}
