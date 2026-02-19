// Package export provides RDF export profiles and type assertion utilities
// for mapping semspec entities to standard ontology classes.
package export

import (
	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/bfo"
	"github.com/c360studio/semstreams/vocabulary/cco"
)

// Profile determines which ontology type assertions are included in the export.
type Profile string

const (
	// ProfileMinimal includes only PROV-O, Dublin Core, and SKOS predicates.
	ProfileMinimal Profile = "minimal"

	// ProfileBFO includes BFO type assertions plus minimal profile.
	ProfileBFO Profile = "bfo"

	// ProfileCCO includes CCO type assertions plus BFO profile.
	ProfileCCO Profile = "cco"
)

// ProfileConfig contains configuration for an export profile.
type ProfileConfig struct {
	// Name is the profile identifier.
	Name Profile

	// Description describes the profile.
	Description string

	// IncludeBFO indicates whether to include BFO type assertions.
	IncludeBFO bool

	// IncludeCCO indicates whether to include CCO type assertions.
	IncludeCCO bool

	// IncludePROV indicates whether to include PROV-O type assertions.
	IncludePROV bool

	// IncludeSemspec indicates whether to include Semspec type assertions.
	IncludeSemspec bool

	// TranslatePredicates indicates whether to translate predicates to standard IRIs.
	TranslatePredicates bool
}

// Profiles contains the configuration for all available export profiles.
var Profiles = map[Profile]ProfileConfig{
	ProfileMinimal: {
		Name:                ProfileMinimal,
		Description:         "PROV-O, Dublin Core, and SKOS predicates only",
		IncludeBFO:          false,
		IncludeCCO:          false,
		IncludePROV:         true,
		IncludeSemspec:      true,
		TranslatePredicates: true,
	},
	ProfileBFO: {
		Name:                ProfileBFO,
		Description:         "BFO type assertions plus minimal profile",
		IncludeBFO:          true,
		IncludeCCO:          false,
		IncludePROV:         true,
		IncludeSemspec:      true,
		TranslatePredicates: true,
	},
	ProfileCCO: {
		Name:                ProfileCCO,
		Description:         "Full CCO/BFO/PROV-O alignment",
		IncludeBFO:          true,
		IncludeCCO:          true,
		IncludePROV:         true,
		IncludeSemspec:      true,
		TranslatePredicates: true,
	},
}

// GetProfileConfig returns the configuration for a profile.
func GetProfileConfig(profile Profile) ProfileConfig {
	if config, ok := Profiles[profile]; ok {
		return config
	}
	return Profiles[ProfileMinimal]
}

// TypeAsserter generates type assertions for entities based on profile.
type TypeAsserter struct {
	profile ProfileConfig
}

// NewTypeAsserter creates a new type asserter for the given profile.
func NewTypeAsserter(profile Profile) *TypeAsserter {
	return &TypeAsserter{
		profile: GetProfileConfig(profile),
	}
}

// GetTypeIRIs returns all type IRIs for an entity type based on the profile.
func (t *TypeAsserter) GetTypeIRIs(entityType semspec.EntityType) []string {
	types := make([]string, 0, 4)

	// Always include Semspec type when enabled
	if t.profile.IncludeSemspec {
		if semspecClass, ok := semspec.SemspecClassMap[entityType]; ok {
			types = append(types, semspecClass)
		}
	}

	// Include PROV-O type when enabled
	if t.profile.IncludePROV {
		if provClass, ok := semspec.PROVClassMap[entityType]; ok {
			types = append(types, provClass)
		}
	}

	// Include BFO type when enabled
	if t.profile.IncludeBFO {
		if bfoClass, ok := semspec.BFOClassMap[entityType]; ok {
			types = append(types, bfoClass)
		}
	}

	// Include CCO type when enabled
	if t.profile.IncludeCCO {
		if ccoClass, ok := semspec.CCOClassMap[entityType]; ok {
			types = append(types, ccoClass)
		}
	}

	return types
}

// TypeTriples returns rdf:type triples as []message.Triple for an entity
// based on its inferred type and the given profile.
func TypeTriples(entityID string, entityType semspec.EntityType, profile Profile) []message.Triple {
	asserter := NewTypeAsserter(profile)
	typeIRIs := asserter.GetTypeIRIs(entityType)
	triples := make([]message.Triple, 0, len(typeIRIs))
	for _, typeIRI := range typeIRIs {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  "rdf.syntax.type",
			Object:     typeIRI,
			Source:     "semspec.rdf-export",
			Confidence: 1.0,
		})
	}
	return triples
}

// TypeHierarchy represents the ontology type hierarchy for an entity.
type TypeHierarchy struct {
	// SemspecClass is the Semspec-specific class.
	SemspecClass string

	// PROVClass is the PROV-O class.
	PROVClass string

	// BFOClass is the BFO class.
	BFOClass string

	// CCOClass is the CCO class.
	CCOClass string
}

// GetTypeHierarchy returns the full type hierarchy for an entity type.
func GetTypeHierarchy(entityType semspec.EntityType) TypeHierarchy {
	return TypeHierarchy{
		SemspecClass: semspec.SemspecClassMap[entityType],
		PROVClass:    semspec.PROVClassMap[entityType],
		BFOClass:     semspec.BFOClassMap[entityType],
		CCOClass:     semspec.CCOClassMap[entityType],
	}
}

// BFOClassDescriptions provides human-readable descriptions for BFO classes.
var BFOClassDescriptions = map[string]string{
	bfo.Entity:                         "The root class of all BFO entities",
	bfo.Continuant:                     "Entities that persist through time",
	bfo.Occurrent:                      "Entities that unfold in time",
	bfo.IndependentContinuant:          "Entities that can exist on their own",
	bfo.GenericallyDependentContinuant: "Information patterns that can be copied",
	bfo.Process:                        "Events that unfold over time",
	bfo.Quality:                        "Measurable properties",
	bfo.Role:                           "Context-dependent functions",
}

// CCOClassDescriptions provides human-readable descriptions for CCO classes.
var CCOClassDescriptions = map[string]string{
	cco.InformationContentEntity:          "Root class for information entities",
	cco.DirectiveInformationContentEntity: "Prescriptive information content",
	cco.PlanSpecification:                 "Planned set of actions",
	cco.SoftwareCode:                      "Source code artifact",
	cco.ActOfArtifactProcessing:           "Processing of an artifact",
	cco.ActOfCommunication:                "Information transmission",
	cco.Person:                            "Human agent",
	cco.IntelligentSoftwareAgent:          "Autonomous software agent",
}

// PROVClassDescriptions provides human-readable descriptions for PROV-O classes.
var PROVClassDescriptions = map[string]string{
	vocabulary.ProvEntity:        "Thing with fixed aspects",
	vocabulary.ProvActivity:      "Something that occurs over time",
	vocabulary.ProvAgent:         "Something bearing responsibility",
	vocabulary.ProvPerson:        "Human agent",
	vocabulary.ProvSoftwareAgent: "Software agent",
}

// InferEntityType attempts to infer the entity type from an entity ID.
func InferEntityType(entityID string) semspec.EntityType {
	// Entity ID format: org.semspec.context.domain.type.instance
	// Examples:
	//   acme.semspec.project.proposal.api.auth-refresh
	//   acme.semspec.agent.loop.api.loop-abc123
	//   acme.semspec.code.file.api.a1b2c3d4

	parts := splitEntityID(entityID)
	if len(parts) < 5 {
		return ""
	}

	context := parts[2]
	domain := parts[3]
	entityType := parts[4]

	// Map based on context and type
	switch context {
	case "project", "workflow":
		switch domain {
		case "plan":
			return semspec.EntityTypePlan
		case "spec":
			return semspec.EntityTypeSpec
		case "task":
			return semspec.EntityTypeTask
		}
	case "agent":
		switch domain {
		case "loop":
			return semspec.EntityTypeLoop
		case "activity":
			if entityType == "model_call" {
				return semspec.EntityTypeModelCall
			}
			if entityType == "tool_call" {
				return semspec.EntityTypeToolCall
			}
			return semspec.EntityTypeActivity
		case "result":
			return semspec.EntityTypeResult
		case "model":
			return semspec.EntityTypeAIModel
		}
	case "user":
		return semspec.EntityTypeUser
	case "code":
		switch domain {
		case "file":
			return semspec.EntityTypeCodeFile
		case "function":
			return semspec.EntityTypeCodeFunction
		case "struct":
			return semspec.EntityTypeCodeStruct
		case "interface":
			return semspec.EntityTypeCodeInterface
		}
	case "config":
		if domain == "constitution" {
			return semspec.EntityTypeConstitution
		}
	}

	return ""
}

// splitEntityID splits an entity ID into its component parts.
func splitEntityID(entityID string) []string {
	result := make([]string, 0, 8)
	current := ""
	for _, c := range entityID {
		if c == '.' {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
