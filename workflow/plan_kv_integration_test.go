//go:build integration

package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
)

// newTestKV creates a real NATS KV store for integration tests.
func newTestKV(t *testing.T) *natsclient.KVStore {
	t.Helper()
	tc := natsclient.NewTestClient(t,
		natsclient.WithKVBuckets("ENTITY_STATES"),
	)
	bucket, err := tc.Client.GetKeyValueBucket(context.Background(), "ENTITY_STATES")
	if err != nil {
		t.Fatalf("get ENTITY_STATES bucket: %v", err)
	}
	return tc.Client.NewKVStore(bucket)
}

func TestKV_CreateAndLoadPlan(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	plan, err := CreatePlan(ctx, kv, "test-plan", "Test Plan")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	if plan.Slug != "test-plan" {
		t.Errorf("Slug = %q, want %q", plan.Slug, "test-plan")
	}
	if plan.Title != "Test Plan" {
		t.Errorf("Title = %q, want %q", plan.Title, "Test Plan")
	}

	loaded, err := LoadPlan(ctx, kv, "test-plan")
	if err != nil {
		t.Fatalf("LoadPlan: %v", err)
	}

	if loaded.Slug != plan.Slug {
		t.Errorf("loaded Slug = %q, want %q", loaded.Slug, plan.Slug)
	}
	if loaded.Title != plan.Title {
		t.Errorf("loaded Title = %q, want %q", loaded.Title, plan.Title)
	}
}

func TestKV_PlanExists(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	if PlanExists(ctx, kv, "nonexistent") {
		t.Error("PlanExists should return false for nonexistent plan")
	}

	if _, err := CreatePlan(ctx, kv, "exists", "Exists"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	if !PlanExists(ctx, kv, "exists") {
		t.Error("PlanExists should return true after creation")
	}
}

func TestKV_SetPlanStatus(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	plan, err := CreatePlan(ctx, kv, "status-test", "Status Test")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	plan.Status = StatusCreated
	if err := SetPlanStatus(ctx, kv, plan, StatusDrafted); err != nil {
		t.Fatalf("SetPlanStatus to drafted: %v", err)
	}

	loaded, err := LoadPlan(ctx, kv, "status-test")
	if err != nil {
		t.Fatalf("LoadPlan after status change: %v", err)
	}
	if loaded.EffectiveStatus() != StatusDrafted {
		t.Errorf("Status = %q, want %q", loaded.EffectiveStatus(), StatusDrafted)
	}
}

func TestKV_SaveAndLoadRequirements(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, kv, "req-test", "Req Test"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	reqs := []Requirement{
		{
			ID:          "req-001",
			PlanID:      PlanEntityID("req-test"),
			Title:       "First Requirement",
			Description: "Do the first thing",
			Status:      RequirementStatusActive,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "req-002",
			PlanID:      PlanEntityID("req-test"),
			Title:       "Second Requirement",
			Description: "Do the second thing",
			Status:      RequirementStatusActive,
			DependsOn:   []string{"req-001"},
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	if err := SaveRequirements(ctx, kv, reqs, "req-test"); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}

	loaded, err := LoadRequirements(ctx, kv, "req-test")
	if err != nil {
		t.Fatalf("LoadRequirements: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("LoadRequirements returned %d, want 2", len(loaded))
	}
}

func TestKV_SaveAndLoadScenarios(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, kv, "scen-test", "Scenario Test"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	reqs := []Requirement{
		{ID: "req-001", PlanID: PlanEntityID("scen-test"), Title: "Req", Status: RequirementStatusActive, CreatedAt: now, UpdatedAt: now},
	}
	if err := SaveRequirements(ctx, kv, reqs, "scen-test"); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}

	scenarios := []Scenario{
		{
			ID:            "scen-001",
			RequirementID: "req-001",
			Given:         "A system",
			When:          "Something happens",
			Then:          []string{"Result A", "Result B"},
			Status:        ScenarioStatusPending,
			CreatedAt:     now,
		},
	}

	if err := SaveScenarios(ctx, kv, scenarios, "scen-test"); err != nil {
		t.Fatalf("SaveScenarios: %v", err)
	}

	loaded, err := LoadScenarios(ctx, kv, "scen-test")
	if err != nil {
		t.Fatalf("LoadScenarios: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("LoadScenarios returned %d, want 1", len(loaded))
	}

	if len(loaded[0].Then) != 2 {
		t.Errorf("Then has %d items, want 2", len(loaded[0].Then))
	}
}

func TestKV_SaveAndLoadChangeProposals(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, kv, "cp-test", "CP Test"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	proposals := []ChangeProposal{
		{
			ID:             "cp-001",
			PlanID:         PlanEntityID("cp-test"),
			Title:          "Change Auth",
			Rationale:      "Need SAML",
			Status:         ChangeProposalStatusProposed,
			ProposedBy:     "reviewer",
			AffectedReqIDs: []string{"req-001", "req-002"},
			CreatedAt:      now,
		},
	}

	if err := SaveChangeProposals(ctx, kv, proposals, "cp-test"); err != nil {
		t.Fatalf("SaveChangeProposals: %v", err)
	}

	loaded, err := LoadChangeProposals(ctx, kv, "cp-test")
	if err != nil {
		t.Fatalf("LoadChangeProposals: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("LoadChangeProposals returned %d, want 1", len(loaded))
	}

	if len(loaded[0].AffectedReqIDs) != 2 {
		t.Errorf("AffectedReqIDs has %d items, want 2", len(loaded[0].AffectedReqIDs))
	}
}

func TestKV_DeletePlan(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, kv, "delete-me", "Delete Me"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	if !PlanExists(ctx, kv, "delete-me") {
		t.Fatal("plan should exist before delete")
	}

	if err := DeletePlan(ctx, kv, "delete-me"); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}

	if PlanExists(ctx, kv, "delete-me") {
		t.Error("plan should not exist after delete")
	}
}

func TestKV_ListPlans(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, kv, "plan-a", "Plan A"); err != nil {
		t.Fatalf("CreatePlan A: %v", err)
	}
	if _, err := CreatePlan(ctx, kv, "plan-b", "Plan B"); err != nil {
		t.Fatalf("CreatePlan B: %v", err)
	}

	result, err := ListPlans(ctx, kv)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}

	if len(result.Plans) != 2 {
		t.Errorf("ListPlans returned %d plans, want 2", len(result.Plans))
	}
}

func TestKV_ApprovePlan(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	plan, err := CreatePlan(ctx, kv, "approve-test", "Approve Test")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	if err := ApprovePlan(ctx, kv, plan); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}

	loaded, err := LoadPlan(ctx, kv, "approve-test")
	if err != nil {
		t.Fatalf("LoadPlan: %v", err)
	}
	if loaded.EffectiveStatus() != StatusApproved {
		t.Errorf("Status = %q, want %q", loaded.EffectiveStatus(), StatusApproved)
	}
	if !loaded.Approved {
		t.Error("Approved should be true")
	}

	// Double approve should fail
	if err := ApprovePlan(ctx, kv, loaded); err == nil {
		t.Error("ApprovePlan on already-approved plan should fail")
	}
}

func TestKV_UpdatePlan(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, kv, "update-test", "Original Title"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	newTitle := "Updated Title"
	newGoal := "New Goal"
	updated, err := UpdatePlan(ctx, kv, "update-test", UpdatePlanRequest{
		Title: &newTitle,
		Goal:  &newGoal,
	})
	if err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	if updated.Title != newTitle {
		t.Errorf("Title = %q, want %q", updated.Title, newTitle)
	}
	if updated.Goal != newGoal {
		t.Errorf("Goal = %q, want %q", updated.Goal, newGoal)
	}

	// Verify persisted
	loaded, err := LoadPlan(ctx, kv, "update-test")
	if err != nil {
		t.Fatalf("LoadPlan: %v", err)
	}
	if loaded.Title != newTitle {
		t.Errorf("persisted Title = %q, want %q", loaded.Title, newTitle)
	}
}

func TestKV_UpdatePlan_StateGuard(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	plan, err := CreatePlan(ctx, kv, "guard-test", "Guard Test")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Walk the valid transition path to implementing:
	// created → drafted → reviewed → approved → requirements_generated → scenarios_generated → ready_for_execution → implementing
	transitions := []Status{StatusDrafted, StatusReviewed, StatusApproved, StatusRequirementsGenerated, StatusScenariosGenerated, StatusReadyForExecution, StatusImplementing}
	for _, target := range transitions {
		if err := SetPlanStatus(ctx, kv, plan, target); err != nil {
			t.Fatalf("SetPlanStatus %s → %s: %v", plan.Status, target, err)
		}
		plan.Status = target
	}

	// UpdatePlan should fail on implementing plan
	newTitle := "Nope"
	if _, err := UpdatePlan(ctx, kv, "guard-test", UpdatePlanRequest{Title: &newTitle}); err == nil {
		t.Error("UpdatePlan on implementing plan should fail")
	}
}

func TestKV_ArchiveAndUnarchive(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, kv, "archive-test", "Archive Test"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	if err := ArchivePlan(ctx, kv, "archive-test"); err != nil {
		t.Fatalf("ArchivePlan: %v", err)
	}

	loaded, err := LoadPlan(ctx, kv, "archive-test")
	if err != nil {
		t.Fatalf("LoadPlan after archive: %v", err)
	}
	if loaded.EffectiveStatus() != StatusArchived {
		t.Errorf("Status = %q, want %q", loaded.EffectiveStatus(), StatusArchived)
	}

	// Unarchive
	if err := UnarchivePlan(ctx, kv, "archive-test"); err != nil {
		t.Fatalf("UnarchivePlan: %v", err)
	}

	loaded, err = LoadPlan(ctx, kv, "archive-test")
	if err != nil {
		t.Fatalf("LoadPlan after unarchive: %v", err)
	}
	if loaded.EffectiveStatus() != StatusComplete {
		t.Errorf("Status = %q, want %q", loaded.EffectiveStatus(), StatusComplete)
	}
}

func TestKV_ResetPlan(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	plan, err := CreatePlan(ctx, kv, "reset-test", "Reset Test")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Move to rejected (created → drafted → rejected)
	plan.Status = StatusCreated
	if err := SetPlanStatus(ctx, kv, plan, StatusDrafted); err != nil {
		t.Fatalf("SetPlanStatus drafted: %v", err)
	}
	plan.Status = StatusDrafted
	if err := SetPlanStatus(ctx, kv, plan, StatusRejected); err != nil {
		t.Fatalf("SetPlanStatus rejected: %v", err)
	}

	// Reset back to approved
	if err := ResetPlan(ctx, kv, "reset-test"); err != nil {
		t.Fatalf("ResetPlan: %v", err)
	}

	loaded, err := LoadPlan(ctx, kv, "reset-test")
	if err != nil {
		t.Fatalf("LoadPlan after reset: %v", err)
	}
	if loaded.EffectiveStatus() != StatusApproved {
		t.Errorf("Status = %q, want %q", loaded.EffectiveStatus(), StatusApproved)
	}
	if loaded.ReviewVerdict != "" {
		t.Errorf("ReviewVerdict = %q, want empty", loaded.ReviewVerdict)
	}
}

func TestKV_CreatePlan_DuplicateRejected(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, kv, "dupe-test", "First"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	_, err := CreatePlan(ctx, kv, "dupe-test", "Second")
	if err == nil {
		t.Fatal("CreatePlan with duplicate slug should fail")
	}
}

func TestKV_InvalidSlug(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	slugs := []string{"", "../escape", "has spaces", "UPPERCASE", "a/b"}
	for _, slug := range slugs {
		if _, err := CreatePlan(ctx, kv, slug, "Bad"); err == nil {
			t.Errorf("CreatePlan(%q) should fail", slug)
		}
		if _, err := LoadPlan(ctx, kv, slug); err == nil {
			t.Errorf("LoadPlan(%q) should fail", slug)
		}
	}
}

func TestKV_RequirementDAGValidation(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, kv, "dag-test", "DAG Test"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	now := time.Now().Truncate(time.Second)

	// Self-reference should fail
	reqs := []Requirement{
		{ID: "req-self", PlanID: PlanEntityID("dag-test"), Title: "Self", Status: RequirementStatusActive, DependsOn: []string{"req-self"}, CreatedAt: now, UpdatedAt: now},
	}
	if err := SaveRequirements(ctx, kv, reqs, "dag-test"); err == nil {
		t.Error("SaveRequirements with self-reference should fail")
	}

	// Cycle should fail
	cycleReqs := []Requirement{
		{ID: "req-a", PlanID: PlanEntityID("dag-test"), Title: "A", Status: RequirementStatusActive, DependsOn: []string{"req-b"}, CreatedAt: now, UpdatedAt: now},
		{ID: "req-b", PlanID: PlanEntityID("dag-test"), Title: "B", Status: RequirementStatusActive, DependsOn: []string{"req-a"}, CreatedAt: now, UpdatedAt: now},
	}
	if err := SaveRequirements(ctx, kv, cycleReqs, "dag-test"); err == nil {
		t.Error("SaveRequirements with cycle should fail")
	}
}

func TestKV_CrossPlanIsolation_Scenarios(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	// Create two plans with requirements and scenarios
	if _, err := CreatePlan(ctx, kv, "plan-x", "Plan X"); err != nil {
		t.Fatalf("CreatePlan X: %v", err)
	}
	if _, err := CreatePlan(ctx, kv, "plan-y", "Plan Y"); err != nil {
		t.Fatalf("CreatePlan Y: %v", err)
	}

	reqsX := []Requirement{{ID: "req-x1", PlanID: PlanEntityID("plan-x"), Title: "X Req", Status: RequirementStatusActive, CreatedAt: now, UpdatedAt: now}}
	reqsY := []Requirement{{ID: "req-y1", PlanID: PlanEntityID("plan-y"), Title: "Y Req", Status: RequirementStatusActive, CreatedAt: now, UpdatedAt: now}}

	if err := SaveRequirements(ctx, kv, reqsX, "plan-x"); err != nil {
		t.Fatalf("SaveRequirements X: %v", err)
	}
	if err := SaveRequirements(ctx, kv, reqsY, "plan-y"); err != nil {
		t.Fatalf("SaveRequirements Y: %v", err)
	}

	scenX := []Scenario{{ID: "sc-x1", RequirementID: "req-x1", Given: "X", When: "X happens", Then: []string{"X result"}, Status: ScenarioStatusPending, CreatedAt: now}}
	scenY := []Scenario{{ID: "sc-y1", RequirementID: "req-y1", Given: "Y", When: "Y happens", Then: []string{"Y result"}, Status: ScenarioStatusPending, CreatedAt: now}}

	if err := SaveScenarios(ctx, kv, scenX, "plan-x"); err != nil {
		t.Fatalf("SaveScenarios X: %v", err)
	}
	if err := SaveScenarios(ctx, kv, scenY, "plan-y"); err != nil {
		t.Fatalf("SaveScenarios Y: %v", err)
	}

	// LoadScenarios for plan-x should only return scenX
	loadedX, err := LoadScenarios(ctx, kv, "plan-x")
	if err != nil {
		t.Fatalf("LoadScenarios X: %v", err)
	}
	if len(loadedX) != 1 {
		t.Fatalf("plan-x scenarios = %d, want 1", len(loadedX))
	}
	if loadedX[0].ID != "sc-x1" {
		t.Errorf("plan-x scenario ID = %q, want %q", loadedX[0].ID, "sc-x1")
	}

	// LoadScenarios for plan-y should only return scenY
	loadedY, err := LoadScenarios(ctx, kv, "plan-y")
	if err != nil {
		t.Fatalf("LoadScenarios Y: %v", err)
	}
	if len(loadedY) != 1 {
		t.Fatalf("plan-y scenarios = %d, want 1", len(loadedY))
	}
	if loadedY[0].ID != "sc-y1" {
		t.Errorf("plan-y scenario ID = %q, want %q", loadedY[0].ID, "sc-y1")
	}
}

func TestKV_CrossPlanIsolation_ChangeProposals(t *testing.T) {
	kv := newTestKV(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	if _, err := CreatePlan(ctx, kv, "iso-a", "Iso A"); err != nil {
		t.Fatalf("CreatePlan A: %v", err)
	}
	if _, err := CreatePlan(ctx, kv, "iso-b", "Iso B"); err != nil {
		t.Fatalf("CreatePlan B: %v", err)
	}

	propA := []ChangeProposal{{ID: "cp-a1", PlanID: PlanEntityID("iso-a"), Title: "A prop", Status: ChangeProposalStatusProposed, CreatedAt: now}}
	propB := []ChangeProposal{{ID: "cp-b1", PlanID: PlanEntityID("iso-b"), Title: "B prop", Status: ChangeProposalStatusProposed, CreatedAt: now}}

	if err := SaveChangeProposals(ctx, kv, propA, "iso-a"); err != nil {
		t.Fatalf("SaveChangeProposals A: %v", err)
	}
	if err := SaveChangeProposals(ctx, kv, propB, "iso-b"); err != nil {
		t.Fatalf("SaveChangeProposals B: %v", err)
	}

	loadedA, err := LoadChangeProposals(ctx, kv, "iso-a")
	if err != nil {
		t.Fatalf("LoadChangeProposals A: %v", err)
	}
	if len(loadedA) != 1 || loadedA[0].ID != "cp-a1" {
		t.Errorf("plan iso-a proposals: got %d, want 1 with ID cp-a1", len(loadedA))
	}

	loadedB, err := LoadChangeProposals(ctx, kv, "iso-b")
	if err != nil {
		t.Fatalf("LoadChangeProposals B: %v", err)
	}
	if len(loadedB) != 1 || loadedB[0].ID != "cp-b1" {
		t.Errorf("plan iso-b proposals: got %d, want 1 with ID cp-b1", len(loadedB))
	}
}

func TestKV_NilKVSafety(t *testing.T) {
	ctx := context.Background()

	// kvPut with nil KV should not panic
	if err := kvPut(ctx, nil, "test-id", "value"); err != nil {
		t.Errorf("kvPut(nil) should silently succeed, got: %v", err)
	}

	// kvGet with nil KV should return error, not panic
	var target string
	if err := kvGet(ctx, nil, "test-id", &target); err == nil {
		t.Error("kvGet(nil) should return error")
	}

	// kvExists with nil KV should return false, not panic
	if kvExists(ctx, nil, "test-id") {
		t.Error("kvExists(nil) should return false")
	}

	// PlanExists with nil KV should return false, not panic
	if PlanExists(ctx, nil, "test-slug") {
		t.Error("PlanExists(nil) should return false")
	}
}
