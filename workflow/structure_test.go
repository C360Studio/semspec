package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlugify(t *testing.T) {
	t.Run("empty input returns empty string", func(t *testing.T) {
		result := Slugify("")
		if result != "" {
			t.Errorf("Slugify(%q) = %q, want %q", "", result, "")
		}
	})

	t.Run("whitespace-only input returns empty string", func(t *testing.T) {
		result := Slugify("   ")
		if result != "" {
			t.Errorf("Slugify(%q) = %q, want %q", "   ", result, "")
		}
	})

	t.Run("non-empty input returns 12-char hex string", func(t *testing.T) {
		result := Slugify("Add auth refresh")
		if len(result) != 12 {
			t.Errorf("Slugify returned %d chars, want 12: %q", len(result), result)
		}
		for _, c := range result {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("Slugify returned non-hex char %q in %q", c, result)
			}
		}
	})

	t.Run("same input always returns the same slug (deterministic)", func(t *testing.T) {
		input := "Add auth refresh"
		a := Slugify(input)
		b := Slugify(input)
		if a != b {
			t.Errorf("Slugify(%q) non-deterministic: %q != %q", input, a, b)
		}
	})

	t.Run("different inputs return different slugs", func(t *testing.T) {
		a := Slugify("Add auth refresh")
		b := Slugify("Fix the login bug")
		if a == b {
			t.Errorf("Slugify collision: %q and %q both produced %q", "Add auth refresh", "Fix the login bug", a)
		}
	})

	t.Run("case-insensitive: same slug for same words in different cases", func(t *testing.T) {
		a := Slugify("Add Auth Refresh")
		b := Slugify("add auth refresh")
		if a != b {
			t.Errorf("Slugify case-insensitive failed: %q != %q", a, b)
		}
	})

	t.Run("leading/trailing whitespace is trimmed before hashing", func(t *testing.T) {
		a := Slugify("  Add auth refresh  ")
		b := Slugify("Add auth refresh")
		if a != b {
			t.Errorf("Slugify whitespace trimming failed: %q != %q", a, b)
		}
	})

	t.Run("long descriptions produce valid 12-char slug", func(t *testing.T) {
		result := Slugify("a-very-long-description-that-exceeds-the-maximum-allowed-length-for-slugs")
		if len(result) != 12 {
			t.Errorf("Slugify returned %d chars for long input, want 12: %q", len(result), result)
		}
	})
}

func TestEnsureDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	if err := EnsureDirectories(tmpDir); err != nil {
		t.Fatalf("EnsureDirectories failed: %v", err)
	}

	dirs := []string{
		RootPath(tmpDir),
		SpecsPath(tmpDir),
		PlansPath(tmpDir),
		ArchivePath(tmpDir),
		ProjectsPath(tmpDir),
	}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("directory not created: %s", dir)
		}
	}
}

func TestPathHelpers(t *testing.T) {
	root := "/repo"

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"RootPath", RootPath(root), filepath.Join(root, ".semspec")},
		{"ConstitutionPath", ConstitutionPath(root), filepath.Join(root, ".semspec", "constitution.md")},
		{"SpecsPath", SpecsPath(root), filepath.Join(root, ".semspec", "specs")},
		{"PlansPath", PlansPath(root), filepath.Join(root, ".semspec", "plans")},
		{"ArchivePath", ArchivePath(root), filepath.Join(root, ".semspec", "archive")},
		{"PlanPath", PlanPath(root, "my-plan"), filepath.Join(root, ".semspec", "plans", "my-plan")},
		{"ProjectsPath", ProjectsPath(root), filepath.Join(root, ".semspec", "projects")},
		{"ProjectPath", ProjectPath(root, "proj"), filepath.Join(root, ".semspec", "projects", "proj")},
		{"ProjectPlansPath", ProjectPlansPath(root, "proj"), filepath.Join(root, ".semspec", "projects", "proj", "plans")},
		{"ProjectPlanPath", ProjectPlanPath(root, "proj", "plan"), filepath.Join(root, ".semspec", "projects", "proj", "plans", "plan")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
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
		{StatusApproved, StatusRequirementsGenerated, true},
		{StatusApproved, StatusReadyForExecution, true},
		{StatusApproved, StatusImplementing, false},
		{StatusRequirementsGenerated, StatusScenariosGenerated, true},
		{StatusRequirementsGenerated, StatusRejected, true},
		{StatusScenariosGenerated, StatusReviewed, true},
		{StatusScenariosGenerated, StatusReadyForExecution, true},
		{StatusScenariosGenerated, StatusRejected, true},
		{StatusReadyForExecution, StatusImplementing, true},
		{StatusReadyForExecution, StatusRejected, true},
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

func TestLoadConstitution(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("returns error when constitution does not exist", func(t *testing.T) {
		_, err := LoadConstitution(tmpDir)
		if err == nil {
			t.Error("expected error for missing constitution")
		}
	})

	t.Run("loads constitution when file exists", func(t *testing.T) {
		content := "# Constitution\nVersion: 2.0.0\n"
		constPath := ConstitutionPath(tmpDir)
		if err := os.MkdirAll(filepath.Dir(constPath), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(constPath, []byte(content), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}

		c, err := LoadConstitution(tmpDir)
		if err != nil {
			t.Fatalf("LoadConstitution failed: %v", err)
		}
		if c.Version != "2.0.0" {
			t.Errorf("Version = %q, want %q", c.Version, "2.0.0")
		}
	})
}
