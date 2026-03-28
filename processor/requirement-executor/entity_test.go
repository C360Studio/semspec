package requirementexecutor

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/tools/decompose"
	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
)

func TestRequirementExecutionEntity_EntityID(t *testing.T) {
	tests := []struct {
		name          string
		slug          string
		requirementID string
	}{
		{
			name:          "basic",
			slug:          "my-feature",
			requirementID: "requirement-001",
		},
		{
			name:          "auth",
			slug:          "auth-refresh",
			requirementID: "user-login",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := workflow.RequirementExecutionEntityID(tt.slug, tt.requirementID)
			e := &RequirementExecutionEntity{Slug: tt.slug, RequirementID: tt.requirementID}
			got := e.EntityID()
			if got != want {
				t.Errorf("EntityID() = %q, want %q", got, want)
			}
		})
	}
}

func TestRequirementExecutionEntity_EntityID_6PartFormat(t *testing.T) {
	e := &RequirementExecutionEntity{Slug: "test-slug", RequirementID: "req-1"}
	parts := strings.Split(e.EntityID(), ".")
	if len(parts) != 6 {
		t.Errorf("EntityID() has %d dot-separated parts, want 6: %q", len(parts), e.EntityID())
	}
}

func TestRequirementExecutionEntity_Triples_RequiredPredicates(t *testing.T) {
	e := &RequirementExecutionEntity{
		Slug:          "test-slug",
		RequirementID: "req-1",
	}

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

func TestRequirementExecutionEntity_Triples_TypeIsRequirementExecution(t *testing.T) {
	e := &RequirementExecutionEntity{Slug: "s", RequirementID: "req"}
	for _, tr := range e.Triples() {
		if tr.Predicate == wf.Type {
			if tr.Object != "requirement-execution" {
				t.Errorf("wf.Type triple object = %q, want %q", tr.Object, "requirement-execution")
			}
			return
		}
	}
	t.Error("Triples() missing wf.Type triple")
}

func TestRequirementExecutionEntity_Triples_OptionalPredicatesOmittedWhenEmpty(t *testing.T) {
	e := &RequirementExecutionEntity{
		Slug:          "test-slug",
		RequirementID: "req-1",
	}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	optional := []string{
		wf.Phase, wf.TraceID, wf.NodeCount, wf.FailureReason, wf.ErrorReason,
		wf.RelRequirement, wf.RelProject, wf.RelLoop,
	}
	for _, pred := range optional {
		if predicates[pred] {
			t.Errorf("Triples() should not emit predicate %q when field is empty/zero", pred)
		}
	}
}

func TestRequirementExecutionEntity_Triples_OptionalPredicatesIncludedWhenSet(t *testing.T) {
	e := &RequirementExecutionEntity{
		Slug:                "test-slug",
		RequirementID:       "req-1",
		Phase:               "executing",
		TraceID:             "trace-abc",
		NodeCount:           5,
		FailureReason:       "node failed",
		RequirementEntityID: "local.semspec.requirement.default.requirement.req-1",
	}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	expected := []string{
		wf.Phase, wf.TraceID, wf.NodeCount, wf.FailureReason, wf.RelRequirement,
	}
	for _, pred := range expected {
		if !predicates[pred] {
			t.Errorf("Triples() missing predicate %q when field is set", pred)
		}
	}
}

func TestRequirementExecutionEntity_Triples_RelationshipEntityIDFormat(t *testing.T) {
	requirementID := "local.semspec.requirement.default.requirement.req-1"
	projectID := "local.semspec.project.default.project.p"
	loopID := "local.semspec.loop.default.loop.l"

	e := &RequirementExecutionEntity{
		Slug:                "my-slug",
		RequirementID:       "req-1",
		RequirementEntityID: requirementID,
		ProjectEntityID:     projectID,
		LoopEntityID:        loopID,
	}

	relTriples := make(map[string]string)
	for _, tr := range e.Triples() {
		switch tr.Predicate {
		case wf.RelRequirement, wf.RelProject, wf.RelLoop:
			relTriples[tr.Predicate] = tr.Object.(string)
		}
	}

	if got := relTriples[wf.RelRequirement]; got != requirementID {
		t.Errorf("RelRequirement = %q, want %q", got, requirementID)
	}
	if got := relTriples[wf.RelProject]; got != projectID {
		t.Errorf("RelProject = %q, want %q", got, projectID)
	}
	if got := relTriples[wf.RelLoop]; got != loopID {
		t.Errorf("RelLoop = %q, want %q", got, loopID)
	}
}

func TestRequirementExecutionEntity_Triples_SubjectMatchesEntityID(t *testing.T) {
	e := &RequirementExecutionEntity{Slug: "slug", RequirementID: "req-1"}

	entityID := e.EntityID()
	for _, tr := range e.Triples() {
		if tr.Subject != entityID {
			t.Errorf("triple Subject = %q, want %q (predicate: %s)", tr.Subject, entityID, tr.Predicate)
		}
	}
}

func TestNewRequirementExecutionEntity_FromState(t *testing.T) {
	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "node-1"},
			{ID: "node-2"},
			{ID: "node-3"},
		},
	}

	exec := &requirementExecution{
		EntityID:      "semspec.local.exec.req.run.my-slug-req-1",
		Slug:          "my-slug",
		RequirementID: "req-1",
		TraceID:       "trace-xyz",
		DAG:           dag,
	}

	entity := NewRequirementExecutionEntity(exec)

	if entity.Slug != exec.Slug {
		t.Errorf("Slug = %q, want %q", entity.Slug, exec.Slug)
	}
	if entity.RequirementID != exec.RequirementID {
		t.Errorf("RequirementID = %q, want %q", entity.RequirementID, exec.RequirementID)
	}
	if entity.TraceID != exec.TraceID {
		t.Errorf("TraceID = %q, want %q", entity.TraceID, exec.TraceID)
	}
	if entity.NodeCount != len(dag.Nodes) {
		t.Errorf("NodeCount = %d, want %d", entity.NodeCount, len(dag.Nodes))
	}

	expectedID := workflow.RequirementExecutionEntityID(exec.Slug, exec.RequirementID)
	if got := entity.EntityID(); got != expectedID {
		t.Errorf("EntityID() = %q, want %q", got, expectedID)
	}
}

func TestNewRequirementExecutionEntity_NilDAG(t *testing.T) {
	exec := &requirementExecution{
		Slug:          "my-slug",
		RequirementID: "req-1",
		DAG:           nil, // not yet decomposed
	}

	entity := NewRequirementExecutionEntity(exec)
	if entity.NodeCount != 0 {
		t.Errorf("NodeCount should be 0 when DAG is nil, got %d", entity.NodeCount)
	}
}
