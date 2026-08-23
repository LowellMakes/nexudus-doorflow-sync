package reconcile_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/engine"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/fakes"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/mapping"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/reconcile"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/remediation"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/store"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func setup(t *testing.T) (*engine.Engine, *fakes.FakeNexudus, *fakes.FakeDoorflow) {
	t.Helper()
	fN := fakes.NewFakeNexudus()
	fD := fakes.NewFakeDoorflow()
	m := mapping.Load(map[int32]int32{10: 20, 30: 60}, 50)
	rem := remediation.New(fN, fD, store.NewMemory(), &fakes.FakeNotifier{}, discard())
	eng := engine.New(fN, fD, m, rem, discard())
	return eng, fN, fD
}

// mk builds an eligible, non-contact coworker.
func mk(id int, first, last, email string, teams ...int32) *domain.Coworker {
	return &domain.Coworker{ID: id, UserID: id, FirstName: first, LastName: last, Email: email, TeamIDs: teams, ContractCount: 1}
}

func findPerson(d *fakes.FakeDoorflow, email string) *domain.Person {
	for _, p := range d.Persons {
		if p.Email == email {
			return p
		}
	}
	return nil
}

func recFor(eng *engine.Engine, fN *fakes.FakeNexudus, proc reconcile.Processer, maxCon int) *reconcile.Reconciler {
	if proc == nil {
		proc = func(ctx context.Context, id int) (bool, error) { return eng.ProcessMember(ctx, id), nil }
	}
	return reconcile.New(fN, proc, eng.Commit, 100, maxCon, discard())
}

// 5.1: dues cleared → re-enable. An eligible member (in the incremental window)
// whose Doorflow person was disabled is re-enabled by the sweep.
func TestIncremental_ReenableOnDuesCleared(t *testing.T) {
	eng, fN, fD := setup(t)
	fN.Coworkers[1] = mk(1, "Ada", "Y", "a@y.c", 10)
	fN.Contracts[1] = 1          // dues cleared -> eligible
	fN.RecordPaid(1, time.Now()) // also present in the paid-since leg
	// Previously disabled (was overdue when last synced).
	_, _ = fD.CreatePerson(t.Context(), &domain.Person{Email: "a@y.c", Enabled: false, FirstName: "Ada", LastName: "Y"})

	_, mutated := recFor(eng, fN, nil, 4).Incremental(t.Context(), time.Now().Add(-time.Hour))
	if !mutated {
		t.Fatal("expected a mutation (re-enable)")
	}
	if p := findPerson(fD, "a@y.c"); p == nil || !p.Enabled {
		t.Fatalf("member should be enabled after dues cleared, got %+v", p)
	}
}

// 5.2: startup reconciliation converges a change that happened while down,
// independent of the durable log.
func TestStartupConvergesMissedChange(t *testing.T) {
	eng, fN, fD := setup(t)
	fN.Coworkers[7] = mk(7, "Bob", "B", "b@b.c", 30)
	fN.Contracts[7] = 1
	_, mutated := recFor(eng, fN, nil, 4).Incremental(t.Context(), time.Now().Add(-24*time.Hour))
	if !mutated {
		t.Fatal("expected the missed change to be converged (created)")
	}
	if findPerson(fD, "b@b.c") == nil {
		t.Fatal("should have created the person missed while down")
	}
}

// 5.3: full sweep converges EVERY member (no size cap) and stays within the
// configured concurrent limit.
func TestFullSweep_CoversAllWithinConcurrencyLimit(t *testing.T) {
	eng, fN, fD := setup(t)
	const n = 9
	for i := 1; i <= n; i++ {
		email := "m" + string(rune('0'+i)) + "@x.c"
		fN.Coworkers[i] = mk(i, "M", "U", email, 10)
		fN.Contracts[i] = 1
	}
	var mu sync.Mutex
	running, maxCon := 0, 0
	proc := func(ctx context.Context, id int) (bool, error) {
		mu.Lock()
		running++
		if running > maxCon {
			maxCon = running
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			running--
			mu.Unlock()
		}()
		return eng.ProcessMember(ctx, id), nil
	}
	processed, _ := recFor(eng, fN, proc, 3).Full(t.Context())
	if processed != n {
		t.Fatalf("full sweep should process all %d, got %d", n, processed)
	}
	if maxCon > 3 {
		t.Errorf("concurrency exceeded limit 3, observed %d", maxCon)
	}
	if got := len(fD.Persons); got != n {
		t.Errorf("expected %d persons created, got %d", n, got)
	}
}

// 5.4: reconciliation corrects drift (an external change) by reconciling state,
// not just logging it.
func TestSweep_CorrectsDrift(t *testing.T) {
	eng, fN, fD := setup(t)
	fN.Coworkers[1] = mk(1, "Ada", "Y", "a@y.c", 10)
	fN.Contracts[1] = 1
	// Someone externally disabled the member (drift).
	_, _ = fD.CreatePerson(t.Context(), &domain.Person{Email: "a@y.c", Enabled: false, FirstName: "Ada", LastName: "Y"})
	_, mutated := recFor(eng, fN, nil, 4).Incremental(t.Context(), time.Now())
	if !mutated {
		t.Fatal("drift should be corrected (mutation expected)")
	}
	p := findPerson(fD, "a@y.c")
	if p == nil || !p.Enabled {
		t.Fatalf("drift should be corrected: person re-enabled, got %+v", p)
	}
}

// 5.5: exactly one reader commit for a mutating batch; none for an all-no-op batch.
func TestCommitOnceOnMutatingBatchZeroOnNoop(t *testing.T) {
	// Mutating batch -> exactly one commit.
	{
		eng, fN, fD := setup(t)
		fN.Coworkers[1] = mk(1, "Ada", "Y", "a@y.c", 10)
		fN.Contracts[1] = 1
		recFor(eng, fN, nil, 2).Incremental(t.Context(), time.Now())
		if fD.Syncs != 1 {
			t.Errorf("mutating batch should commit exactly once, got %d", fD.Syncs)
		}
	}
	// No-op batch -> no additional commit (member already fully in sync).
	{
		eng, fN, fD := setup(t)
		cw := mk(1, "Ada", "Y", "a@y.c", 10)
		cw.AccessPin = "4242"
		fN.Coworkers[1] = cw
		fN.Contracts[1] = 1
		p, _ := fD.CreatePerson(t.Context(), &domain.Person{Email: "a@y.c", Enabled: true, GroupIDs: []int32{20, 50}, FirstName: "Ada", LastName: "Y"})
		_, _ = fD.CreateCredential(t.Context(), p.ID, domain.KindPin, "4242")
		recFor(eng, fN, nil, 2).Incremental(t.Context(), time.Now())
		if fD.Syncs != 0 {
			t.Errorf("no-op batch must not commit, got %d", fD.Syncs)
		}
	}
}
