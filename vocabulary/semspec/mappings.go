package semspec

import (
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/bfo"
	"github.com/c360studio/semstreams/vocabulary/cco"
)

// EntityType represents the type of a semspec entity for mapping purposes.
type EntityType string

// Entity type constants map entity kinds to their string identifiers.
const (
	// EntityTypePlan is the entity type for development plans.
	EntityTypePlan EntityType = "plan"
	// EntityTypeSpec is the entity type for technical specifications.
	EntityTypeSpec EntityType = "spec"
	// EntityTypeTask is the entity type for work items derived from specifications.
	EntityTypeTask EntityType = "task"
	// EntityTypeCodeFile is the entity type for source code files.
	EntityTypeCodeFile EntityType = "code_file"
	// EntityTypeCodeFunction is the entity type for functions and methods.
	EntityTypeCodeFunction EntityType = "code_function"
	// EntityTypeCodeStruct is the entity type for struct type definitions.
	EntityTypeCodeStruct EntityType = "code_struct"
	// EntityTypeCodeInterface is the entity type for interface type definitions.
	EntityTypeCodeInterface EntityType = "code_interface"
	// EntityTypeLoop is the entity type for agent execution loops.
	EntityTypeLoop EntityType = "loop"
	// EntityTypeActivity is the entity type for individual agent actions.
	EntityTypeActivity EntityType = "activity"
	// EntityTypeModelCall is the entity type for LLM model invocations.
	EntityTypeModelCall EntityType = "model_call"
	// EntityTypeToolCall is the entity type for tool execution events.
	EntityTypeToolCall EntityType = "tool_call"
	// EntityTypeResult is the entity type for loop outcomes.
	EntityTypeResult EntityType = "result"
	// EntityTypeUser is the entity type for human principals.
	EntityTypeUser EntityType = "user"
	// EntityTypeAIModel is the entity type for AI model agents.
	EntityTypeAIModel EntityType = "ai_model"
	// EntityTypeConstitution is the entity type for project governance documents.
	EntityTypeConstitution EntityType = "constitution"
	// EntityTypeConstitutionRule is the entity type for individual governance rules.
	EntityTypeConstitutionRule EntityType = "constitution_rule"
	// EntityTypeRequirement is the entity type for functional or non-functional requirements.
	EntityTypeRequirement EntityType = "requirement"
)

// BFOClassMap maps entity types to BFO class IRIs.
// Use this for BFO profile RDF export.
var BFOClassMap = map[EntityType]string{
	// Information entities → GenericallyDependentContinuant
	EntityTypePlan:             bfo.GenericallyDependentContinuant,
	EntityTypeSpec:             bfo.GenericallyDependentContinuant,
	EntityTypeTask:             bfo.GenericallyDependentContinuant,
	EntityTypeCodeFile:         bfo.GenericallyDependentContinuant,
	EntityTypeCodeFunction:     bfo.GenericallyDependentContinuant,
	EntityTypeCodeStruct:       bfo.GenericallyDependentContinuant,
	EntityTypeCodeInterface:    bfo.GenericallyDependentContinuant,
	EntityTypeResult:           bfo.GenericallyDependentContinuant,
	EntityTypeConstitution:     bfo.GenericallyDependentContinuant,
	EntityTypeConstitutionRule: bfo.GenericallyDependentContinuant,
	EntityTypeRequirement:      bfo.GenericallyDependentContinuant,

	// Processes → Process
	EntityTypeLoop:      bfo.Process,
	EntityTypeActivity:  bfo.Process,
	EntityTypeModelCall: bfo.Process,
	EntityTypeToolCall:  bfo.Process,

	// Agents → IndependentContinuant
	EntityTypeUser:    bfo.IndependentContinuant,
	EntityTypeAIModel: bfo.IndependentContinuant,
}

// CCOClassMap maps entity types to CCO class IRIs.
// Use this for CCO profile RDF export.
var CCOClassMap = map[EntityType]string{
	// Information entities
	EntityTypePlan:             cco.InformationContentEntity,
	EntityTypeSpec:             cco.DirectiveInformationContentEntity,
	EntityTypeTask:             cco.PlanSpecification,
	EntityTypeCodeFile:         cco.SoftwareCode,
	EntityTypeCodeFunction:     cco.Algorithm, // Functions contain algorithms
	EntityTypeCodeStruct:       cco.SoftwareCode,
	EntityTypeCodeInterface:    cco.Specification,
	EntityTypeResult:           cco.InformationContentEntity,
	EntityTypeConstitution:     cco.DirectiveInformationContentEntity,
	EntityTypeConstitutionRule: cco.Requirement,
	EntityTypeRequirement:      cco.Requirement,

	// Processes
	EntityTypeLoop:      cco.ActOfArtifactProcessing,
	EntityTypeActivity:  cco.Act,
	EntityTypeModelCall: cco.ActOfCommunication,
	EntityTypeToolCall:  cco.ActOfArtifactProcessing,

	// Agents
	EntityTypeUser:    cco.Person,
	EntityTypeAIModel: cco.IntelligentSoftwareAgent,
}

// PROVClassMap maps entity types to PROV-O class IRIs.
// Use this for minimal and all profile RDF exports.
var PROVClassMap = map[EntityType]string{
	// Entities
	EntityTypePlan:             vocabulary.ProvEntity,
	EntityTypeSpec:             vocabulary.ProvEntity,
	EntityTypeTask:             vocabulary.ProvEntity,
	EntityTypeCodeFile:         vocabulary.ProvEntity,
	EntityTypeCodeFunction:     vocabulary.ProvEntity,
	EntityTypeCodeStruct:       vocabulary.ProvEntity,
	EntityTypeCodeInterface:    vocabulary.ProvEntity,
	EntityTypeResult:           vocabulary.ProvEntity,
	EntityTypeConstitution:     vocabulary.ProvEntity,
	EntityTypeConstitutionRule: vocabulary.ProvEntity,
	EntityTypeRequirement:      vocabulary.ProvEntity,

	// Activities
	EntityTypeLoop:      vocabulary.ProvActivity,
	EntityTypeActivity:  vocabulary.ProvActivity,
	EntityTypeModelCall: vocabulary.ProvActivity,
	EntityTypeToolCall:  vocabulary.ProvActivity,

	// Agents
	EntityTypeUser:    vocabulary.ProvPerson,
	EntityTypeAIModel: vocabulary.ProvSoftwareAgent,
}

// SemspecClassMap maps entity types to Semspec class IRIs.
// Use this for all profile RDF exports.
var SemspecClassMap = map[EntityType]string{
	EntityTypePlan:             ClassPlan,
	EntityTypeSpec:             ClassSpecification,
	EntityTypeTask:             ClassTask,
	EntityTypeCodeFile:         ClassCodeFile,
	EntityTypeCodeFunction:     ClassCodeFunction,
	EntityTypeCodeStruct:       ClassCodeStruct,
	EntityTypeCodeInterface:    ClassCodeInterface,
	EntityTypeLoop:             ClassLoop,
	EntityTypeActivity:         ClassActivity,
	EntityTypeModelCall:        ClassModelCall,
	EntityTypeToolCall:         ClassToolCall,
	EntityTypeResult:           ClassResult,
	EntityTypeUser:             ClassUser,
	EntityTypeAIModel:          ClassAIModel,
	EntityTypeConstitution:     ClassConstitution,
	EntityTypeConstitutionRule: ClassConstitutionRule,
	EntityTypeRequirement:      ClassRequirement,
}

// PredicateIRIMap maps internal predicates to standard IRIs.
// Use this for RDF export to translate dotted predicates to standard IRIs.
var PredicateIRIMap = map[string]string{
	// Plan predicates
	PlanAuthor:    vocabulary.ProvWasAttributedTo,
	PlanReviewer:  Namespace + "reviewer",
	PlanSpec:      Namespace + "hasSpec",
	PlanTask:      Namespace + "hasTask",
	PlanCreatedAt: vocabulary.ProvGeneratedAtTime,

	// Spec predicates
	SpecPlan:      vocabulary.ProvWasDerivedFrom,
	SpecAuthor:    vocabulary.ProvWasAttributedTo,
	SpecAffects:   cco.IsAbout,
	SpecDependsOn: vocabulary.ProvWasDerivedFrom,

	// Task predicates
	TaskSpec:        vocabulary.ProvWasDerivedFrom,
	TaskLoop:        vocabulary.ProvWasGeneratedBy,
	TaskAssignee:    vocabulary.ProvWasAttributedTo,
	TaskPredecessor: bfo.PrecedesTemporally,
	TaskSuccessor:   bfo.PrecedesTemporally, // inverse

	// Loop predicates
	LoopTask:      vocabulary.ProvUsed,
	LoopUser:      vocabulary.ProvWasAssociatedWith,
	LoopAgent:     vocabulary.ProvWasAssociatedWith,
	LoopStartedAt: vocabulary.ProvStartedAtTime,
	LoopEndedAt:   vocabulary.ProvEndedAtTime,

	// Activity predicates
	ActivityLoop:      bfo.PartOf,
	ActivityPrecedes:  bfo.PrecedesTemporally,
	ActivityFollows:   bfo.PrecedesTemporally, // inverse
	ActivityInput:     vocabulary.ProvUsed,
	ActivityOutput:    vocabulary.ProvGenerated,
	ActivityStartedAt: vocabulary.ProvStartedAtTime,
	ActivityEndedAt:   vocabulary.ProvEndedAtTime,

	// Result predicates
	ResultLoop:       vocabulary.ProvWasGeneratedBy,
	ResultArtifacts:  vocabulary.ProvGenerated,
	ResultApprovedBy: vocabulary.ProvWasAttributedTo,

	// Code structure predicates
	CodeContains:  bfo.HasPart,
	CodeBelongsTo: bfo.PartOf,

	// PROV-O aligned predicates
	ProvGeneratedBy:     vocabulary.ProvWasGeneratedBy,
	ProvAttributedTo:    vocabulary.ProvWasAttributedTo,
	ProvDerivedFrom:     vocabulary.ProvWasDerivedFrom,
	ProvUsed:            vocabulary.ProvUsed,
	ProvAssociatedWith:  vocabulary.ProvWasAssociatedWith,
	ProvActedOnBehalfOf: vocabulary.ProvActedOnBehalfOf,
	ProvStartedAt:       vocabulary.ProvStartedAtTime,
	ProvEndedAt:         vocabulary.ProvEndedAtTime,
	ProvGeneratedAt:     vocabulary.ProvGeneratedAtTime,

	// Dublin Core predicates
	DCTitle:       vocabulary.DcTitle,
	DCDescription: "http://purl.org/dc/terms/description",
	DCCreator:     "http://purl.org/dc/terms/creator",
	DCCreated:     "http://purl.org/dc/terms/created",
	DCModified:    "http://purl.org/dc/terms/modified",
	DCIdentifier:  vocabulary.DcIdentifier,
	DCSource:      vocabulary.DcSource,

	// SKOS predicates
	SKOSPrefLabel: vocabulary.SkosPrefLabel,
	SKOSAltLabel:  vocabulary.SkosAltLabel,
	SKOSBroader:   vocabulary.SkosBroader,
	SKOSNarrower:  vocabulary.SkosNarrower,
	SKOSRelated:   vocabulary.SkosRelated,
}

// GetTypesForEntity returns all type IRIs for a given entity type and profile.
// Profile determines which ontology types are included:
//   - "minimal": PROV-O + Semspec types
//   - "bfo": BFO + PROV-O + Semspec types
//   - "cco": CCO + BFO + PROV-O + Semspec types
func GetTypesForEntity(entityType EntityType, profile string) []string {
	types := make([]string, 0, 4)

	// Always include Semspec type
	if semspecClass, ok := SemspecClassMap[entityType]; ok {
		types = append(types, semspecClass)
	}

	// Always include PROV-O type
	if provClass, ok := PROVClassMap[entityType]; ok {
		types = append(types, provClass)
	}

	// Include BFO type for bfo and cco profiles
	if profile == "bfo" || profile == "cco" {
		if bfoClass, ok := BFOClassMap[entityType]; ok {
			types = append(types, bfoClass)
		}
	}

	// Include CCO type for cco profile
	if profile == "cco" {
		if ccoClass, ok := CCOClassMap[entityType]; ok {
			types = append(types, ccoClass)
		}
	}

	return types
}

// GetPredicateIRI returns the standard IRI for a predicate, if mapped.
// Returns the original predicate if no mapping exists.
func GetPredicateIRI(predicate string) string {
	if iri, ok := PredicateIRIMap[predicate]; ok {
		return iri
	}
	// Fall back to semspec namespace for unmapped predicates
	return Namespace + predicate
}
