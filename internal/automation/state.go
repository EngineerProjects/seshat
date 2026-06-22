package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ExecutionState tracks the persistent history of a workflow across runs.
type ExecutionState struct {
	WorkflowName string    `json:"workflow_name"`
	LastRunAt    time.Time `json:"last_run_at"`
	NextRunAt    time.Time `json:"next_run_at,omitempty"`
	LastStatus   string    `json:"last_status"` // "success" | "error" | "running"
	LastError    string    `json:"last_error,omitempty"`
	RunCount     int64     `json:"run_count"`
	SuccessCount int64     `json:"success_count"`
	FailureCount int64     `json:"failure_count"`
	ConsecErrors int       `json:"consec_errors"`
}

// StateStore persists and retrieves execution state across process restarts.
type StateStore interface {
	Load(ctx context.Context, workflowName string) (*ExecutionState, error)
	Save(ctx context.Context, state ExecutionState) error
	List(ctx context.Context) ([]ExecutionState, error)
	Delete(ctx context.Context, workflowName string) error
}

// ─── MemoryStateStore ─────────────────────────────────────────────────────────

// MemoryStateStore is an in-process store with no persistence.
// Suitable for testing or ephemeral single-run scenarios.
type MemoryStateStore struct {
	mu     sync.RWMutex
	states map[string]ExecutionState
}

func NewMemoryStateStore() *MemoryStateStore {
	return &MemoryStateStore{states: make(map[string]ExecutionState)}
}

func (m *MemoryStateStore) Load(_ context.Context, name string) (*ExecutionState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.states[name]
	if !ok {
		return nil, nil
	}
	cp := s
	return &cp, nil
}

func (m *MemoryStateStore) Save(_ context.Context, s ExecutionState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[s.WorkflowName] = s
	return nil
}

func (m *MemoryStateStore) List(_ context.Context) ([]ExecutionState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ExecutionState, 0, len(m.states))
	for _, s := range m.states {
		out = append(out, s)
	}
	return out, nil
}

func (m *MemoryStateStore) Delete(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, name)
	return nil
}

// ─── FileStateStore ───────────────────────────────────────────────────────────

// FileStateStore persists one JSON file per workflow in a directory.
// It is safe for use by a single process (no cross-process locking).
type FileStateStore struct {
	Dir string
	mu  sync.Mutex
}

func NewFileStateStore(dir string) (*FileStateStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("file state store: mkdir %s: %w", dir, err)
	}
	return &FileStateStore{Dir: dir}, nil
}

func (f *FileStateStore) path(name string) string {
	return filepath.Join(f.Dir, sanitizeName(name)+".json")
}

func (f *FileStateStore) Load(_ context.Context, name string) (*ExecutionState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := os.ReadFile(f.path(name))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("file state store load: %w", err)
	}
	var s ExecutionState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("file state store parse: %w", err)
	}
	return &s, nil
}

func (f *FileStateStore) Save(_ context.Context, s ExecutionState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("file state store marshal: %w", err)
	}
	return os.WriteFile(f.path(s.WorkflowName), data, 0o644)
}

func (f *FileStateStore) List(_ context.Context) ([]ExecutionState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries, err := os.ReadDir(f.Dir)
	if err != nil {
		return nil, fmt.Errorf("file state store list: %w", err)
	}
	var out []ExecutionState
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(f.Dir, e.Name()))
		if err != nil {
			continue
		}
		var s ExecutionState
		if json.Unmarshal(data, &s) == nil {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *FileStateStore) Delete(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := os.Remove(f.path(name)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("file state store delete: %w", err)
	}
	return nil
}
