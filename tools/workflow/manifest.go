package workflow

import (
	"context"

	"github.com/c360studio/semspec/graph"
)

// graphManifestMuted gates injection of the Knowledge Graph manifest into agent
// prompts. MUTED 2026-06-09: agent roles have no graph_* query tools (removed
// 2026-05-12 — see prompt.DefaultToolFilters), so the manifest advertised a
// queryable graph and handed out entity IDs ("e.g. <id>") that no agent could
// act on. It was injected into all 10 prompt-building components
// (~200-500 tokens/prompt plus a per-prompt graph-gateway summary fetch) for
// zero actionable benefit, and the paired graph_query "copy IDs from the
// manifest" guidance pointed at a tool no role can call. When fetchFn returns
// "", GraphManifestFragment's Condition (fetchFn()!="") excludes the fragment
// entirely — no header, no render, no HTTP. Flip to false to restore when graph
// query tools return (e.g. semstreams research_graph).
var graphManifestMuted = true

// RegistrySummaryFetchFn returns a closure suitable for GraphManifestFragment
// that fetches the formatted graph summary from the global SourceRegistry.
// Replaces the legacy ManifestClient and FederatedManifestFetchFn with a single
// path through the registry's FormatSummaryForPrompt (which handles caching,
// readiness, and multi-source aggregation internally).
func RegistrySummaryFetchFn() func() string {
	return func() string {
		if graphManifestMuted {
			return ""
		}
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
