//go:build integration

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		wantErr error
	}{
		{"valid_simple", "test", nil},
		{"valid_with_hyphens", "test-feature", nil},
		{"valid_with_numbers", "test123", nil},
		{"valid_mixed", "auth-refresh-2", nil},
		{"empty", "", ErrSlugRequired},
		{"path_traversal_dots", "../etc/passwd", ErrInvalidSlug},
		{"path_traversal_slash", "foo/bar", ErrInvalidSlug},
		{"path_traversal_backslash", "foo\\bar", ErrInvalidSlug},
		{"uppercase", "TestFeature", ErrInvalidSlug},
		{"starts_with_hyphen", "-test", ErrInvalidSlug},
		{"ends_with_hyphen", "test-", ErrInvalidSlug},
		{"special_chars", "test@feature", ErrInvalidSlug},
		{"spaces", "test feature", ErrInvalidSlug},
		{"single_char", "a", nil},
		{"two_chars", "ab", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSlug(tt.slug)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateSlug(%q) = %v, want nil", tt.slug, err)
				}
			} else {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidateSlug(%q) = %v, want %v", tt.slug, err, tt.wantErr)
				}
			}
		})
	}
}

// TestCreatePlan_Validation tests slug and title validation without NATS.
func TestCreatePlan_Validation(t *testing.T) {
	ctx := context.Background()

	_, err := CreatePlan(ctx, nil, "", "Title")
	if !errors.Is(err, ErrSlugRequired) {
		t.Errorf("expected ErrSlugRequired, got %v", err)
	}

	_, err = CreatePlan(ctx, nil, "slug", "")
	if !errors.Is(err, ErrTitleRequired) {
		t.Errorf("expected ErrTitleRequired, got %v", err)
	}

	_, err = CreatePlan(ctx, nil, "../path/traversal", "Title")
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug for path traversal, got %v", err)
	}
}

func TestPlan_EffectiveStatus(t *testing.T) {
	tests := []struct {
		name     string
		plan     Plan
		expected Status
	}{
		{
			name:     "explicit status takes priority",
			plan:     Plan{Status: StatusRequirementsGenerated, Approved: true},
			expected: StatusRequirementsGenerated,
		},
		{
			name:     "infers approved from boolean",
			plan:     Plan{Approved: true},
			expected: StatusApproved,
		},
		{
			name:     "infers reviewed from needs_changes verdict",
			plan:     Plan{ReviewVerdict: "needs_changes"},
			expected: StatusReviewed,
		},
		{
			name:     "infers reviewed from approved verdict",
			plan:     Plan{ReviewVerdict: "approved"},
			expected: StatusReviewed,
		},
		{
			name:     "infers drafted from goal+context",
			plan:     Plan{Goal: "do something", Context: "why it matters"},
			expected: StatusDrafted,
		},
		{
			name:     "defaults to created",
			plan:     Plan{},
			expected: StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.plan.EffectiveStatus()
			if result != tt.expected {
				t.Errorf("EffectiveStatus() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestPlan_JSON(t *testing.T) {
	now := time.Now()
	plan := Plan{
		ID:       PlanEntityID("test"),
		Slug:     "test",
		Title:    "Test Plan",
		Approved: true,
		Goal:     "Implement feature X",
		Context:  "Current system lacks feature X",
		Scope: Scope{
			Include:    []string{"api/", "lib/"},
			Exclude:    []string{"vendor/"},
			DoNotTouch: []string{"config.yaml"},
		},
		CreatedAt:  now,
		ApprovedAt: &now,
	}

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Plan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != plan.ID {
		t.Errorf("ID mismatch")
	}
	if !decoded.Approved {
		t.Errorf("Approved should be true")
	}
	if decoded.Goal != plan.Goal {
		t.Errorf("Goal mismatch")
	}
	if decoded.Context != plan.Context {
		t.Errorf("Context mismatch")
	}
	if len(decoded.Scope.Include) != 2 {
		t.Errorf("Scope.Include length = %d, want 2", len(decoded.Scope.Include))
	}
	if decoded.ApprovedAt == nil {
		t.Error("ApprovedAt should not be nil")
	}
}

func TestPlan_ExecutionTraceIDs_JSON(t *testing.T) {
	now := time.Now()
	plan := Plan{
		ID:                PlanEntityID("test"),
		Slug:              "test",
		Title:             "Test Plan",
		Goal:              "Implement feature X",
		CreatedAt:         now,
		ExecutionTraceIDs: []string{"trace-abc123", "trace-def456"},
	}

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Verify the JSON contains execution_trace_ids
	jsonStr := string(data)
	if !contains(jsonStr, "execution_trace_ids") {
		t.Error("JSON should contain execution_trace_ids field")
	}
	if !contains(jsonStr, "trace-abc123") {
		t.Error("JSON should contain trace-abc123")
	}

	var decoded Plan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(decoded.ExecutionTraceIDs) != 2 {
		t.Errorf("ExecutionTraceIDs length = %d, want 2", len(decoded.ExecutionTraceIDs))
	}
	if decoded.ExecutionTraceIDs[0] != "trace-abc123" {
		t.Errorf("ExecutionTraceIDs[0] = %q, want %q", decoded.ExecutionTraceIDs[0], "trace-abc123")
	}
}

func TestPlan_ExecutionTraceIDs_OmitEmpty(t *testing.T) {
	plan := Plan{
		ID:    PlanEntityID("test"),
		Slug:  "test",
		Title: "Test Plan",
		Goal:  "Implement feature X",
	}

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Verify the JSON does NOT contain execution_trace_ids when empty (omitempty)
	jsonStr := string(data)
	if contains(jsonStr, "execution_trace_ids") {
		t.Error("JSON should NOT contain execution_trace_ids field when empty")
	}
}

// contains is a simple helper for checking if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// CreatePlan should fail with a cancelled context.
	_, err := CreatePlan(ctx, nil, "test", "Test")
	if err == nil {
		t.Error("CreatePlan should fail with cancelled context")
	}
}
