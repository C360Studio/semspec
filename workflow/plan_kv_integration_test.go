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
