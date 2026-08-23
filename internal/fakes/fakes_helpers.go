package fakes

import (
	"sort"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
)

func sortCoworkers(all []*domain.Coworker) {
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
}

func sortInts(ids []int) {
	sort.Ints(ids)
}
