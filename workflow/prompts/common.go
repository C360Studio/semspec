package prompts

// GapDetectionInstructions provides shared instructions for LLMs to signal knowledge gaps.
// This is appended to all workflow prompts.
const GapDetectionInstructions = `
## Knowledge Gaps

If you encounter any uncertainty, unknown information, or need clarification during document generation, signal this with a <gap> block. DO NOT guess or make assumptions about uncertain information.

**When to use gaps:**
- Missing API or interface details
- Unclear requirements or specifications
- Architecture decisions that need stakeholder input
- Security considerations requiring expert review
- Performance trade-offs needing team discussion

**Gap format:**
` + "```xml" + `
<gap>
  <topic>category.subcategory</topic>
  <question>Your specific question here?</question>
  <context>Why you need this information</context>
  <urgency>normal</urgency>
</gap>
` + "```" + `

**Topic categories:**
- api.* - API/interface questions (e.g., api.semstreams, api.authentication)
- architecture.* - Design decisions (e.g., architecture.database, architecture.messaging)
- requirements.* - Requirements clarification (e.g., requirements.auth, requirements.ux)
- security.* - Security considerations (e.g., security.tokens, security.encryption)
- performance.* - Performance trade-offs (e.g., performance.caching, performance.indexing)

**Urgency levels:**
- low - Nice to know, can proceed with reasonable assumption
- normal - Should be answered before implementation
- high - Important decision, should be answered soon
- blocking - Cannot proceed without this information

Include gaps inline where they're relevant. The workflow will pause until critical gaps are answered.
`

// WorkflowRoles defines the available workflow writer roles.
var WorkflowRoles = map[string]string{
	"proposal-writer": "Generates proposal documents from feature descriptions",
	"design-writer":   "Generates technical design documents from proposals",
	"spec-writer":     "Generates specification documents with GIVEN/WHEN/THEN scenarios",
	"tasks-writer":    "Generates task breakdowns from specs",
}

// RoleForStep returns the role name for a workflow step.
func RoleForStep(step string) string {
	switch step {
	case "propose", "proposal":
		return ProposalWriterRole()
	case "design":
		return DesignWriterRole()
	case "spec", "specification":
		return SpecWriterRole()
	case "tasks", "task":
		return TasksWriterRole()
	default:
		return ""
	}
}

// PromptForStep returns the appropriate prompt for a workflow step.
// All prompts include gap detection instructions.
func PromptForStep(step, workflowSlug, title, description string) string {
	var basePrompt string
	switch step {
	case "propose", "proposal":
		basePrompt = ProposalWriterPrompt(workflowSlug, description)
	case "design":
		basePrompt = DesignWriterPrompt(workflowSlug, title)
	case "spec", "specification":
		basePrompt = SpecWriterPrompt(workflowSlug, title)
	case "tasks", "task":
		basePrompt = TasksWriterPrompt(workflowSlug, title)
	default:
		return ""
	}
	return WithGapDetection(basePrompt)
}

// NextStep returns the next workflow step after the given step.
func NextStep(currentStep string) string {
	switch currentStep {
	case "propose", "proposal":
		return "design"
	case "design":
		return "spec"
	case "spec", "specification":
		return "tasks"
	case "tasks", "task":
		return "" // Final step
	default:
		return ""
	}
}

// StepDocument returns the document name for a workflow step.
func StepDocument(step string) string {
	switch step {
	case "propose", "proposal":
		return "proposal"
	case "design":
		return "design"
	case "spec", "specification":
		return "spec"
	case "tasks", "task":
		return "tasks"
	default:
		return ""
	}
}

// WithGapDetection appends gap detection instructions to a prompt.
func WithGapDetection(prompt string) string {
	return prompt + "\n" + GapDetectionInstructions
}
