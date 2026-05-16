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

	// CapabilityTaskDecomposition is for breaking a requirement into a
	// DAG of executable nodes (requirement-executor's decomposer step).
	// Benefits from planner-class reasoning rather than coder-class
	// generation; deployments typically point this at the same endpoint
	// as CapabilityPlanning.
	CapabilityTaskDecomposition Capability = "task_decomposition"

	// CapabilityPlanWedgeRecovery is the ADR-037 stage-1 phase-local
	// recovery for plan-manager escalations (review revision cap reached).
	// A manager-role agent reads the wedged plan + last feedback +
	// trajectory and picks a bounded RecoveryAction.
	CapabilityPlanWedgeRecovery Capability = "plan_wedge_recovery"

	// CapabilityExecutionWedgeRecovery is the ADR-037 stage-1 phase-local
	// recovery for execution-manager escalations (TDD cycle exhaustion,
	// iter=N tool loops). Same shape as plan-wedge-recovery, scoped to
	// the developer's trajectory + reviewer feedback.
	CapabilityExecutionWedgeRecovery Capability = "execution_wedge_recovery"

	// CapabilityCoordinatorRecovery is the ADR-037 stage-2 cross-phase
	// recovery handled by the (not-yet-built) coordinator component.
	// Reserved here so capability resolution doesn't surprise stage-2
	// when it lands.
	CapabilityCoordinatorRecovery Capability = "coordinator_recovery"

	// CapabilityResearch is the dispatch target for the researcher
	// sub-agent the developer's research() tool spawns. Single-shot
	// upstream-API-surface investigation: bash/http_request/web_search
	// read-only, terminal answer_research, no recursive delegation.
	// Defaults to a cheap fast-synthesis model (gemini-flash class).
	// See project_research_tool_plan_2026_05_14.
	CapabilityResearch Capability = "research"
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
	"code-reviewer":         CapabilityReviewing,
	"requirement-reviewer":  CapabilityReviewing,
	"plan-reviewer":         CapabilityPlanReview,
	"qa":                    CapabilityQA,
	"coordinator":           CapabilityPlanning,
	"writer":                CapabilityWriting,
	"lesson-decomposer":     CapabilityLessonDecomposition,
	"task-decomposer":       CapabilityTaskDecomposition,
	"recovery-agent":        CapabilityExecutionWedgeRecovery, // overridden per-dispatch by recovery-agent based on layer
	"researcher":            CapabilityResearch,
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
		CapabilityLessonDecomposition, CapabilityTaskDecomposition,
		CapabilityPlanWedgeRecovery, CapabilityExecutionWedgeRecovery, CapabilityCoordinatorRecovery,
		CapabilityResearch:
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
