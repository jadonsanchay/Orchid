package domain

import (
	"fmt"
	"sync"
)

// Registry manages the mapping of task names to their Task implementations.
// It is safe for concurrent use by multiple worker goroutines.
type Registry struct {
	mu    sync.RWMutex
	tasks map[string]Task
}

// NewRegistry creates a new, empty Task Registry.
func NewRegistry() *Registry {
	return &Registry{
		tasks: make(map[string]Task),
	}
}

// Register adds a new Task to the registry. 
// It returns an error if a task with the same name is already registered.
func (r *Registry) Register(t Task) error {
	if t == nil {
		return fmt.Errorf("cannot register nil task")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	name := t.Name()
	if _, exists := r.tasks[name]; exists {
		return fmt.Errorf("task already registered: %s", name)
	}

	r.tasks[name] = t
	return nil
}

// Get retrieves a Task by its registration name. 
// It returns nil if no matching task exists.
func (r *Registry) Get(name string) Task {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.tasks[name]
}
