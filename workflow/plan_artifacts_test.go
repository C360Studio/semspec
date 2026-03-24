//go:build integration

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportSpecFiles(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := context.Background()
	slug := "test-plan"

	// Create plan with requirements and scenarios.
	plan := &Plan{
		Slug:      slug,
		Title:     "Test Plan",
		Goal:      "Test goal",
		ProjectID: ProjectEntityID("default"),
		Status:    StatusComplete,
		CreatedAt: time.Now(),
	}
	if err := SavePlan(ctx, nil, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	requirements := []Requirement{
		{
			ID:          "req-1",
			PlanID:      PlanEntityID(slug),
			Title:       "User Authentication",
			Description: "Users must be able to authenticate via OAuth2.",
			Status:      RequirementStatusActive,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			ID:          "req-2",
			PlanID:      PlanEntityID(slug),
			Title:       "Session Management",
			Description: "Sessions must persist across browser restarts.",
			Status:      RequirementStatusActive,
			DependsOn:   []string{"req-1"},
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
	}
	if err := SaveRequirements(ctx, nil, requirements, slug); err != nil {
		t.Fatalf("save requirements: %v", err)
	}

	scenarios := []Scenario{
		{
			ID:            "scen-1",
			RequirementID: "req-1",
			Given:         "a user with valid OAuth2 credentials",
			When:          "the user submits login credentials",
			Then:          []string{"a session token is returned", "the token expires in 1 hour"},
			Status:        ScenarioStatusPassing,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		},
		{
			ID:            "scen-2",
			RequirementID: "req-2",
			Given:         "an authenticated session",
			When:          "the browser is restarted",
			Then:          []string{"the session is restored from persistent storage"},
			Status:        ScenarioStatusPending,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		},
	}
	if err := SaveScenarios(ctx, nil, scenarios, slug); err != nil {
		t.Fatalf("save scenarios: %v", err)
	}

	// Export specs.
	files, err := ExportSpecFiles(ctx, nil, tmpDir, slug)
	if err != nil {
		t.Fatalf("export spec files: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	// Verify first spec file content.
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read spec file: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "# User Authentication") {
		t.Error("spec file missing requirement title")
	}
	if !strings.Contains(content, "Users must be able to authenticate via OAuth2.") {
		t.Error("spec file missing description")
	}
	if !strings.Contains(content, "**Given** a user with valid OAuth2 credentials") {
		t.Error("spec file missing Given clause")
	}
	if !strings.Contains(content, "**When** the user submits login credentials") {
		t.Error("spec file missing When clause")
	}
	if !strings.Contains(content, "- a session token is returned") {
		t.Error("spec file missing Then assertion")
	}

	// Verify second spec file has dependency info.
	data2, err := os.ReadFile(files[1])
	if err != nil {
		t.Fatalf("read second spec file: %v", err)
	}
	content2 := string(data2)

	if !strings.Contains(content2, "## Dependencies") {
		t.Error("spec file missing dependencies section")
	}
	if !strings.Contains(content2, "req-1") {
		t.Error("spec file missing dependency reference")
	}
}

func TestExportSpecFiles_NoRequirements(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()
	slug := "empty-plan"

	plan := &Plan{
		Slug:      slug,
		Title:     "Empty Plan",
		ProjectID: ProjectEntityID("default"),
		Status:    StatusComplete,
		CreatedAt: time.Now(),
	}
	if err := SavePlan(ctx, nil, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	files, err := ExportSpecFiles(ctx, nil, tmpDir, slug)
	if err != nil {
		t.Fatalf("export spec files: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files for plan with no requirements, got %d", len(files))
	}
}

func TestGenerateArchive(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()
	slug := "archive-plan"

	approvedAt := time.Now().Add(-24 * time.Hour)
	plan := &Plan{
		Slug:       slug,
		Title:      "Archive Plan",
		Goal:       "Build the authentication system",
		ProjectID:  ProjectEntityID("default"),
		Status:     StatusComplete,
		Approved:   true,
		ApprovedAt: &approvedAt,
		CreatedAt:  time.Now().Add(-48 * time.Hour),
	}
	if err := SavePlan(ctx, nil, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	requirements := []Requirement{
		{
			ID:        "req-1",
			Title:     "Auth System",
			Status:    RequirementStatusActive,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	if err := SaveRequirements(ctx, nil, requirements, slug); err != nil {
		t.Fatalf("save requirements: %v", err)
	}

	scenarios := []Scenario{
		{
			ID:            "scen-1",
			RequirementID: "req-1",
			Given:         "valid creds",
			When:          "login",
			Then:          []string{"token returned"},
			Status:        ScenarioStatusPassing,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		},
		{
			ID:            "scen-2",
			RequirementID: "req-1",
			Given:         "invalid creds",
			When:          "login",
			Then:          []string{"error returned"},
			Status:        ScenarioStatusFailing,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		},
	}
	if err := SaveScenarios(ctx, nil, scenarios, slug); err != nil {
		t.Fatalf("save scenarios: %v", err)
	}

	changeProposals := []ChangeProposal{
		{
			ID:             "cp-1",
			Title:          "Add MFA support",
			Rationale:      "Security audit recommended MFA",
			Status:         ChangeProposalStatusAccepted,
			ProposedBy:     "security-reviewer",
			AffectedReqIDs: []string{"req-1"},
			CreatedAt:      time.Now(),
		},
	}
	if err := SaveChangeProposals(ctx, nil, changeProposals, slug); err != nil {
		t.Fatalf("save change proposals: %v", err)
	}

	// Generate archive.
	filePath, err := GenerateArchive(ctx, nil, tmpDir, slug)
	if err != nil {
		t.Fatalf("generate archive: %v", err)
	}

	// Verify file exists in archive dir.
	expected := filepath.Join(tmpDir, ".semspec", "archive", slug+".md")
	if filePath != expected {
		t.Errorf("expected path %s, got %s", expected, filePath)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	content := string(data)

	// Verify content sections.
	checks := []struct {
		label string
		text  string
	}{
		{"title", "# Archive: Archive Plan"},
		{"goal", "Build the authentication system"},
		{"timeline", "## Timeline"},
		{"requirements heading", "## Requirements (1)"},
		{"requirement title", "Auth System"},
		{"scenarios heading", "## Scenarios (2)"},
		{"passing count", "Passing: 1"},
		{"failing count", "Failing: 1"},
		{"change proposals heading", "## Change Proposals (1)"},
		{"proposal title", "Add MFA support"},
		{"proposal rationale", "Security audit recommended MFA"},
	}

	for _, c := range checks {
		if !strings.Contains(content, c.text) {
			t.Errorf("archive missing %s: expected to contain %q", c.label, c.text)
		}
	}
}

func TestGenerateArchive_InvalidSlug(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	_, err := GenerateArchive(ctx, nil, tmpDir, "../escape")
	if err == nil {
		t.Error("expected error for invalid slug")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, "30 minutes"},
		{2 * time.Hour, "2 hours"},
		{24 * time.Hour, "1 day"},
		{72 * time.Hour, "3 days"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
