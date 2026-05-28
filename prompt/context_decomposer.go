package prompt

// DecomposerScenario is the minimal projection of an acceptance-criteria
// scenario the decomposer user-prompt template needs to render. Keeping
// this on the prompt package (rather than importing workflow.Scenario)
// avoids the workflow → prompt → workflow dependency cycle.
type DecomposerScenario struct {
	ID    string
	Given string
	When  string
	Then  []string
}

// DecomposerPrereqContext is the minimal projection of a prerequisite
// requirement's completed work that the decomposer needs to reason about
// what's already done.
type DecomposerPrereqContext struct {
	// Title and Description identify the prereq.
	Title       string
	Description string

	// FilesModified lists the files the prereq's execution touched.
	// Empty when the prereq had no observed file output.
	FilesModified []string

	// Summary is the prereq's aggregate change summary, when available.
	Summary string
}

// DecomposerPromptContext carries everything the decomposer user-prompt
// fragment needs to render. The req-executor component constructs one of
// these from the in-flight requirementExecution before calling
// Assembler.Assemble; keeping the shape on prompt/ (instead of importing
// workflow types) avoids the workflow → prompt → workflow dependency
// cycle.
//
// The decomposer's job is to partition a requirement into a DAG of
// executable nodes. The structure here mirrors what was previously
// hand-rolled inside requirement-executor's buildDecomposerPrompt;
// porting it to a typed context is the wiring fix that lets the persona
// system (system-base + tool-directive + output-format + lessons + tool
// guidance) contribute to the dispatch the same way every other
// generator does.
type DecomposerPromptContext struct {
	// RequirementTitle is the requirement the decomposer is breaking down.
	RequirementTitle string

	// RequirementDescription is the longer-form description of the
	// requirement. Empty when the upstream did not provide one.
	RequirementDescription string

	// ScopeInclude / ScopeExclude / ScopeDoNotTouch surface the plan's
	// file scope so the decomposer can choose node-level file_scope arrays
	// within the requirement's bounds.
	ScopeInclude    []string
	ScopeExclude    []string
	ScopeDoNotTouch []string

	// DependsOn surfaces completed prereq requirements so the decomposer
	// can reference files those reqs produced rather than re-emit them.
	DependsOn []DecomposerPrereqContext

	// Scenarios are the per-requirement acceptance criteria. The
	// decomposer must cover every scenario ID in at least one node's
	// scenario_ids array — enforced at parse time by the structural
	// coverage check.
	Scenarios []DecomposerScenario

	// HarnessProfiles are selected catalog profiles resolved to full
	// test-authoring details. The decomposer should allocate explicit test
	// nodes when a requirement touches one of these profiles.
	HarnessProfiles []ResolvedHarnessProfileContext

	// RetryFeedback is the prior decomposer attempt's failure reason,
	// when retrying. Empty on the first attempt. Renderer prepends this
	// to the prompt so the LLM is primed to correct the specific
	// problem before reading the requirement.
	RetryFeedback string
}
