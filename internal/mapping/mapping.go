// Package mapping converts a coworker's Nexudus team membership into the exact
// set of Doorflow group ids they should be in. This is the fix for the legacy
// "member left Woodshop but kept Woodshop group access" gap: resolution is an
// *exact* set (mapped teams ∪ BasicMember), not a superset, so removed teams
// desync.
package mapping

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Mapping is a resolved team→group table plus the always-present BasicMember.
type Mapping struct {
	teamToGroup map[int32]int32
	basicMember int32
}

// LoadFromCSV reads a "nexudus_team_id,doorflow_group_id" CSV (header optional)
// and the required BasicMember id, producing a Mapping.
func LoadFromCSV(path string, basicMember string) (*Mapping, error) {
	bi, err := strconv.Atoi(strings.TrimSpace(basicMember))
	if err != nil {
		return nil, fmt.Errorf("mapping: invalid BasicMember id %q: %w", basicMember, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("mapping: open %s: %w", path, err)
	}
	defer f.Close()

	r := csv.NewReader(bufio.NewReader(f))
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = 2
	recs, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("mapping: parse %s: %w", path, err)
	}

	m := &Mapping{teamToGroup: make(map[int32]int32), basicMember: int32(bi)}
	for i, rec := range recs {
		if i == 0 && rec[0] == "nexudus_team_id" {
			continue // header
		}
		ni, err := strconv.Atoi(strings.TrimSpace(rec[0]))
		if err != nil {
			return nil, fmt.Errorf("mapping: row %d: bad nexudus_team_id %q: %w", i+1, rec[0], err)
		}
		di, err := strconv.Atoi(strings.TrimSpace(rec[1]))
		if err != nil {
			return nil, fmt.Errorf("mapping: row %d: bad doorflow_group_id %q: %w", i+1, rec[1], err)
		}
		m.teamToGroup[int32(ni)] = int32(di)
	}
	return m, nil
}

// Load builds a Mapping in memory (used by tests / the doctor for ephemeral maps).
func Load(teamMap map[int32]int32, basicMember int32) *Mapping {
	m := &Mapping{teamToGroup: make(map[int32]int32, len(teamMap)), basicMember: basicMember}
	for k, v := range teamMap {
		m.teamToGroup[k] = v
	}
	return m
}

// BasicMember returns the always-present doorflow group id.
func (m *Mapping) BasicMember() int32 { return m.basicMember }

// Known reports whether a nexudus team id is in the map (used to exclude
// unmapped teams).
func (m *Mapping) Known(teamID int32) bool {
	_, ok := m.teamToGroup[teamID]
	return ok
}

// GroupIDFor returns the doorflow group id for a nexudus team id.
func (m *Mapping) GroupIDFor(teamID int32) (int32, bool) {
	g, ok := m.teamToGroup[teamID]
	return g, ok
}

// Resolve returns the exact set of doorflow group ids a coworker with the given
// nexudus team ids should belong to: mapped teams ∪ BasicMember, deduped,
// sorted. Unmapped teams are excluded.
func (m *Mapping) Resolve(teamIDs []int32) []int32 {
	seen := make(map[int32]struct{}, len(teamIDs)+1)
	seen[m.basicMember] = struct{}{}
	for _, id := range teamIDs {
		if g, ok := m.teamToGroup[id]; ok {
			seen[g] = struct{}{}
		}
	}
	out := make([]int32, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
