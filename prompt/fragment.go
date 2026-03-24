// Package prompt provides fragment-based prompt composition with provider-aware formatting.
// It replaces hardcoded prompt string functions with composable, gated fragments
// that support domain skinning, dynamic tool awareness, and provider-optimized output.
package prompt

// Category controls assembly ordering. Fragments are sorted by category
// first, then by priority within category. Lower values appear earlier in the prompt.
type Category int

const (
	// CategorySystemBase is the domain identity fragment ("You are a developer...").
	CategorySystemBase Category = 0
	// CategoryToolDirective contains mandatory tool-call instructions.
	CategoryToolDirective Category = 100
	// CategoryProviderHints contains provider-specific formatting instructions.
	CategoryProviderHints Category = 200
	// CategoryBehavioralGate contains mandatory behavioral rules injected early
	// (exploration gates, anti-description directives, structural checklist, tool budget).
	CategoryBehavioralGate Category = 275
	// CategoryRoleContext contains role-specific behavioral instructions.
	CategoryRoleContext Category = 300
	// CategoryKnowledgeManifest contains a summary of indexed knowledge graph contents.
	CategoryKnowledgeManifest Category = 325
	// CategoryPeerFeedback contains error trend warnings from peer reviews.
	CategoryPeerFeedback Category = 350
	// CategoryDomainContext contains domain-specific knowledge and conventions.
	CategoryDomainContext Category = 400
	// CategoryToolGuidance contains advisory guidance on when to use which tool.
	CategoryToolGuidance Category = 500
	// CategoryOutputFormat contains output format instructions (JSON structure, etc).
	CategoryOutputFormat Category = 600
	// CategoryGapDetection contains gap detection instructions.
	CategoryGapDetection Category = 700
)

// Provider identifies an LLM provider for formatting purposes.
type Provider string

// LLM provider constants for prompt formatting.
const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
	ProviderOllama    Provider = "ollama"
	ProviderGoogle    Provider = "google"
)

// Role aligns with model.RoleCapabilities keys.
type Role string

// Agent role constants aligned with model.RoleCapabilities keys.
const (
	RolePlanner              Role = "planner"
	RoleDeveloper            Role = "developer" // Deprecated: use RoleBuilder for implementation
	RoleBuilder              Role = "builder"
	RoleTester               Role = "tester"
	RoleValidator            Role = "validator"
	RoleReviewer             Role = "reviewer"
	RolePlanReviewer         Role = "plan-reviewer"
	RoleTaskReviewer         Role = "task-reviewer"
	RoleRequirementGenerator Role = "requirement-generator"
	RoleScenarioGenerator    Role = "scenario-generator"
	RoleTaskGenerator        Role = "task-generator"
	RolePlanCoordinator      Role = "plan-coordinator"
	RoleCoordinator          Role = "coordinator"
	RoleScenarioReviewer     Role = "scenario-reviewer"
	RolePlanRollupReviewer   Role = "plan-rollup-reviewer"
)

// Fragment is the atomic unit of prompt composition.
// Fragments are gated by roles, providers, and optional runtime conditions.
// Only matching fragments are included in the assembled prompt.
type Fragment struct {
	// ID uniquely identifies this fragment (e.g., "software.developer.system-base").
	ID string

	// Category controls ordering in the assembled prompt.
	Category Category

	// Priority controls ordering within the same category (lower = first).
	Priority int

	// Content is the static text content for this fragment.
	// Ignored when ContentFunc is set.
	Content string

	// ContentFunc generates content dynamically from the AssemblyContext.
	// Takes precedence over Content when set.
	ContentFunc func(*AssemblyContext) string

	// Roles limits this fragment to specific roles. Empty means all roles.
	Roles []Role

	// Providers limits this fragment to specific providers. Empty means all providers.
	Providers []Provider

	// Condition is an optional runtime predicate. When non-nil, the fragment
	// is included only when Condition returns true.
	Condition func(*AssemblyContext) bool
}

// ProviderStyle controls formatting conventions per LLM provider.
type ProviderStyle struct {
	Provider       Provider
	PreferXML      bool // Anthropic: wrap sections in XML tags
	PreferMarkdown bool // OpenAI/Ollama: markdown headers
}

// DefaultProviderStyles returns the standard formatting conventions.
func DefaultProviderStyles() map[Provider]ProviderStyle {
	return map[Provider]ProviderStyle{
		ProviderAnthropic: {Provider: ProviderAnthropic, PreferXML: true},
		ProviderOpenAI:    {Provider: ProviderOpenAI, PreferMarkdown: true},
		ProviderOllama:    {Provider: ProviderOllama, PreferMarkdown: true},
		ProviderGoogle:    {Provider: ProviderGoogle, PreferMarkdown: true},
	}
}
