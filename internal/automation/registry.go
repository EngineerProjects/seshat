package automation

import (
	"fmt"
	"sort"
	"sync"
)

// Registry is a thread-safe catalog of named workflows.
// Workflows are registered once at startup and looked up by name at runtime.
type Registry struct {
	mu        sync.RWMutex
	workflows map[string]Workflow
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{workflows: make(map[string]Workflow)}
}

// Register adds w to the registry. Returns an error if a workflow with the
// same name is already registered.
func (r *Registry) Register(w Workflow) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.workflows[w.Name()]; exists {
		return fmt.Errorf("registry: workflow %q already registered", w.Name())
	}
	r.workflows[w.Name()] = w
	return nil
}

// MustRegister calls Register and panics on error.
// Intended for package-level init blocks where duplicate registration is a
// programming error.
func (r *Registry) MustRegister(w Workflow) {
	if err := r.Register(w); err != nil {
		panic(err)
	}
}

// Get returns the workflow registered under name, or (nil, false) if absent.
func (r *Registry) Get(name string) (Workflow, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w, ok := r.workflows[name]
	return w, ok
}

// List returns all registered workflows sorted by name.
func (r *Registry) List() []Workflow {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Workflow, 0, len(r.workflows))
	for _, w := range r.workflows {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}

// Names returns all registered workflow names sorted alphabetically.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.workflows))
	for name := range r.workflows {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Unregister removes w from the registry. No-op if the name is not present.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.workflows, name)
}

// Len returns the number of registered workflows.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.workflows)
}
