package export_test

import (
	"testing"

	"github.com/c360studio/semspec/export"
	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/bfo"
	"github.com/c360studio/semstreams/vocabulary/cco"
)

func TestGetProfileConfig(t *testing.T) {
	tests := []struct {
		profile    export.Profile
		wantBFO    bool
		wantCCO    bool
		wantPROV   bool
	}{
		{export.ProfileMinimal, false, false, true},
		{export.ProfileBFO, true, false, true},
		{export.ProfileCCO, true, true, true},
	}

	for _, tc := range tests {
		t.Run(string(tc.profile), func(t *testing.T) {
			config := export.GetProfileConfig(tc.profile)
			if config.IncludeBFO != tc.wantBFO {
				t.Errorf("IncludeBFO = %v, want %v", config.IncludeBFO, tc.wantBFO)
			}
			if config.IncludeCCO != tc.wantCCO {
				t.Errorf("IncludeCCO = %v, want %v", config.IncludeCCO, tc.wantCCO)
			}
			if config.IncludePROV != tc.wantPROV {
				t.Errorf("IncludePROV = %v, want %v", config.IncludePROV, tc.wantPROV)
			}
		})
	}
}

func TestGetProfileConfigUnknown(t *testing.T) {
	// Unknown profile should default to minimal
	config := export.GetProfileConfig("unknown")
	if config.Name != export.ProfileMinimal {
		t.Errorf("Unknown profile should default to minimal, got %s", config.Name)
	}
}

func TestTypeAsserterMinimal(t *testing.T) {
	asserter := export.NewTypeAsserter(export.ProfileMinimal)

	types := asserter.GetTypeIRIs(semspec.EntityTypeProposal)

	hasProvEntity := false
	hasSemspecClass := false
	for _, typ := range types {
		if typ == vocabulary.ProvEntity {
			hasProvEntity = true
		}
		if typ == semspec.ClassProposal {
			hasSemspecClass = true
		}
	}

	if !hasProvEntity {
		t.Error("Minimal profile should include PROV-O type")
	}
	if !hasSemspecClass {
		t.Error("Minimal profile should include Semspec type")
	}
}

func TestTypeAsserterBFO(t *testing.T) {
	asserter := export.NewTypeAsserter(export.ProfileBFO)

	types := asserter.GetTypeIRIs(semspec.EntityTypeProposal)

	hasBFOClass := false
	for _, typ := range types {
		if typ == bfo.GenericallyDependentContinuant {
			hasBFOClass = true
		}
	}

	if !hasBFOClass {
		t.Error("BFO profile should include BFO type")
	}
}

func TestTypeAsserterCCO(t *testing.T) {
	asserter := export.NewTypeAsserter(export.ProfileCCO)

	types := asserter.GetTypeIRIs(semspec.EntityTypeProposal)

	hasCCOClass := false
	for _, typ := range types {
		if typ == cco.InformationContentEntity {
			hasCCOClass = true
		}
	}

	if !hasCCOClass {
		t.Error("CCO profile should include CCO type")
	}
}

func TestGetTypeHierarchy(t *testing.T) {
	tests := []struct {
		entityType     semspec.EntityType
		wantSemspec    string
		wantPROV       string
		wantBFO        string
		wantCCO        string
	}{
		{
			semspec.EntityTypeProposal,
			semspec.ClassProposal,
			vocabulary.ProvEntity,
			bfo.GenericallyDependentContinuant,
			cco.InformationContentEntity,
		},
		{
			semspec.EntityTypeLoop,
			semspec.ClassLoop,
			vocabulary.ProvActivity,
			bfo.Process,
			cco.ActOfArtifactProcessing,
		},
		{
			semspec.EntityTypeUser,
			semspec.ClassUser,
			vocabulary.ProvPerson,
			bfo.IndependentContinuant,
			cco.Person,
		},
	}

	for _, tc := range tests {
		t.Run(string(tc.entityType), func(t *testing.T) {
			h := export.GetTypeHierarchy(tc.entityType)
			if h.SemspecClass != tc.wantSemspec {
				t.Errorf("SemspecClass = %q, want %q", h.SemspecClass, tc.wantSemspec)
			}
			if h.PROVClass != tc.wantPROV {
				t.Errorf("PROVClass = %q, want %q", h.PROVClass, tc.wantPROV)
			}
			if h.BFOClass != tc.wantBFO {
				t.Errorf("BFOClass = %q, want %q", h.BFOClass, tc.wantBFO)
			}
			if h.CCOClass != tc.wantCCO {
				t.Errorf("CCOClass = %q, want %q", h.CCOClass, tc.wantCCO)
			}
		})
	}
}

func TestInferEntityType(t *testing.T) {
	tests := []struct {
		entityID   string
		wantType   semspec.EntityType
	}{
		{"acme.semspec.project.proposal.api.auth-refresh", semspec.EntityTypeProposal},
		{"acme.semspec.project.spec.api.auth-refresh-v1", semspec.EntityTypeSpec},
		{"acme.semspec.project.task.api.task-1", semspec.EntityTypeTask},
		{"acme.semspec.agent.loop.api.loop-123", semspec.EntityTypeLoop},
		{"acme.semspec.agent.result.api.res-1", semspec.EntityTypeResult},
		{"acme.semspec.code.file.api.a1b2c3", semspec.EntityTypeCodeFile},
		{"acme.semspec.code.function.api.DoSomething", semspec.EntityTypeCodeFunction},
		{"acme.semspec.user.person.dev.coby", semspec.EntityTypeUser},
		{"acme.semspec.agent.model.ollama.qwen-32b", semspec.EntityTypeAIModel},
		{"acme.semspec.config.constitution.api.v1", semspec.EntityTypeConstitution},
	}

	for _, tc := range tests {
		t.Run(tc.entityID, func(t *testing.T) {
			got := export.InferEntityType(tc.entityID)
			if got != tc.wantType {
				t.Errorf("InferEntityType(%q) = %q, want %q", tc.entityID, got, tc.wantType)
			}
		})
	}
}

func TestInferEntityTypeShortID(t *testing.T) {
	// Short IDs should return empty string
	got := export.InferEntityType("too.short")
	if got != "" {
		t.Errorf("Short ID should return empty entity type, got %q", got)
	}
}

func TestBFOClassDescriptions(t *testing.T) {
	if len(export.BFOClassDescriptions) == 0 {
		t.Error("BFOClassDescriptions should not be empty")
	}

	// Check for some expected entries
	if _, ok := export.BFOClassDescriptions[bfo.Process]; !ok {
		t.Error("BFOClassDescriptions should contain Process")
	}
	if _, ok := export.BFOClassDescriptions[bfo.GenericallyDependentContinuant]; !ok {
		t.Error("BFOClassDescriptions should contain GenericallyDependentContinuant")
	}
}

func TestCCOClassDescriptions(t *testing.T) {
	if len(export.CCOClassDescriptions) == 0 {
		t.Error("CCOClassDescriptions should not be empty")
	}

	// Check for some expected entries
	if _, ok := export.CCOClassDescriptions[cco.InformationContentEntity]; !ok {
		t.Error("CCOClassDescriptions should contain InformationContentEntity")
	}
	if _, ok := export.CCOClassDescriptions[cco.Person]; !ok {
		t.Error("CCOClassDescriptions should contain Person")
	}
}

func TestPROVClassDescriptions(t *testing.T) {
	if len(export.PROVClassDescriptions) == 0 {
		t.Error("PROVClassDescriptions should not be empty")
	}

	// Check for some expected entries
	if _, ok := export.PROVClassDescriptions[vocabulary.ProvEntity]; !ok {
		t.Error("PROVClassDescriptions should contain Entity")
	}
	if _, ok := export.PROVClassDescriptions[vocabulary.ProvActivity]; !ok {
		t.Error("PROVClassDescriptions should contain Activity")
	}
}
