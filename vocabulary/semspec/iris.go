package semspec

// Namespace is the base IRI prefix for all Semspec ontology terms.
const Namespace = "https://semspec.dev/ontology/"

// EntityNamespace is the base IRI for Semspec entity instances.
const EntityNamespace = "https://semspec.dev/entity/"

// Class IRIs define the types of entities in the Semspec ontology.
// These classes extend standard ontology classes from BFO, CCO, and PROV-O.
const (
	// ClassProposal represents a development proposal.
	// Extends: bfo:GenericallyDependentContinuant, cco:InformationContentEntity, prov:Entity
	ClassProposal = Namespace + "Proposal"

	// ClassSpecification represents a technical specification.
	// Extends: bfo:GenericallyDependentContinuant, cco:DirectiveInformationContentEntity, prov:Entity
	ClassSpecification = Namespace + "Specification"

	// ClassTask represents a work item or task.
	// Extends: bfo:GenericallyDependentContinuant, cco:PlanSpecification, prov:Entity
	ClassTask = Namespace + "Task"

	// ClassCodeArtifact represents a source code artifact.
	// Extends: bfo:GenericallyDependentContinuant, cco:SoftwareCode, prov:Entity
	ClassCodeArtifact = Namespace + "CodeArtifact"

	// ClassCodeFile represents a source code file.
	// Extends: ClassCodeArtifact
	ClassCodeFile = Namespace + "CodeFile"

	// ClassCodeFunction represents a function or method.
	// Extends: ClassCodeArtifact, cco:SoftwareModule
	ClassCodeFunction = Namespace + "CodeFunction"

	// ClassCodeStruct represents a struct, class, or type definition.
	// Extends: ClassCodeArtifact
	ClassCodeStruct = Namespace + "CodeStruct"

	// ClassCodeInterface represents an interface definition.
	// Extends: ClassCodeArtifact
	ClassCodeInterface = Namespace + "CodeInterface"

	// ClassLoop represents an agent execution loop.
	// Extends: bfo:Process, cco:ActOfArtifactProcessing, prov:Activity
	ClassLoop = Namespace + "Loop"

	// ClassActivity represents an individual agent action.
	// Extends: bfo:Process, prov:Activity
	ClassActivity = Namespace + "Activity"

	// ClassModelCall represents a call to an AI model.
	// Extends: ClassActivity, cco:ActOfCommunication
	ClassModelCall = Namespace + "ModelCall"

	// ClassToolCall represents a call to a tool.
	// Extends: ClassActivity, cco:ActOfArtifactModification
	ClassToolCall = Namespace + "ToolCall"

	// ClassResult represents the result of an execution.
	// Extends: bfo:GenericallyDependentContinuant, cco:InformationContentEntity, prov:Entity
	ClassResult = Namespace + "Result"

	// ClassUser represents a human user.
	// Extends: bfo:IndependentContinuant, cco:Person, prov:Agent
	ClassUser = Namespace + "User"

	// ClassAIModel represents an AI model agent.
	// Extends: bfo:IndependentContinuant, cco:IntelligentSoftwareAgent, prov:SoftwareAgent
	ClassAIModel = Namespace + "AIModel"

	// ClassConstitution represents a project constitution.
	// Extends: bfo:GenericallyDependentContinuant, cco:DirectiveInformationContentEntity
	ClassConstitution = Namespace + "Constitution"

	// ClassConstitutionRule represents a rule within a constitution.
	// Extends: cco:Requirement
	ClassConstitutionRule = Namespace + "ConstitutionRule"

	// ClassRequirement represents a specification requirement.
	// Extends: cco:Requirement
	ClassRequirement = Namespace + "Requirement"
)

// Object Property IRIs define relationships between entities.
const (
	// PropHasSpec links a proposal to its specification.
	// Domain: ClassProposal, Range: ClassSpecification
	PropHasSpec = Namespace + "hasSpec"

	// PropHasTask links a proposal or spec to tasks.
	// Domain: ClassProposal | ClassSpecification, Range: ClassTask
	PropHasTask = Namespace + "hasTask"

	// PropHasLoop links a task to its execution loop.
	// Domain: ClassTask, Range: ClassLoop
	PropHasLoop = Namespace + "hasLoop"

	// PropHasActivity links a loop to its activities.
	// Domain: ClassLoop, Range: ClassActivity
	PropHasActivity = Namespace + "hasActivity"

	// PropHasResult links a loop to its result.
	// Domain: ClassLoop, Range: ClassResult
	PropHasResult = Namespace + "hasResult"

	// PropAffectsCode links a spec to affected code.
	// Domain: ClassSpecification, Range: ClassCodeArtifact
	PropAffectsCode = Namespace + "affectsCode"

	// PropExecutedBy links a loop to its executing agent.
	// Domain: ClassLoop, Range: ClassAIModel
	PropExecutedBy = Namespace + "executedBy"

	// PropInitiatedBy links a loop to the initiating user.
	// Domain: ClassLoop, Range: ClassUser
	PropInitiatedBy = Namespace + "initiatedBy"

	// PropImplementsSpec links a task to the spec it implements.
	// Domain: ClassTask, Range: ClassSpecification
	PropImplementsSpec = Namespace + "implementsSpec"

	// PropGeneratedCode links a loop to generated code.
	// Domain: ClassLoop, Range: ClassCodeArtifact
	PropGeneratedCode = Namespace + "generatedCode"

	// PropContainsRule links a constitution to its rules.
	// Domain: ClassConstitution, Range: ClassConstitutionRule
	PropContainsRule = Namespace + "containsRule"

	// PropHasRequirement links a spec to its requirements.
	// Domain: ClassSpecification, Range: ClassRequirement
	PropHasRequirement = Namespace + "hasRequirement"
)

// Data Property IRIs define literal-valued attributes.
const (
	// PropStatus is the status of an entity.
	PropStatus = Namespace + "status"

	// PropPriority is the priority level.
	PropPriority = Namespace + "priority"

	// PropRole is the agent role.
	PropRole = Namespace + "role"

	// PropIterations is the iteration count.
	PropIterations = Namespace + "iterations"

	// PropMaxIterations is the maximum iteration limit.
	PropMaxIterations = Namespace + "maxIterations"

	// PropTokensIn is the input token count.
	PropTokensIn = Namespace + "tokensIn"

	// PropTokensOut is the output token count.
	PropTokensOut = Namespace + "tokensOut"

	// PropDuration is the duration in milliseconds.
	PropDuration = Namespace + "duration"

	// PropPath is the file path.
	PropPath = Namespace + "path"

	// PropLanguage is the programming language.
	PropLanguage = Namespace + "language"

	// PropVersion is the version string.
	PropVersion = Namespace + "version"

	// PropSlug is the URL-safe slug.
	PropSlug = Namespace + "slug"

	// PropOutcome is the result outcome.
	PropOutcome = Namespace + "outcome"
)
