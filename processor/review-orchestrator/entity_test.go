package revieworchestrator

import (
	"strings"
	"testing"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
)

func TestReviewExecutionEntity_EntityID(t *testing.T) {
	tests := []struct {
		name       string
		reviewType string
		slug       string
		want       string
	}{
		{
			name:       "plan-review",
			reviewType: reviewTypePlanReview,
			slug:       "my-feature",
			want:       "local.semspec.workflow.plan-review.execution.my-feature",
		},
		{
			name:       "phase-review",
			reviewType: reviewTypePhaseReview,
			slug:       "auth-refresh",
			want:       "local.semspec.workflow.phase-review.execution.auth-refresh",
		},
		{
			name:       "task-review",
			reviewType: reviewTypeTaskReview,
			slug:       "add-login",
			want:       "local.semspec.workflow.task-review.execution.add-login",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &ReviewExecutionEntity{ReviewType: tt.reviewType, Slug: tt.slug}
			got := e.EntityID()
			if got != tt.want {
				t.Errorf("EntityID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReviewExecutionEntity_EntityID_6PartFormat(t *testing.T) {
	e := &ReviewExecutionEntity{ReviewType: reviewTypePlanReview, Slug: "test-slug"}
	parts := strings.Split(e.EntityID(), ".")
	if len(parts) != 6 {
		t.Errorf("EntityID() has %d dot-separated parts, want 6: %q", len(parts), e.EntityID())
	}
}

func TestReviewExecutionEntity_Triples_RequiredPredicates(t *testing.T) {
	e := &ReviewExecutionEntity{
		ReviewType:    reviewTypePlanReview,
		Slug:          "test-slug",
		Iteration:     0,
		MaxIterations: 3,
	}

	triples := e.Triples()

	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	required := []string{wf.Type, wf.Slug, wf.Iteration, wf.MaxIterations}
	for _, pred := range required {
		if !predicates[pred] {
			t.Errorf("Triples() missing required predicate %q", pred)
		}
	}
}

func TestReviewExecutionEntity_Triples_OptionalPredicatesOmittedWhenEmpty(t *testing.T) {
	e := &ReviewExecutionEntity{
		ReviewType:    reviewTypePlanReview,
		Slug:          "test-slug",
		Iteration:     0,
		MaxIterations: 3,
		// Phase, Prompt, TraceID, PlanContent, Verdict, Summary, Findings left empty
	}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	optional := []string{
		wf.Phase, wf.Prompt, wf.TraceID, wf.PlanContent,
		wf.Verdict, wf.Summary, wf.Findings, wf.ErrorReason,
		wf.RelPlan, wf.RelProject, wf.RelLoop,
	}
	for _, pred := range optional {
		if predicates[pred] {
			t.Errorf("Triples() should not emit predicate %q when field is empty", pred)
		}
	}
}

func TestReviewExecutionEntity_Triples_OptionalPredicatesIncludedWhenSet(t *testing.T) {
	e := &ReviewExecutionEntity{
		ReviewType:    reviewTypePlanReview,
		Slug:          "test-slug",
		Iteration:     1,
		MaxIterations: 3,
		Phase:         "reviewing",
		Prompt:        "Build a login system",
		TraceID:       "trace-abc-123",
		PlanContent:   `{"plan": "content"}`,
		Verdict:       "approved",
		Summary:       "Looks good",
		Findings:      `[]`,
		ErrorReason:   "",
		PlanEntityID:  "local.semspec.plan.default.plan.test-slug",
	}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	expected := []string{
		wf.Phase, wf.Prompt, wf.TraceID, wf.PlanContent,
		wf.Verdict, wf.Summary, wf.Findings, wf.RelPlan,
	}
	for _, pred := range expected {
		if !predicates[pred] {
			t.Errorf("Triples() missing predicate %q when field is set", pred)
		}
	}
}

func TestReviewExecutionEntity_Triples_RelationshipEntityIDFormat(t *testing.T) {
	planID := "local.semspec.plan.default.plan.my-slug"
	projectID := "local.semspec.project.default.project.my-project"
	loopID := "local.semspec.loop.default.loop.abc-123"

	e := &ReviewExecutionEntity{
		ReviewType:      reviewTypePlanReview,
		Slug:            "test-slug",
		PlanEntityID:    planID,
		ProjectEntityID: projectID,
		LoopEntityID:    loopID,
	}

	triples := e.Triples()
	relTriples := make(map[string]string)
	for _, tr := range triples {
		switch tr.Predicate {
		case wf.RelPlan, wf.RelProject, wf.RelLoop:
			relTriples[tr.Predicate] = tr.Object.(string)
		}
	}

	if got := relTriples[wf.RelPlan]; got != planID {
		t.Errorf("RelPlan triple object = %q, want %q", got, planID)
	}
	if got := relTriples[wf.RelProject]; got != projectID {
		t.Errorf("RelProject triple object = %q, want %q", got, projectID)
	}
	if got := relTriples[wf.RelLoop]; got != loopID {
		t.Errorf("RelLoop triple object = %q, want %q", got, loopID)
	}
}

func TestReviewExecutionEntity_Triples_SubjectMatchesEntityID(t *testing.T) {
	e := &ReviewExecutionEntity{
		ReviewType:    reviewTypePlanReview,
		Slug:          "consistency-test",
		Iteration:     0,
		MaxIterations: 2,
	}

	entityID := e.EntityID()
	for _, tr := range e.Triples() {
		if tr.Subject != entityID {
			t.Errorf("triple Subject = %q, want %q (predicate: %s)", tr.Subject, entityID, tr.Predicate)
		}
	}
}

func TestNewReviewExecutionEntity_FromState(t *testing.T) {
	exec := &reviewExecution{
		EntityID:      "local.semspec.workflow.plan-review.execution.my-plan",
		ReviewType:    reviewTypePlanReview,
		Slug:          "my-plan",
		Iteration:     2,
		MaxIterations: 3,
		Prompt:        "Create an auth plan",
		TraceID:       "trace-xyz",
		Verdict:       "approved",
		Summary:       "Good plan",
	}

	entity := NewReviewExecutionEntity(exec)

	if entity.ReviewType != exec.ReviewType {
		t.Errorf("ReviewType = %q, want %q", entity.ReviewType, exec.ReviewType)
	}
	if entity.Slug != exec.Slug {
		t.Errorf("Slug = %q, want %q", entity.Slug, exec.Slug)
	}
	if entity.Iteration != exec.Iteration {
		t.Errorf("Iteration = %d, want %d", entity.Iteration, exec.Iteration)
	}
	if entity.MaxIterations != exec.MaxIterations {
		t.Errorf("MaxIterations = %d, want %d", entity.MaxIterations, exec.MaxIterations)
	}
	if entity.Prompt != exec.Prompt {
		t.Errorf("Prompt = %q, want %q", entity.Prompt, exec.Prompt)
	}
	if entity.TraceID != exec.TraceID {
		t.Errorf("TraceID = %q, want %q", entity.TraceID, exec.TraceID)
	}
	if entity.Verdict != exec.Verdict {
		t.Errorf("Verdict = %q, want %q", entity.Verdict, exec.Verdict)
	}
	if entity.Summary != exec.Summary {
		t.Errorf("Summary = %q, want %q", entity.Summary, exec.Summary)
	}

	// EntityID from entity should match what handleTrigger would produce.
	expectedID := "local.semspec.workflow.plan-review.execution.my-plan"
	if got := entity.EntityID(); got != expectedID {
		t.Errorf("EntityID() = %q, want %q", got, expectedID)
	}
}

func TestReviewExecutionEntity_WithMethods(t *testing.T) {
	e := &ReviewExecutionEntity{ReviewType: reviewTypePlanReview, Slug: "slug"}

	e.WithPhase("approved").
		WithPlanEntityID("local.semspec.plan.default.plan.slug").
		WithProjectEntityID("local.semspec.project.default.project.p").
		WithLoopEntityID("local.semspec.loop.default.loop.l").
		WithErrorReason("some error")

	if e.Phase != "approved" {
		t.Errorf("Phase = %q, want %q", e.Phase, "approved")
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
	if e.ErrorReason != "some error" {
		t.Errorf("ErrorReason = %q, want %q", e.ErrorReason, "some error")
	}
}
