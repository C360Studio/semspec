package planmanager

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

func TestHandleDraftedMutationCapturesFirstRealScopeAsContractBaseline(t *testing.T) {
	c := setupTestComponent(t)
	slug := "first-real-scope"
	plan := &workflow.Plan{
		ID:     workflow.PlanEntityID(slug),
		Slug:   slug,
		Title:  slug,
		Status: workflow.StatusCreated,
	}
	plan.EnsureContractPacket("planner has not drafted scope yet", time.Now())
	if err := c.plans.save(context.Background(), plan); err != nil {
		t.Fatalf("save: %v", err)
	}

	body := marshalJSON(t, DraftedMutationRequest{
		Slug:        slug,
		Goal:        "Draft the first concrete scope",
		Context:     "The initial contract was created before planner scope existed.",
		Constraints: []string{"Keep the baseline integration intact."},
		Scope: &workflow.Scope{
			Create:     []string{"src/required.go"},
			Include:    []string{"README.md"},
			DoNotTouch: []string{"secrets.yaml"},
		},
	})
	resp := c.handleDraftedMutation(context.Background(), body)
	if !resp.Success {
		t.Fatalf("handleDraftedMutation failed: %s", resp.Error)
	}

	got, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan not found after mutation")
	}
	if got.Contract == nil {
		t.Fatal("Contract is nil after drafted mutation")
	}
	if got.Contract.Scope.Create[0] != "src/required.go" {
		t.Fatalf("Contract.Scope.Create = %v, want first drafted scope", got.Contract.Scope.Create)
	}
	if got.Contract.Scope.Include[0] != "README.md" {
		t.Fatalf("Contract.Scope.Include = %v, want first drafted scope", got.Contract.Scope.Include)
	}
	if got.Contract.Scope.DoNotTouch[0] != "secrets.yaml" {
		t.Fatalf("Contract.Scope.DoNotTouch = %v, want first drafted scope", got.Contract.Scope.DoNotTouch)
	}
	if len(got.Contract.Constraints) != 1 || got.Contract.Constraints[0] != "Keep the baseline integration intact." {
		t.Fatalf("Contract.Constraints = %v, want planner constraints copied into empty root packet", got.Contract.Constraints)
	}
}

func TestHandleDraftedMutationRejectsUnamendedContractScopeShrinkage(t *testing.T) {
	c := setupTestComponent(t)
	slug := "scope-shrink"
	plan := &workflow.Plan{
		ID:     workflow.PlanEntityID(slug),
		Slug:   slug,
		Title:  slug,
		Status: workflow.StatusCreated,
		Scope: workflow.Scope{
			Create:     []string{"src/required.go"},
			Include:    []string{"README.md"},
			DoNotTouch: []string{"secrets.yaml"},
		},
	}
	plan.EnsureContractPacket("keep the brownfield scope intact", time.Now())
	if err := c.plans.save(context.Background(), plan); err != nil {
		t.Fatalf("save: %v", err)
	}

	body := marshalJSON(t, DraftedMutationRequest{
		Slug:    slug,
		Goal:    "Draft revised plan",
		Context: "Planner narrowed scope without an amendment.",
		Scope: &workflow.Scope{
			Include: []string{"README.md"},
		},
	})
	resp := c.handleDraftedMutation(context.Background(), body)
	if resp.Success {
		t.Fatal("handleDraftedMutation succeeded; want rejection")
	}
	if !strings.Contains(resp.Error, "scope shrinkage requires accepted amendment provenance") {
		t.Fatalf("error = %q, want scope shrinkage guardrail", resp.Error)
	}
	if !strings.Contains(resp.Error, "contract.scope.create:src/required.go") {
		t.Fatalf("error = %q, want missing create obligation", resp.Error)
	}
	if !strings.Contains(resp.Error, "contract.scope.do_not_touch:secrets.yaml") {
		t.Fatalf("error = %q, want missing do_not_touch obligation", resp.Error)
	}

	got, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan not found after rejected mutation")
	}
	if got.Status != workflow.StatusCreated {
		t.Fatalf("Status = %q, want unchanged created", got.Status)
	}
	if len(got.Scope.Create) != 1 || got.Scope.Create[0] != "src/required.go" {
		t.Fatalf("Scope.Create = %v, want original scope preserved", got.Scope.Create)
	}
}

func TestHandleDraftedMutationAllowsAmendedContractScopeDrop(t *testing.T) {
	c := setupTestComponent(t)
	slug := "scope-drop-amended"
	plan := &workflow.Plan{
		ID:     workflow.PlanEntityID(slug),
		Slug:   slug,
		Title:  slug,
		Status: workflow.StatusCreated,
		Scope: workflow.Scope{
			Create:  []string{"src/required.go"},
			Include: []string{"README.md"},
		},
	}
	plan.EnsureContractPacket("root scope before accepted amendment", time.Now())
	plan.Contract.Amendments = append(plan.Contract.Amendments, workflow.ContractAmendment{
		ID:             "contract-amendment.scope-drop",
		PlanDecisionID: "plan-decision.scope-drop",
		Impact: workflow.ContractImpact{
			Kind:        workflow.ContractImpactChange,
			Summary:     "Drop src/required.go from the contract scope.",
			AffectedIDs: []string{"contract.scope.create:src/required.go"},
		},
		CreatedAt: time.Now(),
	})
	if err := c.plans.save(context.Background(), plan); err != nil {
		t.Fatalf("save: %v", err)
	}

	body := marshalJSON(t, DraftedMutationRequest{
		Slug:    slug,
		Goal:    "Draft revised plan",
		Context: "Planner applied an accepted scope amendment.",
		Scope: &workflow.Scope{
			Include: []string{"README.md"},
		},
	})
	resp := c.handleDraftedMutation(context.Background(), body)
	if !resp.Success {
		t.Fatalf("handleDraftedMutation failed: %s", resp.Error)
	}

	got, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan not found after mutation")
	}
	if got.Status != workflow.StatusDrafted {
		t.Fatalf("Status = %q, want drafted", got.Status)
	}
	if len(got.Scope.Create) != 0 {
		t.Fatalf("Scope.Create = %v, want accepted drop to apply", got.Scope.Create)
	}
}

func TestApplyPlanDecisionAcceptEffectsAddsContractAmendment(t *testing.T) {
	c := setupTestComponent(t)
	decidedAt := time.Date(2026, 6, 16, 8, 30, 0, 0, time.UTC)
	plan := &workflow.Plan{
		Slug:    "scope-amendment",
		Goal:    "Keep contract amendments auditable",
		Context: "Original user brief",
		Scope:   workflow.Scope{Create: []string{"src/required.go"}},
	}
	plan.EnsureContractPacket(plan.Context, time.Now())
	proposal := &workflow.PlanDecision{
		ID:        "plan-decision.scope-amendment.change",
		Kind:      workflow.PlanDecisionKindRequirementChange,
		Status:    workflow.PlanDecisionStatusAccepted,
		DecidedAt: &decidedAt,
		ContractImpact: &workflow.ContractImpact{
			Kind:        workflow.ContractImpactChange,
			Summary:     "Drop obsolete deliverable.",
			AffectedIDs: []string{"contract.scope.create:src/required.go"},
		},
	}

	if err := c.applyPlanDecisionAcceptEffects(context.Background(), plan, proposal, plan.Slug); err != nil {
		t.Fatalf("applyPlanDecisionAcceptEffects: %v", err)
	}
	if len(plan.Contract.Amendments) != 1 {
		t.Fatalf("len(Amendments) = %d, want 1", len(plan.Contract.Amendments))
	}
	amendment := plan.Contract.Amendments[0]
	if amendment.PlanDecisionID != proposal.ID {
		t.Fatalf("PlanDecisionID = %q, want %q", amendment.PlanDecisionID, proposal.ID)
	}
	if amendment.Impact.Kind != workflow.ContractImpactChange {
		t.Fatalf("Impact.Kind = %q, want change", amendment.Impact.Kind)
	}
	if amendment.Impact.AffectedIDs[0] != "contract.scope.create:src/required.go" {
		t.Fatalf("AffectedIDs = %v", amendment.Impact.AffectedIDs)
	}
	if !amendment.CreatedAt.Equal(decidedAt) {
		t.Fatalf("CreatedAt = %s, want %s", amendment.CreatedAt, decidedAt)
	}

	proposal.ContractImpact.AffectedIDs[0] = "mutated-after-accept"
	if plan.Contract.Amendments[0].Impact.AffectedIDs[0] != "contract.scope.create:src/required.go" {
		t.Fatalf("amendment impact aliases proposal impact: %v", plan.Contract.Amendments[0].Impact.AffectedIDs)
	}
	if err := c.applyPlanDecisionAcceptEffects(context.Background(), plan, proposal, plan.Slug); err != nil {
		t.Fatalf("second applyPlanDecisionAcceptEffects: %v", err)
	}
	if len(plan.Contract.Amendments) != 1 {
		t.Fatalf("len(Amendments) after idempotent apply = %d, want 1", len(plan.Contract.Amendments))
	}
}

func TestValidatePlanDecisionAcceptContractImpactRejectsNarrowScopeSelfDowngrade(t *testing.T) {
	proposal := &workflow.PlanDecision{
		Title:     "Recovery: narrow_scope",
		Rationale: "Recommended action: narrow_scope\n\nDiagnosis:\nDrop work from accepted scope.",
		Kind:      workflow.PlanDecisionKindRequirementChange,
		ContractImpact: &workflow.ContractImpact{
			Kind:    workflow.ContractImpactPreserve,
			Summary: "incorrectly marked as preserving the contract",
		},
	}

	if err := validatePlanDecisionAcceptContractImpact(proposal); err == nil {
		t.Fatal("validatePlanDecisionAcceptContractImpact returned nil; want narrow_scope self-downgrade rejected")
	}

	proposal.ContractImpact.Kind = workflow.ContractImpactChange
	if err := validatePlanDecisionAcceptContractImpact(proposal); err != nil {
		t.Fatalf("validatePlanDecisionAcceptContractImpact with change impact: %v", err)
	}
}

func TestValidatePlanDecisionAcceptContractImpactRejectsArchitectureReviseSelfDowngrade(t *testing.T) {
	proposal := &workflow.PlanDecision{
		Kind: workflow.PlanDecisionKindArchitectureRevise,
		ContractImpact: &workflow.ContractImpact{
			Kind:    workflow.ContractImpactRefine,
			Summary: "incorrectly marked as refining the contract",
		},
	}

	if err := validatePlanDecisionAcceptContractImpact(proposal); err == nil {
		t.Fatal("validatePlanDecisionAcceptContractImpact returned nil; want architecture_revise self-downgrade rejected")
	}

	proposal.ContractImpact.Kind = workflow.ContractImpactChange
	if err := validatePlanDecisionAcceptContractImpact(proposal); err != nil {
		t.Fatalf("validatePlanDecisionAcceptContractImpact with change impact: %v", err)
	}
}
