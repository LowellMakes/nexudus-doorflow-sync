// Package store is the small durable ledger that records an auto-generated PIN
// for a member whose Nexudus write-back is not yet confirmed (spec:
// pin-remediation, "Guard against regeneration churn"). It survives restart so
// a later pass retries the write-back of the *existing* Doorflow PIN instead of
// generating a fresh one.
package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Guard is one pending back-write record.
type Guard struct {
	MemberID    int       `json:"member_id"`
	DoorflowPin string    `json:"doorflow_pin"`
	Attempts    int       `json:"attempts"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Store persists the pending state.
type Store interface {
	// Pending returns the guard for a member, or (nil, nil) if none.
	Pending(ctx context.Context, memberID int) (*Guard, error)
	// SetPending records/upserts a guard.
	SetPending(ctx context.Context, g Guard) error
	// Clear removes a guard (write-back confirmed).
	Clear(ctx context.Context, memberID int) error
	// All returns every guard (for observability / diagnostics).
	All(ctx context.Context) ([]Guard, error)
}

// Memory is a concurrency-safe in-memory Store (for tests and single-node use).
type Memory struct {
	mu    sync.Mutex
	items map[int]*Guard
}

// NewMemory returns an empty Memory store.
func NewMemory() *Memory { return &Memory{items: map[int]*Guard{}} }

func (m *Memory) Pending(ctx context.Context, id int) (*Guard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g, ok := m.items[id]; ok {
		cp := *g
		return &cp, nil
	}
	return nil, nil
}

func (m *Memory) SetPending(ctx context.Context, g Guard) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := g
	if g.UpdatedAt.IsZero() {
		cp.UpdatedAt = time.Now()
	}
	m.items[g.MemberID] = &cp
	return nil
}

func (m *Memory) Clear(ctx context.Context, id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, id)
	return nil
}

func (m *Memory) All(ctx context.Context) ([]Guard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Guard, 0, len(m.items))
	for _, g := range m.items {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MemberID < out[j].MemberID })
	return out, nil
}

// File is a JSON-file-backed Store so the guard survives process restarts.
type File struct {
	path string
	mu   *Memory
}

// NewFile (re)loads a File store at path.
func NewFile(path string) (*File, error) {
	m := NewMemory()
	if data, err := os.ReadFile(path); err == nil {
		var items []Guard
		if uerr := json.Unmarshal(data, &items); uerr != nil {
			return nil, uerr
		}
		for _, g := range items {
			m.items[g.MemberID] = &g
		}
	}
	return &File{path: path, mu: m}, nil
}

func (f *File) Pending(ctx context.Context, id int) (*Guard, error) { return f.mu.Pending(ctx, id) }
func (f *File) Clear(ctx context.Context, id int) error {
	if err := flush(f.path, f.mu); err != nil {
		return err
	}
	return f.mu.Clear(ctx, id)
}

func (f *File) SetPending(ctx context.Context, g Guard) error {
	f.mu.SetPending(ctx, g)
	return flush(f.path, f.mu)
}

func (f *File) All(ctx context.Context) ([]Guard, error) { return f.mu.All(ctx) }

func flush(path string, m *Memory) error {
	items, _ := m.All(context.Background())
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if werr := os.WriteFile(tmp, data, 0o600); werr != nil {
		return werr
	}
	return os.Rename(tmp, path)
}
