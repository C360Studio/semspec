package model

// CapabilityResolver is the minimum interface ResolveModel needs from a
// model registry. Both semspec's *Registry (via stringResolverAdapter
// below) and semstreams' RegistryReader satisfy this directly.
//
// Defining a tiny local interface here avoids importing semstreams from
// the semspec/model package while still letting downstream components
// (which use the semstreams RegistryReader) thread the same resolver.
type CapabilityResolver interface {
	Resolve(capability string) string
}

// stringResolverAdapter wraps semspec's *Registry (whose Resolve takes
// the typed Capability) so it satisfies CapabilityResolver.
type stringResolverAdapter struct {
	r *Registry
}

func (a stringResolverAdapter) Resolve(capability string) string {
	return a.r.Resolve(Capability(capability))
}

// AsCapabilityResolver returns reg adapted to the CapabilityResolver
// interface. Returns nil when reg is nil so callers can pass through.
func AsCapabilityResolver(reg *Registry) CapabilityResolver {
	if reg == nil {
		return nil
	}
	return stringResolverAdapter{r: reg}
}

// ResolveModel returns the model endpoint name for a dispatch by
// capability with an optional component-level override.
//
// The pattern: every dispatch site declares the capability it needs
// (CapabilityCoding, CapabilityReviewing, CapabilityTaskDecomposition,
// etc.) and the registry returns the deployment-configured endpoint for
// that capability. Components MAY also expose a `model` config field as
// a hard override — handy for fixtures, debugging a single role on a
// specific endpoint, or pinning during a regression hunt.
//
// Order of precedence:
//  1. Explicit override (non-empty) — operator pinned this site
//  2. Registry resolution by capability — normal path
//  3. Empty string — no registry available; caller decides what to do
//     (typically dispatch with an empty Model field and let the dispatch
//     stack apply its own default)
//
// This function is the single seam tested by resolver_test.go. Adding
// branches here without table-test coverage is a regression magnet — the
// hardcoded `agentic-dispatch.default_model` bug shipped in fcf79f0 was
// caught precisely because the dispatch sites were not routing through a
// pure, testable resolver.
func ResolveModel(reg CapabilityResolver, override string, capability Capability) string {
	if override != "" {
		return override
	}
	if reg == nil {
		return ""
	}
	return reg.Resolve(string(capability))
}
