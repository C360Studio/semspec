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
	// Format: semspec.local.project.{project-slug}
	PlanProject = "semspec.plan.project"
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

func init() {
	registerPlanPredicates()
	registerTaskPredicates()
	registerActivityPredicates()
	registerCodePredicates()
	registerSemanticPredicates()
}
