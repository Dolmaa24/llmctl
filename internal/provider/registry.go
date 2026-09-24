package provider

import "fmt"

// Registry maps a provider identifier to its adapter instance.
type Registry struct {
	adapters map[string]Adapter
}

func NewRegistry() *Registry {
	return &Registry{adapters: make(map[string]Adapter)}
}

// Register adds an adapter under its own Name().
func (r *Registry) Register(a Adapter) {
	r.adapters[a.Name()] = a
}

// Get returns the adapter registered under name.
func (r *Registry) Get(name string) (Adapter, error) {
	a, ok := r.adapters[name]
	if !ok {
		return nil, fmt.Errorf("provider: no adapter registered for %q", name)
	}
	return a, nil
}

// All returns every registered adapter.
func (r *Registry) All() []Adapter {
	out := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a)
	}
	return out
}
