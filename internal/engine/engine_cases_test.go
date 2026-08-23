package engine_test

import (
	"testing"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

// 3.1: A member already at the desired state results in zero Doorflow writes; a
// changed state (new team) results in the right mutation (exact-set groups).
func TestEngineNoopThenChanged(t *testing.T) {
	eng, nw, df, _ := setup(t)
	ctx := t.Context()
	nw.Upsert(&domain.Coworker{ID: 1, UserID: 5, Email: "a@b.c", TeamIDs: []int32{111}, AccessPin: "1234", FirstName: "Ann", LastName: "Lee"})
	nw.Contracts[1] = 1
	p, _ := df.CreatePerson(ctx, &domain.Person{Email: "a@b.c", Enabled: true, GroupIDs: []int32{4482, 4483}})
	df.CreateCredential(ctx, p.ID, domain.KindPin, "1234")

	w0, s0 := df.Written, df.Syncs
	if r := eng.SyncBatch(ctx, []int{1}); r[0].Mutated || df.Written != w0 || df.Syncs != s0 {
		t.Fatalf("no-op member mutated: mutated=%v written=%d->%d syncs=%d", r[0].Mutated, w0, df.Written, df.Syncs)
	}

	// Member moves to a new team: exact-set membership must become [4482,4484].
	nw.Coworkers[1].TeamIDs = []int32{222}
	if r := eng.SyncBatch(ctx, []int{1}); !r[0].Mutated {
		t.Fatal("expected a mutation after the member changed teams")
	}
	got, _ := df.FindPersonByEmail(ctx, "a@b.c")
	has := map[int32]bool{}
	for _, g := range got.GroupIDs {
		has[g] = true
	}
	if len(got.GroupIDs) != 2 || !has[4482] || !has[4484] || has[4483] {
		t.Errorf("groups = %v; want exact-set [4482 4484] with old 4483 removed", got.GroupIDs)
	}
}

// 3.2 + 6.1: A member with an in-use PIN ends with a working auto-generated PIN
// in both systems; the person write is still applied (not dropped), and an
// unrelated member is left untouched.
func TestEnginePinInUseRemediated(t *testing.T) {
	eng, nw, df, nf := setup(t)
	ctx := t.Context()
	nw.Upsert(&domain.Coworker{ID: 1, UserID: 5, Email: "a@b.c", TeamIDs: []int32{111}, AccessPin: "1234"})
	nw.Contracts[1] = 1
	nw.Upsert(&domain.Coworker{ID: 2, UserID: 6, Email: "x@b.c", TeamIDs: []int32{111}, AccessPin: "7777"})
	nw.Contracts[2] = 1
	p2, _ := df.CreatePerson(ctx, &domain.Person{Email: "x@b.c", Enabled: true, GroupIDs: []int32{4482, 4483}})
	df.CreateCredential(ctx, p2.ID, domain.KindPin, "7777")
	df.MarkPinTaken("1234")

	res := eng.SyncBatch(ctx, []int{1, 2})
	if len(res) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(res))
	}
	if res[0].Err != nil {
		t.Fatalf("member1 sync errored: %v", res[0].Err)
	}
	if !res[0].Remediated {
		t.Errorf("member1 pin should be remediated; report=%+v", res[0])
	}
	// Person write for member1 was applied even though its PIN was in use.
	p1, err := df.FindPersonByEmail(ctx, "a@b.c")
	if err != nil || !p1.Enabled {
		t.Fatalf("member1 person not applied/enabled: %v %v", p1, err)
	}
	// Member1 ends with a working (generated) PIN in both systems.
	final := nw.CoworkerPin[1]
	if final == "" || final == "1234" {
		t.Errorf("member1 Nexudus PIN %q not remediated to a generated value", final)
	}
	var dfPin string
	for _, c := range df.CredentialsFor(p1.ID) {
		if c.Kind == domain.KindPin {
			dfPin = c.Value
		}
	}
	if dfPin != final {
		t.Errorf("Doorflow PIN %q != Nexudus PIN %q (must match)", dfPin, final)
	}
	// The unrelated member is untouched.
	touched := false
	for _, w := range nw.PinWrites {
		if w.CoworkerID == 2 {
			touched = true
		}
	}
	if touched {
		t.Error("unrelated member's PIN was written; must stay untouched")
	}
	if _, err := df.FindPersonByEmail(ctx, "x@b.c"); err != nil {
		t.Fatalf("unrelated member person missing: %v", err)
	}
	member2Pins := 0
	for _, c := range df.CredentialsFor(p2.ID) {
		if c.Kind == domain.KindPin {
			member2Pins++
			if c.Value != "7777" {
				t.Errorf("unrelated member PIN changed to %q; want 7777", c.Value)
			}
		}
	}
	if member2Pins != 1 {
		t.Errorf("unrelated member pin credential count = %d; want 1 (untouched)", member2Pins)
	}
	// Notification delivered to the remediated member only.
	if len(nf.PIN) != 1 || nf.PIN[0].Email != "a@b.c" {
		t.Errorf("notifications = %+v; want exactly one to a@b.c", nf.PIN)
	}
}

// 3.3: Adding a new credential type to a member applies the credential write
// successfully (person + credential both end in the desired state).
func TestEngineAddNewCredentialType(t *testing.T) {
	eng, nw, df, _ := setup(t)
	ctx := t.Context()
	nw.Upsert(&domain.Coworker{ID: 1, UserID: 5, Email: "a@b.c", TeamIDs: []int32{111}, AccessPin: "1", KeyFob: "FOB-9"})
	nw.Contracts[1] = 1
	p, _ := df.CreatePerson(ctx, &domain.Person{Email: "a@b.c", Enabled: true, GroupIDs: []int32{4482, 4483}})
	df.CreateCredential(ctx, p.ID, domain.KindPin, "1")
	w0 := df.Written

	if r := eng.SyncBatch(ctx, []int{1}); !r[0].Mutated {
		t.Fatal("expected a mutation when adding a new keyfob")
	}
	var hasFob bool
	for _, c := range df.CredentialsFor(p.ID) {
		if c.Kind == domain.KindKeyFob && c.Value == "FOB-9" {
			hasFob = true
		}
	}
	if !hasFob {
		t.Errorf("new keyfob not added: %+v", df.CredentialsFor(p.ID))
	}
	if df.Written != w0+1 {
		t.Errorf("expected exactly one credential write, delta=%d", df.Written-w0)
	}
}

// 3.5: A batch of mutations from multiple members issues exactly one sync at the
// end; a fully-converged batch issues none.
func TestEngineBatchOneSyncAndNoopNoSync(t *testing.T) {
	eng, nw, df, _ := setup(t)
	ctx := t.Context()
	nw.Upsert(&domain.Coworker{ID: 1, UserID: 5, Email: "a@b.c", TeamIDs: []int32{111}, AccessPin: "1"})
	nw.Contracts[1] = 1
	nw.Upsert(&domain.Coworker{ID: 2, UserID: 6, Email: "b@b.c", TeamIDs: []int32{222}, KeyFob: "F"})
	nw.Contracts[2] = 1

	res := eng.SyncBatch(ctx, []int{1, 2})
	if !res[0].Mutated || !res[1].Mutated {
		t.Fatalf("both members should mutate on the first pass: %+v", res)
	}
	if df.Syncs != 1 {
		t.Errorf("expected exactly one batch sync, got %d", df.Syncs)
	}

	// Converged: another batch must not issue a sync.
	res2 := eng.SyncBatch(ctx, []int{1, 2})
	for i, r := range res2 {
		if r.Mutated {
			t.Errorf("member %d should be a no-op on the second pass: %+v", i+1, r)
		}
	}
	if df.Syncs != 1 {
		t.Errorf("a no-op batch should not sync; syncs=%d", df.Syncs)
	}
}
