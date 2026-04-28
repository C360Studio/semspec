package prompt

import (
	"fmt"
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

// Register adds a single fragment to the registry. Panics if a
// CategoryUserPrompt fragment would create a second user-prompt for any role —
// each role must have exactly one user-prompt template, and the loud failure
// at registration is the structural guarantee preventing the dual-pattern
// orphaning that bit dial #1 (where editing the wrong user-prompt builder
// silently shipped).
func (r *Registry) Register(f *Fragment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assertUserPromptUniqueLocked(f)
	r.fragments[f.ID] = f
}

// RegisterAll adds multiple fragments to the registry. Same uniqueness
// guarantee as Register: the call panics if any incoming fragment creates a
// duplicate user-prompt template for a role.
func (r *Registry) RegisterAll(fragments ...*Fragment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, f := range fragments {
		r.assertUserPromptUniqueLocked(f)
		r.fragments[f.ID] = f
	}
}

// assertUserPromptUniqueLocked panics if registering f would put two
// CategoryUserPrompt fragments on the same Role. Replacing an existing fragment
// (same ID) is allowed — that's a normal update. Caller must hold r.mu write
// lock.
func (r *Registry) assertUserPromptUniqueLocked(f *Fragment) {
	if f == nil || f.Category != CategoryUserPrompt {
		return
	}
	for _, role := range f.Roles {
		for _, existing := range r.fragments {
			if existing.ID == f.ID {
				continue // updating the same fragment is fine
			}
			if existing.Category != CategoryUserPrompt {
				continue
			}
			if !slices.Contains(existing.Roles, role) {
				continue
			}
			panic(fmt.Sprintf(
				"prompt.Registry: duplicate CategoryUserPrompt for role %q — fragments %q and %q both claim the user-prompt slot. Each role may have at most one user-prompt template.",
				role, existing.ID, f.ID,
			))
		}
	}
}

// UserPromptFragmentFor returns the CategoryUserPrompt fragment registered for
// the given role, or nil if none exists. Used by the assembler; exposed so
// tests can introspect.
func (r *Registry) UserPromptFragmentFor(role Role) *Fragment {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, f := range r.fragments {
		if f.Category != CategoryUserPrompt {
			continue
		}
		if slices.Contains(f.Roles, role) {
			return f
		}
	}
	return nil
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
