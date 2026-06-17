package planmanager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestHandleQAStartMutation_AttachesRunAfterReviewerClaim(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)

	slug := "qa-start-race"
	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusReviewingQA
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	req := qaStartRequest{
		Slug: slug,
		QARun: &workflow.QARun{
			RunID:  "qa-topology",
			Passed: false,
			Failures: []workflow.QAFailure{
				{JobName: "integration", Category: workflow.QAFailureCategoryTopology, Message: "standalone build root"},
			},
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp := c.handleQAStartMutation(ctx, data)
	if !resp.Success {
		t.Fatalf("mutation failed: %s", resp.Error)
	}

	stored, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan missing from store after mutation")
	}
	if stored.Status != workflow.StatusReviewingQA {
		t.Fatalf("status = %s, want reviewing_qa", stored.Status)
	}
	if stored.QARun == nil {
		t.Fatal("QARun was not attached after reviewer claim")
	}
	if stored.QARun.RunID != "qa-topology" {
		t.Errorf("QARun.RunID = %q, want qa-topology", stored.QARun.RunID)
	}
	if len(stored.QARun.Failures) != 1 || stored.QARun.Failures[0].Category != workflow.QAFailureCategoryTopology {
		t.Fatalf("QARun.Failures = %+v, want topology failure evidence", stored.QARun.Failures)
	}
}
