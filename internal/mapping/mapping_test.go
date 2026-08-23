package mapping

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeCSV(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "m.csv")
	content := "nexudus_team_id,doorflow_group_id\n" +
		"1414848104,4622\n" + // Woodshop → 4622
		"1415036464,6071\n" + // BrewShop → 6071
		"1414982431,5466\n" // Memcom → 5466
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadFromCSVAndResolve(t *testing.T) {
	m, err := LoadFromCSV(writeCSV(t), "4482")
	if err != nil {
		t.Fatalf("LoadFromCSV: %v", err)
	}
	if m.BasicMember() != 4482 {
		t.Fatalf("BasicMember = %d, want 4482", m.BasicMember())
	}
	if !m.Known(1414848104) {
		t.Fatalf("woodshop 1414848104 should be Known")
	}
	if m.Known(123) {
		t.Fatalf("123 should not be Known")
	}

	// Basic member always, even with no teams.
	if got := m.Resolve(nil); !reflect.DeepEqual(got, []int32{4482}) {
		t.Fatalf("Resolve(nil) = %v, want [4482]", got)
	}
	// Woodshop + Basic.
	if got := m.Resolve([]int32{1414848104}); !reflect.DeepEqual(got, []int32{4482, 4622}) {
		t.Fatalf("Resolve(woodshop) = %v, want [4482 4622]", got)
	}
	// Multiple teams, sorted, deduped.
	if got := m.Resolve([]int32{1415036464, 1414982431, 1414848104}); !reflect.DeepEqual(got, []int32{4482, 4622, 5466, 6071}) {
		t.Fatalf("Resolve(3 teams) = %v, want [4482 4622 5466 6071]", got)
	}
}

func TestTeamsNotInMapAreExcluded(t *testing.T) {
	m, err := LoadFromCSV(writeCSV(t), "4482")
	if err != nil {
		t.Fatalf("LoadFromCSV: %v", err)
	}
	// A team id that is not in the map must NOT contribute a group…
	if got := m.Resolve([]int32{999999999, 1414848104}); !reflect.DeepEqual(got, []int32{4482, 4622}) {
		t.Fatalf("Resolve([unknown, woodshop]) = %v, want [4482 4622] (unknown excluded)", got)
	}
	// …and only Basic remains when the sole team is unknown.
	if got := m.Resolve([]int32{999999999}); !reflect.DeepEqual(got, []int32{4482}) {
		t.Fatalf("Resolve([unknown]) = %v, want [4482]", got)
	}
}

func TestLoadRejectsBadRows(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.csv")
	os.WriteFile(p, []byte("nexudus_team_id,doorflow_group_id\nabc,5"), 0o644)
	if _, err := LoadFromCSV(p, "4482"); err == nil {
		t.Fatalf("expected error for a malformed team id")
	}
}
