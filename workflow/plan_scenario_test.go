//go:build integration

package workflow

import (
	"context"
	"testing"
	"time"
)

func TestSaveLoadScenarios_RoundTrip(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	plan, err := CreatePlan(ctx, nil, "test-plan", "Test Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	scenarios := []Scenario{
		{
			ID:            "scenario.test-plan.1.1",
			RequirementID: "requirement.test-plan.1",
			Given:         "a valid user session exists",
			When:          "the user requests protected data",
			Then:          []string{"the data is returned", "the audit log is updated"},
			Status:        ScenarioStatusPending,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            "scenario.test-plan.1.2",
			RequirementID: "requirement.test-plan.1",
			Given:         "no user session exists",
			When:          "the user requests protected data",
			Then:          []string{"a 401 error is returned"},
			Status:        ScenarioStatusFailing,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}

	if err := SaveScenarios(ctx, nil, scenarios, plan.Slug); err != nil {
		t.Fatalf("SaveScenarios() error: %v", err)
	}

	got, err := LoadScenarios(ctx, nil, plan.Slug)
	if err != nil {
		t.Fatalf("LoadScenarios() error: %v", err)
	}

	if len(got) != len(scenarios) {
		t.Fatalf("LoadScenarios() returned %d items, want %d", len(got), len(scenarios))
	}

	for i, want := range scenarios {
		if got[i].ID != want.ID {
			t.Errorf("[%d] ID = %q, want %q", i, got[i].ID, want.ID)
		}
		if got[i].RequirementID != want.RequirementID {
			t.Errorf("[%d] RequirementID = %q, want %q", i, got[i].RequirementID, want.RequirementID)
		}
		if got[i].Given != want.Given {
			t.Errorf("[%d] Given = %q, want %q", i, got[i].Given, want.Given)
		}
		if got[i].When != want.When {
			t.Errorf("[%d] When = %q, want %q", i, got[i].When, want.When)
		}
		if len(got[i].Then) != len(want.Then) {
			t.Errorf("[%d] Then len = %d, want %d", i, len(got[i].Then), len(want.Then))
		}
		if got[i].Status != want.Status {
			t.Errorf("[%d] Status = %q, want %q", i, got[i].Status, want.Status)
		}
		if !got[i].CreatedAt.Equal(want.CreatedAt) {
			t.Errorf("[%d] CreatedAt = %v, want %v", i, got[i].CreatedAt, want.CreatedAt)
		}
	}
}

func TestLoadScenarios_MissingFile_ReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	plan, err := CreatePlan(ctx, nil, "new-plan", "New Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	got, err := LoadScenarios(ctx, nil, plan.Slug)
	if err != nil {
		t.Fatalf("LoadScenarios() on missing file should not error, got: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LoadScenarios() = %d items, want 0", len(got))
	}
}

func TestSaveScenarios_InvalidSlug(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	err := SaveScenarios(ctx, nil, []Scenario{}, "invalid slug!")
	if err == nil {
		t.Error("SaveScenarios() with invalid slug should return error")
	}
}

func TestLoadScenarios_InvalidSlug(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	_, err := LoadScenarios(ctx, nil, "invalid slug!")
	if err == nil {
		t.Error("LoadScenarios() with invalid slug should return error")
	}
}
