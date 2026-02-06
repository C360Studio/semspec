package prompts

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
func PromptForStep(step, workflowSlug, title, description string) string {
	switch step {
	case "propose", "proposal":
		return ProposalWriterPrompt(workflowSlug, description)
	case "design":
		return DesignWriterPrompt(workflowSlug, title)
	case "spec", "specification":
		return SpecWriterPrompt(workflowSlug, title)
	case "tasks", "task":
		return TasksWriterPrompt(workflowSlug, title)
	default:
		return ""
	}
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
