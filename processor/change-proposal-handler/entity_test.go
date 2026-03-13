package changeproposalhandler

import (
	"strings"
	"testing"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
)

func TestCascadeEntity_EntityID(t *testing.T) {
	tests := []struct {
		name       string
		slug       string
		proposalID string
		want       string
	}{
		{
			name:       "basic",
			slug:       "my-feature",
			proposalID: "prop-abc-123",
			want:       "local.semspec.workflow.cascade.execution.my-feature-prop-abc-123",
		},
		{
			name:       "auth",
			slug:       "auth-refresh",
			proposalID: "cp-001",
			want:       "local.semspec.workflow.cascade.execution.auth-refresh-cp-001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &CascadeEntity{Slug: tt.slug, ProposalID: tt.proposalID}
			got := e.EntityID()
			if got != tt.want {
				t.Errorf("EntityID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCascadeEntity_EntityID_6PartFormat(t *testing.T) {
	e := &CascadeEntity{Slug: "test-slug", ProposalID: "prop-1"}
	parts := strings.Split(e.EntityID(), ".")
	if len(parts) != 6 {
		t.Errorf("EntityID() has %d dot-separated parts, want 6: %q", len(parts), e.EntityID())
	}
}

func TestCascadeEntity_Triples_RequiredPredicates(t *testing.T) {
	e := &CascadeEntity{
		Slug:                      "test-slug",
		ProposalID:                "prop-1",
		AffectedRequirementsCount: 0,
		AffectedScenariosCount:    0,
		TasksDirtied:              0,
	}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	required := []string{
		wf.Type, wf.Slug,
		wf.CascadeAffectedRequirements,
		wf.CascadeAffectedScenarios,
		wf.CascadeTasksDirtied,
	}
	for _, pred := range required {
		if !predicates[pred] {
			t.Errorf("Triples() missing required predicate %q", pred)
		}
	}
}

func TestCascadeEntity_Triples_TypeIsCascade(t *testing.T) {
	e := &CascadeEntity{Slug: "s", ProposalID: "p"}
	for _, tr := range e.Triples() {
		if tr.Predicate == wf.Type {
			if tr.Object != "cascade" {
				t.Errorf("wf.Type triple object = %q, want %q", tr.Object, "cascade")
			}
			return
		}
	}
	t.Error("Triples() missing wf.Type triple")
}

func TestCascadeEntity_Triples_OptionalPredicatesOmittedWhenEmpty(t *testing.T) {
	e := &CascadeEntity{Slug: "test-slug", ProposalID: "prop-1"}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	optional := []string{wf.Phase, wf.TraceID, wf.ErrorReason, wf.RelRequirement}
	for _, pred := range optional {
		if predicates[pred] {
			t.Errorf("Triples() should not emit predicate %q when field is empty", pred)
		}
	}
}

func TestCascadeEntity_Triples_OptionalPredicatesIncludedWhenSet(t *testing.T) {
	e := &CascadeEntity{
		Slug:       "test-slug",
		ProposalID: "prop-1",
		Phase:      "completed",
		TraceID:    "trace-abc",
	}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	expected := []string{wf.Phase, wf.TraceID}
	for _, pred := range expected {
		if !predicates[pred] {
			t.Errorf("Triples() missing predicate %q when field is set", pred)
		}
	}
}

func TestCascadeEntity_Triples_RequirementRelationshipTriples(t *testing.T) {
	reqID1 := "local.semspec.requirement.default.requirement.req-1"
	reqID2 := "local.semspec.requirement.default.requirement.req-2"
	reqID3 := "" // empty — should be skipped

	e := &CascadeEntity{
		Slug:       "test-slug",
		ProposalID: "prop-1",
		AffectedRequirementEntityIDs: []string{reqID1, reqID2, reqID3},
	}

	relCount := 0
	var objects []string
	for _, tr := range e.Triples() {
		if tr.Predicate == wf.RelRequirement {
			relCount++
			objects = append(objects, tr.Object.(string))
		}
	}

	// Only 2 non-empty IDs should produce triples.
	if relCount != 2 {
		t.Errorf("RelRequirement triple count = %d, want 2", relCount)
	}

	found1 := false
	found2 := false
	for _, obj := range objects {
		if obj == reqID1 {
			found1 = true
		}
		if obj == reqID2 {
			found2 = true
		}
	}
	if !found1 {
		t.Errorf("RelRequirement triple for %q not found", reqID1)
	}
	if !found2 {
		t.Errorf("RelRequirement triple for %q not found", reqID2)
	}
}

func TestCascadeEntity_Triples_EmptyRequirementListProducesNoRelTriples(t *testing.T) {
	e := &CascadeEntity{
		Slug:                         "test-slug",
		ProposalID:                   "prop-1",
		AffectedRequirementEntityIDs: nil,
	}

	for _, tr := range e.Triples() {
		if tr.Predicate == wf.RelRequirement {
			t.Error("RelRequirement triple should not be emitted when list is empty")
		}
	}
}

func TestCascadeEntity_Triples_SubjectMatchesEntityID(t *testing.T) {
	e := &CascadeEntity{Slug: "slug", ProposalID: "prop-1"}

	entityID := e.EntityID()
	for _, tr := range e.Triples() {
		if tr.Subject != entityID {
			t.Errorf("triple Subject = %q, want %q (predicate: %s)", tr.Subject, entityID, tr.Predicate)
		}
	}
}

func TestNewCascadeEntity_Fields(t *testing.T) {
	entity := NewCascadeEntity("prop-xyz", "my-slug", "trace-abc", 3, 2, 5)

	if entity.ProposalID != "prop-xyz" {
		t.Errorf("ProposalID = %q, want %q", entity.ProposalID, "prop-xyz")
	}
	if entity.Slug != "my-slug" {
		t.Errorf("Slug = %q, want %q", entity.Slug, "my-slug")
	}
	if entity.TraceID != "trace-abc" {
		t.Errorf("TraceID = %q, want %q", entity.TraceID, "trace-abc")
	}
	if entity.AffectedRequirementsCount != 3 {
		t.Errorf("AffectedRequirementsCount = %d, want 3", entity.AffectedRequirementsCount)
	}
	if entity.AffectedScenariosCount != 2 {
		t.Errorf("AffectedScenariosCount = %d, want 2", entity.AffectedScenariosCount)
	}
	if entity.TasksDirtied != 5 {
		t.Errorf("TasksDirtied = %d, want 5", entity.TasksDirtied)
	}

	expectedID := "local.semspec.workflow.cascade.execution.my-slug-prop-xyz"
	if got := entity.EntityID(); got != expectedID {
		t.Errorf("EntityID() = %q, want %q", got, expectedID)
	}
}

func TestCascadeEntity_Triples_MetricValues(t *testing.T) {
	e := NewCascadeEntity("prop-1", "slug", "", 4, 7, 12)

	metricValues := make(map[string]any)
	for _, tr := range e.Triples() {
		switch tr.Predicate {
		case wf.CascadeAffectedRequirements, wf.CascadeAffectedScenarios, wf.CascadeTasksDirtied:
			metricValues[tr.Predicate] = tr.Object
		}
	}

	if got := metricValues[wf.CascadeAffectedRequirements]; got != 4 {
		t.Errorf("CascadeAffectedRequirements = %v, want 4", got)
	}
	if got := metricValues[wf.CascadeAffectedScenarios]; got != 7 {
		t.Errorf("CascadeAffectedScenarios = %v, want 7", got)
	}
	if got := metricValues[wf.CascadeTasksDirtied]; got != 12 {
		t.Errorf("CascadeTasksDirtied = %v, want 12", got)
	}
}
