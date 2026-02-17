package semspec_test

import (
	"testing"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/bfo"
	"github.com/c360studio/semstreams/vocabulary/cco"
)

func TestBFOClassMap(t *testing.T) {
	tests := []struct {
		entityType semspec.EntityType
		wantBFO    string
	}{
		{semspec.EntityTypePlan, bfo.GenericallyDependentContinuant},
		{semspec.EntityTypeSpec, bfo.GenericallyDependentContinuant},
		{semspec.EntityTypeTask, bfo.GenericallyDependentContinuant},
		{semspec.EntityTypeCodeFile, bfo.GenericallyDependentContinuant},
		{semspec.EntityTypeLoop, bfo.Process},
		{semspec.EntityTypeModelCall, bfo.Process},
		{semspec.EntityTypeUser, bfo.IndependentContinuant},
		{semspec.EntityTypeAIModel, bfo.IndependentContinuant},
	}

	for _, tc := range tests {
		t.Run(string(tc.entityType), func(t *testing.T) {
			got, ok := semspec.BFOClassMap[tc.entityType]
			if !ok {
				t.Fatalf("entity type %q not in BFOClassMap", tc.entityType)
			}
			if got != tc.wantBFO {
				t.Errorf("got %q, want %q", got, tc.wantBFO)
			}
		})
	}
}

func TestCCOClassMap(t *testing.T) {
	tests := []struct {
		entityType semspec.EntityType
		wantCCO    string
	}{
		{semspec.EntityTypePlan, cco.InformationContentEntity},
		{semspec.EntityTypeSpec, cco.DirectiveInformationContentEntity},
		{semspec.EntityTypeTask, cco.PlanSpecification},
		{semspec.EntityTypeCodeFile, cco.SoftwareCode},
		{semspec.EntityTypeLoop, cco.ActOfArtifactProcessing},
		{semspec.EntityTypeModelCall, cco.ActOfCommunication},
		{semspec.EntityTypeUser, cco.Person},
		{semspec.EntityTypeAIModel, cco.IntelligentSoftwareAgent},
	}

	for _, tc := range tests {
		t.Run(string(tc.entityType), func(t *testing.T) {
			got, ok := semspec.CCOClassMap[tc.entityType]
			if !ok {
				t.Fatalf("entity type %q not in CCOClassMap", tc.entityType)
			}
			if got != tc.wantCCO {
				t.Errorf("got %q, want %q", got, tc.wantCCO)
			}
		})
	}
}

func TestPROVClassMap(t *testing.T) {
	tests := []struct {
		entityType semspec.EntityType
		wantPROV   string
	}{
		{semspec.EntityTypePlan, vocabulary.ProvEntity},
		{semspec.EntityTypeSpec, vocabulary.ProvEntity},
		{semspec.EntityTypeTask, vocabulary.ProvEntity},
		{semspec.EntityTypeLoop, vocabulary.ProvActivity},
		{semspec.EntityTypeActivity, vocabulary.ProvActivity},
		{semspec.EntityTypeUser, vocabulary.ProvPerson},
		{semspec.EntityTypeAIModel, vocabulary.ProvSoftwareAgent},
	}

	for _, tc := range tests {
		t.Run(string(tc.entityType), func(t *testing.T) {
			got, ok := semspec.PROVClassMap[tc.entityType]
			if !ok {
				t.Fatalf("entity type %q not in PROVClassMap", tc.entityType)
			}
			if got != tc.wantPROV {
				t.Errorf("got %q, want %q", got, tc.wantPROV)
			}
		})
	}
}

func TestSemspecClassMap(t *testing.T) {
	tests := []struct {
		entityType  semspec.EntityType
		wantSemspec string
	}{
		{semspec.EntityTypePlan, semspec.ClassPlan},
		{semspec.EntityTypeSpec, semspec.ClassSpecification},
		{semspec.EntityTypeTask, semspec.ClassTask},
		{semspec.EntityTypeCodeFile, semspec.ClassCodeFile},
		{semspec.EntityTypeLoop, semspec.ClassLoop},
		{semspec.EntityTypeUser, semspec.ClassUser},
		{semspec.EntityTypeAIModel, semspec.ClassAIModel},
	}

	for _, tc := range tests {
		t.Run(string(tc.entityType), func(t *testing.T) {
			got, ok := semspec.SemspecClassMap[tc.entityType]
			if !ok {
				t.Fatalf("entity type %q not in SemspecClassMap", tc.entityType)
			}
			if got != tc.wantSemspec {
				t.Errorf("got %q, want %q", got, tc.wantSemspec)
			}
		})
	}
}

func TestGetTypesForEntity_MinimalProfile(t *testing.T) {
	types := semspec.GetTypesForEntity(semspec.EntityTypePlan, "minimal")

	// Minimal should include Semspec + PROV types
	if len(types) < 2 {
		t.Errorf("expected at least 2 types, got %d", len(types))
	}

	hasProvEntity := false
	hasSemspecPlan := false
	for _, typ := range types {
		if typ == vocabulary.ProvEntity {
			hasProvEntity = true
		}
		if typ == semspec.ClassPlan {
			hasSemspecPlan = true
		}
	}

	if !hasProvEntity {
		t.Error("minimal profile should include prov:Entity")
	}
	if !hasSemspecPlan {
		t.Error("minimal profile should include semspec:Plan")
	}
}

func TestGetTypesForEntity_BFOProfile(t *testing.T) {
	types := semspec.GetTypesForEntity(semspec.EntityTypePlan, "bfo")

	// BFO should include Semspec + PROV + BFO types
	if len(types) < 3 {
		t.Errorf("expected at least 3 types, got %d", len(types))
	}

	hasBFOGDC := false
	for _, typ := range types {
		if typ == bfo.GenericallyDependentContinuant {
			hasBFOGDC = true
		}
	}

	if !hasBFOGDC {
		t.Error("bfo profile should include bfo:GenericallyDependentContinuant")
	}
}

func TestGetTypesForEntity_CCOProfile(t *testing.T) {
	types := semspec.GetTypesForEntity(semspec.EntityTypePlan, "cco")

	// CCO should include all types: Semspec + PROV + BFO + CCO
	if len(types) < 4 {
		t.Errorf("expected at least 4 types, got %d", len(types))
	}

	hasCCOICE := false
	for _, typ := range types {
		if typ == cco.InformationContentEntity {
			hasCCOICE = true
		}
	}

	if !hasCCOICE {
		t.Error("cco profile should include cco:InformationContentEntity")
	}
}

func TestGetPredicateIRI(t *testing.T) {
	tests := []struct {
		predicate string
		wantIRI   string
	}{
		{semspec.PlanAuthor, vocabulary.ProvWasAttributedTo},
		{semspec.SpecPlan, vocabulary.ProvWasDerivedFrom},
		{semspec.LoopTask, vocabulary.ProvUsed},
		{semspec.CodeContains, bfo.HasPart},
		// Unmapped predicate should get semspec namespace
		{"some.unknown.predicate", semspec.Namespace + "some.unknown.predicate"},
	}

	for _, tc := range tests {
		t.Run(tc.predicate, func(t *testing.T) {
			got := semspec.GetPredicateIRI(tc.predicate)
			if got != tc.wantIRI {
				t.Errorf("got %q, want %q", got, tc.wantIRI)
			}
		})
	}
}

func TestEntityTypesComplete(t *testing.T) {
	// Verify all entity types are in all maps
	entityTypes := []semspec.EntityType{
		semspec.EntityTypePlan,
		semspec.EntityTypeSpec,
		semspec.EntityTypeTask,
		semspec.EntityTypeCodeFile,
		semspec.EntityTypeCodeFunction,
		semspec.EntityTypeCodeStruct,
		semspec.EntityTypeCodeInterface,
		semspec.EntityTypeLoop,
		semspec.EntityTypeActivity,
		semspec.EntityTypeModelCall,
		semspec.EntityTypeToolCall,
		semspec.EntityTypeResult,
		semspec.EntityTypeUser,
		semspec.EntityTypeAIModel,
		semspec.EntityTypeConstitution,
		semspec.EntityTypeConstitutionRule,
		semspec.EntityTypeRequirement,
	}

	for _, et := range entityTypes {
		t.Run(string(et), func(t *testing.T) {
			if _, ok := semspec.BFOClassMap[et]; !ok {
				t.Errorf("entity type %q missing from BFOClassMap", et)
			}
			if _, ok := semspec.CCOClassMap[et]; !ok {
				t.Errorf("entity type %q missing from CCOClassMap", et)
			}
			if _, ok := semspec.PROVClassMap[et]; !ok {
				t.Errorf("entity type %q missing from PROVClassMap", et)
			}
			if _, ok := semspec.SemspecClassMap[et]; !ok {
				t.Errorf("entity type %q missing from SemspecClassMap", et)
			}
		})
	}
}
