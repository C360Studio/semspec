package planmanager

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// setupTestComponent creates a Component with an in-memory plan store.
// Integration tests must pre-populate plans (and their inline requirements/
// scenarios) into the store via setupTestPlan before exercising HTTP handlers.
func setupTestComponent(t *testing.T) *Component {
	t.Helper()

	ps, err := newPlanStore(context.Background(), nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("newPlanStore: %v", err)
	}

	c := &Component{
		logger: slog.Default(),
		plans:  ps,
	}

	return c
}

// setupTestPlan inserts a minimal Plan into c.plans with the given slug.
// Use the returned *workflow.Plan to append Requirements/Scenarios if needed,
// then call c.plans.save(ctx, plan) again to persist those changes.
// All workflow.CreatePlan / workflow.Save* calls with nil TripleWriter are
// no-ops (KV-only, no filesystem fallback); test data must be set directly.
func setupTestPlan(t *testing.T, c *Component, slug string) *workflow.Plan {
	t.Helper()

	plan := &workflow.Plan{
		ID:    workflow.PlanEntityID(slug),
		Slug:  slug,
		Title: slug,
	}
	_ = c.plans.save(context.Background(), plan)
	return plan
}

// setupTestPlanWith inserts a Plan into c.plans populated with the given
// requirements and scenarios. Use this when handler tests depend on specific
// pre-existing requirements or scenarios.
func setupTestPlanWith(t *testing.T, c *Component, slug string, reqs []workflow.Requirement, scenarios []workflow.Scenario) *workflow.Plan {
	t.Helper()

	plan := &workflow.Plan{
		ID:           workflow.PlanEntityID(slug),
		Slug:         slug,
		Title:        slug,
		Requirements: reqs,
		Scenarios:    scenarios,
	}
	_ = c.plans.save(context.Background(), plan)
	return plan
}

func TestExtractSlugAndEndpoint(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantSlug     string
		wantEndpoint string
	}{
		{
			name:         "standard path",
			path:         "/plan-manager/plans/authentication-options/reviews",
			wantSlug:     "authentication-options",
			wantEndpoint: "reviews",
		},
		{
			name:         "with trailing slash",
			path:         "/plan-manager/plans/my-feature/reviews/",
			wantSlug:     "my-feature",
			wantEndpoint: "reviews",
		},
		{
			name:         "no endpoint",
			path:         "/plan-manager/plans/test-slug",
			wantSlug:     "test-slug",
			wantEndpoint: "",
		},
		{
			name:         "empty path",
			path:         "",
			wantSlug:     "",
			wantEndpoint: "",
		},
		{
			name:         "no plans segment",
			path:         "/plan-manager/something/else",
			wantSlug:     "",
			wantEndpoint: "",
		},
		{
			name:         "slug with dashes",
			path:         "/plan-manager/plans/add-user-auth-flow/reviews",
			wantSlug:     "add-user-auth-flow",
			wantEndpoint: "reviews",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSlug, gotEndpoint := extractSlugAndEndpoint(tt.path)
			if gotSlug != tt.wantSlug {
				t.Errorf("extractSlugAndEndpoint() slug = %q, want %q", gotSlug, tt.wantSlug)
			}
			if gotEndpoint != tt.wantEndpoint {
				t.Errorf("extractSlugAndEndpoint() endpoint = %q, want %q", gotEndpoint, tt.wantEndpoint)
			}
		})
	}
}

func TestFindReviewResult(t *testing.T) {
	c := &Component{}

	tests := []struct {
		name     string
		exec     *WorkflowExecution
		wantName string
		wantNil  bool
	}{
		{
			name: "finds review step",
			exec: &WorkflowExecution{
				StepResults: map[string]*StepResult{
					"review": {
						StepName: "review",
						Status:   "success",
						Output:   json.RawMessage(`{"verdict":"approved"}`),
					},
				},
			},
			wantName: "review",
			wantNil:  false,
		},
		{
			name: "finds review-synthesis step",
			exec: &WorkflowExecution{
				StepResults: map[string]*StepResult{
					"review-synthesis": {
						StepName: "review-synthesis",
						Status:   "success",
						Output:   json.RawMessage(`{"verdict":"approved"}`),
					},
				},
			},
			wantName: "review-synthesis",
			wantNil:  false,
		},
		{
			name: "ignores failed review",
			exec: &WorkflowExecution{
				StepResults: map[string]*StepResult{
					"review": {
						StepName: "review",
						Status:   "failed",
						Output:   json.RawMessage(`{"error":"timeout"}`),
					},
				},
			},
			wantNil: true,
		},
		{
			name: "no step results",
			exec: &WorkflowExecution{
				StepResults: nil,
			},
			wantNil: true,
		},
		{
			name: "empty step results",
			exec: &WorkflowExecution{
				StepResults: map[string]*StepResult{},
			},
			wantNil: true,
		},
		{
			name: "non-review step",
			exec: &WorkflowExecution{
				StepResults: map[string]*StepResult{
					"implement": {
						StepName: "implement",
						Status:   "success",
						Output:   json.RawMessage(`{"result":"done"}`),
					},
				},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.findReviewResult(tt.exec)
			if tt.wantNil {
				if result != nil {
					t.Errorf("findReviewResult() expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Error("findReviewResult() got nil, expected non-nil")
				return
			}
			if result.StepName != tt.wantName {
				t.Errorf("findReviewResult() step name = %q, want %q", result.StepName, tt.wantName)
			}
		})
	}
}

func TestTriggerPayloadParsing(t *testing.T) {
	tests := []struct {
		name     string
		trigger  string
		wantSlug string
		wantOk   bool
	}{
		{
			name:     "valid trigger with data",
			trigger:  `{"workflow_id":"review","data":{"slug":"my-feature","title":"My Feature"}}`,
			wantSlug: "my-feature",
			wantOk:   true,
		},
		{
			name:     "trigger without data",
			trigger:  `{"workflow_id":"review"}`,
			wantSlug: "",
			wantOk:   true,
		},
		{
			name:     "invalid JSON",
			trigger:  `{invalid}`,
			wantSlug: "",
			wantOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var trigger TriggerPayload
			err := json.Unmarshal([]byte(tt.trigger), &trigger)
			if tt.wantOk {
				if err != nil {
					t.Errorf("Unmarshal() error = %v, want nil", err)
					return
				}
				gotSlug := trigger.GetSlug()
				if gotSlug != tt.wantSlug {
					t.Errorf("slug = %q, want %q", gotSlug, tt.wantSlug)
				}
			} else {
				if err == nil {
					t.Error("Unmarshal() expected error, got nil")
				}
			}
		})
	}
}
