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

func TestManager_CreateChange(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	change, err := m.CreateChange("Add auth refresh", "testuser")
	if err != nil {
		t.Fatalf("CreateChange failed: %v", err)
	}

	if change.Slug != "add-auth-refresh" {
		t.Errorf("Slug = %q, want %q", change.Slug, "add-auth-refresh")
	}

	if change.Status != StatusCreated {
		t.Errorf("Status = %q, want %q", change.Status, StatusCreated)
	}

	if change.Author != "testuser" {
		t.Errorf("Author = %q, want %q", change.Author, "testuser")
	}

	// Verify directory structure
	changePath := filepath.Join(tempDir, RootDir, ChangesDir, "add-auth-refresh")
	if _, err := os.Stat(changePath); os.IsNotExist(err) {
		t.Error("Change directory not created")
	}

	if _, err := os.Stat(filepath.Join(changePath, MetadataFile)); os.IsNotExist(err) {
		t.Error("Metadata file not created")
	}

	if _, err := os.Stat(filepath.Join(changePath, ChangeSpecsDir)); os.IsNotExist(err) {
		t.Error("Specs subdirectory not created")
	}
}

func TestManager_CreateChange_Duplicate(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	_, err := m.CreateChange("Add auth refresh", "user1")
	if err != nil {
		t.Fatalf("First CreateChange failed: %v", err)
	}

	_, err = m.CreateChange("Add auth refresh", "user2")
	if err == nil {
		t.Error("Expected error for duplicate change, got nil")
	}
}

func TestManager_LoadChange(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	created, err := m.CreateChange("Test change", "testuser")
	if err != nil {
		t.Fatalf("CreateChange failed: %v", err)
	}

	loaded, err := m.LoadChange(created.Slug)
	if err != nil {
		t.Fatalf("LoadChange failed: %v", err)
	}

	if loaded.Slug != created.Slug {
		t.Errorf("Slug = %q, want %q", loaded.Slug, created.Slug)
	}

	if loaded.Title != created.Title {
		t.Errorf("Title = %q, want %q", loaded.Title, created.Title)
	}
}

func TestManager_LoadChange_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	_, err := m.LoadChange("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent change, got nil")
	}
}

func TestManager_ListChanges(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	// Create multiple changes
	_, _ = m.CreateChange("First change", "user1")
	_, _ = m.CreateChange("Second change", "user2")

	changes, err := m.ListChanges()
	if err != nil {
		t.Fatalf("ListChanges failed: %v", err)
	}

	if len(changes) != 2 {
		t.Errorf("len(changes) = %d, want 2", len(changes))
	}
}

func TestManager_WriteAndReadProposal(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	change, _ := m.CreateChange("Test proposal", "testuser")

	content := "# Test Proposal\n\nThis is a test."
	if err := m.WriteProposal(change.Slug, content); err != nil {
		t.Fatalf("WriteProposal failed: %v", err)
	}

	read, err := m.ReadProposal(change.Slug)
	if err != nil {
		t.Fatalf("ReadProposal failed: %v", err)
	}

	if read != content {
		t.Errorf("ReadProposal = %q, want %q", read, content)
	}

	// Verify file flags update
	loaded, _ := m.LoadChange(change.Slug)
	if !loaded.Files.HasProposal {
		t.Error("HasProposal = false, want true")
	}
}

func TestManager_WriteAndReadDesign(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	change, _ := m.CreateChange("Test design", "testuser")

	content := "# Design\n\nTechnical approach."
	if err := m.WriteDesign(change.Slug, content); err != nil {
		t.Fatalf("WriteDesign failed: %v", err)
	}

	read, err := m.ReadDesign(change.Slug)
	if err != nil {
		t.Fatalf("ReadDesign failed: %v", err)
	}

	if read != content {
		t.Errorf("ReadDesign = %q, want %q", read, content)
	}
}

func TestManager_WriteAndReadSpec(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	change, _ := m.CreateChange("Test spec", "testuser")

	content := "# Spec\n\nRequirements."
	if err := m.WriteSpec(change.Slug, content); err != nil {
		t.Fatalf("WriteSpec failed: %v", err)
	}

	read, err := m.ReadSpec(change.Slug)
	if err != nil {
		t.Fatalf("ReadSpec failed: %v", err)
	}

	if read != content {
		t.Errorf("ReadSpec = %q, want %q", read, content)
	}
}

func TestManager_WriteAndReadTasks(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	change, _ := m.CreateChange("Test tasks", "testuser")

	content := "# Tasks\n\n- [ ] Task 1"
	if err := m.WriteTasks(change.Slug, content); err != nil {
		t.Fatalf("WriteTasks failed: %v", err)
	}

	read, err := m.ReadTasks(change.Slug)
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
		{StatusApproved, StatusImplementing, true},
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

func TestManager_UpdateChangeStatus(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	change, _ := m.CreateChange("Test status", "testuser")

	// Valid transition
	err := m.UpdateChangeStatus(change.Slug, StatusDrafted)
	if err != nil {
		t.Fatalf("UpdateChangeStatus failed: %v", err)
	}

	loaded, _ := m.LoadChange(change.Slug)
	if loaded.Status != StatusDrafted {
		t.Errorf("Status = %q, want %q", loaded.Status, StatusDrafted)
	}

	// Invalid transition
	err = m.UpdateChangeStatus(change.Slug, StatusArchived)
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

func TestManager_ArchiveChange(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager(tempDir)

	change, _ := m.CreateChange("Test archive", "testuser")

	// Cannot archive created change
	err := m.ArchiveChange(change.Slug)
	if err == nil {
		t.Error("Expected error archiving non-complete change, got nil")
	}

	// Transition to complete
	_ = m.UpdateChangeStatus(change.Slug, StatusDrafted)
	_ = m.UpdateChangeStatus(change.Slug, StatusReviewed)
	_ = m.UpdateChangeStatus(change.Slug, StatusApproved)
	_ = m.UpdateChangeStatus(change.Slug, StatusImplementing)
	_ = m.UpdateChangeStatus(change.Slug, StatusComplete)

	// Now archive
	err = m.ArchiveChange(change.Slug)
	if err != nil {
		t.Fatalf("ArchiveChange failed: %v", err)
	}

	// Verify moved to archive
	archivePath := filepath.Join(tempDir, RootDir, ArchiveDir, change.Slug)
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		t.Error("Change not moved to archive")
	}

	// Verify removed from changes
	changePath := filepath.Join(tempDir, RootDir, ChangesDir, change.Slug)
	if _, err := os.Stat(changePath); !os.IsNotExist(err) {
		t.Error("Change still exists in changes directory")
	}
}
