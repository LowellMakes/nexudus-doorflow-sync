package engine_test

import (
	"testing"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

// 7.2: with the write flag off (DryRun), a sync that WOULD add a person and a
// credential issues ZERO Doorflow/Nexudus mutations.
func TestWriteFlagOff_NoMutations(t *testing.T) {
	eng, nw, df, _ := setup(t)
	eng.DryRun = true // write flag off

	nw.Coworkers[77] = &domain.Coworker{
		ID: 77, UserID: 77, FirstName: "A", LastName: "B",
		Email: "m@x.c", TeamIDs: []int32{111}, ContractCount: 1, AccessPin: "1234",
	}
	nw.Contracts[77] = 1 // eligible -> would create person + pin

	eng.SyncOne(t.Context(), 77)

	if df.Written != 0 {
		t.Errorf("flag off must not write Doorflow, wrote %d", df.Written)
	}
	if df.Syncs != 0 {
		t.Errorf("flag off must not commit, got %d", df.Syncs)
	}
	if len(nw.PinWrites) != 0 || len(nw.Reminders) != 0 {
		t.Errorf("flag off must not write Nexudus/notify, got %d/%d", len(nw.PinWrites), len(nw.Reminders))
	}
}
