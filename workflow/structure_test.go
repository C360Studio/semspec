package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Add auth refresh", "add-auth-refresh"},
		{"Fix Bug #123", "fix-bug-123"},
		{"Multiple   spaces", "multiple-spaces"},
		{"Already-slugified", "already-slugified"},
		{"UPPERCASE", "uppercase"},
		{"special!@#$%chars", "specialchars"},
		{"", ""},
		{"   leading and trailing   ", "leading-and-trailing"},
		{"a-very-long-description-that-exceeds-the-maximum-allowed-length-for-slugs", "a-very-long-description-that-exceeds-the-maximum-a"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := Slugify(tt.input)
			if result != tt.expected {
				t.Errorf("Slugify(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestManager_CreatePlanRecord(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	plan, err := m.CreatePlanRecord("Add auth refresh", "testuser")
	if err != nil {
		t.Fatalf("CreatePlanRecord failed: %v", err)
	}

	if plan.Slug != "add-auth-refresh" {
		t.Errorf("Slug = %q, want %q", plan.Slug, "add-auth-refresh")
	}

	if plan.Status != StatusCreated {
		t.Errorf("Status = %q, want %q", plan.Status, StatusCreated)
	}

	if plan.Author != "testuser" {
		t.Errorf("Author = %q, want %q", plan.Author, "testuser")
	}

	// Verify directory structure
	planPath := filepath.Join(tempDir, RootDir, PlansDir, "add-auth-refresh")
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		t.Error("Plan directory not created")
	}

	if _, err := os.Stat(filepath.Join(planPath, MetadataFile)); os.IsNotExist(err) {
		t.Error("Metadata file not created")
	}

	if _, err := os.Stat(filepath.Join(planPath, PlanSpecsDir)); os.IsNotExist(err) {
		t.Error("Specs subdirectory not created")
	}
}

func TestManager_CreatePlanRecord_Duplicate(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	_, err := m.CreatePlanRecord("Add auth refresh", "user1")
	if err != nil {
		t.Fatalf("First CreatePlanRecord failed: %v", err)
	}

	_, err = m.CreatePlanRecord("Add auth refresh", "user2")
	if err == nil {
		t.Error("Expected error for duplicate plan, got nil")
	}
}

func TestManager_LoadPlanRecord(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	created, err := m.CreatePlanRecord("Test plan", "testuser")
	if err != nil {
		t.Fatalf("CreatePlanRecord failed: %v", err)
	}

	loaded, err := m.LoadPlanRecord(created.Slug)
	if err != nil {
		t.Fatalf("LoadPlanRecord failed: %v", err)
	}

	if loaded.Slug != created.Slug {
		t.Errorf("Slug = %q, want %q", loaded.Slug, created.Slug)
	}

	if loaded.Title != created.Title {
		t.Errorf("Title = %q, want %q", loaded.Title, created.Title)
	}
}

func TestManager_LoadPlanRecord_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	_, err := m.LoadPlanRecord("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent plan, got nil")
	}
}

func TestManager_ListPlanRecords(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	// Create multiple plans
	_, _ = m.CreatePlanRecord("First plan", "user1")
	_, _ = m.CreatePlanRecord("Second plan", "user2")

	plans, err := m.ListPlanRecords()
	if err != nil {
		t.Fatalf("ListPlanRecords failed: %v", err)
	}

	if len(plans) != 2 {
		t.Errorf("len(plans) = %d, want 2", len(plans))
	}
}

func TestManager_WriteAndReadTasks(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	plan, _ := m.CreatePlanRecord("Test tasks", "testuser")

	content := "# Tasks\n\n- [ ] Task 1"
	if err := m.WriteTasks(plan.Slug, content); err != nil {
		t.Fatalf("WriteTasks failed: %v", err)
	}

	read, err := m.ReadTasks(plan.Slug)
	if err != nil {
		t.Fatalf("ReadTasks failed: %v", err)
	}

	if read != content {
		t.Errorf("ReadTasks = %q, want %q", read, content)
	}
}

func TestStatus_CanTransitionTo(t *testing.T) {
	tests := []struct {
		from     Status
		to       Status
		expected bool
	}{
		{StatusCreated, StatusDrafted, true},
		{StatusCreated, StatusRejected, true},
		{StatusCreated, StatusApproved, false},
		{StatusDrafted, StatusReviewed, true},
		{StatusDrafted, StatusRejected, true},
		{StatusReviewed, StatusApproved, true},
		{StatusReviewed, StatusRejected, true},
		{StatusApproved, StatusPhasesGenerated, true},
		{StatusApproved, StatusTasksGenerated, false}, // must go through phases
		{StatusApproved, StatusImplementing, false},    // must go through phases
		{StatusPhasesGenerated, StatusPhasesApproved, true},
		{StatusPhasesGenerated, StatusRejected, true},
		{StatusPhasesApproved, StatusTasksGenerated, true},
		{StatusPhasesApproved, StatusRejected, true},
		{StatusTasksGenerated, StatusTasksApproved, true},
		{StatusTasksGenerated, StatusImplementing, false},
		{StatusTasksApproved, StatusImplementing, true},
		{StatusTasksApproved, StatusComplete, false},
		{StatusImplementing, StatusComplete, true},
		{StatusComplete, StatusArchived, true},
		{StatusArchived, StatusCreated, false},
		{StatusRejected, StatusCreated, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			result := tt.from.CanTransitionTo(tt.to)
			if result != tt.expected {
				t.Errorf("CanTransitionTo(%s, %s) = %v, want %v", tt.from, tt.to, result, tt.expected)
			}
		})
	}
}

func TestManager_UpdatePlanStatus(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	plan, _ := m.CreatePlanRecord("Test status", "testuser")

	// Valid transition
	err := m.UpdatePlanStatus(plan.Slug, StatusDrafted)
	if err != nil {
		t.Fatalf("UpdatePlanStatus failed: %v", err)
	}

	loaded, _ := m.LoadPlanRecord(plan.Slug)
	if loaded.Status != StatusDrafted {
		t.Errorf("Status = %q, want %q", loaded.Status, StatusDrafted)
	}

	// Invalid transition
	err = m.UpdatePlanStatus(plan.Slug, StatusArchived)
	if err == nil {
		t.Error("Expected error for invalid transition, got nil")
	}
}

func TestParseConstitution(t *testing.T) {
	content := `# Project Constitution

Version: 1.0.0
Ratified: 2025-01-30

## Principles

### 1. Test-First Development

All features MUST have tests written before implementation.

Rationale: Ensures testability and catches design issues early.

### 2. No Direct Database Access

All data access MUST go through repository interfaces.

Rationale: Enables testing and future storage changes.
`

	constitution, err := ParseConstitution(content)
	if err != nil {
		t.Fatalf("ParseConstitution failed: %v", err)
	}

	if constitution.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", constitution.Version, "1.0.0")
	}

	if len(constitution.Principles) != 2 {
		t.Fatalf("len(Principles) = %d, want 2", len(constitution.Principles))
	}

	p1 := constitution.Principles[0]
	if p1.Number != 1 {
		t.Errorf("Principle 1 Number = %d, want 1", p1.Number)
	}
	if p1.Title != "Test-First Development" {
		t.Errorf("Principle 1 Title = %q, want %q", p1.Title, "Test-First Development")
	}
	if p1.Rationale == "" {
		t.Error("Principle 1 Rationale is empty")
	}
}

func TestManager_ArchivePlan(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	plan, _ := m.CreatePlanRecord("Test archive", "testuser")

	// Cannot archive created plan
	err := m.ArchivePlanRecord(plan.Slug)
	if err == nil {
		t.Error("Expected error archiving non-complete plan, got nil")
	}

	// Transition to complete via full status chain
	_ = m.UpdatePlanStatus(plan.Slug, StatusDrafted)
	_ = m.UpdatePlanStatus(plan.Slug, StatusReviewed)
	_ = m.UpdatePlanStatus(plan.Slug, StatusApproved)
	_ = m.UpdatePlanStatus(plan.Slug, StatusPhasesGenerated)
	_ = m.UpdatePlanStatus(plan.Slug, StatusPhasesApproved)
	_ = m.UpdatePlanStatus(plan.Slug, StatusTasksGenerated)
	_ = m.UpdatePlanStatus(plan.Slug, StatusTasksApproved)
	_ = m.UpdatePlanStatus(plan.Slug, StatusImplementing)
	_ = m.UpdatePlanStatus(plan.Slug, StatusComplete)

	// Now archive
	err = m.ArchivePlanRecord(plan.Slug)
	if err != nil {
		t.Fatalf("ArchivePlanRecord failed: %v", err)
	}

	// Verify moved to archive
	archivePath := filepath.Join(tempDir, RootDir, ArchiveDir, plan.Slug)
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		t.Error("Plan not moved to archive")
	}

	// Verify removed from plans
	planPath := filepath.Join(tempDir, RootDir, PlansDir, plan.Slug)
	if _, err := os.Stat(planPath); !os.IsNotExist(err) {
		t.Error("Plan still exists in plans directory")
	}
}
