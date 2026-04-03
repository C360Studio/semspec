package workflow

import (
	"context"

	"github.com/c360studio/semspec/graph"
)

// RegistrySummaryFetchFn returns a closure suitable for GraphManifestFragment
// that fetches the formatted graph summary from the global SourceRegistry.
// Replaces the legacy ManifestClient and FederatedManifestFetchFn with a single
// path through the registry's FormatSummaryForPrompt (which handles caching,
// readiness, and multi-source aggregation internally).
func RegistrySummaryFetchFn() func() string {
	return func() string {
		reg := graph.GlobalSources()
		if reg == nil {
			return ""
		}
		// context.Background is acceptable here: the GraphManifestFragment interface
		// requires func() string (no ctx param). FormatSummaryForPrompt uses the
		// registry's 5s HTTP client timeout and 5-min summary cache, so the actual
		// network call is bounded and rare.
		return reg.FormatSummaryForPrompt(context.Background())
	}
}
