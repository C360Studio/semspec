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
	tw := newTestTripleWriter(t)
	ctx := context.Background()
	slug := "test-plan"

	// Create plan with requirements and scenarios.
	if _, err := CreatePlan(ctx, tw, slug, "Test Plan"); err != nil {
		t.Fatalf("create plan: %v", err)
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
	if err := SaveRequirements(ctx, tw, requirements, slug); err != nil {
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
	if err := SaveScenarios(ctx, tw, scenarios, slug); err != nil {
		t.Fatalf("save scenarios: %v", err)
	}

	// Export specs.
	files, err := ExportSpecFiles(ctx, tw, tmpDir, slug)
	if err != nil {
		t.Fatalf("export spec files: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	// Read all files into a single corpus — map iteration order from
	// ReadEntitiesByPrefix is non-deterministic, so we can't assume
	// which requirement ends up in files[0] vs files[1].
	var allContent strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read spec file %s: %v", f, err)
		}
		allContent.Write(data)
		allContent.WriteByte('\n')
	}
	corpus := allContent.String()

	if !strings.Contains(corpus, "# User Authentication") {
		t.Error("spec files missing requirement title 'User Authentication'")
	}
	if !strings.Contains(corpus, "Users must be able to authenticate via OAuth2.") {
		t.Error("spec files missing description")
	}
	if !strings.Contains(corpus, "**Given** a user with valid OAuth2 credentials") {
		t.Error("spec files missing Given clause")
	}
	if !strings.Contains(corpus, "**When** the user submits login credentials") {
		t.Error("spec files missing When clause")
	}
	if !strings.Contains(corpus, "- a session token is returned") {
		t.Error("spec files missing Then assertion")
	}
	if !strings.Contains(corpus, "## Dependencies") {
		t.Error("spec files missing dependencies section")
	}
	// DependsOn IDs are hashed when round-tripping through triples.
	if !strings.Contains(corpus, HashInstanceID("req-1")) {
		t.Error("spec files missing dependency reference")
	}
}

func TestExportSpecFiles_NoRequirements(t *testing.T) {
	tmpDir := t.TempDir()
	tw := newTestTripleWriter(t)
	ctx := context.Background()
	slug := "empty-plan"

	if _, err := CreatePlan(ctx, tw, slug, "Empty Plan"); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	files, err := ExportSpecFiles(ctx, tw, tmpDir, slug)
	if err != nil {
		t.Fatalf("export spec files: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files for plan with no requirements, got %d", len(files))
	}
}

func TestGenerateArchive(t *testing.T) {
	tmpDir := t.TempDir()
	tw := newTestTripleWriter(t)
	ctx := context.Background()
	slug := "archive-plan"

	if _, err := CreatePlan(ctx, tw, slug, "Archive Plan"); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	requirements := []Requirement{
		{
			ID:        "req-1",
			PlanID:    PlanEntityID(slug),
			Title:     "Auth System",
			Status:    RequirementStatusActive,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	if err := SaveRequirements(ctx, tw, requirements, slug); err != nil {
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
	if err := SaveScenarios(ctx, tw, scenarios, slug); err != nil {
		t.Fatalf("save scenarios: %v", err)
	}

	changeProposals := []ChangeProposal{
		{
			ID:             "cp-1",
			PlanID:         PlanEntityID(slug),
			Title:          "Add MFA support",
			Rationale:      "Security audit recommended MFA",
			Status:         ChangeProposalStatusAccepted,
			ProposedBy:     "security-reviewer",
			AffectedReqIDs: []string{"req-1"},
			CreatedAt:      time.Now(),
		},
	}
	if err := SaveChangeProposals(ctx, tw, changeProposals, slug); err != nil {
		t.Fatalf("save change proposals: %v", err)
	}

	// Generate archive.
	filePath, err := GenerateArchive(ctx, tw, tmpDir, slug)
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
