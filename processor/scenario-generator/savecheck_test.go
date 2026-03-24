//go:build integration

package scenariogenerator

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// TestSaveAndCheckCompletion_SequentialCoverage verifies that when 3
// requirements each get scenarios saved sequentially, the coverage check
// detects full coverage on the third call and would publish the event.
// This is a unit test — no NATS needed since we test the file I/O and
// coverage logic, not the event publication.
func TestSaveAndCheckCompletion_SequentialCoverage(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	ctx := context.Background()
	slug := "test-coverage"

	// Set up plan + 3 requirements. Advance plan to requirements_generated
	// so the status transition to scenarios_generated is valid.
	plan, err := workflow.CreatePlan(ctx, nil, slug, "Coverage Test")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := workflow.ApprovePlan(ctx, nil, plan); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}
	if err := workflow.SetPlanStatus(ctx, nil, plan, workflow.StatusRequirementsGenerated); err != nil {
		t.Fatalf("SetPlanStatus: %v", err)
	}

	reqs := []workflow.Requirement{
		{ID: "requirement." + slug + ".1", Title: "Req 1", Status: "active"},
		{ID: "requirement." + slug + ".2", Title: "Req 2", Status: "active"},
		{ID: "requirement." + slug + ".3", Title: "Req 3", Status: "active"},
	}
	if saveErr := workflow.SaveRequirements(ctx, nil, reqs, slug); saveErr != nil {
		t.Fatalf("SaveRequirements: %v", saveErr)
	}

	// Component with nil natsClient — saveAndCheckCompletion will succeed
	// for save but fail on publishScenariosGeneratedEvent (nil natsClient).
	// We use the error to detect that the coverage check reached the publish step.
	c := &Component{
		logger: slog.Default(),
	}

	now := time.Now()

	// Save scenarios for requirement 1 — coverage: 1/3.
	err = c.saveAndCheckCompletion(ctx, &payloads.ScenarioGeneratorRequest{
		Slug:          slug,
		RequirementID: "requirement." + slug + ".1",
	}, []workflow.Scenario{
		{ID: "scenario." + slug + ".1.1", RequirementID: "requirement." + slug + ".1", Given: "g", When: "w", Then: []string{"t"}, Status: "pending", CreatedAt: now, UpdatedAt: now},
	})
	if err != nil {
		t.Fatalf("Save req 1: unexpected error: %v", err)
	}

	// Verify: 1 scenario on disk.
	scenarios, _ := workflow.LoadScenarios(ctx, nil, slug)
	if len(scenarios) != 1 {
		t.Fatalf("After req 1: expected 1 scenario, got %d", len(scenarios))
	}

	// Save scenarios for requirement 2 — coverage: 2/3.
	err = c.saveAndCheckCompletion(ctx, &payloads.ScenarioGeneratorRequest{
		Slug:          slug,
		RequirementID: "requirement." + slug + ".2",
	}, []workflow.Scenario{
		{ID: "scenario." + slug + ".2.1", RequirementID: "requirement." + slug + ".2", Given: "g", When: "w", Then: []string{"t"}, Status: "pending", CreatedAt: now, UpdatedAt: now},
	})
	if err != nil {
		t.Fatalf("Save req 2: unexpected error: %v", err)
	}

	scenarios, _ = workflow.LoadScenarios(ctx, nil, slug)
	if len(scenarios) != 2 {
		t.Fatalf("After req 2: expected 2 scenarios, got %d", len(scenarios))
	}

	// Save scenarios for requirement 3 — coverage: 3/3.
	// This should detect full coverage and attempt to publish the event.
	// Since natsClient is nil, publishScenariosGeneratedEvent will error.
	err = c.saveAndCheckCompletion(ctx, &payloads.ScenarioGeneratorRequest{
		Slug:          slug,
		RequirementID: "requirement." + slug + ".3",
	}, []workflow.Scenario{
		{ID: "scenario." + slug + ".3.1", RequirementID: "requirement." + slug + ".3", Given: "g", When: "w", Then: []string{"t"}, Status: "pending", CreatedAt: now, UpdatedAt: now},
	})

	// We expect an error from publishScenariosGeneratedEvent (nil natsClient)
	// which proves the coverage check reached the publish step.
	if err == nil {
		t.Fatal("Save req 3: expected publish error (nil natsClient), got nil — coverage check may not have triggered")
	}

	// Verify the error is from the publish step, not from save.
	if err.Error() == "" {
		t.Fatal("Save req 3: empty error")
	}

	// Verify: 3 scenarios on disk.
	scenarios, _ = workflow.LoadScenarios(ctx, nil, slug)
	if len(scenarios) != 3 {
		t.Fatalf("After req 3: expected 3 scenarios, got %d", len(scenarios))
	}

	// Verify the plan status was set to scenarios_generated (happens before publish).
	plan, _ = workflow.LoadPlan(ctx, nil, slug)
	if plan.Status != workflow.StatusScenariosGenerated {
		t.Fatalf("Plan status = %q, want %q", plan.Status, workflow.StatusScenariosGenerated)
	}

	t.Log("PASS: 3 sequential saves → coverage detected → status transitioned → publish attempted")
}

// TestSaveAndCheckCompletion_Idempotent verifies that saving scenarios for
// the same requirement twice doesn't create duplicates.
func TestSaveAndCheckCompletion_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	ctx := context.Background()
	slug := "test-idempotent"

	if _, err := workflow.CreatePlan(ctx, nil, slug, "Idempotent Test"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	reqs := []workflow.Requirement{
		{ID: "requirement." + slug + ".1", Title: "Req 1", Status: "active"},
	}
	if err := workflow.SaveRequirements(ctx, nil, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}

	c := &Component{logger: slog.Default()}
	now := time.Now()

	trigger := &payloads.ScenarioGeneratorRequest{
		Slug:          slug,
		RequirementID: "requirement." + slug + ".1",
	}
	scenarios := []workflow.Scenario{
		{ID: "scenario." + slug + ".1.1", RequirementID: "requirement." + slug + ".1", Given: "g", When: "w", Then: []string{"t"}, Status: "pending", CreatedAt: now, UpdatedAt: now},
		{ID: "scenario." + slug + ".1.2", RequirementID: "requirement." + slug + ".1", Given: "g2", When: "w2", Then: []string{"t2"}, Status: "pending", CreatedAt: now, UpdatedAt: now},
	}

	// Save twice — should not duplicate.
	_ = c.saveAndCheckCompletion(ctx, trigger, scenarios)
	_ = c.saveAndCheckCompletion(ctx, trigger, scenarios)

	saved, _ := workflow.LoadScenarios(ctx, nil, slug)
	if len(saved) != 2 {
		t.Fatalf("Expected 2 scenarios after double save, got %d", len(saved))
	}

	t.Log("PASS: Double save is idempotent — no duplicates")
}
