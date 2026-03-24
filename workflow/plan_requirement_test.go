//go:build integration

package workflow

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSaveLoadRequirements_RoundTrip(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	plan, err := CreatePlan(ctx, nil, "test-plan", "Test Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	requirements := []Requirement{
		{
			ID:          "requirement.test-plan.1",
			PlanID:      plan.ID,
			Title:       "First Requirement",
			Description: "Description of first requirement",
			Status:      RequirementStatusActive,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "requirement.test-plan.2",
			PlanID:      plan.ID,
			Title:       "Second Requirement",
			Description: "Description of second requirement",
			Status:      RequirementStatusSuperseded,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	if err := SaveRequirements(ctx, nil, requirements, plan.Slug); err != nil {
		t.Fatalf("SaveRequirements() error: %v", err)
	}

	got, err := LoadRequirements(ctx, nil, plan.Slug)
	if err != nil {
		t.Fatalf("LoadRequirements() error: %v", err)
	}

	if len(got) != len(requirements) {
		t.Fatalf("LoadRequirements() returned %d items, want %d", len(got), len(requirements))
	}

	for i, want := range requirements {
		if got[i].ID != want.ID {
			t.Errorf("[%d] ID = %q, want %q", i, got[i].ID, want.ID)
		}
		if got[i].PlanID != want.PlanID {
			t.Errorf("[%d] PlanID = %q, want %q", i, got[i].PlanID, want.PlanID)
		}
		if got[i].Title != want.Title {
			t.Errorf("[%d] Title = %q, want %q", i, got[i].Title, want.Title)
		}
		if got[i].Status != want.Status {
			t.Errorf("[%d] Status = %q, want %q", i, got[i].Status, want.Status)
		}
		if !got[i].CreatedAt.Equal(want.CreatedAt) {
			t.Errorf("[%d] CreatedAt = %v, want %v", i, got[i].CreatedAt, want.CreatedAt)
		}
	}
}

func TestLoadRequirements_MissingFile_ReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	plan, err := CreatePlan(ctx, nil, "new-plan", "New Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	got, err := LoadRequirements(ctx, nil, plan.Slug)
	if err != nil {
		t.Fatalf("LoadRequirements() on missing file should not error, got: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LoadRequirements() = %d items, want 0", len(got))
	}
}

func TestSaveRequirements_InvalidSlug(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	err := SaveRequirements(ctx, nil, []Requirement{}, "invalid slug!")
	if err == nil {
		t.Error("SaveRequirements() with invalid slug should return error")
	}
}

func TestLoadRequirements_InvalidSlug(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	_, err := LoadRequirements(ctx, nil, "invalid slug!")
	if err == nil {
		t.Error("LoadRequirements() with invalid slug should return error")
	}
}

func TestValidateRequirementDAG(t *testing.T) {
	req := func(id string, deps ...string) Requirement {
		return Requirement{ID: id, DependsOn: deps}
	}

	tests := []struct {
		name        string
		reqs        []Requirement
		wantErr     bool
		errContains string
	}{
		{
			name:    "empty slice passes",
			reqs:    []Requirement{},
			wantErr: false,
		},
		{
			name:    "root requirement with no dependencies passes",
			reqs:    []Requirement{req("req-a")},
			wantErr: false,
		},
		{
			name: "valid linear chain passes",
			reqs: []Requirement{
				req("req-a"),
				req("req-b", "req-a"),
				req("req-c", "req-b"),
			},
			wantErr: false,
		},
		{
			name: "valid diamond dependency passes",
			reqs: []Requirement{
				req("req-a"),
				req("req-b", "req-a"),
				req("req-c", "req-a"),
				req("req-d", "req-b", "req-c"),
			},
			wantErr: false,
		},
		{
			name:        "self-reference returns error",
			reqs:        []Requirement{req("req-a", "req-a")},
			wantErr:     true,
			errContains: "depends on itself",
		},
		{
			name:        "reference to nonexistent requirement returns error",
			reqs:        []Requirement{req("req-a", "req-missing")},
			wantErr:     true,
			errContains: "unknown requirement",
		},
		{
			name: "simple two-node cycle returns error",
			reqs: []Requirement{
				req("req-a", "req-b"),
				req("req-b", "req-a"),
			},
			wantErr:     true,
			errContains: "cycle detected",
		},
		{
			name: "three-node cycle returns error",
			reqs: []Requirement{
				req("req-a", "req-c"),
				req("req-b", "req-a"),
				req("req-c", "req-b"),
			},
			wantErr:     true,
			errContains: "cycle detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRequirementDAG(tt.reqs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateRequirementDAG() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateRequirementDAG() error = %q, want it to contain %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestSaveRequirements_RejectsInvalidDAG(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	plan, err := CreatePlan(ctx, nil, "dag-test", "DAG Test Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	cyclic := []Requirement{
		{ID: "req-a", DependsOn: []string{"req-b"}},
		{ID: "req-b", DependsOn: []string{"req-a"}},
	}

	if err := SaveRequirements(ctx, nil, cyclic, plan.Slug); err == nil {
		t.Error("SaveRequirements() with cyclic requirements should return error")
	}
}

func TestSaveRequirements_AcceptsValidDAG(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	plan, err := CreatePlan(ctx, nil, "dag-valid", "DAG Valid Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}

	diamond := []Requirement{
		{ID: "req-a"},
		{ID: "req-b", DependsOn: []string{"req-a"}},
		{ID: "req-c", DependsOn: []string{"req-a"}},
		{ID: "req-d", DependsOn: []string{"req-b", "req-c"}},
	}

	if err := SaveRequirements(ctx, nil, diamond, plan.Slug); err != nil {
		t.Errorf("SaveRequirements() with valid diamond DAG should not error, got: %v", err)
	}
}
