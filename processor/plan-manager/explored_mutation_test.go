package planmanager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// marshalExplored is a small helper mirroring the package's marshalRevision.
func marshalExplored(t *testing.T, req ExploredMutationRequest) []byte {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal explored request: %v", err)
	}
	return data
}

func TestHandleExploredMutation(t *testing.T) {
	ctx := context.Background()

	validExploration := workflow.Exploration{
		Capabilities: []workflow.Capability{
			{Name: "user-auth", Lifecycle: workflow.CapabilityNew, Description: "Authenticate users."},
			{Name: "session-store", Lifecycle: workflow.CapabilityNew, Description: "Sessions.", DependsOn: []string{"user-auth"}},
		},
		OpenQuestions: []string{"Are we supporting OAuth?"},
	}

	tests := []struct {
		name            string
		setup           func(t *testing.T) (*Component, string)
		req             ExploredMutationRequest
		wantSuccess     bool
		wantErrorSubstr string
		checkPlan       func(t *testing.T, plan *workflow.Plan)
	}{
		{
			name: "happy path: created plan transitions to explored",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "test-slug")
				plan.Status = workflow.StatusExploring
				_ = c.plans.save(ctx, plan)
				return c, "test-slug"
			},
			req: ExploredMutationRequest{
				Slug:        "test-slug",
				Exploration: validExploration,
			},
			wantSuccess: true,
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if plan.Status != workflow.StatusExplored {
					t.Errorf("expected status explored, got %s", plan.Status)
				}
				if plan.Exploration == nil {
					t.Fatal("expected Exploration set on plan")
				}
				if got := len(plan.Exploration.Capabilities); got != 2 {
					t.Errorf("expected 2 capabilities, got %d", got)
				}
				if got := len(plan.Exploration.OpenQuestions); got != 1 {
					t.Errorf("expected 1 open question, got %d", got)
				}
			},
		},
		{
			name: "transition from created path: created → explored direct skip not allowed; plan must be exploring",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				setupTestPlan(t, c, "fresh-plan") // stays at StatusCreated
				return c, "fresh-plan"
			},
			req: ExploredMutationRequest{
				Slug:        "fresh-plan",
				Exploration: validExploration,
			},
			wantSuccess:     false,
			wantErrorSubstr: "invalid transition",
		},
		{
			name: "missing slug rejected",
			setup: func(t *testing.T) (*Component, string) {
				return setupTestComponent(t), ""
			},
			req: ExploredMutationRequest{
				Slug:        "",
				Exploration: validExploration,
			},
			wantSuccess:     false,
			wantErrorSubstr: "slug required",
		},
		{
			name: "empty capabilities rejected",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "empty-caps")
				plan.Status = workflow.StatusExploring
				_ = c.plans.save(ctx, plan)
				return c, "empty-caps"
			},
			req: ExploredMutationRequest{
				Slug:        "empty-caps",
				Exploration: workflow.Exploration{},
			},
			wantSuccess:     false,
			wantErrorSubstr: "at least one capability",
		},
		{
			name: "invalid capability set rejected",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "bad-cap")
				plan.Status = workflow.StatusExploring
				_ = c.plans.save(ctx, plan)
				return c, "bad-cap"
			},
			req: ExploredMutationRequest{
				Slug: "bad-cap",
				Exploration: workflow.Exploration{
					Capabilities: []workflow.Capability{
						{Name: "broken", Lifecycle: "ancient", Description: "Bad."},
					},
				},
			},
			wantSuccess:     false,
			wantErrorSubstr: "invalid lifecycle",
		},
		{
			name: "plan not found rejected",
			setup: func(t *testing.T) (*Component, string) {
				return setupTestComponent(t), "nonexistent"
			},
			req: ExploredMutationRequest{
				Slug:        "nonexistent",
				Exploration: validExploration,
			},
			wantSuccess:     false,
			wantErrorSubstr: "plan not found",
		},
		{
			name: "cycle in depends_on rejected",
			setup: func(t *testing.T) (*Component, string) {
				c := setupTestComponent(t)
				plan := setupTestPlan(t, c, "cycle-test")
				plan.Status = workflow.StatusExploring
				_ = c.plans.save(ctx, plan)
				return c, "cycle-test"
			},
			req: ExploredMutationRequest{
				Slug: "cycle-test",
				Exploration: workflow.Exploration{
					Capabilities: []workflow.Capability{
						{Name: "a", Lifecycle: workflow.CapabilityNew, Description: "A.", DependsOn: []string{"b"}},
						{Name: "b", Lifecycle: workflow.CapabilityNew, Description: "B.", DependsOn: []string{"a"}},
					},
				},
			},
			wantSuccess:     false,
			wantErrorSubstr: "cycle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, slug := tt.setup(t)
			data := marshalExplored(t, tt.req)
			resp := c.handleExploredMutation(ctx, data)

			if resp.Success != tt.wantSuccess {
				t.Errorf("Success=%v want %v (err=%q)", resp.Success, tt.wantSuccess, resp.Error)
			}
			if tt.wantErrorSubstr != "" && !strings.Contains(resp.Error, tt.wantErrorSubstr) {
				t.Errorf("error %q does not contain %q", resp.Error, tt.wantErrorSubstr)
			}

			if tt.wantSuccess && tt.checkPlan != nil {
				plan, ok := c.plans.get(slug)
				if !ok {
					t.Fatalf("plan %q not found after mutation", slug)
				}
				tt.checkPlan(t, plan)
			}
		})
	}
}
