package prompt

// ArchitectPromptContext carries data for the architect user-prompt fragment.
type ArchitectPromptContext struct {
	Goal           string
	PlanContext    string
	ScopeInclude   []string
	ScopeExclude   []string
	ScopeCreate    []string
	ScopeProtected []string
	Requirements   []ExistingRequirementSummary
	// Capabilities is the analyst's capability list, rendered to the architect
	// with 0-based indices. The architect references these by capability_index
	// in component_boundaries (2026-06-07) instead of re-typing names.
	Capabilities    []CapabilityCard
	HarnessProfiles []HarnessProfileCard
	PreviousError   string
	ReviewFindings  string

	// PreviousArchitectureJSON is the prior architecture document (JSON) on a
	// revision dispatch. When non-empty, the architect revises this design to
	// address the review findings instead of rewriting from scratch (which
	// tends to re-introduce the rejected shape). Empty on the first pass.
	PreviousArchitectureJSON string
}

// HarnessProfileCard is the compact catalog projection shown to the architect.
// Full details are resolved later for decomposer/developer prompts.
type HarnessProfileCard struct {
	ID                 string
	Tier               string
	Proves             []string
	Covers             map[string][]string
	RunnerSupport      []string
	Cost               string
	Constraints        []string
	RequiredAssertions []string
}

// ActorInfo is a lightweight view of an actor for prompt injection. Used by
// architecture context rendering.
type ActorInfo struct {
	Name     string
	Type     string
	Triggers []string
}

// IntegrationInfo is a lightweight view of an integration point for prompt
// injection.
type IntegrationInfo struct {
	Name      string
	Direction string
	Protocol  string
}

// ComponentInfo is a lightweight view of an architecture component for prompt
// injection. Mirrors workflow.ComponentDef without pulling the workflow type
// into the prompt package (Plan B consolidation: pre-render in component code).
type ComponentInfo struct {
	Name                string
	Responsibility      string
	UpstreamRefs        []string
	ImplementationFiles []string
	Capabilities        []string
}

// UpstreamResolutionInfo is a lightweight view of an architect-resolved
// external dependency. Mirrors workflow.UpstreamResolution. The Coordinate +
// APIs are the load-bearing facts the developer needs so it never re-discovers
// (or hallucinates) a dependency the architect already resolved.
type UpstreamResolutionInfo struct {
	Name           string
	Coordinate     string
	SourceRef      string
	ResolutionKind string // maven_central | source_build | kmp_multiplatform | unresolved
	Role           string // build_dep | runtime_dep | integration_target
	UsedBy         []string
	APIs           []APISurfaceInfo
}

// APISurfaceInfo is a lightweight view of a resolved external symbol the dev
// integrates against. Mirrors workflow.APISurface.
type APISurfaceInfo struct {
	Symbol    string
	Import    string
	Artifact  string
	Kind      string
	Signature string
	Lifecycle string
	Notes     string
	Citation  string
}

// ArchitectureProjection is the full architecture surface available for
// projection into any role's prompt. Callers populate only the sections the
// role needs; empty sections are omitted from the rendered output. This is the
// single faithful graph→role projection — roles draw the architecture facts
// their BMAD contract requires instead of each re-projecting through a lossy
// hand-rolled lens.
type ArchitectureProjection struct {
	Actors       []ActorInfo
	Integrations []IntegrationInfo
	Components   []ComponentInfo
	Upstreams    []UpstreamResolutionInfo
}
