package semspec

import "github.com/c360studio/semstreams/vocabulary"

// Plan predicates (workflow) define attributes for development plans.
// These are the workflow-level metadata predicates for the plan lifecycle.
const (
	// PlanTitle is the plan title.
	PlanTitle = "semspec.plan.title"

	// PlanDescription is the plan description/summary.
	PlanDescription = "semspec.plan.description"

	// PredicatePlanStatus is the workflow status predicate.
	// Values: exploring, drafted, approved, implementing, complete, rejected, abandoned
	PredicatePlanStatus = "semspec.plan.status"

	// PlanPriority is the plan priority level.
	// Values: critical, high, medium, low
	PlanPriority = "semspec.plan.priority"

	// PlanRationale explains why this plan exists.
	PlanRationale = "semspec.plan.rationale"

	// PlanScope describes affected areas.
	PlanScope = "semspec.plan.scope"

	// PlanSlug is the URL-safe identifier.
	PlanSlug = "semspec.plan.slug"

	// PlanAuthor links to the user who created the plan.
	PlanAuthor = "semspec.plan.author"

	// PlanReviewer links to the user who reviews the plan.
	PlanReviewer = "semspec.plan.reviewer"

	// PlanSpec links a plan to its specification entity.
	PlanSpec = "semspec.plan.spec"

	// PlanTask links a plan to task entities.
	PlanTask = "semspec.plan.task"

	// PlanCreatedAt is the RFC3339 creation timestamp.
	PlanCreatedAt = "semspec.plan.created_at"

	// PlanUpdatedAt is the RFC3339 last update timestamp.
	PlanUpdatedAt = "semspec.plan.updated_at"

	// PlanHasPlan indicates whether plan.md exists.
	PlanHasPlan = "semspec.plan.has_plan"

	// PlanHasTasks indicates whether tasks.md exists.
	PlanHasTasks = "semspec.plan.has_tasks"

	// PlanGitHubEpic is the GitHub epic issue number.
	PlanGitHubEpic = "semspec.plan.github-epic"

	// PlanGitHubRepo is the GitHub repository (owner/repo format).
	PlanGitHubRepo = "semspec.plan.github-repo"

	// PlanGoal describes what we're building or fixing.
	PlanGoal = "semspec.plan.goal"

	// PlanContext describes the current state and why this matters.
	PlanContext = "semspec.plan.context"

	// PlanScopeInclude lists files/directories in scope for the plan.
	PlanScopeInclude = "semspec.plan.scope_include"

	// PlanScopeExclude lists files/directories explicitly out of scope.
	PlanScopeExclude = "semspec.plan.scope_exclude"

	// PlanScopeProtected lists files/directories that must not be modified.
	PlanScopeProtected = "semspec.plan.scope_protected"

	// PlanApproved indicates the plan is ready for execution.
	PlanApproved = "semspec.plan.approved"

	// PlanProject links a plan to its parent project entity.
	// Format: {prefix}.wf.project.project.{project-slug}
	PlanProject = "semspec.plan.project"

	// PlanApprovedAt is the RFC3339 timestamp when the plan was approved.
	PlanApprovedAt = "semspec.plan.approved_at"

	// PlanReviewVerdict is the plan-reviewer's verdict: "approved" or "needs_changes".
	PlanReviewVerdict = "semspec.plan.review_verdict"

	// PlanReviewSummary is the plan-reviewer's summary of findings.
	PlanReviewSummary = "semspec.plan.review_summary"

	// PlanReviewedAt is the RFC3339 timestamp when plan review completed.
	PlanReviewedAt = "semspec.plan.reviewed_at"

	// PlanReviewFindings contains the structured findings from the plan reviewer (JSON string).
	PlanReviewFindings = "semspec.plan.review_findings"

	// PlanReviewFormattedFindings is the human-readable text version of review findings.
	PlanReviewFormattedFindings = "semspec.plan.review_formatted_findings"

	// PlanReviewIteration is the number of review iterations completed.
	PlanReviewIteration = "semspec.plan.review_iteration"

	// PlanLastError is the most recent error from a workflow step.
	PlanLastError = "semspec.plan.last_error"

	// PlanLastErrorAt is the RFC3339 timestamp when the last error occurred.
	PlanLastErrorAt = "semspec.plan.last_error_at"

	// PlanExecutionTraceIDs is the JSON-encoded array of execution trace IDs.
	PlanExecutionTraceIDs = "semspec.plan.execution_trace_ids"

	// PlanLLMCallHistory is the JSON-encoded LLM call history for review iterations.
	PlanLLMCallHistory = "semspec.plan.llm_call_history"
)

// Project config predicates define attributes for project initialization config files.
// These track approval state for project.json, checklist.json, and standards.json.
const (
	// ProjectConfigStatus is the config file status ("draft" or "approved").
	ProjectConfigStatus = "semspec.config.status"

	// ProjectConfigApproved indicates whether the config file has been approved.
	ProjectConfigApproved = "semspec.config.approved"

	// ProjectConfigFile identifies which config file (e.g., "project.json").
	ProjectConfigFile = "semspec.config.file"

	// ProjectConfigApprovedAt is the RFC3339 approval timestamp.
	ProjectConfigApprovedAt = "semspec.config.approved_at"

	// ProjectConfigUpdatedAt is the RFC3339 last mutation timestamp for reconciliation.
	ProjectConfigUpdatedAt = "semspec.config.updated_at"

	// ProjectConfigType identifies the config type: "project", "checklist", or "standards".
	ProjectConfigType = "semspec.config.type"

	// ProjectConfigJSON is the full config JSON blob for round-trip reconciliation.
	ProjectConfigJSON = "semspec.config.json"

	// ProjectConfigName is the project name (denormalized from project.json).
	ProjectConfigName = "semspec.config.name"

	// ProjectConfigOrg is the organization segment for entity IDs.
	ProjectConfigOrg = "semspec.config.org"

	// ProjectConfigPlatform is the project identifier segment for entity IDs.
	ProjectConfigPlatform = "semspec.config.platform"
)

// Specification predicates define attributes for technical specifications.
const (
	// SpecTitle is the specification title.
	SpecTitle = "semspec.spec.title"

	// SpecContent is the specification content (markdown).
	SpecContent = "semspec.spec.content"

	// PredicateSpecStatus is the specification status predicate.
	// Values: draft, in_review, approved, implemented, superseded
	PredicateSpecStatus = "semspec.spec.status"

	// SpecVersion is the specification version (semver).
	SpecVersion = "semspec.spec.version"

	// SpecPlan links to the plan this spec derives from.
	SpecPlan = "semspec.spec.plan"

	// SpecTasks links to task entities derived from this spec.
	SpecTasks = "semspec.spec.tasks"

	// SpecAffects links to code entities this spec affects.
	SpecAffects = "semspec.spec.affects"

	// SpecAuthor links to the user/agent who authored this spec.
	SpecAuthor = "semspec.spec.author"

	// SpecApprovedBy links to the user who approved this spec.
	SpecApprovedBy = "semspec.spec.approved_by"

	// SpecApprovedAt is the RFC3339 approval timestamp.
	SpecApprovedAt = "semspec.spec.approved_at"

	// SpecDependsOn links to other specs this spec depends on.
	SpecDependsOn = "semspec.spec.depends_on"

	// SpecCreatedAt is the RFC3339 creation timestamp.
	SpecCreatedAt = "semspec.spec.created_at"

	// SpecUpdatedAt is the RFC3339 last update timestamp.
	SpecUpdatedAt = "semspec.spec.updated_at"

	// SpecRequirement links a spec to a requirement.
	SpecRequirement = "semspec.spec.requirement"

	// SpecGiven is the precondition (GIVEN) text.
	SpecGiven = "semspec.spec.given"

	// SpecWhen is the action (WHEN) text.
	SpecWhen = "semspec.spec.when"

	// SpecThen is the expected outcome (THEN) text.
	SpecThen = "semspec.spec.then"
)

// Task predicates define attributes for work items.
const (
	// TaskTitle is the task title.
	TaskTitle = "semspec.task.title"

	// TaskDescription is the task description.
	TaskDescription = "semspec.task.description"

	// PredicateTaskStatus is the task status predicate.
	// Values: pending, in_progress, complete, failed, blocked, cancelled
	PredicateTaskStatus = "semspec.task.status"

	// PredicateTaskType is the task type predicate.
	// Values: implement, test, document, review, refactor
	PredicateTaskType = "semspec.task.type"

	// TaskGiven is the precondition (GIVEN) for a BDD acceptance criterion.
	TaskGiven = "semspec.task.given"

	// TaskWhen is the action (WHEN) for a BDD acceptance criterion.
	TaskWhen = "semspec.task.when"

	// TaskThen is the expected outcome (THEN) for a BDD acceptance criterion.
	TaskThen = "semspec.task.then"

	// TaskSpec links to the parent spec.
	TaskSpec = "semspec.task.spec"

	// TaskLoop links to the loop executing this task.
	TaskLoop = "semspec.task.loop"

	// TaskAssignee links to the assigned agent or user.
	TaskAssignee = "semspec.task.assignee"

	// TaskPredecessor links to the preceding task (ordering).
	TaskPredecessor = "semspec.task.predecessor"

	// TaskSuccessor links to the following task (ordering).
	TaskSuccessor = "semspec.task.successor"

	// TaskOrder is the task order/priority within the spec.
	TaskOrder = "semspec.task.order"

	// TaskEstimate is the complexity estimate.
	TaskEstimate = "semspec.task.estimate"

	// TaskActualEffort is the actual time/iterations taken.
	TaskActualEffort = "semspec.task.actual_effort"

	// TaskCreatedAt is the RFC3339 creation timestamp.
	TaskCreatedAt = "semspec.task.created_at"

	// TaskUpdatedAt is the RFC3339 last update timestamp.
	TaskUpdatedAt = "semspec.task.updated_at"
)

// Loop predicates define attributes for agent execution loops.
const (
	// PredicateLoopStatus is the loop execution status predicate.
	// Values: executing, paused, awaiting_approval, complete, failed, cancelled
	PredicateLoopStatus = "agent.loop.status"

	// PredicateLoopRole is the agent role predicate.
	// Values: planner, implementer, reviewer, general
	PredicateLoopRole = "agent.loop.role"

	// LoopModel is the model identifier.
	LoopModel = "agent.loop.model"

	// LoopIterations is the current iteration count.
	LoopIterations = "agent.loop.iterations"

	// LoopMaxIterations is the maximum allowed iterations.
	LoopMaxIterations = "agent.loop.max_iterations"

	// LoopTask links to the task being executed.
	LoopTask = "agent.loop.task"

	// LoopUser links to the user who initiated the loop.
	LoopUser = "agent.loop.user"

	// LoopAgent links to the AI model agent.
	LoopAgent = "agent.loop.agent"

	// LoopPrompt is the initial prompt text.
	LoopPrompt = "agent.loop.prompt"

	// LoopContext is the context provided.
	LoopContext = "agent.loop.context"

	// LoopStartedAt is the RFC3339 start timestamp.
	LoopStartedAt = "agent.loop.started_at"

	// LoopEndedAt is the RFC3339 end timestamp.
	LoopEndedAt = "agent.loop.ended_at"

	// LoopDuration is the duration in milliseconds.
	LoopDuration = "agent.loop.duration"
)

// Activity predicates define attributes for individual agent actions.
const (
	// PredicateActivityType is the activity classification predicate.
	// Values: model_call, tool_call
	PredicateActivityType = "agent.activity.type"

	// ActivityTool is the tool name for tool_call activities.
	ActivityTool = "agent.activity.tool"

	// ActivityModel is the model name for model_call activities.
	ActivityModel = "agent.activity.model"

	// ActivityLoop links to the parent loop.
	ActivityLoop = "agent.activity.loop"

	// ActivityPrecedes links to the next activity.
	ActivityPrecedes = "agent.activity.precedes"

	// ActivityFollows links to the previous activity.
	ActivityFollows = "agent.activity.follows"

	// ActivityInput links to input entities.
	ActivityInput = "agent.activity.input"

	// ActivityOutput links to output entities.
	ActivityOutput = "agent.activity.output"

	// ActivityArgs is the tool arguments (JSON).
	ActivityArgs = "agent.activity.args"

	// ActivityResult is the result summary.
	ActivityResult = "agent.activity.result"

	// ActivityDuration is the duration in milliseconds.
	ActivityDuration = "agent.activity.duration"

	// ActivityTokensIn is the input token count.
	ActivityTokensIn = "agent.activity.tokens_in"

	// ActivityTokensOut is the output token count.
	ActivityTokensOut = "agent.activity.tokens_out"

	// ActivitySuccess indicates whether the activity succeeded.
	ActivitySuccess = "agent.activity.success"

	// ActivityError is the error message if failed.
	ActivityError = "agent.activity.error"

	// ActivityStartedAt is the RFC3339 start timestamp.
	ActivityStartedAt = "agent.activity.started_at"

	// ActivityEndedAt is the RFC3339 end timestamp.
	ActivityEndedAt = "agent.activity.ended_at"
)

// LLM call predicates define attributes for model invocations.
// These extend agent.activity.* predicates with LLM-specific data.
const (
	// LLMCapability is the semantic capability requested (planning, coding, writing, etc.).
	LLMCapability = "llm.call.capability"

	// LLMProvider is the LLM provider (anthropic, ollama, openai, etc.).
	LLMProvider = "llm.call.provider"

	// LLMFinishReason indicates why generation stopped (stop, length, tool_use).
	LLMFinishReason = "llm.call.finish_reason"

	// LLMContextBudget is the maximum context window size for this model.
	LLMContextBudget = "llm.call.context_budget"

	// LLMContextTruncated indicates if context was truncated to fit budget.
	LLMContextTruncated = "llm.call.context_truncated"

	// LLMRetries is the number of retry attempts made.
	LLMRetries = "llm.call.retries"

	// LLMFallback lists models tried before success (if fallback was needed).
	LLMFallback = "llm.call.fallback"

	// LLMRequestID uniquely identifies this LLM call.
	LLMRequestID = "llm.call.request_id"

	// LLMResponsePreview is a truncated response for lightweight queries.
	LLMResponsePreview = "llm.call.response_preview"

	// LLMMessagesCount is the number of messages in the conversation.
	LLMMessagesCount = "llm.call.messages_count"
)

// Result predicates define attributes for execution results.
const (
	// PredicateResultOutcome is the result status predicate.
	// Values: success, failure, partial
	PredicateResultOutcome = "agent.result.outcome"

	// ResultLoop links to the parent loop.
	ResultLoop = "agent.result.loop"

	// ResultSummary is the human-readable summary.
	ResultSummary = "agent.result.summary"

	// ResultArtifacts links to created entities.
	ResultArtifacts = "agent.result.artifacts"

	// ResultDiff is the unified diff (if applicable).
	ResultDiff = "agent.result.diff"

	// ResultApproved indicates whether the result was approved.
	ResultApproved = "agent.result.approved"

	// ResultApprovedBy links to the approving user.
	ResultApprovedBy = "agent.result.approved_by"

	// ResultApprovedAt is the RFC3339 approval timestamp.
	ResultApprovedAt = "agent.result.approved_at"

	// ResultRejectedBy links to the rejecting user.
	ResultRejectedBy = "agent.result.rejected_by"

	// ResultRejectedAt is the RFC3339 rejection timestamp.
	ResultRejectedAt = "agent.result.rejected_at"

	// ResultRejectionReason is the rejection reason text.
	ResultRejectionReason = "agent.result.rejection_reason"
)

// Code artifact predicates define attributes for source code entities.
const (
	// CodePath is the file path.
	CodePath = "code.artifact.path"

	// CodeHash is the content hash.
	CodeHash = "code.artifact.hash"

	// CodeLanguage is the programming language.
	CodeLanguage = "code.artifact.language"

	// CodePackage is the package name.
	CodePackage = "code.artifact.package"

	// PredicateCodeType is the code element type predicate.
	// Values: file, package, function, method, struct, interface, const, var, type
	PredicateCodeType = "code.artifact.type"

	// CodeVisibility is the visibility level.
	// Values: public, private, internal
	CodeVisibility = "code.artifact.visibility"

	// CodeLines is the line count.
	CodeLines = "code.metric.lines"

	// CodeComplexity is the cyclomatic complexity.
	CodeComplexity = "code.metric.complexity"
)

// Code structure predicates define containment relationships.
const (
	// CodeContains links a parent to child elements (file → functions).
	CodeContains = "code.structure.contains"

	// CodeBelongsTo links a child to its parent (function → file).
	CodeBelongsTo = "code.structure.belongs"
)

// Code dependency predicates define import/export relationships.
const (
	// CodeImports links to imported code entities.
	CodeImports = "code.dependency.imports"

	// CodeExports is the exported symbols.
	CodeExports = "code.dependency.exports"
)

// Code relationship predicates define semantic connections.
const (
	// CodeImplements links to the interface being implemented.
	CodeImplements = "code.relationship.implements"

	// CodeExtends links to the struct being extended.
	CodeExtends = "code.relationship.extends"

	// CodeCalls links to the function being called.
	CodeCalls = "code.relationship.calls"

	// CodeReferences links to any referenced code entity.
	CodeReferences = "code.relationship.references"
)

// Constitution predicates define project rules and constraints.
const (
	// ConstitutionProject is the project identifier.
	ConstitutionProject = "constitution.project.name"

	// ConstitutionVersion is the constitution version number.
	ConstitutionVersion = "constitution.version.number"

	// PredicateConstitutionSection is the section name predicate.
	// Values: code_quality, testing, security, architecture
	PredicateConstitutionSection = "constitution.section.name"

	// ConstitutionRule is the rule text.
	ConstitutionRule = "constitution.rule.text"

	// ConstitutionRuleID is the rule identifier.
	ConstitutionRuleID = "constitution.rule.id"

	// ConstitutionEnforced indicates whether this rule is enforced.
	ConstitutionEnforced = "constitution.rule.enforced"

	// ConstitutionPriority is the enforcement priority.
	// Values: must, should, may
	ConstitutionRulePriority = "constitution.rule.priority"
)

// Approval predicates define attributes for approval/rejection audit trail.
const (
	// ApprovalTargetType is the type of entity being approved ("plan", "phase", "task").
	ApprovalTargetType = "semspec.approval.target_type"

	// ApprovalTargetID is the entity ID of what was approved.
	ApprovalTargetID = "semspec.approval.target_id"

	// ApprovalDecision is the approval decision ("approved" or "rejected").
	ApprovalDecision = "semspec.approval.decision"

	// ApprovalApprovedBy identifies who made the approval decision.
	ApprovalApprovedBy = "semspec.approval.approved_by"

	// ApprovalReason is the rejection reason (if rejected).
	ApprovalReason = "semspec.approval.reason"

	// ApprovalCreatedAt is the RFC3339 timestamp of the decision.
	ApprovalCreatedAt = "semspec.approval.created_at"
)

// Question predicates define attributes for knowledge gap questions.
// These track questions asked by agents during workflow execution.
const (
	// QuestionContent is the question text.
	QuestionContent = "semspec.question.content"

	// QuestionTopic is the hierarchical topic (e.g., "api.semstreams.loop-info").
	QuestionTopic = "semspec.question.topic"

	// QuestionFromAgent identifies who asked the question.
	QuestionFromAgent = "semspec.question.from_agent"

	// QuestionContext provides background information for the answerer.
	QuestionContext = "semspec.question.context"

	// QuestionStatus is the current state (pending, answered, timeout).
	QuestionStatus = "semspec.question.status"

	// QuestionUrgency indicates priority level (low, normal, high, blocking).
	QuestionUrgency = "semspec.question.urgency"

	// QuestionBlockedLoopID is the loop waiting for this answer.
	QuestionBlockedLoopID = "semspec.question.blocked_loop_id"

	// QuestionPlanSlug is the plan slug this question relates to.
	QuestionPlanSlug = "semspec.question.plan_slug"

	// QuestionTaskID is the task this question relates to.
	QuestionTaskID = "semspec.question.task_id"

	// QuestionPhaseID is the phase this question relates to.
	QuestionPhaseID = "semspec.question.phase_id"

	// QuestionAssignedTo is the answerer assigned to this question.
	QuestionAssignedTo = "semspec.question.assigned_to"

	// QuestionAnswer is the response text.
	QuestionAnswer = "semspec.question.answer"

	// QuestionAnsweredBy identifies who answered.
	QuestionAnsweredBy = "semspec.question.answered_by"

	// QuestionAnswererType is "agent", "team", or "human".
	QuestionAnswererType = "semspec.question.answerer_type"

	// QuestionAnsweredAt is the RFC3339 answer timestamp.
	QuestionAnsweredAt = "semspec.question.answered_at"

	// QuestionConfidence is the answerer's confidence level.
	QuestionConfidence = "semspec.question.confidence"

	// QuestionSources describes where the answer came from.
	QuestionSources = "semspec.question.sources"

	// QuestionPlanID is the plan entity ID this question relates to.
	QuestionPlanID = "semspec.question.plan_id"

	// QuestionTraceID correlates with distributed tracing.
	QuestionTraceID = "semspec.question.trace_id"

	// QuestionCreatedAt is the RFC3339 creation timestamp.
	QuestionCreatedAt = "semspec.question.created_at"
)

// Task linking predicates.
const (
	// TaskPlan links a task to its parent plan entity.
	TaskPlan = "semspec.task.plan"

	// TaskScenario links to scenarios this task satisfies (SATISFIES edge).
	TaskScenario = "semspec.task.scenario"
)

// Requirement predicates define attributes for plan-level requirements.
const (
	// RequirementTitle is the requirement title.
	RequirementTitle = "semspec.requirement.title"

	// RequirementDescription is the requirement description.
	RequirementDescription = "semspec.requirement.description"

	// RequirementStatus is the requirement lifecycle status.
	// Values: active, deprecated, superseded
	RequirementStatus = "semspec.requirement.status"

	// RequirementPlan links to the parent plan entity.
	RequirementPlan = "semspec.requirement.plan"

	// RequirementScenario links to child scenario entities.
	RequirementScenario = "semspec.requirement.scenario"

	// RequirementCreatedAt is the RFC3339 creation timestamp.
	RequirementCreatedAt = "semspec.requirement.created_at"

	// RequirementUpdatedAt is the RFC3339 last update timestamp.
	RequirementUpdatedAt = "semspec.requirement.updated_at"

	// RequirementSupersededBy links to the requirement that supersedes this one.
	RequirementSupersededBy = "semspec.requirement.superseded_by"

	// RequirementDependsOn links to a prerequisite requirement entity.
	// Requirements form a DAG — a requirement is only ready for execution
	// when all of its dependencies have their scenarios passing.
	RequirementDependsOn = "semspec.requirement.depends_on"
)

// Scenario predicates define attributes for behavioral contracts.
const (
	// ScenarioGiven is the precondition state.
	ScenarioGiven = "semspec.scenario.given"

	// ScenarioWhen is the triggering action.
	ScenarioWhen = "semspec.scenario.when"

	// ScenarioThen is the expected outcomes (multiple assertions).
	ScenarioThen = "semspec.scenario.then"

	// ScenarioStatus is the verification status.
	// Values: pending, passing, failing, skipped
	ScenarioStatus = "semspec.scenario.status"

	// ScenarioRequirement links to the parent requirement entity.
	ScenarioRequirement = "semspec.scenario.requirement"

	// ScenarioTask links to satisfying task entities.
	ScenarioTask = "semspec.scenario.task"

	// ScenarioCreatedAt is the RFC3339 creation timestamp.
	ScenarioCreatedAt = "semspec.scenario.created_at"

	// ScenarioUpdatedAt is the RFC3339 last update timestamp.
	ScenarioUpdatedAt = "semspec.scenario.updated_at"
)

// ChangeProposal predicates define attributes for mid-stream change proposals.
const (
	// ChangeProposalTitle is the proposal title.
	ChangeProposalTitle = "semspec.change_proposal.title"

	// ChangeProposalRationale explains why the change is needed.
	ChangeProposalRationale = "semspec.change_proposal.rationale"

	// ChangeProposalStatus is the proposal lifecycle status.
	// Values: proposed, under_review, accepted, rejected, archived
	ChangeProposalStatus = "semspec.change_proposal.status"

	// ChangeProposalProposedBy identifies who proposed the change (agent role or "user").
	ChangeProposalProposedBy = "semspec.change_proposal.proposed_by"

	// ChangeProposalPlan links to the parent plan entity.
	ChangeProposalPlan = "semspec.change_proposal.plan"

	// ChangeProposalMutates links to affected requirement entities.
	ChangeProposalMutates = "semspec.change_proposal.mutates"

	// ChangeProposalCreatedAt is the RFC3339 creation timestamp.
	ChangeProposalCreatedAt = "semspec.change_proposal.created_at"

	// ChangeProposalDecidedAt is the RFC3339 decision timestamp.
	ChangeProposalDecidedAt = "semspec.change_proposal.decided_at"
)

// Standard metadata predicates aligned with Dublin Core.
const (
	// DCTitle is the human-readable title.
	DCTitle = "dc.terms.title"

	// DCDescription is the description text.
	DCDescription = "dc.terms.description"

	// DCCreator is the creator identifier.
	DCCreator = "dc.terms.creator"

	// DCCreated is the creation timestamp.
	DCCreated = "dc.terms.created"

	// DCModified is the modification timestamp.
	DCModified = "dc.terms.modified"

	// DCType is the type classification.
	DCType = "dc.terms.type"

	// DCIdentifier is the external identifier.
	DCIdentifier = "dc.terms.identifier"

	// DCSource is the source reference.
	DCSource = "dc.terms.source"

	// DCFormat is the MIME type.
	DCFormat = "dc.terms.format"

	// DCLanguage is the language code.
	DCLanguage = "dc.terms.language"
)

// SKOS-aligned predicates for concept relationships.
const (
	// SKOSPrefLabel is the preferred label.
	SKOSPrefLabel = "skos.label.preferred"

	// SKOSAltLabel is the alternate label.
	SKOSAltLabel = "skos.label.alternate"

	// SKOSBroader links to a parent concept.
	SKOSBroader = "skos.semantic.broader"

	// SKOSNarrower links to child concepts.
	SKOSNarrower = "skos.semantic.narrower"

	// SKOSRelated links to related concepts.
	SKOSRelated = "skos.semantic.related"

	// SKOSNote is documentation text.
	SKOSNote = "skos.documentation.note"

	// SKOSDefinition is the formal definition.
	SKOSDefinition = "skos.documentation.definition"
)

// PROV-O-aligned predicates for provenance tracking.
const (
	// ProvGeneratedBy links to the generating activity.
	ProvGeneratedBy = "prov.generation.activity"

	// ProvAttributedTo links to the responsible agent.
	ProvAttributedTo = "prov.attribution.agent"

	// ProvDerivedFrom links to the source entity.
	ProvDerivedFrom = "prov.derivation.source"

	// ProvUsed links to the input entity.
	ProvUsed = "prov.usage.entity"

	// ProvAssociatedWith links to the associated agent.
	ProvAssociatedWith = "prov.association.agent"

	// ProvActedOnBehalfOf links to the principal agent.
	ProvActedOnBehalfOf = "prov.delegation.principal"

	// ProvStartedAt is the start timestamp.
	ProvStartedAt = "prov.time.started"

	// ProvEndedAt is the end timestamp.
	ProvEndedAt = "prov.time.ended"

	// ProvGeneratedAt is the generation timestamp.
	ProvGeneratedAt = "prov.time.generated"
)

func registerPlanPredicates() {
	// Register plan (workflow) predicates
	vocabulary.Register(PlanTitle,
		vocabulary.WithDescription("Plan title"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"title"))

	vocabulary.Register(PlanDescription,
		vocabulary.WithDescription("Plan description or summary"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"description"))

	vocabulary.Register(PredicatePlanStatus,
		vocabulary.WithDescription("Workflow status"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"status"))

	vocabulary.Register(PlanPriority,
		vocabulary.WithDescription("Priority level"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"priority"))

	vocabulary.Register(PlanRationale,
		vocabulary.WithDescription("Rationale for the plan"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"rationale"))

	vocabulary.Register(PlanScope,
		vocabulary.WithDescription("Affected areas"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"scope"))

	vocabulary.Register(PlanSlug,
		vocabulary.WithDescription("URL-safe identifier"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"slug"))

	vocabulary.Register(PlanAuthor,
		vocabulary.WithDescription("Creator of the plan"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasAttributedTo))

	vocabulary.Register(PlanReviewer,
		vocabulary.WithDescription("Reviewer of the plan"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"reviewer"))

	vocabulary.Register(PlanSpec,
		vocabulary.WithDescription("Link to specification entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"hasSpec"))

	vocabulary.Register(PlanTask,
		vocabulary.WithDescription("Link to task entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"hasTask"))

	vocabulary.Register(PlanCreatedAt,
		vocabulary.WithDescription("Creation timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvGeneratedAtTime))

	vocabulary.Register(PlanUpdatedAt,
		vocabulary.WithDescription("Last update timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI("http://purl.org/dc/terms/modified"))

	vocabulary.Register(PlanHasPlan,
		vocabulary.WithDescription("Whether plan.md exists"),
		vocabulary.WithDataType("bool"))

	vocabulary.Register(PlanHasTasks,
		vocabulary.WithDescription("Whether tasks.md exists"),
		vocabulary.WithDataType("bool"))

	vocabulary.Register(PlanGitHubEpic,
		vocabulary.WithDescription("GitHub epic issue number"),
		vocabulary.WithDataType("int"))

	vocabulary.Register(PlanGitHubRepo,
		vocabulary.WithDescription("GitHub repository (owner/repo)"),
		vocabulary.WithDataType("string"))

	// Register specification predicates
	vocabulary.Register(SpecTitle,
		vocabulary.WithDescription("Specification title"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"title"))

	vocabulary.Register(SpecContent,
		vocabulary.WithDescription("Specification content (markdown)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"content"))

	vocabulary.Register(PredicateSpecStatus,
		vocabulary.WithDescription("Specification status"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"status"))

	vocabulary.Register(SpecVersion,
		vocabulary.WithDescription("Specification version"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"version"))

	vocabulary.Register(SpecPlan,
		vocabulary.WithDescription("Plan this spec derives from"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasDerivedFrom))

	vocabulary.Register(SpecTasks,
		vocabulary.WithDescription("Tasks derived from this spec"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"hasTasks"))

	vocabulary.Register(SpecAffects,
		vocabulary.WithDescription("Code entities this spec affects"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"affects"))

	vocabulary.Register(SpecAuthor,
		vocabulary.WithDescription("Author of this spec"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasAttributedTo))

	vocabulary.Register(SpecApprovedBy,
		vocabulary.WithDescription("User who approved this spec"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"approvedBy"))

	vocabulary.Register(SpecApprovedAt,
		vocabulary.WithDescription("Approval timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"))

	vocabulary.Register(SpecDependsOn,
		vocabulary.WithDescription("Specs this spec depends on"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasDerivedFrom))

	vocabulary.Register(SpecCreatedAt,
		vocabulary.WithDescription("Creation timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"))

	vocabulary.Register(SpecUpdatedAt,
		vocabulary.WithDescription("Last update timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"))

	vocabulary.Register(SpecRequirement,
		vocabulary.WithDescription("Requirement linked to this spec"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"requirement"))

	vocabulary.Register(SpecGiven,
		vocabulary.WithDescription("Precondition (GIVEN)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"given"))

	vocabulary.Register(SpecWhen,
		vocabulary.WithDescription("Action (WHEN)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"when"))

	vocabulary.Register(SpecThen,
		vocabulary.WithDescription("Expected outcome (THEN)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"then"))

}

func registerTaskPredicates() {
	// Register task predicates
	vocabulary.Register(TaskTitle,
		vocabulary.WithDescription("Task title"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"taskTitle"))

	vocabulary.Register(TaskDescription,
		vocabulary.WithDescription("Task description"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"taskDescription"))

	vocabulary.Register(PredicateTaskStatus,
		vocabulary.WithDescription("Task status"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"taskStatus"))

	vocabulary.Register(PredicateTaskType,
		vocabulary.WithDescription("Task type"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"taskType"))

	vocabulary.Register(TaskSpec,
		vocabulary.WithDescription("Parent spec for this task"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasDerivedFrom))

	vocabulary.Register(TaskLoop,
		vocabulary.WithDescription("Loop executing this task"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasGeneratedBy))

	vocabulary.Register(TaskAssignee,
		vocabulary.WithDescription("Assigned agent or user"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasAttributedTo))

	vocabulary.Register(TaskPredecessor,
		vocabulary.WithDescription("Preceding task"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI("http://purl.obolibrary.org/obo/BFO_0000062")) // bfo:preceded_by

	vocabulary.Register(TaskSuccessor,
		vocabulary.WithDescription("Following task"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI("http://purl.obolibrary.org/obo/BFO_0000063")) // bfo:precedes

	vocabulary.Register(TaskOrder,
		vocabulary.WithDescription("Task order/priority"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"taskOrder"))

	vocabulary.Register(TaskEstimate,
		vocabulary.WithDescription("Complexity estimate"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(TaskActualEffort,
		vocabulary.WithDescription("Actual time/iterations taken"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(TaskCreatedAt,
		vocabulary.WithDescription("Creation timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"))

	vocabulary.Register(TaskUpdatedAt,
		vocabulary.WithDescription("Last update timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"))

	// Register task BDD acceptance criteria predicates
	vocabulary.Register(TaskGiven,
		vocabulary.WithDescription("Precondition (GIVEN) for BDD acceptance criterion"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"given"))

	vocabulary.Register(TaskWhen,
		vocabulary.WithDescription("Action (WHEN) for BDD acceptance criterion"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"when"))

	vocabulary.Register(TaskThen,
		vocabulary.WithDescription("Expected outcome (THEN) for BDD acceptance criterion"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"then"))

	// Register plan predicates
	vocabulary.Register(PlanGoal,
		vocabulary.WithDescription("What we're building or fixing"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://purl.org/dc/terms/description"))

	vocabulary.Register(PlanContext,
		vocabulary.WithDescription("Current state and why this matters"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"context"))

	vocabulary.Register(PlanScopeInclude,
		vocabulary.WithDescription("Files/directories in scope"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"scopeInclude"))

	vocabulary.Register(PlanScopeExclude,
		vocabulary.WithDescription("Files/directories out of scope"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"scopeExclude"))

	vocabulary.Register(PlanScopeProtected,
		vocabulary.WithDescription("Files/directories that must not be modified"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"scopeProtected"))

	vocabulary.Register(PlanApproved,
		vocabulary.WithDescription("Plan is ready for execution"),
		vocabulary.WithDataType("bool"),
		vocabulary.WithIRI(Namespace+"approved"))

	vocabulary.Register(PlanProject,
		vocabulary.WithDescription("Parent project entity ID"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"planProject"))

	vocabulary.Register(PlanApprovedAt,
		vocabulary.WithDescription("Plan approval timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(Namespace+"planApprovedAt"))

	vocabulary.Register(PlanReviewVerdict,
		vocabulary.WithDescription("Plan reviewer verdict: approved or needs_changes"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"planReviewVerdict"))

	vocabulary.Register(PlanReviewSummary,
		vocabulary.WithDescription("Plan reviewer summary of findings"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"planReviewSummary"))

	vocabulary.Register(PlanReviewedAt,
		vocabulary.WithDescription("Plan review completion timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(Namespace+"planReviewedAt"))

	vocabulary.Register(PlanReviewFindings,
		vocabulary.WithDescription("Structured review findings (JSON)"),
		vocabulary.WithDataType("json"),
		vocabulary.WithIRI(Namespace+"planReviewFindings"))

	vocabulary.Register(PlanReviewFormattedFindings,
		vocabulary.WithDescription("Human-readable review findings text"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"planReviewFormattedFindings"))

	vocabulary.Register(PlanReviewIteration,
		vocabulary.WithDescription("Number of review iterations completed"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"planReviewIteration"))

	vocabulary.Register(PlanLastError,
		vocabulary.WithDescription("Most recent workflow step error"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"planLastError"))

	vocabulary.Register(PlanLastErrorAt,
		vocabulary.WithDescription("Last error timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(Namespace+"planLastErrorAt"))

	vocabulary.Register(PlanExecutionTraceIDs,
		vocabulary.WithDescription("Execution trace IDs (JSON array)"),
		vocabulary.WithDataType("json"),
		vocabulary.WithIRI(Namespace+"planExecutionTraceIds"))

	vocabulary.Register(PlanLLMCallHistory,
		vocabulary.WithDescription("Per-iteration LLM call history (JSON)"),
		vocabulary.WithDataType("json"),
		vocabulary.WithIRI(Namespace+"planLlmCallHistory"))

	// Register loop predicates
	vocabulary.Register(PredicateLoopStatus,
		vocabulary.WithDescription("Loop execution status"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"loopStatus"))

	vocabulary.Register(PredicateLoopRole,
		vocabulary.WithDescription("Agent role in this loop"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"role"))

	vocabulary.Register(LoopModel,
		vocabulary.WithDescription("Model identifier"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(LoopIterations,
		vocabulary.WithDescription("Current iteration count"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"iterations"))

	vocabulary.Register(LoopMaxIterations,
		vocabulary.WithDescription("Maximum allowed iterations"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"maxIterations"))

	vocabulary.Register(LoopTask,
		vocabulary.WithDescription("Task being executed"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvUsed))

	vocabulary.Register(LoopUser,
		vocabulary.WithDescription("User who initiated the loop"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasAssociatedWith))

	vocabulary.Register(LoopAgent,
		vocabulary.WithDescription("AI model agent"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasAssociatedWith))

	vocabulary.Register(LoopPrompt,
		vocabulary.WithDescription("Initial prompt text"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(LoopContext,
		vocabulary.WithDescription("Context provided"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(LoopStartedAt,
		vocabulary.WithDescription("Start timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvStartedAtTime))

	vocabulary.Register(LoopEndedAt,
		vocabulary.WithDescription("End timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvEndedAtTime))

	vocabulary.Register(LoopDuration,
		vocabulary.WithDescription("Duration in milliseconds"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"duration"))

}

func registerActivityPredicates() {
	// Register activity predicates
	vocabulary.Register(PredicateActivityType,
		vocabulary.WithDescription("Activity classification"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"activityType"))

	vocabulary.Register(ActivityTool,
		vocabulary.WithDescription("Tool name for tool_call activities"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ActivityModel,
		vocabulary.WithDescription("Model name for model_call activities"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ActivityLoop,
		vocabulary.WithDescription("Parent loop"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI("http://purl.obolibrary.org/obo/BFO_0000050")) // bfo:part_of

	vocabulary.Register(ActivityPrecedes,
		vocabulary.WithDescription("Next activity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI("http://purl.obolibrary.org/obo/BFO_0000063")) // bfo:precedes

	vocabulary.Register(ActivityFollows,
		vocabulary.WithDescription("Previous activity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI("http://purl.obolibrary.org/obo/BFO_0000062")) // bfo:preceded_by

	vocabulary.Register(ActivityInput,
		vocabulary.WithDescription("Input entities"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvUsed))

	vocabulary.Register(ActivityOutput,
		vocabulary.WithDescription("Output entities"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvGenerated))

	vocabulary.Register(ActivityArgs,
		vocabulary.WithDescription("Tool arguments (JSON)"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ActivityResult,
		vocabulary.WithDescription("Result summary"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ActivityDuration,
		vocabulary.WithDescription("Duration in milliseconds"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"duration"))

	vocabulary.Register(ActivityTokensIn,
		vocabulary.WithDescription("Input token count"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"tokensIn"))

	vocabulary.Register(ActivityTokensOut,
		vocabulary.WithDescription("Output token count"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"tokensOut"))

	vocabulary.Register(ActivitySuccess,
		vocabulary.WithDescription("Whether the activity succeeded"),
		vocabulary.WithDataType("bool"))

	vocabulary.Register(ActivityError,
		vocabulary.WithDescription("Error message if failed"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ActivityStartedAt,
		vocabulary.WithDescription("Start timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvStartedAtTime))

	vocabulary.Register(ActivityEndedAt,
		vocabulary.WithDescription("End timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvEndedAtTime))

	// Register result predicates
	vocabulary.Register(PredicateResultOutcome,
		vocabulary.WithDescription("Result status"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"outcome"))

	vocabulary.Register(ResultLoop,
		vocabulary.WithDescription("Parent loop"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasGeneratedBy))

	vocabulary.Register(ResultSummary,
		vocabulary.WithDescription("Human-readable summary"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ResultArtifacts,
		vocabulary.WithDescription("Created entities"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvGenerated))

	vocabulary.Register(ResultDiff,
		vocabulary.WithDescription("Unified diff"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ResultApproved,
		vocabulary.WithDescription("Whether the result was approved"),
		vocabulary.WithDataType("bool"))

	vocabulary.Register(ResultApprovedBy,
		vocabulary.WithDescription("Approving user"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasAttributedTo))

	vocabulary.Register(ResultApprovedAt,
		vocabulary.WithDescription("Approval timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"))

	vocabulary.Register(ResultRejectedBy,
		vocabulary.WithDescription("Rejecting user"),
		vocabulary.WithDataType("entity_id"))

	vocabulary.Register(ResultRejectedAt,
		vocabulary.WithDescription("Rejection timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"))

	vocabulary.Register(ResultRejectionReason,
		vocabulary.WithDescription("Rejection reason text"),
		vocabulary.WithDataType("string"))

}

func registerCodePredicates() {
	// Register code artifact predicates
	vocabulary.Register(CodePath,
		vocabulary.WithDescription("File path"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"path"))

	vocabulary.Register(CodeHash,
		vocabulary.WithDescription("Content hash"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(CodeLanguage,
		vocabulary.WithDescription("Programming language"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"language"))

	vocabulary.Register(CodePackage,
		vocabulary.WithDescription("Package name"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(PredicateCodeType,
		vocabulary.WithDescription("Code element type"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"codeType"))

	vocabulary.Register(CodeVisibility,
		vocabulary.WithDescription("Visibility level"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(CodeLines,
		vocabulary.WithDescription("Line count"),
		vocabulary.WithDataType("int"))

	vocabulary.Register(CodeComplexity,
		vocabulary.WithDescription("Cyclomatic complexity"),
		vocabulary.WithDataType("int"))

	// Register code structure predicates
	vocabulary.Register(CodeContains,
		vocabulary.WithDescription("Contains child elements"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI("http://purl.obolibrary.org/obo/BFO_0000051")) // bfo:has_part

	vocabulary.Register(CodeBelongsTo,
		vocabulary.WithDescription("Belongs to parent"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI("http://purl.obolibrary.org/obo/BFO_0000050")) // bfo:part_of

	// Register code dependency predicates
	vocabulary.Register(CodeImports,
		vocabulary.WithDescription("Imported code entities"),
		vocabulary.WithDataType("entity_id"))

	vocabulary.Register(CodeExports,
		vocabulary.WithDescription("Exported symbols"),
		vocabulary.WithDataType("string"))

	// Register code relationship predicates
	vocabulary.Register(CodeImplements,
		vocabulary.WithDescription("Interface being implemented"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"implements"))

	vocabulary.Register(CodeExtends,
		vocabulary.WithDescription("Struct being extended"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"extends"))

	vocabulary.Register(CodeCalls,
		vocabulary.WithDescription("Function being called"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"calls"))

	vocabulary.Register(CodeReferences,
		vocabulary.WithDescription("Referenced code entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"references"))

}

func registerSemanticPredicates() {
	// Register constitution predicates
	vocabulary.Register(ConstitutionProject,
		vocabulary.WithDescription("Project identifier"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ConstitutionVersion,
		vocabulary.WithDescription("Constitution version number"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(PredicateConstitutionSection,
		vocabulary.WithDescription("Section name"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ConstitutionRule,
		vocabulary.WithDescription("Rule text"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ConstitutionRuleID,
		vocabulary.WithDescription("Rule identifier"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ConstitutionEnforced,
		vocabulary.WithDescription("Whether this rule is enforced"),
		vocabulary.WithDataType("bool"))

	vocabulary.Register(ConstitutionRulePriority,
		vocabulary.WithDescription("Enforcement priority"),
		vocabulary.WithDataType("string"))

	// Register Dublin Core aligned predicates
	vocabulary.Register(DCTitle,
		vocabulary.WithDescription("Human-readable title"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(vocabulary.DcTitle))

	vocabulary.Register(DCDescription,
		vocabulary.WithDescription("Description text"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://purl.org/dc/terms/description"))

	vocabulary.Register(DCCreator,
		vocabulary.WithDescription("Creator identifier"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://purl.org/dc/terms/creator"))

	vocabulary.Register(DCCreated,
		vocabulary.WithDescription("Creation timestamp"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI("http://purl.org/dc/terms/created"))

	vocabulary.Register(DCModified,
		vocabulary.WithDescription("Modification timestamp"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI("http://purl.org/dc/terms/modified"))

	vocabulary.Register(DCType,
		vocabulary.WithDescription("Type classification"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://purl.org/dc/terms/type"))

	vocabulary.Register(DCIdentifier,
		vocabulary.WithDescription("External identifier"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(vocabulary.DcIdentifier))

	vocabulary.Register(DCSource,
		vocabulary.WithDescription("Source reference"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(vocabulary.DcSource))

	vocabulary.Register(DCFormat,
		vocabulary.WithDescription("MIME type"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://purl.org/dc/terms/format"))

	vocabulary.Register(DCLanguage,
		vocabulary.WithDescription("Language code"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://purl.org/dc/terms/language"))

	// Register SKOS aligned predicates
	vocabulary.Register(SKOSPrefLabel,
		vocabulary.WithDescription("Preferred label"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(vocabulary.SkosPrefLabel))

	vocabulary.Register(SKOSAltLabel,
		vocabulary.WithDescription("Alternate label"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(vocabulary.SkosAltLabel))

	vocabulary.Register(SKOSBroader,
		vocabulary.WithDescription("Parent concept"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.SkosBroader))

	vocabulary.Register(SKOSNarrower,
		vocabulary.WithDescription("Child concepts"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.SkosNarrower))

	vocabulary.Register(SKOSRelated,
		vocabulary.WithDescription("Related concepts"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.SkosRelated))

	vocabulary.Register(SKOSNote,
		vocabulary.WithDescription("Documentation text"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://www.w3.org/2004/02/skos/core#note"))

	vocabulary.Register(SKOSDefinition,
		vocabulary.WithDescription("Formal definition"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI("http://www.w3.org/2004/02/skos/core#definition"))

	// Register PROV-O aligned predicates
	vocabulary.Register(ProvGeneratedBy,
		vocabulary.WithDescription("Generating activity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasGeneratedBy))

	vocabulary.Register(ProvAttributedTo,
		vocabulary.WithDescription("Responsible agent"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasAttributedTo))

	vocabulary.Register(ProvDerivedFrom,
		vocabulary.WithDescription("Source entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasDerivedFrom))

	vocabulary.Register(ProvUsed,
		vocabulary.WithDescription("Input entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvUsed))

	vocabulary.Register(ProvAssociatedWith,
		vocabulary.WithDescription("Associated agent"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasAssociatedWith))

	vocabulary.Register(ProvActedOnBehalfOf,
		vocabulary.WithDescription("Principal agent"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvActedOnBehalfOf))

	vocabulary.Register(ProvStartedAt,
		vocabulary.WithDescription("Start timestamp"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvStartedAtTime))

	vocabulary.Register(ProvEndedAt,
		vocabulary.WithDescription("End timestamp"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvEndedAtTime))

	vocabulary.Register(ProvGeneratedAt,
		vocabulary.WithDescription("Generation timestamp"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvGeneratedAtTime))
}

func registerProjectConfigPredicates() {
	vocabulary.Register(ProjectConfigStatus,
		vocabulary.WithDescription("Project config file status (draft or approved)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"projectConfigStatus"))

	vocabulary.Register(ProjectConfigApproved,
		vocabulary.WithDescription("Whether the config file has been approved"),
		vocabulary.WithDataType("bool"),
		vocabulary.WithIRI(Namespace+"projectConfigApproved"))

	vocabulary.Register(ProjectConfigFile,
		vocabulary.WithDescription("Config file name (project.json, checklist.json, standards.json)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"projectConfigFile"))

	vocabulary.Register(ProjectConfigApprovedAt,
		vocabulary.WithDescription("Config file approval timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(Namespace+"projectConfigApprovedAt"))

	vocabulary.Register(ProjectConfigUpdatedAt,
		vocabulary.WithDescription("Config file last mutation timestamp for reconciliation (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(Namespace+"projectConfigUpdatedAt"))

	vocabulary.Register(ProjectConfigType,
		vocabulary.WithDescription("Config type: project, checklist, or standards"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"projectConfigType"))

	vocabulary.Register(ProjectConfigJSON,
		vocabulary.WithDescription("Full config JSON blob for round-trip reconciliation"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"projectConfigJSON"))

	vocabulary.Register(ProjectConfigName,
		vocabulary.WithDescription("Project name from project.json"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"projectConfigName"))

	vocabulary.Register(ProjectConfigOrg,
		vocabulary.WithDescription("Organization segment for entity IDs"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"projectConfigOrg"))

	vocabulary.Register(ProjectConfigPlatform,
		vocabulary.WithDescription("Project identifier segment for entity IDs"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"projectConfigPlatform"))
}

func registerLLMPredicates() {
	vocabulary.Register(LLMCapability,
		vocabulary.WithDescription("Semantic capability requested (planning, coding, writing, etc.)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"llmCapability"))

	vocabulary.Register(LLMProvider,
		vocabulary.WithDescription("LLM provider (anthropic, ollama, openai, etc.)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"llmProvider"))

	vocabulary.Register(LLMFinishReason,
		vocabulary.WithDescription("Why generation stopped (stop, length, tool_use)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"llmFinishReason"))

	vocabulary.Register(LLMContextBudget,
		vocabulary.WithDescription("Maximum context window size for this model"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"llmContextBudget"))

	vocabulary.Register(LLMContextTruncated,
		vocabulary.WithDescription("Whether context was truncated to fit budget"),
		vocabulary.WithDataType("bool"),
		vocabulary.WithIRI(Namespace+"llmContextTruncated"))

	vocabulary.Register(LLMRetries,
		vocabulary.WithDescription("Number of retry attempts made"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"llmRetries"))

	vocabulary.Register(LLMFallback,
		vocabulary.WithDescription("Models tried before success (if fallback was needed)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"llmFallback"))

	vocabulary.Register(LLMRequestID,
		vocabulary.WithDescription("Unique identifier for this LLM call"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"llmRequestId"))

	vocabulary.Register(LLMResponsePreview,
		vocabulary.WithDescription("Truncated response for lightweight queries"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"llmResponsePreview"))

	vocabulary.Register(LLMMessagesCount,
		vocabulary.WithDescription("Number of messages in the conversation"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"llmMessagesCount"))
}

func registerApprovalPredicates() {
	vocabulary.Register(ApprovalTargetType,
		vocabulary.WithDescription("Type of entity being approved (plan, phase, task)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"approvalTargetType"))

	vocabulary.Register(ApprovalTargetID,
		vocabulary.WithDescription("Entity ID of what was approved"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"approvalTargetId"))

	vocabulary.Register(ApprovalDecision,
		vocabulary.WithDescription("Approval decision (approved or rejected)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"approvalDecision"))

	vocabulary.Register(ApprovalApprovedBy,
		vocabulary.WithDescription("Who made the approval decision"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"approvalApprovedBy"))

	vocabulary.Register(ApprovalReason,
		vocabulary.WithDescription("Rejection reason (if rejected)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"approvalReason"))

	vocabulary.Register(ApprovalCreatedAt,
		vocabulary.WithDescription("Timestamp of the decision (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvGeneratedAtTime))

	vocabulary.Register(TaskPlan,
		vocabulary.WithDescription("Links a task to its parent plan entity"),
		vocabulary.WithDataType("reference"),
		vocabulary.WithIRI("http://www.w3.org/ns/prov#wasDerivedFrom"))
}

func registerQuestionPredicates() {
	vocabulary.Register(QuestionContent,
		vocabulary.WithDescription("The question text"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"questionContent"))

	vocabulary.Register(QuestionTopic,
		vocabulary.WithDescription("Hierarchical topic (e.g., api.semstreams.loop-info)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"questionTopic"))

	vocabulary.Register(QuestionFromAgent,
		vocabulary.WithDescription("Identifies who asked the question"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(vocabulary.ProvWasAttributedTo))

	vocabulary.Register(QuestionContext,
		vocabulary.WithDescription("Background information for the answerer"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"questionContext"))

	vocabulary.Register(QuestionStatus,
		vocabulary.WithDescription("Current state (pending, answered, timeout)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"questionStatus"))

	vocabulary.Register(QuestionUrgency,
		vocabulary.WithDescription("Priority level (low, normal, high, blocking)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"questionUrgency"))

	vocabulary.Register(QuestionBlockedLoopID,
		vocabulary.WithDescription("The loop waiting for this answer"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"questionBlockedLoopId"))

	vocabulary.Register(QuestionPlanSlug,
		vocabulary.WithDescription("Plan slug this question relates to"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"questionPlanSlug"))

	vocabulary.Register(QuestionTaskID,
		vocabulary.WithDescription("Task this question relates to (if any)"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"questionTaskId"))

	vocabulary.Register(QuestionPhaseID,
		vocabulary.WithDescription("Phase this question relates to (if any)"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"questionPhaseId"))

	vocabulary.Register(QuestionAssignedTo,
		vocabulary.WithDescription("Answerer assigned to this question"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"questionAssignedTo"))

	vocabulary.Register(QuestionAnswer,
		vocabulary.WithDescription("The response text"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"questionAnswer"))

	vocabulary.Register(QuestionAnsweredBy,
		vocabulary.WithDescription("Identifies who answered"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"questionAnsweredBy"))

	vocabulary.Register(QuestionAnswererType,
		vocabulary.WithDescription("Answerer type: agent, team, or human"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"questionAnswererType"))

	vocabulary.Register(QuestionAnsweredAt,
		vocabulary.WithDescription("RFC3339 answer timestamp"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(Namespace+"questionAnsweredAt"))

	vocabulary.Register(QuestionConfidence,
		vocabulary.WithDescription("Answerer's confidence level"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"questionConfidence"))

	vocabulary.Register(QuestionSources,
		vocabulary.WithDescription("Where the answer came from"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"questionSources"))

	vocabulary.Register(QuestionPlanID,
		vocabulary.WithDescription("Plan entity ID this question relates to"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"questionPlanId"))

	vocabulary.Register(QuestionTraceID,
		vocabulary.WithDescription("Distributed tracing correlation identifier"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"questionTraceId"))

	vocabulary.Register(QuestionCreatedAt,
		vocabulary.WithDescription("RFC3339 creation timestamp"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvGeneratedAtTime))
}

func init() {
	registerPlanPredicates()
	registerTaskPredicates()
	registerActivityPredicates()
	registerCodePredicates()
	registerSemanticPredicates()
	registerProjectConfigPredicates()
	registerLLMPredicates()
	registerApprovalPredicates()
	registerQuestionPredicates()
	registerRequirementPredicates()
	registerScenarioPredicates()
	registerChangeProposalPredicates()
	registerAgenticPredicates()
	registerReviewPredicates()
	registerErrorCategoryPredicates()
	registerAgentPredicates()
	registerTeamPredicates()
}

func registerRequirementPredicates() {
	vocabulary.Register(RequirementTitle,
		vocabulary.WithDescription("Requirement title"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"requirementTitle"))

	vocabulary.Register(RequirementDescription,
		vocabulary.WithDescription("Requirement description"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"requirementDescription"))

	vocabulary.Register(RequirementStatus,
		vocabulary.WithDescription("Requirement lifecycle status (active, deprecated, superseded)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"requirementStatus"))

	vocabulary.Register(RequirementPlan,
		vocabulary.WithDescription("Link to parent plan entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"requirementPlan"))

	vocabulary.Register(RequirementScenario,
		vocabulary.WithDescription("Link to child scenario entities"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"requirementScenario"))

	vocabulary.Register(RequirementCreatedAt,
		vocabulary.WithDescription("Creation timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvGeneratedAtTime))

	vocabulary.Register(RequirementUpdatedAt,
		vocabulary.WithDescription("Last update timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI("http://purl.org/dc/terms/modified"))

	vocabulary.Register(RequirementSupersededBy,
		vocabulary.WithDescription("Link to the requirement that supersedes this one"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"requirementSupersededBy"))

	vocabulary.Register(RequirementDependsOn,
		vocabulary.WithDescription("Link to prerequisite requirement entity (DAG edge)"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"requirementDependsOn"))

	vocabulary.Register(TaskScenario,
		vocabulary.WithDescription("Link to scenarios this task satisfies (SATISFIES edge)"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"taskScenario"))
}

func registerScenarioPredicates() {
	vocabulary.Register(ScenarioGiven,
		vocabulary.WithDescription("Precondition state"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"scenarioGiven"))

	vocabulary.Register(ScenarioWhen,
		vocabulary.WithDescription("Triggering action"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"scenarioWhen"))

	vocabulary.Register(ScenarioThen,
		vocabulary.WithDescription("Expected outcomes (multiple assertions)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"scenarioThen"))

	vocabulary.Register(ScenarioStatus,
		vocabulary.WithDescription("Verification status (pending, passing, failing, skipped)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"scenarioStatus"))

	vocabulary.Register(ScenarioRequirement,
		vocabulary.WithDescription("Link to parent requirement entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"scenarioRequirement"))

	vocabulary.Register(ScenarioTask,
		vocabulary.WithDescription("Link to satisfying task entities"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"scenarioTask"))

	vocabulary.Register(ScenarioCreatedAt,
		vocabulary.WithDescription("Creation timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvGeneratedAtTime))

	vocabulary.Register(ScenarioUpdatedAt,
		vocabulary.WithDescription("Last update timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI("http://purl.org/dc/terms/modified"))
}

func registerChangeProposalPredicates() {
	vocabulary.Register(ChangeProposalTitle,
		vocabulary.WithDescription("Change proposal title"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"changeProposalTitle"))

	vocabulary.Register(ChangeProposalRationale,
		vocabulary.WithDescription("Rationale for the change"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"changeProposalRationale"))

	vocabulary.Register(ChangeProposalStatus,
		vocabulary.WithDescription("Proposal lifecycle status (proposed, under_review, accepted, rejected, archived)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"changeProposalStatus"))

	vocabulary.Register(ChangeProposalProposedBy,
		vocabulary.WithDescription("Who proposed the change (agent role or user)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(vocabulary.ProvWasAttributedTo))

	vocabulary.Register(ChangeProposalPlan,
		vocabulary.WithDescription("Link to parent plan entity"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"changeProposalPlan"))

	vocabulary.Register(ChangeProposalMutates,
		vocabulary.WithDescription("Link to affected requirement entities"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"changeProposalMutates"))

	vocabulary.Register(ChangeProposalCreatedAt,
		vocabulary.WithDescription("Creation timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvGeneratedAtTime))

	vocabulary.Register(ChangeProposalDecidedAt,
		vocabulary.WithDescription("Decision timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(Namespace+"changeProposalDecidedAt"))
}

// Agentic loop predicates (ADR-025) define the reactive execution model's
// graph representation for agent spawning, DAG task decomposition, and hierarchy tracking.
const (
	// PredicateLoopSpawned records a parent loop spawning a child loop.
	// Direction: parent loop entity -> child loop entity.
	PredicateLoopSpawned = "agentic.loop.spawned"

	// PredicateLoopTaskLink records the association between a loop and a task it owns.
	// Direction: loop entity -> task entity.
	PredicateLoopTaskLink = "agentic.loop.task"

	// PredicateLoopStatus records the current lifecycle status of a loop.
	// Values: created, running, paused, complete, failed, cancelled
	PredicateAgenticLoopStatus = "agentic.loop.status"

	// PredicateLoopRole records the functional role of a loop (e.g., "planner", "executor").
	PredicateAgenticLoopRole = "agentic.loop.role"

	// PredicateLoopModel records the LLM model identifier used by a loop.
	PredicateAgenticLoopModel = "agentic.loop.model"

	// PredicateLoopParent records the parent loop for a child loop (inverse of PredicateLoopSpawned).
	// Direction: child loop entity -> parent loop entity.
	PredicateLoopParent = "agentic.loop.parent"

	// PredicateLoopDepth records the nesting depth of a loop within the spawn hierarchy.
	// Root loops have depth 0; each spawn increments depth by 1.
	PredicateLoopDepth = "agentic.loop.depth"

	// PredicateLoopCreatedAt records the creation timestamp of a loop (RFC3339).
	PredicateLoopCreatedAt = "agentic.loop.created_at"

	// PredicateTaskDependsOn records a task-to-task dependency (DAG edge).
	// Direction: dependent task entity -> prerequisite task entity.
	PredicateTaskDependsOn = "agentic.task.depends_on"

	// PredicateTaskDAG links a task to its parent DAG execution entity.
	// Direction: task entity -> DAG entity.
	PredicateTaskDAG = "agentic.task.dag"

	// PredicateTaskPrompt stores the task prompt content.
	PredicateTaskPrompt = "agentic.task.prompt"

	// PredicateTaskNodeID stores the node ID of the task within its DAG.
	PredicateTaskNodeID = "agentic.task.node_id"
)

// Review predicates for peer review tracking.
// Reviews are stored as graph entities linking the reviewed agent, the reviewer,
// and the scenario under evaluation, with rubric scores and optional error refs.
const (
	PredicateReviewScenarioID    = "review.scenario.id"
	PredicateReviewVerdict       = "review.verdict"
	PredicateReviewCorrectness   = "review.rating.correctness"
	PredicateReviewQuality       = "review.rating.quality"
	PredicateReviewCompleteness  = "review.rating.completeness"
	PredicateReviewExplanation   = "review.explanation"
	PredicateReviewAgentID       = "review.agent.id"
	PredicateReviewReviewerID    = "review.reviewer.id"
	PredicateReviewErrorCategory = "review.error.category"
	PredicateReviewRelatedEntity = "review.error.related_entity"
	PredicateReviewTimestamp     = "review.timestamp"
)

// Error category predicates for error category entity definitions.
// Error category entities are seeded from configs/error_categories.json on startup
// and stored in the graph for reference by agent triples and review error refs.
const (
	PredicateErrorCategoryID          = "error.category.id"
	PredicateErrorCategoryLabel       = "error.category.label"
	PredicateErrorCategoryDescription = "error.category.description"
	PredicateErrorCategorySignal      = "error.category.signal"
	PredicateErrorCategoryGuidance    = "error.category.guidance"
)

// Agent identity predicates for the persistent agent roster.
// Agent entities accumulate review scores and error counts across task executions,
// enabling trend detection and prompt injection without re-reading history each run.
const (
	PredicateAgentName        = "agent.identity.name"
	PredicateAgentRole        = "agent.identity.role"
	PredicateAgentModel       = "agent.config.model"
	PredicateAgentState       = "agent.status.state"
	PredicateAgentErrorCounts = "agent.error.counts"
	PredicateAgentQ1Avg       = "agent.review.q1_avg"
	PredicateAgentQ2Avg       = "agent.review.q2_avg"
	PredicateAgentQ3Avg       = "agent.review.q3_avg"
	PredicateAgentOverallAvg  = "agent.review.overall_avg"
	PredicateAgentReviewCount = "agent.review.count"
	PredicateAgentCreatedAt   = "agent.lifecycle.created_at"
	PredicateAgentUpdatedAt   = "agent.lifecycle.updated_at"

	// PredicateAgentTeamID records the team this agent is currently assigned to.
	PredicateAgentTeamID = "agent.team.id"
)

// Team predicates for the persistent team roster.
// Team entities group agents, accumulate aggregated review scores, shared knowledge
// insights, and red-team assessment statistics across collaborative task executions.
const (
	PredicateTeamName           = "team.identity.name"
	PredicateTeamState          = "team.status.state"
	PredicateTeamMember         = "team.member.agent_id"
	PredicateTeamInsight        = "team.knowledge.insight"
	PredicateTeamQ1Avg          = "team.review.q1_avg"
	PredicateTeamQ2Avg          = "team.review.q2_avg"
	PredicateTeamQ3Avg          = "team.review.q3_avg"
	PredicateTeamOverallAvg     = "team.review.overall_avg"
	PredicateTeamReviewCount    = "team.review.count"
	PredicateTeamRedQ1Avg       = "team.redteam.q1_avg"
	PredicateTeamRedQ2Avg       = "team.redteam.q2_avg"
	PredicateTeamRedQ3Avg       = "team.redteam.q3_avg"
	PredicateTeamRedOverallAvg  = "team.redteam.overall_avg"
	PredicateTeamRedReviewCount = "team.redteam.count"
	PredicateTeamErrorCounts    = "team.error.counts"
	PredicateTeamCreatedAt      = "team.lifecycle.created_at"
	PredicateTeamUpdatedAt      = "team.lifecycle.updated_at"
)

func registerAgenticPredicates() {
	// Loop hierarchy predicates
	vocabulary.Register(PredicateLoopSpawned,
		vocabulary.WithDescription("Parent loop -> child loop spawn relationship"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"agenticLoopSpawned"))

	vocabulary.Register(PredicateLoopTaskLink,
		vocabulary.WithDescription("Loop -> task association"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"agenticLoopTask"))

	vocabulary.Register(PredicateAgenticLoopStatus,
		vocabulary.WithDescription("Loop lifecycle status (created, running, complete, failed)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"agenticLoopStatus"))

	vocabulary.Register(PredicateAgenticLoopRole,
		vocabulary.WithDescription("Agent functional role (planner, executor, reviewer)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"agenticLoopRole"))

	vocabulary.Register(PredicateAgenticLoopModel,
		vocabulary.WithDescription("LLM model identifier used by the loop"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"agenticLoopModel"))

	vocabulary.Register(PredicateLoopParent,
		vocabulary.WithDescription("Child loop -> parent loop reference (inverse of spawned)"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"agenticLoopParent"))

	vocabulary.Register(PredicateLoopDepth,
		vocabulary.WithDescription("Nesting depth within spawn hierarchy (root=0)"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"agenticLoopDepth"))

	vocabulary.Register(PredicateLoopCreatedAt,
		vocabulary.WithDescription("Loop creation timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvGeneratedAtTime))

	// Task DAG predicates
	vocabulary.Register(PredicateTaskDependsOn,
		vocabulary.WithDescription("DAG dependency edge: dependent task -> prerequisite task"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"agenticTaskDependsOn"))

	vocabulary.Register(PredicateTaskDAG,
		vocabulary.WithDescription("Task -> DAG execution entity association"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"agenticTaskDag"))

	vocabulary.Register(PredicateTaskPrompt,
		vocabulary.WithDescription("Task prompt content"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"agenticTaskPrompt"))

	vocabulary.Register(PredicateTaskNodeID,
		vocabulary.WithDescription("Node ID of the task within its DAG"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"agenticTaskNodeId"))
}

func registerReviewPredicates() {
	vocabulary.Register(PredicateReviewScenarioID,
		vocabulary.WithDescription("Scenario entity ID whose implementation is under review"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"reviewScenarioId"))

	vocabulary.Register(PredicateReviewVerdict,
		vocabulary.WithDescription("Review outcome: accepted or rejected"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"reviewVerdict"))

	vocabulary.Register(PredicateReviewCorrectness,
		vocabulary.WithDescription("Q1 correctness score: are acceptance criteria met? (1-5)"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"reviewRatingCorrectness"))

	vocabulary.Register(PredicateReviewQuality,
		vocabulary.WithDescription("Q2 quality score: are patterns and SOPs followed? (1-5)"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"reviewRatingQuality"))

	vocabulary.Register(PredicateReviewCompleteness,
		vocabulary.WithDescription("Q3 completeness score: edge cases, tests, and docs covered? (1-5)"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"reviewRatingCompleteness"))

	vocabulary.Register(PredicateReviewExplanation,
		vocabulary.WithDescription("Human-readable explanation for the verdict (required on all-5s accept or below-3 reject)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"reviewExplanation"))

	vocabulary.Register(PredicateReviewAgentID,
		vocabulary.WithDescription("Persistent agent entity ID whose work is being reviewed"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"reviewAgentId"))

	vocabulary.Register(PredicateReviewReviewerID,
		vocabulary.WithDescription("Persistent reviewer agent entity ID performing the review"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"reviewReviewerId"))

	vocabulary.Register(PredicateReviewErrorCategory,
		vocabulary.WithDescription("Error category entity ID cited in this review (multi-valued)"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"reviewErrorCategory"))

	vocabulary.Register(PredicateReviewRelatedEntity,
		vocabulary.WithDescription("Related entity ID linked to a review error citation (e.g., SOP, file)"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"reviewErrorRelatedEntity"))

	vocabulary.Register(PredicateReviewTimestamp,
		vocabulary.WithDescription("RFC3339 timestamp when the review was submitted"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvGeneratedAtTime))
}

func registerErrorCategoryPredicates() {
	vocabulary.Register(PredicateErrorCategoryID,
		vocabulary.WithDescription("Stable machine-readable error category identifier (e.g., missing_tests)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"errorCategoryId"))

	vocabulary.Register(PredicateErrorCategoryLabel,
		vocabulary.WithDescription("Short human-readable label for the error category"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"errorCategoryLabel"))

	vocabulary.Register(PredicateErrorCategoryDescription,
		vocabulary.WithDescription("Full description of what this error category covers"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"errorCategoryDescription"))

	vocabulary.Register(PredicateErrorCategorySignal,
		vocabulary.WithDescription("Observable pattern or symptom that triggers this error category"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"errorCategorySignal"))

	vocabulary.Register(PredicateErrorCategoryGuidance,
		vocabulary.WithDescription("Corrective guidance injected into prompts when trend threshold is reached"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"errorCategoryGuidance"))
}

func registerAgentPredicates() {
	vocabulary.Register(PredicateAgentName,
		vocabulary.WithDescription("Human-readable agent name (e.g., developer-alpha)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"agentIdentityName"))

	vocabulary.Register(PredicateAgentRole,
		vocabulary.WithDescription("Agent functional role (e.g., developer, reviewer)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"agentIdentityRole"))

	vocabulary.Register(PredicateAgentModel,
		vocabulary.WithDescription("Current LLM model assignment for this agent"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"agentConfigModel"))

	vocabulary.Register(PredicateAgentState,
		vocabulary.WithDescription("Agent lifecycle state: available, busy, benched, retired"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"agentStatusState"))

	vocabulary.Register(PredicateAgentErrorCounts,
		vocabulary.WithDescription("JSON-encoded map of error category IDs to accumulated occurrence counts"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"agentErrorCounts"))

	vocabulary.Register(PredicateAgentQ1Avg,
		vocabulary.WithDescription("Running average Q1 correctness score from peer reviews (0–5)"),
		vocabulary.WithDataType("float"),
		vocabulary.WithIRI(Namespace+"agentReviewQ1Avg"))

	vocabulary.Register(PredicateAgentQ2Avg,
		vocabulary.WithDescription("Running average Q2 quality score from peer reviews (0–5)"),
		vocabulary.WithDataType("float"),
		vocabulary.WithIRI(Namespace+"agentReviewQ2Avg"))

	vocabulary.Register(PredicateAgentQ3Avg,
		vocabulary.WithDescription("Running average Q3 completeness score from peer reviews (0–5)"),
		vocabulary.WithDataType("float"),
		vocabulary.WithIRI(Namespace+"agentReviewQ3Avg"))

	vocabulary.Register(PredicateAgentOverallAvg,
		vocabulary.WithDescription("Mean of Q1, Q2, and Q3 running averages"),
		vocabulary.WithDataType("float"),
		vocabulary.WithIRI(Namespace+"agentReviewOverallAvg"))

	vocabulary.Register(PredicateAgentReviewCount,
		vocabulary.WithDescription("Total number of peer reviews incorporated into the running averages"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"agentReviewCount"))

	vocabulary.Register(PredicateAgentCreatedAt,
		vocabulary.WithDescription("RFC3339 timestamp when the agent entity was first created"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvGeneratedAtTime))

	vocabulary.Register(PredicateAgentUpdatedAt,
		vocabulary.WithDescription("RFC3339 timestamp when the agent entity was last modified"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI("http://purl.org/dc/terms/modified"))

	vocabulary.Register(PredicateAgentTeamID,
		vocabulary.WithDescription("Team entity ID this agent is currently assigned to"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"agentTeamId"))
}

func registerTeamPredicates() {
	vocabulary.Register(PredicateTeamName,
		vocabulary.WithDescription("Human-readable team name (e.g., backend-alpha)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"teamIdentityName"))

	vocabulary.Register(PredicateTeamState,
		vocabulary.WithDescription("Team lifecycle state: active, benched, retired"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"teamStatusState"))

	vocabulary.Register(PredicateTeamMember,
		vocabulary.WithDescription("Persistent agent entity ID that is a member of this team (multi-valued)"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"teamMemberAgentId"))

	vocabulary.Register(PredicateTeamInsight,
		vocabulary.WithDescription("Shared knowledge entry accumulated by the team (JSON or plain text, multi-valued)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"teamKnowledgeInsight"))

	vocabulary.Register(PredicateTeamQ1Avg,
		vocabulary.WithDescription("Running average Q1 correctness score aggregated across team members (0–5)"),
		vocabulary.WithDataType("float"),
		vocabulary.WithIRI(Namespace+"teamReviewQ1Avg"))

	vocabulary.Register(PredicateTeamQ2Avg,
		vocabulary.WithDescription("Running average Q2 quality score aggregated across team members (0–5)"),
		vocabulary.WithDataType("float"),
		vocabulary.WithIRI(Namespace+"teamReviewQ2Avg"))

	vocabulary.Register(PredicateTeamQ3Avg,
		vocabulary.WithDescription("Running average Q3 completeness score aggregated across team members (0–5)"),
		vocabulary.WithDataType("float"),
		vocabulary.WithIRI(Namespace+"teamReviewQ3Avg"))

	vocabulary.Register(PredicateTeamOverallAvg,
		vocabulary.WithDescription("Mean of team Q1, Q2, and Q3 running averages"),
		vocabulary.WithDataType("float"),
		vocabulary.WithIRI(Namespace+"teamReviewOverallAvg"))

	vocabulary.Register(PredicateTeamReviewCount,
		vocabulary.WithDescription("Total number of peer reviews incorporated into the team running averages"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"teamReviewCount"))

	vocabulary.Register(PredicateTeamRedQ1Avg,
		vocabulary.WithDescription("Running average red-team Q1 accuracy score for this team (0–5)"),
		vocabulary.WithDataType("float"),
		vocabulary.WithIRI(Namespace+"teamRedteamQ1Avg"))

	vocabulary.Register(PredicateTeamRedQ2Avg,
		vocabulary.WithDescription("Running average red-team Q2 thoroughness score for this team (0–5)"),
		vocabulary.WithDataType("float"),
		vocabulary.WithIRI(Namespace+"teamRedteamQ2Avg"))

	vocabulary.Register(PredicateTeamRedQ3Avg,
		vocabulary.WithDescription("Running average red-team Q3 fairness score for this team (0–5)"),
		vocabulary.WithDataType("float"),
		vocabulary.WithIRI(Namespace+"teamRedteamQ3Avg"))

	vocabulary.Register(PredicateTeamRedOverallAvg,
		vocabulary.WithDescription("Mean of red-team Q1, Q2, and Q3 running averages for this team"),
		vocabulary.WithDataType("float"),
		vocabulary.WithIRI(Namespace+"teamRedteamOverallAvg"))

	vocabulary.Register(PredicateTeamRedReviewCount,
		vocabulary.WithDescription("Total number of red-team reviews incorporated into the team running averages"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"teamRedteamCount"))

	vocabulary.Register(PredicateTeamErrorCounts,
		vocabulary.WithDescription("JSON-encoded map of error category IDs to accumulated team occurrence counts"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"teamErrorCounts"))

	vocabulary.Register(PredicateTeamCreatedAt,
		vocabulary.WithDescription("RFC3339 timestamp when the team entity was first created"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(vocabulary.ProvGeneratedAtTime))

	vocabulary.Register(PredicateTeamUpdatedAt,
		vocabulary.WithDescription("RFC3339 timestamp when the team entity was last modified"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI("http://purl.org/dc/terms/modified"))
}
