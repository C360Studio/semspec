package requirementgenerator

import (
	"reflect"
	"testing"

	"github.com/c360studio/semspec/prompt"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// TestBuildPromptContext_FreshGeneration covers the trigger → prompt context
// adapter for first-pass requirement generation. Prompt content correctness
// is upstream in prompt/domain/software_render_test.go; this test only pins
// the field mapping so refactors of the component or trigger payload don't
// silently drop data on the floor before it reaches the renderer.
func TestBuildPromptContext_FreshGeneration(t *testing.T) {
	trigger := &payloads.RequirementGeneratorRequest{
		Slug:    "test",
		Title:   "Test Plan",
		Goal:    "Add endpoint",
		Context: "Flask API",
		Scope: &workflow.Scope{
			Include:    []string{"api/app.py"},
			Exclude:    []string{"docs/"},
			DoNotTouch: []string{"README.md"},
		},
	}

	got := buildRequirementGeneratorPromptContext(trigger, "")

	if got.Title != "Test Plan" || got.Goal != "Add endpoint" || got.Context != "Flask API" {
		t.Errorf("trigger plan fields not mapped: got %+v", got)
	}
	if !reflect.DeepEqual(got.ScopeInclude, []string{"api/app.py"}) {
		t.Errorf("ScopeInclude mapping wrong: %v", got.ScopeInclude)
	}
	if !reflect.DeepEqual(got.ScopeExclude, []string{"docs/"}) {
		t.Errorf("ScopeExclude mapping wrong: %v", got.ScopeExclude)
	}
	if !reflect.DeepEqual(got.ScopeDoNotTouch, []string{"README.md"}) {
		t.Errorf("ScopeDoNotTouch mapping wrong: %v", got.ScopeDoNotTouch)
	}
	if got.PreviousError != "" {
		t.Errorf("PreviousError should be empty, got %q", got.PreviousError)
	}
	if got.ReviewFindings != "" {
		t.Errorf("ReviewFindings should be empty, got %q", got.ReviewFindings)
	}
	if len(got.ExistingRequirements) != 0 {
		t.Errorf("ExistingRequirements should be empty for fresh generation, got %d", len(got.ExistingRequirements))
	}
}

// TestBuildPromptContext_PartialRegen confirms surviving requirements are
// passed through with their dial-#1 fields intact (FilesOwned, DependsOn) so
// the LLM can reason about scope partitioning, plus that RejectionReasons
// flow through.
func TestBuildPromptContext_PartialRegen(t *testing.T) {
	trigger := &payloads.RequirementGeneratorRequest{
		Slug: "test",
		Goal: "Add endpoint",
		ExistingRequirements: []workflow.Requirement{
			{
				ID:         "requirement.test.1",
				Title:      "kept-req",
				Status:     workflow.RequirementStatusActive,
				FilesOwned: []string{"api/app.py"},
				DependsOn:  []string{"requirement.test.0"},
			},
		},
		ReplaceRequirementIDs: []string{"requirement.test.2"},
		RejectionReasons: map[string]string{
			"requirement.test.2": "scope too broad",
		},
	}

	got := buildRequirementGeneratorPromptContext(trigger, "")

	if len(got.ExistingRequirements) != 1 {
		t.Fatalf("ExistingRequirements length = %d, want 1", len(got.ExistingRequirements))
	}
	r := got.ExistingRequirements[0]
	if r.ID != "requirement.test.1" || r.Title != "kept-req" || r.Status != "active" {
		t.Errorf("kept requirement summary wrong: %+v", r)
	}
	if !reflect.DeepEqual(r.FilesOwned, []string{"api/app.py"}) {
		t.Errorf("FilesOwned not mapped: %v", r.FilesOwned)
	}
	if !reflect.DeepEqual(r.DependsOn, []string{"requirement.test.0"}) {
		t.Errorf("DependsOn not mapped: %v", r.DependsOn)
	}
	if !reflect.DeepEqual(got.ReplaceRequirementIDs, []string{"requirement.test.2"}) {
		t.Errorf("ReplaceRequirementIDs not mapped: %v", got.ReplaceRequirementIDs)
	}
	if got.RejectionReasons["requirement.test.2"] != "scope too broad" {
		t.Errorf("RejectionReasons not mapped: %v", got.RejectionReasons)
	}
}

// TestBuildPromptContext_PreviousErrorAndReviewFindings pins the retry
// surface — prior parse errors and prior review findings must reach the
// prompt context so the renderer can surface them to the LLM.
func TestBuildPromptContext_PreviousErrorAndReviewFindings(t *testing.T) {
	trigger := &payloads.RequirementGeneratorRequest{Slug: "test", Goal: "x"}
	got := buildRequirementGeneratorPromptContext(trigger, "json parse failure", "missing scenarios")
	if got.PreviousError != "json parse failure" {
		t.Errorf("PreviousError = %q, want %q", got.PreviousError, "json parse failure")
	}
	if got.ReviewFindings != "missing scenarios" {
		t.Errorf("ReviewFindings = %q, want %q", got.ReviewFindings, "missing scenarios")
	}
}

// TestBuildPromptContext_ProducesNonNilForEmptyTrigger guards against the
// adapter returning nil — the assembler treats nil RequirementGenerator as a
// programming error and surfaces a render error, so the adapter must always
// return a usable context even for nearly-empty triggers.
func TestBuildPromptContext_ProducesNonNilForEmptyTrigger(t *testing.T) {
	got := buildRequirementGeneratorPromptContext(&payloads.RequirementGeneratorRequest{}, "")
	if got == nil {
		t.Fatal("buildRequirementGeneratorPromptContext must never return nil")
	}
	// Spot-check that we got a real prompt.RequirementGeneratorContext, not
	// a typed nil that satisfies the pointer comparison.
	_ = *got
	_ = (any)(got).(*prompt.RequirementGeneratorContext)
}
