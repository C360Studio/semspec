package prompt

// StoryPreparerPromptContext carries data for the story-preparer user-prompt
// fragment (ADR-043 Move 3). Sarah receives the plan goal/context/scope, the
// analyst's capabilities, the architect's component definitions (with their
// implementation_files and capability mappings), and the requirement
// summaries. She emits Stories with intra-story Task checklists.
//
// The component-aware fields (ArchitectureComponents) are what distinguish
// Sarah's job from Bob's (scenario-generator) and John's (requirement-gen) —
// Sarah is the only persona that needs the architect's file-and-capability
// mapping as primary input, because story-shaping IS the capability →
// component → file resolution at the dispatch-unit granularity.
type StoryPreparerPromptContext struct {
	// PlanTitle, PlanGoal, PlanContext provide the plan-level intent for
	// story-shaping decisions (when to shard, when to bundle).
	PlanTitle   string
	PlanGoal    string
	PlanContext string

	// Capabilities is the analyst's classified capabilities. Sarah uses
	// these as the surface-level grouping when deciding whether a
	// requirement should fan out into multiple stories.
	Capabilities []StoryPreparerCapability

	// ArchitectureComponents projects the architect's ComponentDef set
	// onto the fields Sarah needs to make sharding decisions: which
	// components exist, which capabilities they implement, and which
	// files they own. This is the load-bearing input — Sarah's
	// readiness gate requires that every Story's files_owned be the
	// union of its selected components' implementation_files.
	ArchitectureComponents []StoryPreparerComponent

	// Requirements is the John-emitted requirement set Sarah is sharding.
	// Each entry carries the requirement's capability link so Sarah can
	// match component selection against capability ownership.
	Requirements []ExistingRequirementSummary

	// PreviousError carries the prior parse / validation failure when
	// this is a retry dispatch. Empty on first attempt.
	PreviousError string

	// ReviewFindings carries plan-reviewer R3 findings from a prior
	// review round, injected so Sarah can address them on regen.
	ReviewFindings string
}

// StoryPreparerCapability is the projection of workflow.Capability into the
// fields Sarah's prompt actually displays. Only Name and Description are
// load-bearing here — Sarah doesn't need the full capability lifecycle or
// depends-on graph at story-shaping time.
type StoryPreparerCapability struct {
	Name        string
	Description string
}

// StoryPreparerComponent is the projection of workflow.ComponentDef into
// the fields Sarah uses. ImplementationFiles and Capabilities are the
// inputs to Sarah's union-of-files computation; Responsibility provides
// the prose context that helps Sarah reason about whether a story should
// span multiple components.
type StoryPreparerComponent struct {
	Name                string
	Responsibility      string
	ImplementationFiles []string
	Capabilities        []string
}
