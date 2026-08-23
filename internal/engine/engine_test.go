package engine_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/engine"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/fakes"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/mapping"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/remediation"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/store"
)

func log() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// setup wires a fresh in-memory engine (fake Nexudus + Doorflow + notifier) with
// a team→group map: team 111→group 4483, team 222→group 4484, BasicMember 4482.
func setup(t *testing.T) (*engine.Engine, *fakes.FakeNexudus, *fakes.FakeDoorflow, *fakes.FakeNotifier) {
	t.Helper()
	nw := fakes.NewFakeNexudus()
	df := fakes.NewFakeDoorflow()
	nf := fakes.NewFakeNotifier()
	m := mapping.Load(map[int32]int32{111: 4483, 222: 4484}, 4482)
	rem := remediation.New(nw, df, store.NewMemory(), nf, log())
	eng := engine.New(nw, df, m, rem, log())
	return eng, nw, df, nf
}
