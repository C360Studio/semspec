package prompt

import (
	"slices"
	"sort"
	"sync"
)

// Registry manages prompt fragments and provider styles.
// Thread-safe for concurrent access.
type Registry struct {
	mu        sync.RWMutex
	fragments map[string]*Fragment
	styles    map[Provider]ProviderStyle
}

// NewRegistry creates an empty prompt registry with default provider styles.
func NewRegistry() *Registry {
	return &Registry{
		fragments: make(map[string]*Fragment),
		styles:    DefaultProviderStyles(),
	}
}

// Register adds a single fragment to the registry.
func (r *Registry) Register(f *Fragment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fragments[f.ID] = f
}

// RegisterAll adds multiple fragments to the registry.
func (r *Registry) RegisterAll(fragments ...*Fragment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, f := range fragments {
		r.fragments[f.ID] = f
	}
}

// GetFragmentsForContext returns matching fragments sorted by category then priority.
func (r *Registry) GetFragmentsForContext(ctx *AssemblyContext) []*Fragment {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matched []*Fragment
	for _, f := range r.fragments {
		if r.fragmentMatches(f, ctx) {
			matched = append(matched, f)
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		if matched[i].Category != matched[j].Category {
			return matched[i].Category < matched[j].Category
		}
		return matched[i].Priority < matched[j].Priority
	})

	return matched
}

// GetStyle returns formatting conventions for a provider.
func (r *Registry) GetStyle(provider Provider) ProviderStyle {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.styles[provider]
}

// FragmentCount returns the total number of registered fragments.
func (r *Registry) FragmentCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.fragments)
}

// fragmentMatches checks if a fragment is applicable for the given context.
func (r *Registry) fragmentMatches(f *Fragment, ctx *AssemblyContext) bool {
	// Role gating: context role must be in fragment's role list
	if len(f.Roles) > 0 && !slices.Contains(f.Roles, ctx.Role) {
		return false
	}

	// Provider gating
	if len(f.Providers) > 0 && !slices.Contains(f.Providers, ctx.Provider) {
		return false
	}

	// Runtime condition
	if f.Condition != nil && !f.Condition(ctx) {
		return false
	}

	return true
}
