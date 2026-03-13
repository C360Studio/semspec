package workflow

import "github.com/c360studio/semstreams/vocabulary"

// Namespace is the base IRI prefix for workflow vocabulary terms.
const Namespace = "https://semspec.dev/ontology/workflow/"

// Core execution identity predicates.
// These predicates classify and identify workflow execution entities.
const (
	// Type is the workflow execution type discriminator.
	// Values: "plan-review", "phase-review", "task-review", "task-execution",
	//         "scenario-execution", "coordination", "cascade"
	Type = "workflow.execution.type"

	// Phase is the current lifecycle phase of the execution.
	// Values: "generating", "planned", "reviewing", "approved", "escalated", "error"
	Phase = "workflow.execution.phase"

	// Status is the terminal execution status, set by rules only.
	// Values: "completed", "failed", "escalated"
	Status = "workflow.execution.status"

	// Slug is the plan slug identifier for this execution.
	Slug = "workflow.execution.slug"

	// Title is the human-readable execution title.
	Title = "workflow.execution.title"

	// Description is a longer description of this execution.
	Description = "workflow.execution.description"
)

// Execution tracking predicates.
// These predicates record runtime counters and correlation identifiers.
const (
	// Iteration is the current retry count (0-based).
	Iteration = "workflow.execution.iteration"

	// MaxIterations is the maximum allowed retry count before escalation.
	MaxIterations = "workflow.execution.max_iterations"

	// Prompt is the initial prompt text that initiated this execution.
	Prompt = "workflow.execution.prompt"

	// TraceID is the correlation trace ID for cross-component observability.
	TraceID = "workflow.execution.trace_id"

	// ErrorReason is a human-readable description of why the execution failed.
	ErrorReason = "workflow.execution.error_reason"
)

// Review-specific predicates.
// These predicates capture the inputs and outputs of plan and phase review loops.
const (
	// PlanContent is the raw JSON plan content received from the plan generator.
	PlanContent = "workflow.review.plan_content"

	// Verdict is the reviewer's decision.
	// Values: "approved", "rejected"
	Verdict = "workflow.review.verdict"

	// Summary is the reviewer's assessment narrative.
	Summary = "workflow.review.summary"

	// Findings is the structured findings payload as a JSON string.
	Findings = "workflow.review.findings"

	// EscalationReason explains why this review was escalated to a human.
	EscalationReason = "workflow.review.escalation_reason"
)

// Task-execution-specific predicates.
// These predicates track the inputs, outputs, and feedback for task execution loops.
const (
	// FilesModified is a JSON array of file paths modified during task execution.
	FilesModified = "workflow.task.files_modified"

	// ValidationPassed indicates whether post-execution validation succeeded.
	// Values: "true", "false"
	ValidationPassed = "workflow.task.validation_passed"

	// Feedback is the accumulated feedback text from validation or review failures.
	Feedback = "workflow.task.feedback"

	// RejectionType classifies the task rejection to guide retry strategy.
	// Values: "fixable", "misscoped", "architectural", "too_big"
	RejectionType = "workflow.task.rejection_type"
)

// Scenario-execution-specific predicates.
// These predicates describe the structure and outcome of scenario execution.
const (
	// NodeCount is the number of DAG nodes in this scenario's execution plan.
	NodeCount = "workflow.scenario.node_count"

	// FailureReason explains why a scenario execution failed.
	FailureReason = "workflow.scenario.failure_reason"
)

// Cascade-specific predicates.
// These predicates record impact metrics produced by ChangeProposal cascade logic.
const (
	// CascadeAffectedRequirements is the count of requirements affected by a cascade.
	CascadeAffectedRequirements = "workflow.cascade.affected_requirements"

	// CascadeAffectedScenarios is the count of scenarios affected by a cascade.
	CascadeAffectedScenarios = "workflow.cascade.affected_scenarios"

	// CascadeTasksDirtied is the count of tasks marked dirty by a cascade.
	CascadeTasksDirtied = "workflow.cascade.tasks_dirtied"
)

// Relationship predicates.
// Object values are 6-part entity IDs — these predicates create graph edges.
const (
	// RelPlan links an execution entity to its associated plan entity.
	RelPlan = "workflow.relation.plan"

	// RelTask links an execution entity to its associated task entity.
	RelTask = "workflow.relation.task"

	// RelScenario links an execution entity to its associated scenario entity.
	RelScenario = "workflow.relation.scenario"

	// RelProject links an execution entity to its associated project entity.
	RelProject = "workflow.relation.project"

	// RelLoop links an execution entity to its agentic loop entity.
	RelLoop = "workflow.relation.loop"

	// RelRequirement links a cascade entity to an affected requirement entity.
	RelRequirement = "workflow.relation.requirement"
)

func init() {
	registerExecutionPredicates()
	registerTrackingPredicates()
	registerReviewPredicates()
	registerTaskPredicates()
	registerScenarioPredicates()
	registerCascadePredicates()
	registerRelationPredicates()
}

func registerExecutionPredicates() {
	vocabulary.Register(Type,
		vocabulary.WithDescription("Workflow execution type: plan-review, phase-review, task-review, task-execution, scenario-execution, coordination, cascade"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"executionType"))

	vocabulary.Register(Phase,
		vocabulary.WithDescription("Current lifecycle phase: generating, planned, reviewing, approved, escalated, error"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"phase"))

	vocabulary.Register(Status,
		vocabulary.WithDescription("Terminal execution status set by rules: completed, failed, escalated"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"status"))

	vocabulary.Register(Slug,
		vocabulary.WithDescription("Plan slug identifier for this execution"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"slug"))

	vocabulary.Register(Title,
		vocabulary.WithDescription("Human-readable execution title"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(vocabulary.DcTitle))

	vocabulary.Register(Description,
		vocabulary.WithDescription("Longer description of this execution"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"description"))
}

func registerTrackingPredicates() {
	vocabulary.Register(Iteration,
		vocabulary.WithDescription("Current retry count (0-based)"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"iteration"))

	vocabulary.Register(MaxIterations,
		vocabulary.WithDescription("Maximum allowed retry count before escalation"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"maxIterations"))

	vocabulary.Register(Prompt,
		vocabulary.WithDescription("Initial prompt text that initiated this execution"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"prompt"))

	vocabulary.Register(TraceID,
		vocabulary.WithDescription("Correlation trace ID for cross-component observability"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"traceID"))

	vocabulary.Register(ErrorReason,
		vocabulary.WithDescription("Human-readable description of why the execution failed"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"errorReason"))
}

func registerReviewPredicates() {
	vocabulary.Register(PlanContent,
		vocabulary.WithDescription("Raw JSON plan content received from the plan generator"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"planContent"))

	vocabulary.Register(Verdict,
		vocabulary.WithDescription("Reviewer decision: approved or rejected"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"verdict"))

	vocabulary.Register(Summary,
		vocabulary.WithDescription("Reviewer assessment narrative"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"summary"))

	vocabulary.Register(Findings,
		vocabulary.WithDescription("Structured findings payload as a JSON string"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"findings"))

	vocabulary.Register(EscalationReason,
		vocabulary.WithDescription("Explanation for why this review was escalated to a human"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"escalationReason"))
}

func registerTaskPredicates() {
	vocabulary.Register(FilesModified,
		vocabulary.WithDescription("JSON array of file paths modified during task execution"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"filesModified"))

	vocabulary.Register(ValidationPassed,
		vocabulary.WithDescription("Whether post-execution validation succeeded: true or false"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"validationPassed"))

	vocabulary.Register(Feedback,
		vocabulary.WithDescription("Accumulated feedback text from validation or review failures"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"feedback"))

	vocabulary.Register(RejectionType,
		vocabulary.WithDescription("Task rejection classification: fixable, misscoped, architectural, too_big"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"rejectionType"))
}

func registerScenarioPredicates() {
	vocabulary.Register(NodeCount,
		vocabulary.WithDescription("Number of DAG nodes in this scenario's execution plan"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"nodeCount"))

	vocabulary.Register(FailureReason,
		vocabulary.WithDescription("Explanation of why a scenario execution failed"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"failureReason"))
}

func registerCascadePredicates() {
	vocabulary.Register(CascadeAffectedRequirements,
		vocabulary.WithDescription("Count of requirements affected by a ChangeProposal cascade"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"cascadeAffectedRequirements"))

	vocabulary.Register(CascadeAffectedScenarios,
		vocabulary.WithDescription("Count of scenarios affected by a ChangeProposal cascade"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"cascadeAffectedScenarios"))

	vocabulary.Register(CascadeTasksDirtied,
		vocabulary.WithDescription("Count of tasks marked dirty by a ChangeProposal cascade"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"cascadeTasksDirtied"))
}

func registerRelationPredicates() {
	vocabulary.Register(RelPlan,
		vocabulary.WithDescription("Links execution entity to its associated plan entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"plan"))

	vocabulary.Register(RelTask,
		vocabulary.WithDescription("Links execution entity to its associated task entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"task"))

	vocabulary.Register(RelScenario,
		vocabulary.WithDescription("Links execution entity to its associated scenario entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"scenario"))

	vocabulary.Register(RelProject,
		vocabulary.WithDescription("Links execution entity to its associated project entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"project"))

	vocabulary.Register(RelLoop,
		vocabulary.WithDescription("Links execution entity to its agentic loop entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"loop"))

	vocabulary.Register(RelRequirement,
		vocabulary.WithDescription("Links cascade entity to an affected requirement entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"requirement"))
}
