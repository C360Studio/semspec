package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// categoriesJSON is a minimal valid categories payload used across multiple tests.
const categoriesJSON = `{
	"categories": [
		{
			"id": "missing_tests",
			"label": "Missing Tests",
			"description": "No tests submitted with implementation.",
			"signals": ["No test file created"],
			"guidance": "Create test files alongside implementation."
		},
		{
			"id": "wrong_pattern",
			"label": "Wrong Pattern",
			"description": "Uses a non-idiomatic pattern.",
			"signals": ["Shared memory where channels expected"],
			"guidance": "Follow established project conventions."
		},
		{
			"id": "sop_violation",
			"label": "SOP Violation",
			"description": "Violates a standard operating procedure.",
			"signals": ["SOP rule referenced in feedback"],
			"guidance": "Re-read each SOP rule in the task context."
		},
		{
			"id": "incomplete_implementation",
			"label": "Incomplete Implementation",
			"description": "Missing required components.",
			"signals": ["TODO left in code"],
			"guidance": "All criteria must be fully addressed."
		},
		{
			"id": "edge_case_missed",
			"label": "Edge Case Missed",
			"description": "Boundary conditions not handled.",
			"signals": ["No nil guard"],
			"guidance": "Handle nil, empty, and boundary values."
		},
		{
			"id": "api_contract_mismatch",
			"label": "API Contract Mismatch",
			"description": "Diverges from the API contract.",
			"signals": ["Wrong function signature"],
			"guidance": "Cross-reference against the API contract."
		},
		{
			"id": "scope_violation",
			"label": "Scope Violation",
			"description": "Changes outside the defined scope.",
			"signals": ["Files modified outside task scope"],
			"guidance": "Only modify files in task scope."
		}
	]
}`

func TestLoadErrorCategories_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "error_categories.json")

	if err := os.WriteFile(path, []byte(categoriesJSON), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	registry, err := LoadErrorCategories(path)
	if err != nil {
		t.Fatalf("LoadErrorCategories() error = %v", err)
	}

	all := registry.All()
	if len(all) != 7 {
		t.Errorf("All() len = %d, want 7", len(all))
	}

	expectedIDs := []string{
		"missing_tests", "wrong_pattern", "sop_violation",
		"incomplete_implementation", "edge_case_missed",
		"api_contract_mismatch", "scope_violation",
	}
	for _, id := range expectedIDs {
		cat, ok := registry.Get(id)
		if !ok {
			t.Errorf("Get(%q) not found", id)
			continue
		}
		if cat.ID != id {
			t.Errorf("cat.ID = %q, want %q", cat.ID, id)
		}
		if cat.Label == "" {
			t.Errorf("cat[%q].Label is empty", id)
		}
		if cat.Description == "" {
			t.Errorf("cat[%q].Description is empty", id)
		}
		if len(cat.Signals) == 0 {
			t.Errorf("cat[%q].Signals is empty", id)
		}
		if cat.Guidance == "" {
			t.Errorf("cat[%q].Guidance is empty", id)
		}
	}
}

func TestLoadErrorCategories_RealConfigFile(t *testing.T) {
	// Verify the checked-in config file parses correctly and has all 7 categories.
	registry, err := LoadErrorCategories("../configs/error_categories.json")
	if err != nil {
		t.Fatalf("LoadErrorCategories(configs/error_categories.json) error = %v", err)
	}

	wantIDs := []string{
		"missing_tests", "wrong_pattern", "sop_violation",
		"incomplete_implementation", "edge_case_missed",
		"api_contract_mismatch", "scope_violation",
	}
	for _, id := range wantIDs {
		if !registry.IsValid(id) {
			t.Errorf("real config missing category %q", id)
		}
	}
	if len(registry.All()) != 7 {
		t.Errorf("real config has %d categories, want 7", len(registry.All()))
	}
}

func TestLoadErrorCategories_InvalidJSON(t *testing.T) {
	_, err := LoadErrorCategoriesFromBytes([]byte(`{not valid json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoadErrorCategories_MalformedJSONTypes(t *testing.T) {
	// categories field is a string instead of an array.
	_, err := LoadErrorCategoriesFromBytes([]byte(`{"categories": "not-an-array"}`))
	if err == nil {
		t.Fatal("expected error for wrong type, got nil")
	}
}

func TestLoadErrorCategories_DuplicateIDs(t *testing.T) {
	data, err := json.Marshal(map[string]any{
		"categories": []map[string]any{
			{"id": "missing_tests", "label": "A", "description": "A", "signals": []string{}, "guidance": "A"},
			{"id": "missing_tests", "label": "B", "description": "B", "signals": []string{}, "guidance": "B"},
		},
	})
	if err != nil {
		t.Fatalf("marshal test data: %v", err)
	}

	_, err = LoadErrorCategoriesFromBytes(data)
	if err == nil {
		t.Fatal("expected error for duplicate IDs, got nil")
	}
}

func TestLoadErrorCategories_MissingID(t *testing.T) {
	data, err := json.Marshal(map[string]any{
		"categories": []map[string]any{
			{"id": "", "label": "No ID", "description": "desc", "signals": []string{}, "guidance": "g"},
		},
	})
	if err != nil {
		t.Fatalf("marshal test data: %v", err)
	}

	_, err = LoadErrorCategoriesFromBytes(data)
	if err == nil {
		t.Fatal("expected error for missing ID, got nil")
	}
}

func TestLoadErrorCategories_EmptyCategories(t *testing.T) {
	registry, err := LoadErrorCategoriesFromBytes([]byte(`{"categories": []}`))
	if err != nil {
		t.Fatalf("unexpected error for empty categories: %v", err)
	}
	if len(registry.All()) != 0 {
		t.Errorf("All() len = %d, want 0", len(registry.All()))
	}
}

func TestErrorCategoryRegistry_Get(t *testing.T) {
	registry, err := LoadErrorCategoriesFromBytes([]byte(categoriesJSON))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	t.Run("found", func(t *testing.T) {
		cat, ok := registry.Get("sop_violation")
		if !ok {
			t.Fatal("Get(sop_violation) not found")
		}
		if cat.ID != "sop_violation" {
			t.Errorf("cat.ID = %q, want sop_violation", cat.ID)
		}
		if cat.Label != "SOP Violation" {
			t.Errorf("cat.Label = %q, want SOP Violation", cat.Label)
		}
	})

	t.Run("not found", func(t *testing.T) {
		cat, ok := registry.Get("nonexistent_category")
		if ok {
			t.Errorf("Get(nonexistent_category) returned ok=true with cat=%v", cat)
		}
		if cat != nil {
			t.Errorf("Get(nonexistent_category) returned non-nil cat: %v", cat)
		}
	})
}

func TestErrorCategoryRegistry_IsValid(t *testing.T) {
	registry, err := LoadErrorCategoriesFromBytes([]byte(categoriesJSON))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		id    string
		valid bool
	}{
		{"missing_tests", true},
		{"wrong_pattern", true},
		{"sop_violation", true},
		{"incomplete_implementation", true},
		{"edge_case_missed", true},
		{"api_contract_mismatch", true},
		{"scope_violation", true},
		{"", false},
		{"nonexistent", false},
		{"MISSING_TESTS", false}, // case-sensitive
		{"missing tests", false}, // spaces not valid
	}

	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			got := registry.IsValid(tc.id)
			if got != tc.valid {
				t.Errorf("IsValid(%q) = %v, want %v", tc.id, got, tc.valid)
			}
		})
	}
}
