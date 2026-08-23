package remediation

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/fakes"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/store"
)

func newRemediator(nw *fakes.FakeNexudus, df *fakes.FakeDoorflow, nf *fakes.FakeNotifier, s store.Store) *Remediator {
	return New(nw, df, s, nf, slog.Default())
}

// 6.3: A member PIN that Doorflow rejects as in-use is auto-generated to a
// unique value; the NEW value is written to the member (Nexudus) and the
// original in-use value is never applied.
func TestGenerateAndConverge(t *testing.T) {
	nw := fakes.NewFakeNexudus()
	nw.Upsert(&domain.Coworker{ID: 100, Email: "a@b.c", AccessPin: "1234"})
	df := fakes.NewFakeDoorflow()
	person, _ := df.CreatePerson(t.Context(), &domain.Person{Email: "a@b.c", Enabled: true, SystemID: "NX100"})
	// An existing PIN cred on the person whose value is in use elsewhere.
	if _, err := df.CreateCredential(t.Context(), person.ID, domain.KindPin, "1234"); err != nil {
		t.Fatalf("seed pin: %v", err)
	}
	df.MarkPinTaken("1234")
	nf := fakes.NewFakeNotifier()

	r := newRemediator(nw, df, nf, store.NewMemory())
	pin, ok, err := r.ResolvePin(t.Context(), 100, person.ID, "a@b.c")
	if err != nil || !ok {
		t.Fatalf("ResolvePin ok=%v err=%v", ok, err)
	}
	if pin == "" || pin == "1234" {
		t.Errorf("generated pin should differ from in-use 1234; got %q", pin)
	}
	// The member's Nexudus PIN must now be the NEW auto-generated value.
	if got := nw.CoworkerPin[100]; got != pin {
		t.Errorf("nexudus pin = %q; want generated %q (original %q never applied)", got, pin, "1234")
	}
	// The Doorflow PIN credential must equal the new value, not the old in-use one.
	creds := df.CredentialsFor(person.ID)
	if len(creds) != 1 || creds[0].Value != pin {
		t.Errorf("doorflow pin not updated to generated value: %+v", creds)
	}
	// Notification delivered to the member's email.
	if len(nf.PIN) != 1 || nf.PIN[0].Email != "a@b.c" {
		t.Errorf("expected one pin-changed notice to a@b.c, got %+v", nf.PIN)
	}
}

// 6.4 + 6.5: When the Nexudus write-back is not confirmed yet, the guard holds
// the generated PIN; later passes reuse the SAME PIN (no regeneration) and retry
// the back-write; the notification fires only once, after confirmation.
func TestWriteBackGuardedNoRegen(t *testing.T) {
	nw := fakes.NewFakeNexudus()
	nw.Upsert(&domain.Coworker{ID: 100, Email: "a@b.c", AccessPin: "1234"})
	nw.UpdatePinErr = errors.New("nexudus unavailable") // write-backs fail until cleared
	df := fakes.NewFakeDoorflow()
	person, _ := df.CreatePerson(t.Context(), &domain.Person{Email: "a@b.c", Enabled: true})
	df.CreateCredential(t.Context(), person.ID, domain.KindPin, "1234")
	df.MarkPinTaken("1234")
	nf := fakes.NewFakeNotifier()
	s := store.NewMemory()
	r := newRemediator(nw, df, nf, s)
	w0 := df.Written // baseline: exactly one autogen may be added across all passes below

	// Pass 1: generates a PIN, Nexudus write-back fails (pending, not converged).
	if _, ok, err := r.ResolvePin(t.Context(), 100, person.ID, "a@b.c"); ok || err == nil {
		t.Fatalf("pass1 must be pending, got ok=%v err=%v", ok, err)
	}
	g1, _ := s.Pending(t.Context(), 100)
	if g1 == nil {
		t.Fatal("expected a pending guard after pass1")
	}

	// Pass 2: reuses the SAME PIN (no regen), write-back still fails.
	if _, ok, err := r.ResolvePin(t.Context(), 100, person.ID, "a@b.c"); ok || err == nil {
		t.Fatalf("pass2 must be pending, got ok=%v err=%v", ok, err)
	}
	g2, _ := s.Pending(t.Context(), 100)
	if g2 == nil || g2.DoorflowPin != g1.DoorflowPin {
		t.Fatalf("pass2 must reuse the same generated pin: %+v vs %+v", g2, g1)
	}

	// Pass 3: Nexudus backs up; confirmed, converged, notified exactly once.
	nw.UpdatePinErr = nil
	got, ok, err := r.ResolvePin(t.Context(), 100, person.ID, "a@b.c")
	if err != nil || !ok {
		t.Fatalf("pass3 must converge, got ok=%v err=%v", ok, err)
	}
	if got != g1.DoorflowPin {
		t.Errorf("pass3 pin %q must equal the original generated %q (no regen)", got, g1.DoorflowPin)
	}

	// Exactly ONE Doorflow write (the autogen) across all three passes — no regen.
	if df.Written != w0+1 {
		t.Errorf("doorflow writes delta = %d; want 1 (a single autogen, no regen churn)", df.Written-w0)
	}
	// Notification only after confirmation, and exactly once.
	if len(nf.PIN) != 1 {
		t.Errorf("notifications = %d; want 1 (only after confirmation)", len(nf.PIN))
	}
	// Guard cleared after convergence.
	if g3, _ := s.Pending(t.Context(), 100); g3 != nil {
		t.Errorf("guard not cleared after convergence: %+v", g3)
	}
	// The member's final Nexudus PIN is the guarded one (original in-use never used).
	if nw.CoworkerPin[100] != g1.DoorflowPin {
		t.Errorf("final nexudus pin %q != guarded %q", nw.CoworkerPin[100], g1.DoorflowPin)
	}
}

// 6.6: Remediation only touches the PIN credential; fob and card stay untouched.
func TestRemediationLeaveSFobCard(t *testing.T) {
	nw := fakes.NewFakeNexudus()
	nw.Upsert(&domain.Coworker{ID: 100, Email: "a@b.c", AccessPin: "1234"})
	df := fakes.NewFakeDoorflow()
	person, _ := df.CreatePerson(t.Context(), &domain.Person{Email: "a@b.c", Enabled: true})
	df.CreateCredential(t.Context(), person.ID, domain.KindPin, "1234")
	// seed fob + card
	fob, _ := df.CreateCredential(t.Context(), person.ID, domain.KindKeyFob, "FOB-7")
	card, _ := df.CreateCredential(t.Context(), person.ID, domain.KindCard, "CARD-3")
	df.MarkPinTaken("1234")
	nf := fakes.NewFakeNotifier()
	r := newRemediator(nw, df, nf, store.NewMemory())

	if _, ok, err := r.ResolvePin(t.Context(), 100, person.ID, "a@b.c"); err != nil || !ok {
		t.Fatalf("ResolvePin ok=%v err=%v", ok, err)
	}
	creds := map[domain.CredentialKind]string{}
	for _, c := range df.CredentialsFor(person.ID) {
		creds[c.Kind] = c.Value
	}
	if len(df.CredentialsFor(person.ID)) != 3 {
		t.Errorf("credential count changed: %d (want 3, no fob/card removal)", len(df.CredentialsFor(person.ID)))
	}
	if creds[domain.KindKeyFob] != "FOB-7" || creds[domain.KindCard] != "CARD-3" {
		t.Errorf("fob/card were touched: %+v", creds)
	}
	if creds[domain.KindPin] == "1234" {
		t.Error("pin was left at the in-use value; remediation should have replaced it")
	}
	_ = fob
	_ = card
}

// 6.2: Auto-gen yields unique values across a batch — one member's generated PIN
// is never the same as another member's.
func TestUniquePinsInBatch(t *testing.T) {
	nw := fakes.NewFakeNexudus()
	nw.Upsert(&domain.Coworker{ID: 1, Email: "one@b.c", AccessPin: "1111"})
	nw.Upsert(&domain.Coworker{ID: 2, Email: "two@b.c", AccessPin: "2222"})
	if nw.UpdatePinErr != nil {
		nw.UpdatePinErr = nil
	}
	df := fakes.NewFakeDoorflow()
	p1, _ := df.CreatePerson(t.Context(), &domain.Person{Email: "one@b.c", Enabled: true})
	p2, _ := df.CreatePerson(t.Context(), &domain.Person{Email: "two@b.c", Enabled: true})
	df.CreateCredential(t.Context(), p1.ID, domain.KindPin, "1111")
	df.CreateCredential(t.Context(), p2.ID, domain.KindPin, "2222")
	df.MarkPinTaken("1111")
	df.MarkPinTaken("2222")
	nf := fakes.NewFakeNotifier()
	r := newRemediator(nw, df, nf, store.NewMemory())

	g1, ok, err := r.ResolvePin(t.Context(), 1, p1.ID, "one@b.c")
	if err != nil || !ok {
		t.Fatalf("member1 remediation: ok=%v err=%v", ok, err)
	}
	g2, ok, err := r.ResolvePin(t.Context(), 2, p2.ID, "two@b.c")
	if err != nil || !ok {
		t.Fatalf("member2 remediation: ok=%v err=%v", ok, err)
	}
	if g1 == g2 {
		t.Errorf("two members got the same generated pin %q; must be unique within the batch", g1)
	}
	if g1 == "1111" || g2 == "2222" {
		t.Errorf("an in-use value was reused: g1=%q g2=%q", g1, g2)
	}
}
