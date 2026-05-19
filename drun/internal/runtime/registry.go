package runtime

import (
	"context"
	"fmt"
	goruntime "runtime"
	"sync"
)

// Registry manages available runtime backends and provides backend selection.
type Registry struct {
	mu        sync.RWMutex
	backends  map[BackendType]RuntimeBackend
	preferred BackendType
}

// NewRegistry creates a new backend registry.
func NewRegistry() *Registry {
	return &Registry{
		backends: make(map[BackendType]RuntimeBackend),
	}
}

// Register adds a backend to the registry.
func (r *Registry) Register(t BackendType, b RuntimeBackend) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends[t] = b
}

// Unregister removes a backend from the registry.
func (r *Registry) Unregister(t BackendType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.backends, t)
}

// Get returns a specific backend by type.
func (r *Registry) Get(t BackendType) (RuntimeBackend, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.backends[t]
	return b, ok
}

// GetOrError returns a backend or an error if not found.
func (r *Registry) GetOrError(t BackendType) (RuntimeBackend, error) {
	b, ok := r.Get(t)
	if !ok {
		return nil, fmt.Errorf("backend %q not registered", t)
	}
	if !b.Available() {
		return nil, fmt.Errorf("backend %q is not available on this system", t)
	}
	return b, nil
}

// SetPreferred sets the preferred backend type for auto-selection.
func (r *Registry) SetPreferred(t BackendType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.preferred = t
}

// AutoSelect returns the best available backend based on platform and availability.
// Priority order:
// - User-preferred backend (if set and available)
// - Linux: Firecracker > Docker > Sandbox
// - macOS: VZ > Docker > Sandbox
// - Other: Docker > Sandbox
func (r *Registry) AutoSelect(ctx context.Context) (RuntimeBackend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check preferred backend first
	if r.preferred != "" {
		if b, ok := r.backends[r.preferred]; ok && b.Available() {
			return b, nil
		}
	}

	// Platform-specific priority
	var priority []BackendType
	switch goruntime.GOOS {
	case "linux":
		priority = []BackendType{BackendFirecracker, BackendDocker, BackendSandbox}
	case "darwin":
		priority = []BackendType{BackendVZ, BackendDocker, BackendSandbox}
	default:
		priority = []BackendType{BackendDocker, BackendSandbox}
	}

	// Find first available backend
	for _, t := range priority {
		if b, ok := r.backends[t]; ok && b.Available() {
			return b, nil
		}
	}

	// Return any available backend as last resort
	for _, b := range r.backends {
		if b.Available() {
			return b, nil
		}
	}

	return nil, fmt.Errorf("no available runtime backend found")
}

// List returns all registered backend types.
func (r *Registry) List() []BackendType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]BackendType, 0, len(r.backends))
	for t := range r.backends {
		types = append(types, t)
	}
	return types
}

// ListAvailable returns all available (usable) backend types.
func (r *Registry) ListAvailable() []BackendType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]BackendType, 0, len(r.backends))
	for t, b := range r.backends {
		if b.Available() {
			types = append(types, t)
		}
	}
	return types
}

// Close closes all registered backends.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for _, b := range r.backends {
		if err := b.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing backends: %v", errs)
	}
	return nil
}
