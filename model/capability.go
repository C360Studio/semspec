// Package model provides capability-based model selection for workflow tasks.
// Instead of hardcoding model names, commands specify capabilities (planning, writing, coding)
// and the registry resolves them to available models with fallback chains.
package model

// Capability represents a semantic capability for model selection.
// Instead of specifying "claude-sonnet", users specify "writing" or "planning".
type Capability string

const (
	// CapabilityPlanning is for high-level reasoning, plan drafting.
	CapabilityPlanning Capability = "planning"

	// CapabilityWriting is for documentation, plans, specifications.
	CapabilityWriting Capability = "writing"

	// CapabilityCoding is for code generation, implementation.
	CapabilityCoding Capability = "coding"

	// CapabilityReviewing is for code review, quality analysis.
	CapabilityReviewing Capability = "reviewing"

	// CapabilityPlanReview is for strategic plan assessment (completeness, SOP compliance).
	CapabilityPlanReview Capability = "plan_review"

	// CapabilityArchitecture is for technology choices, component boundaries, deployment topology.
	CapabilityArchitecture Capability = "architecture"

	// CapabilityRequirementGeneration is for generating requirements from plans.
	CapabilityRequirementGeneration Capability = "requirement_generation"

	// CapabilityScenarioGeneration is for generating scenarios from requirements.
	CapabilityScenarioGeneration Capability = "scenario_generation"

	// CapabilityQA is for integration/e2e test execution and cross-requirement validation.
	CapabilityQA Capability = "qa"

	// CapabilityFast is for quick responses, simple tasks.
	CapabilityFast Capability = "fast"

	// CapabilityLessonDecomposition is for the rare reflection step that
	// reads a developer trajectory and reviewer verdict and produces an
	// audited lesson (ADR-033 Phase 2+). Benefits from a smarter model
	// than the executing role; deployments typically point this at the
	// same endpoint as CapabilityReviewing.
	CapabilityLessonDecomposition Capability = "lesson_decomposition"
)

// RoleCapabilities maps workflow roles to their default capability.
// Used when no explicit capability or model is specified.
// BMAD-aligned roles: planner, architect, developer, reviewer, plan-reviewer, qa
var RoleCapabilities = map[string]Capability{
	"general":               CapabilityFast,
	"planner":               CapabilityPlanning,
	"requirement-generator": CapabilityRequirementGeneration,
	"scenario-generator":    CapabilityScenarioGeneration,
	"architect":             CapabilityArchitecture,
	"validator":             CapabilityCoding,
	"developer":             CapabilityCoding,
	"reviewer":              CapabilityReviewing,
	"plan-reviewer":         CapabilityPlanReview,
	"qa":                    CapabilityQA,
	"coordinator":           CapabilityPlanning,
	"writer":                CapabilityWriting,
	"lesson-decomposer":     CapabilityLessonDecomposition,
}

// CapabilityForRole returns the default capability for a given role.
// Returns CapabilityWriting as fallback for unknown roles.
func CapabilityForRole(role string) Capability {
	if capVal, ok := RoleCapabilities[role]; ok {
		return capVal
	}
	return CapabilityWriting
}

// IsValid checks if a capability string is a known capability.
func (c Capability) IsValid() bool {
	switch c {
	case CapabilityPlanning, CapabilityWriting, CapabilityCoding, CapabilityReviewing,
		CapabilityPlanReview, CapabilityArchitecture, CapabilityRequirementGeneration,
		CapabilityScenarioGeneration, CapabilityQA, CapabilityFast,
		CapabilityLessonDecomposition:
		return true
	}
	return false
}

// String returns the string representation of the capability.
func (c Capability) String() string {
	return string(c)
}

// ParseCapability converts a string to a Capability, returning empty for invalid values.
func ParseCapability(s string) Capability {
	capVal := Capability(s)
	if capVal.IsValid() {
		return capVal
	}
	return ""
}
