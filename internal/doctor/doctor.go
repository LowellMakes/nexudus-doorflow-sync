// Package doctor is the read-only self-inspection mode (spec task 7.1). It walks
// the membership and reports, per member, the desired-vs-current Doorflow
// footprint it WOULD apply — without issuing any Doorflow or Nexudus write. Every
// Doorflow/credential mutation method is deliberately absent by construction:
// doctor only calls read methods (GetCoworker, ContractsAndInvoices,
// FindPersonByEmail, ListCredentials) and the local team→group mapping.
package doctor

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/mapping"
)

var kindOrder = []domain.CredentialKind{domain.KindPin, domain.KindKeyFob, domain.KindCard}

// Doctor inspects desired vs current state per member (read-only).
type Doctor struct {
	N domain.Nexudus
	D domain.Doorflow
	M *mapping.Mapping
}

// New builds a Doctor.
func New(n domain.Nexudus, d domain.Doorflow, m *mapping.Mapping) *Doctor {
	return &Doctor{N: n, D: d, M: m}
}

// MemberDiff is the diff for one coworker.
type MemberDiff struct {
	MemberID int
	Email    string
	Lines    []string
}

// Report is the aggregated doctor output across all members.
type Report struct {
	Members   int
	NeedDiffs int // members that are not already in sync
	String    string
}

// Member diffs one coworker read-only. It returns (md, nil) and never writes.
func (d *Doctor) Member(ctx context.Context, id int) (*MemberDiff, error) {
	md := &MemberDiff{MemberID: id}
	cw, err := d.N.GetCoworker(ctx, id)
	if err != nil {
		return nil, err
	}
	md.Email = cw.Email

	if cw.UserID == 0 {
		md.Lines = append(md.Lines, "contact (no membership user): ensure NO person has access")
		return md, nil
	}

	contracts, overdue, err := d.N.ContractsAndInvoices(ctx, id)
	if err != nil {
		return nil, err
	}
	eligible := contracts > 0 && !overdue
	desired := desiredCreds(cw)
	cur, _ := d.D.FindPersonByEmail(ctx, cw.Email)

	if cur == nil {
		if eligible {
			md.Lines = append(md.Lines, fmt.Sprintf("no person: WILL CREATE (enabled=true, groups=%v)", sorted(d.M.Resolve(cw.TeamIDs))))
			for _, k := range kindOrder {
				if v, ok := desired[k]; ok {
					md.Lines = append(md.Lines, fmt.Sprintf("  + credential %s (%s)", k, mask(v)))
				}
			}
		} else {
			md.Lines = append(md.Lines, "ineligible and no person: OK (no access)")
		}
		return md, nil
	}

	wantGroups := sorted(d.M.Resolve(cw.TeamIDs))
	if cur.Enabled != eligible {
		md.Lines = append(md.Lines, fmt.Sprintf("WILL SET enabled=%v (currently %v)", eligible, cur.Enabled))
	}
	if !sameGroups(cur.GroupIDs, wantGroups) {
		md.Lines = append(md.Lines, fmt.Sprintf("WILL SET groups=%v (currently %v)", wantGroups, sorted(cur.GroupIDs)))
	}
	if strings.TrimSpace(cur.FirstName+" "+cur.LastName) != strings.TrimSpace(cw.FirstName+" "+cw.LastName) {
		md.Lines = append(md.Lines, fmt.Sprintf("WILL SET name=\"%s %s\" (currently \"%s %s\")", cw.FirstName, cw.LastName, cur.FirstName, cur.LastName))
	}

	curCreds, _ := d.D.ListCredentials(ctx, cur.ID)
	have := map[domain.CredentialKind]bool{}
	for _, c := range curCreds {
		have[c.Kind] = true
	}
	for _, k := range kindOrder {
		if v, want := desired[k]; want && !have[k] {
			md.Lines = append(md.Lines, fmt.Sprintf("WILL ADD credential %s (%s)", k, mask(v)))
		}
	}
	if len(md.Lines) == 0 {
		md.Lines = append(md.Lines, "already in sync (no changes)")
	}
	return md, nil
}

// Run walks the whole membership (paged, no size cap) and returns the combined
// read-only report. It issues zero writes.
func (d *Doctor) Run(ctx context.Context, pageSize int) (*Report, error) {
	if pageSize <= 0 {
		pageSize = 100
	}
	rep := &Report{}
	var b strings.Builder
	for page := 0; ; page++ {
		cows, more, err := d.N.AllCoworkers(ctx, page, pageSize)
		if err != nil {
			return nil, err
		}
		for _, cw := range cows {
			md, merr := d.Member(ctx, cw.ID)
			if merr != nil {
				fmt.Fprintf(&b, "member %d: ERROR %v\n", cw.ID, merr)
				continue
			}
			rep.Members++
			fmt.Fprintf(&b, "member %d (%s):\n", md.MemberID, md.Email)
			for _, line := range md.Lines {
				fmt.Fprintf(&b, "  %s\n", line)
			}
			if len(md.Lines) > 0 && md.Lines[0] != "already in sync (no changes)" {
				rep.NeedDiffs++
			}
		}
		if !more {
			break
		}
	}
	rep.String = b.String()
	return rep, nil
}

func desiredCreds(cw *domain.Coworker) map[domain.CredentialKind]string {
	m := map[domain.CredentialKind]string{}
	if cw.AccessPin != "" {
		m[domain.KindPin] = cw.AccessPin
	}
	if cw.KeyFob != "" {
		m[domain.KindKeyFob] = cw.KeyFob
	}
	if cw.AccessCard != "" {
		m[domain.KindCard] = cw.AccessCard
	}
	return m
}

func sameGroups(a, b []int32) bool {
	as, bs := sorted(a), sorted(b)
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func sorted(in []int32) []int32 {
	out := append([]int32(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// mask hides a credential value so the report never logs full secrets.
func mask(v string) string {
	if v == "*" {
		return "autogen(*)"
	}
	if len(v) <= 2 {
		return "****"
	}
	return fmt.Sprintf("%s…%s (%d chars)", v[:1], v[len(v)-1:], len(v))
}
