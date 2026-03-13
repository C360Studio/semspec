package plancoordinator

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
	wf "github.com/c360studio/semspec/vocabulary/workflow"
)

func TestCoordinationEntity_EntityID(t *testing.T) {
	tests := []struct {
		name string
		slug string
		want string
	}{
		{
			name: "basic",
			slug: "my-feature",
			want: "local.semspec.workflow.coordination.execution.my-feature",
		},
		{
			name: "auth",
			slug: "auth-refresh",
			want: "local.semspec.workflow.coordination.execution.auth-refresh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &CoordinationEntity{Slug: tt.slug}
			got := e.EntityID()
			if got != tt.want {
				t.Errorf("EntityID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCoordinationEntity_EntityID_6PartFormat(t *testing.T) {
	e := &CoordinationEntity{Slug: "test-slug"}
	parts := strings.Split(e.EntityID(), ".")
	if len(parts) != 6 {
		t.Errorf("EntityID() has %d dot-separated parts, want 6: %q", len(parts), e.EntityID())
	}
}

func TestCoordinationEntity_Triples_RequiredPredicates(t *testing.T) {
	e := &CoordinationEntity{Slug: "test-slug"}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	required := []string{wf.Type, wf.Slug}
	for _, pred := range required {
		if !predicates[pred] {
			t.Errorf("Triples() missing required predicate %q", pred)
		}
	}
}

func TestCoordinationEntity_Triples_TypeIsCoordination(t *testing.T) {
	e := &CoordinationEntity{Slug: "s"}
	for _, tr := range e.Triples() {
		if tr.Predicate == wf.Type {
			if tr.Object != "coordination" {
				t.Errorf("wf.Type triple object = %q, want %q", tr.Object, "coordination")
			}
			return
		}
	}
	t.Error("Triples() missing wf.Type triple")
}

func TestCoordinationEntity_Triples_OptionalPredicatesOmittedWhenEmpty(t *testing.T) {
	e := &CoordinationEntity{Slug: "test-slug"}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	optional := []string{
		wf.Phase, wf.TraceID, wf.MaxIterations, wf.ErrorReason,
		wf.RelPlan, wf.RelProject, wf.RelLoop,
	}
	for _, pred := range optional {
		if predicates[pred] {
			t.Errorf("Triples() should not emit predicate %q when field is empty/zero", pred)
		}
	}
}

func TestCoordinationEntity_Triples_OptionalPredicatesIncludedWhenSet(t *testing.T) {
	e := &CoordinationEntity{
		Slug:         "test-slug",
		Phase:        "planning",
		TraceID:      "trace-abc",
		PlannerCount: 3,
		PlanEntityID: "local.semspec.plan.default.plan.test-slug",
	}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	expected := []string{wf.Phase, wf.TraceID, wf.MaxIterations, wf.RelPlan}
	for _, pred := range expected {
		if !predicates[pred] {
			t.Errorf("Triples() missing predicate %q when field is set", pred)
		}
	}
}

func TestCoordinationEntity_Triples_RelationshipEntityIDFormat(t *testing.T) {
	planID := "local.semspec.plan.default.plan.my-slug"
	projectID := "local.semspec.project.default.project.p"
	loopID := "local.semspec.loop.default.loop.l"

	e := &CoordinationEntity{
		Slug:            "my-slug",
		PlanEntityID:    planID,
		ProjectEntityID: projectID,
		LoopEntityID:    loopID,
	}

	relTriples := make(map[string]string)
	for _, tr := range e.Triples() {
		switch tr.Predicate {
		case wf.RelPlan, wf.RelProject, wf.RelLoop:
			relTriples[tr.Predicate] = tr.Object.(string)
		}
	}

	if got := relTriples[wf.RelPlan]; got != planID {
		t.Errorf("RelPlan = %q, want %q", got, planID)
	}
	if got := relTriples[wf.RelProject]; got != projectID {
		t.Errorf("RelProject = %q, want %q", got, projectID)
	}
	if got := relTriples[wf.RelLoop]; got != loopID {
		t.Errorf("RelLoop = %q, want %q", got, loopID)
	}
}

func TestCoordinationEntity_Triples_SubjectMatchesEntityID(t *testing.T) {
	e := &CoordinationEntity{Slug: "consistency-slug"}

	entityID := e.EntityID()
	for _, tr := range e.Triples() {
		if tr.Subject != entityID {
			t.Errorf("triple Subject = %q, want %q (predicate: %s)", tr.Subject, entityID, tr.Predicate)
		}
	}
}

func TestNewCoordinationEntity_FromState(t *testing.T) {
	exec := &coordinationExecution{
		EntityID:         "local.semspec.workflow.coordination.execution.my-plan",
		Slug:             "my-plan",
		TraceID:          "trace-xyz",
		ExpectedPlanners: 3,
		CompletedResults: make(map[string]*workflow.PlannerResult),
	}

	entity := NewCoordinationEntity(exec)

	if entity.Slug != exec.Slug {
		t.Errorf("Slug = %q, want %q", entity.Slug, exec.Slug)
	}
	if entity.TraceID != exec.TraceID {
		t.Errorf("TraceID = %q, want %q", entity.TraceID, exec.TraceID)
	}
	if entity.PlannerCount != exec.ExpectedPlanners {
		t.Errorf("PlannerCount = %d, want %d", entity.PlannerCount, exec.ExpectedPlanners)
	}

	expectedID := "local.semspec.workflow.coordination.execution.my-plan"
	if got := entity.EntityID(); got != expectedID {
		t.Errorf("EntityID() = %q, want %q", got, expectedID)
	}
}

func TestCoordinationEntity_WithMethods(t *testing.T) {
	e := &CoordinationEntity{Slug: "slug"}

	e.WithPhase("synthesizing").
		WithPlannerCount(4).
		WithPlanEntityID("local.semspec.plan.default.plan.slug").
		WithProjectEntityID("local.semspec.project.default.project.p").
		WithLoopEntityID("local.semspec.loop.default.loop.l").
		WithErrorReason("timeout")

	if e.Phase != "synthesizing" {
		t.Errorf("Phase = %q, want %q", e.Phase, "synthesizing")
	}
	if e.PlannerCount != 4 {
		t.Errorf("PlannerCount = %d, want 4", e.PlannerCount)
	}
	if e.PlanEntityID == "" {
		t.Error("PlanEntityID should be set")
	}
	if e.ProjectEntityID == "" {
		t.Error("ProjectEntityID should be set")
	}
	if e.LoopEntityID == "" {
		t.Error("LoopEntityID should be set")
	}
	if e.ErrorReason != "timeout" {
		t.Errorf("ErrorReason = %q, want %q", e.ErrorReason, "timeout")
	}
}
