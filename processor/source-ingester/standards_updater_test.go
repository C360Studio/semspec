package sourceingester

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestStandardsUpdater_UpdateFromSOP_CreatesStandardsFile(t *testing.T) {
	dir := t.TempDir()
	updater := NewStandardsUpdater(dir)

	meta := &SOPMetadata{
		Filename:  "api-testing-sop.md",
		Category:  "sop",
		Severity:  "warning",
		AppliesTo: []string{"api/**"},
		Requirements: []string{
			"All API endpoints must have corresponding tests",
			"API responses must use JSON format",
		},
	}

	if err := updater.UpdateFromSOP(meta); err != nil {
		t.Fatalf("UpdateFromSOP failed: %v", err)
	}

	// Verify file was created
	standardsPath := filepath.Join(dir, workflow.StandardsFile)
	data, err := os.ReadFile(standardsPath)
	if err != nil {
		t.Fatalf("Failed to read standards.json: %v", err)
	}

	var standards workflow.Standards
	if err := json.Unmarshal(data, &standards); err != nil {
		t.Fatalf("Failed to parse standards.json: %v", err)
	}

	if len(standards.Rules) != 2 {
		t.Fatalf("Expected 2 rules, got %d", len(standards.Rules))
	}

	// Verify rule content
	if standards.Rules[0].Text != "All API endpoints must have corresponding tests" {
		t.Errorf("Unexpected rule text: %s", standards.Rules[0].Text)
	}
	if standards.Rules[0].Severity != workflow.RuleSeverityWarning {
		t.Errorf("Expected warning severity, got %s", standards.Rules[0].Severity)
	}
	if standards.Rules[0].Category != "sop" {
		t.Errorf("Expected sop category, got %s", standards.Rules[0].Category)
	}
	if standards.Rules[0].Origin != "sop:api-testing-sop.md" {
		t.Errorf("Expected sop:api-testing-sop.md origin, got %s", standards.Rules[0].Origin)
	}
}

func TestStandardsUpdater_UpdateFromSOP_MergesWithExisting(t *testing.T) {
	dir := t.TempDir()
	updater := NewStandardsUpdater(dir)

	// Write initial standards with a manual rule
	existing := workflow.Standards{
		Version: "1.0.0",
		Rules: []workflow.Rule{
			{
				ID:       "manual-1",
				Text:     "Use conventional commits",
				Severity: workflow.RuleSeverityError,
				Category: "process",
				Origin:   workflow.RuleOriginManual,
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	standardsPath := filepath.Join(dir, workflow.StandardsFile)
	if err := os.WriteFile(standardsPath, data, 0644); err != nil {
		t.Fatalf("Failed to write initial standards: %v", err)
	}

	// Now ingest an SOP
	meta := &SOPMetadata{
		Filename:     "testing-sop.md",
		Category:     "sop",
		Severity:     "error",
		Requirements: []string{"All functions must have tests"},
	}

	if err := updater.UpdateFromSOP(meta); err != nil {
		t.Fatalf("UpdateFromSOP failed: %v", err)
	}

	// Read updated standards
	data, err := os.ReadFile(standardsPath)
	if err != nil {
		t.Fatalf("Failed to read standards: %v", err)
	}

	var standards workflow.Standards
	if err := json.Unmarshal(data, &standards); err != nil {
		t.Fatalf("Failed to parse standards: %v", err)
	}

	// Should have both the manual rule and the SOP rule
	if len(standards.Rules) != 2 {
		t.Fatalf("Expected 2 rules (1 manual + 1 SOP), got %d", len(standards.Rules))
	}

	// Manual rule should be first (preserved order)
	if standards.Rules[0].ID != "manual-1" {
		t.Errorf("Expected manual rule first, got %s", standards.Rules[0].ID)
	}

	// SOP rule should be second
	if standards.Rules[1].Origin != "sop:testing-sop.md" {
		t.Errorf("Expected SOP origin, got %s", standards.Rules[1].Origin)
	}
	if standards.Rules[1].Severity != workflow.RuleSeverityError {
		t.Errorf("Expected error severity, got %s", standards.Rules[1].Severity)
	}
}

func TestStandardsUpdater_UpdateFromSOP_IdempotentReingestion(t *testing.T) {
	dir := t.TempDir()
	updater := NewStandardsUpdater(dir)

	meta := &SOPMetadata{
		Filename:     "api-sop.md",
		Category:     "sop",
		Severity:     "warning",
		Requirements: []string{"Use JSON responses", "Write tests"},
	}

	// Ingest twice
	if err := updater.UpdateFromSOP(meta); err != nil {
		t.Fatalf("First ingestion failed: %v", err)
	}
	if err := updater.UpdateFromSOP(meta); err != nil {
		t.Fatalf("Second ingestion failed: %v", err)
	}

	// Read and verify
	data, _ := os.ReadFile(filepath.Join(dir, workflow.StandardsFile))
	var standards workflow.Standards
	json.Unmarshal(data, &standards)

	// Should still be 2 rules, not 4
	if len(standards.Rules) != 2 {
		t.Fatalf("Expected 2 rules (idempotent), got %d", len(standards.Rules))
	}
}

func TestStandardsUpdater_UpdateFromSOP_SkipsNonSOP(t *testing.T) {
	dir := t.TempDir()
	updater := NewStandardsUpdater(dir)

	meta := &SOPMetadata{
		Filename:     "readme.md",
		Category:     "documentation",
		Requirements: []string{"Some requirement"},
	}

	if err := updater.UpdateFromSOP(meta); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Standards file should NOT have been created
	standardsPath := filepath.Join(dir, workflow.StandardsFile)
	if _, err := os.Stat(standardsPath); !os.IsNotExist(err) {
		t.Error("Standards file should not have been created for non-SOP document")
	}
}

func TestStandardsUpdater_UpdateFromSOP_SkipsEmptyRequirements(t *testing.T) {
	dir := t.TempDir()
	updater := NewStandardsUpdater(dir)

	meta := &SOPMetadata{
		Filename: "empty-sop.md",
		Category: "sop",
		Severity: "warning",
	}

	if err := updater.UpdateFromSOP(meta); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Standards file should NOT have been created
	standardsPath := filepath.Join(dir, workflow.StandardsFile)
	if _, err := os.Stat(standardsPath); !os.IsNotExist(err) {
		t.Error("Standards file should not have been created for SOP without requirements")
	}
}

func TestStandardsUpdater_UpdateFromSOP_AppliesTo(t *testing.T) {
	dir := t.TempDir()
	updater := NewStandardsUpdater(dir)

	meta := &SOPMetadata{
		Filename:     "go-sop.md",
		Category:     "sop",
		Severity:     "error",
		AppliesTo:    []string{"**/*.go", "api/**"},
		Requirements: []string{"Use error wrapping"},
	}

	if err := updater.UpdateFromSOP(meta); err != nil {
		t.Fatalf("UpdateFromSOP failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, workflow.StandardsFile))
	var standards workflow.Standards
	json.Unmarshal(data, &standards)

	if len(standards.Rules) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(standards.Rules))
	}

	if len(standards.Rules[0].AppliesTo) != 2 {
		t.Fatalf("Expected 2 applies_to patterns, got %d", len(standards.Rules[0].AppliesTo))
	}
}

func TestSopToRules_StableIDs(t *testing.T) {
	meta := &SOPMetadata{
		Filename:     "api-testing-sop.md",
		Category:     "sop",
		Severity:     "warning",
		Requirements: []string{"Rule one", "Rule two"},
	}

	rules := sopToRules(meta)

	if len(rules) != 2 {
		t.Fatalf("Expected 2 rules, got %d", len(rules))
	}

	// IDs should be deterministic and based on filename + index
	if rules[0].ID != "sop-api-testing-sop-1" {
		t.Errorf("Expected sop-api-testing-sop-1, got %s", rules[0].ID)
	}
	if rules[1].ID != "sop-api-testing-sop-2" {
		t.Errorf("Expected sop-api-testing-sop-2, got %s", rules[1].ID)
	}
}

func TestMergeRules_PreservesOrder(t *testing.T) {
	existing := []workflow.Rule{
		{ID: "a", Text: "Rule A"},
		{ID: "b", Text: "Rule B"},
	}
	incoming := []workflow.Rule{
		{ID: "b", Text: "Updated B"},
		{ID: "c", Text: "Rule C"},
	}

	merged := mergeRules(existing, incoming)

	if len(merged) != 3 {
		t.Fatalf("Expected 3 rules, got %d", len(merged))
	}
	if merged[0].ID != "a" {
		t.Errorf("Expected 'a' first, got %s", merged[0].ID)
	}
	if merged[1].ID != "b" || merged[1].Text != "Updated B" {
		t.Errorf("Expected updated 'b', got %s: %s", merged[1].ID, merged[1].Text)
	}
	if merged[2].ID != "c" {
		t.Errorf("Expected 'c' third, got %s", merged[2].ID)
	}
}

func TestMapSeverity(t *testing.T) {
	tests := []struct {
		input    string
		expected workflow.RuleSeverity
	}{
		{"error", workflow.RuleSeverityError},
		{"warning", workflow.RuleSeverityWarning},
		{"info", workflow.RuleSeverityInfo},
		{"Error", workflow.RuleSeverityError},
		{"", workflow.RuleSeverityWarning},
		{"unknown", workflow.RuleSeverityWarning},
	}

	for _, tt := range tests {
		got := mapSeverity(tt.input)
		if got != tt.expected {
			t.Errorf("mapSeverity(%q) = %s, want %s", tt.input, got, tt.expected)
		}
	}
}

func TestSanitizeForID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"api-testing-sop.md", "api-testing-sop"},
		{"Go Conventions.md", "go-conventions"},
		{"my_file (1).md", "my-file--1-"},
	}

	for _, tt := range tests {
		got := sanitizeForID(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizeForID(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
