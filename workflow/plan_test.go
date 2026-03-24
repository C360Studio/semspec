//go:build integration

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskStatus_IsValid(t *testing.T) {
	tests := []struct {
		status TaskStatus
		want   bool
	}{
		{TaskStatusPending, true},
		{TaskStatusPendingApproval, true},
		{TaskStatusApproved, true},
		{TaskStatusRejected, true},
		{TaskStatusInProgress, true},
		{TaskStatusCompleted, true},
		{TaskStatusFailed, true},
		{TaskStatus("unknown"), false},
		{TaskStatus(""), false},
	}

	for _, tt := range tests {
		name := string(tt.status)
		if name == "" {
			name = "empty_status"
		}
		t.Run(name, func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("TaskStatus(%q).IsValid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestTaskStatus_CanTransitionTo(t *testing.T) {
	tests := []struct {
		from TaskStatus
		to   TaskStatus
		want bool
	}{
		// From pending
		{TaskStatusPending, TaskStatusPendingApproval, true},
		{TaskStatusPending, TaskStatusInProgress, true}, // legacy backward compat
		{TaskStatusPending, TaskStatusFailed, true},
		{TaskStatusPending, TaskStatusCompleted, false},
		{TaskStatusPending, TaskStatusPending, false},
		{TaskStatusPending, TaskStatusApproved, false},
		{TaskStatusPending, TaskStatusRejected, false},

		// From pending_approval
		{TaskStatusPendingApproval, TaskStatusApproved, true},
		{TaskStatusPendingApproval, TaskStatusRejected, true},
		{TaskStatusPendingApproval, TaskStatusPending, false},
		{TaskStatusPendingApproval, TaskStatusInProgress, false},
		{TaskStatusPendingApproval, TaskStatusCompleted, false},
		{TaskStatusPendingApproval, TaskStatusFailed, false},

		// From approved
		{TaskStatusApproved, TaskStatusInProgress, true},
		{TaskStatusApproved, TaskStatusPending, false},
		{TaskStatusApproved, TaskStatusCompleted, false},
		{TaskStatusApproved, TaskStatusFailed, false},

		// From rejected
		{TaskStatusRejected, TaskStatusPending, true}, // can re-edit
		{TaskStatusRejected, TaskStatusApproved, false},
		{TaskStatusRejected, TaskStatusInProgress, false},
		{TaskStatusRejected, TaskStatusCompleted, false},

		// From in_progress
		{TaskStatusInProgress, TaskStatusCompleted, true},
		{TaskStatusInProgress, TaskStatusFailed, true},
		{TaskStatusInProgress, TaskStatusPending, false},
		{TaskStatusInProgress, TaskStatusInProgress, false},
		{TaskStatusInProgress, TaskStatusApproved, false},

		// From completed (terminal)
		{TaskStatusCompleted, TaskStatusPending, false},
		{TaskStatusCompleted, TaskStatusInProgress, false},
		{TaskStatusCompleted, TaskStatusFailed, false},

		// From failed (terminal)
		{TaskStatusFailed, TaskStatusPending, false},
		{TaskStatusFailed, TaskStatusInProgress, false},
		{TaskStatusFailed, TaskStatusCompleted, false},
	}

	for _, tt := range tests {
		name := string(tt.from) + "_to_" + string(tt.to)
		t.Run(name, func(t *testing.T) {
			if got := tt.from.CanTransitionTo(tt.to); got != tt.want {
				t.Errorf("TaskStatus(%q).CanTransitionTo(%q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestTaskStatus_String(t *testing.T) {
	tests := []struct {
		status TaskStatus
		want   string
	}{
		{TaskStatusPending, "pending"},
		{TaskStatusPendingApproval, "pending_approval"},
		{TaskStatusApproved, "approved"},
		{TaskStatusRejected, "rejected"},
		{TaskStatusInProgress, "in_progress"},
		{TaskStatusCompleted, "completed"},
		{TaskStatusFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("TaskStatus.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

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

func TestManager_CreatePlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	plan, err := CreatePlan(ctx, nil, "test-feature", "Add test feature")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Verify plan structure
	expectedID := PlanEntityID("test-feature")
	if plan.ID != expectedID {
		t.Errorf("plan.ID = %q, want %q", plan.ID, expectedID)
	}
	if plan.Slug != "test-feature" {
		t.Errorf("plan.Slug = %q, want %q", plan.Slug, "test-feature")
	}
	if plan.Title != "Add test feature" {
		t.Errorf("plan.Title = %q, want %q", plan.Title, "Add test feature")
	}
	if plan.Approved {
		t.Error("new plan should have Approved=false")
	}
	if plan.ApprovedAt != nil {
		t.Error("new plan should have ApprovedAt=nil")
	}
	if plan.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}

	// Verify file was created in project-based path
	planPath := filepath.Join(tmpDir, ".semspec", "projects", "default", "plans", "test-feature", "plan.json")
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		t.Error("plan.json was not created")
	}
}

func TestManager_CreatePlan_Validation(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
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

func TestManager_CreatePlan_AlreadyExists(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	_, err := CreatePlan(ctx, nil, "existing", "First plan")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	_, err = CreatePlan(ctx, nil, "existing", "Second plan")
	if !errors.Is(err, ErrPlanExists) {
		t.Errorf("expected ErrPlanExists, got %v", err)
	}
}

func TestManager_LoadPlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	// Create a plan
	created, err := CreatePlan(ctx, nil, "test-load", "Test load plan")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Load it back
	loaded, err := LoadPlan(ctx, nil, "test-load")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}

	if loaded.ID != created.ID {
		t.Errorf("ID mismatch: got %q, want %q", loaded.ID, created.ID)
	}
	if loaded.Title != created.Title {
		t.Errorf("Title mismatch: got %q, want %q", loaded.Title, created.Title)
	}
}

func TestManager_LoadPlan_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	_, err := LoadPlan(ctx, nil, "nonexistent")
	if !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestManager_LoadPlan_PathTraversal(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	_, err := LoadPlan(ctx, nil, "../../../etc/passwd")
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug for path traversal, got %v", err)
	}
}

func TestManager_LoadPlan_MalformedJSON(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	// Create directory and write malformed JSON at project-based path
	planPath := filepath.Join(tmpDir, ".semspec", "projects", "default", "plans", "malformed")
	os.MkdirAll(planPath, 0755)
	os.WriteFile(filepath.Join(planPath, "plan.json"), []byte("{invalid json"), 0644)

	_, err := LoadPlan(ctx, nil, "malformed")
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
	// Should be a parse error, not ErrPlanNotFound
	if errors.Is(err, ErrPlanNotFound) {
		t.Error("expected parse error, not ErrPlanNotFound")
	}
}

func TestManager_ApprovePlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	plan, err := CreatePlan(ctx, nil, "test-approve", "Approve test")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	if plan.Approved {
		t.Error("plan should start unapproved")
	}

	// Approve it
	err = ApprovePlan(ctx, nil, plan)
	if err != nil {
		t.Fatalf("ApprovePlan failed: %v", err)
	}

	if !plan.Approved {
		t.Error("plan should be approved after approval")
	}
	if plan.ApprovedAt == nil {
		t.Error("ApprovedAt should be set after approval")
	}

	// Verify persistence
	loaded, err := LoadPlan(ctx, nil, "test-approve")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if !loaded.Approved {
		t.Error("loaded plan should be approved")
	}
	if loaded.ApprovedAt == nil {
		t.Error("loaded plan should have ApprovedAt set")
	}
}

func TestManager_ApprovePlan_AlreadyApproved(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	plan, _ := CreatePlan(ctx, nil, "already-approved", "Already approved")
	ApprovePlan(ctx, nil, plan)

	err := ApprovePlan(ctx, nil, plan)
	if !errors.Is(err, ErrAlreadyApproved) {
		t.Errorf("expected ErrAlreadyApproved, got %v", err)
	}
}

func TestManager_ApprovePlan_SetsStatus(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	plan, err := CreatePlan(ctx, nil, "test-status", "Status test")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	if err := ApprovePlan(ctx, nil, plan); err != nil {
		t.Fatalf("ApprovePlan failed: %v", err)
	}

	if plan.Status != StatusApproved {
		t.Errorf("Status = %q, want %q", plan.Status, StatusApproved)
	}

	loaded, err := LoadPlan(ctx, nil, "test-status")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if loaded.Status != StatusApproved {
		t.Errorf("loaded Status = %q, want %q", loaded.Status, StatusApproved)
	}
}

func TestManager_SetPlanStatus(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	plan, err := CreatePlan(ctx, nil, "test-set-status", "Set status test")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Approve plan to get to StatusApproved
	if err := ApprovePlan(ctx, nil, plan); err != nil {
		t.Fatalf("ApprovePlan failed: %v", err)
	}

	// Valid transition: approved → requirements_generated
	if err := SetPlanStatus(ctx, nil, plan, StatusRequirementsGenerated); err != nil {
		t.Fatalf("SetPlanStatus to requirements_generated failed: %v", err)
	}
	if plan.Status != StatusRequirementsGenerated {
		t.Errorf("Status = %q, want %q", plan.Status, StatusRequirementsGenerated)
	}

	// Invalid transition: requirements_generated → implementing (should fail)
	err = SetPlanStatus(ctx, nil, plan, StatusImplementing)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
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

func TestManager_PlanExists(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	if PlanExists(ctx, nil, "nonexistent") {
		t.Error("PlanExists should return false for nonexistent plan")
	}

	if PlanExists(ctx, nil, "../path/traversal") {
		t.Error("PlanExists should return false for invalid slug")
	}

	CreatePlan(ctx, nil, "exists", "Plan exists")

	if !PlanExists(ctx, nil, "exists") {
		t.Error("PlanExists should return true for existing plan")
	}
}

func TestManager_ListPlans(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	// Create some plans
	CreatePlan(ctx, nil, "plan1", "Plan 1")
	CreatePlan(ctx, nil, "plan2", "Plan 2")

	result, err := ListPlans(ctx, nil)
	if err != nil {
		t.Fatalf("ListPlans failed: %v", err)
	}

	if len(result.Plans) != 2 {
		t.Errorf("expected 2 plans, got %d", len(result.Plans))
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got %v", result.Errors)
	}
}

func TestManager_ListPlans_PartialErrors(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	// Create a valid plan
	CreatePlan(ctx, nil, "valid", "Valid plan")

	// Create a directory with malformed JSON at project-based path
	malformedPath := filepath.Join(tmpDir, ".semspec", "projects", "default", "plans", "malformed")
	os.MkdirAll(malformedPath, 0755)
	os.WriteFile(filepath.Join(malformedPath, "plan.json"), []byte("{invalid}"), 0644)

	result, err := ListPlans(ctx, nil)
	if err != nil {
		t.Fatalf("ListPlans failed: %v", err)
	}

	if len(result.Plans) != 1 {
		t.Errorf("expected 1 valid plan, got %d", len(result.Plans))
	}
	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}
}

func TestManager_ListPlans_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := ListPlans(ctx, nil)
	if err == nil {
		t.Error("expected error for cancelled context")
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

func TestTask_JSON(t *testing.T) {
	now := time.Now()
	task := Task{
		ID:          TaskEntityID("test", 1),
		PlanID:      PlanEntityID("test"),
		Sequence:    1,
		Description: "Do something",
		Type:        TaskTypeImplement,
		AcceptanceCriteria: []AcceptanceCriterion{
			{Given: "tests exist", When: "running tests", Then: "tests pass"},
			{Given: "code changes", When: "reviewing docs", Then: "docs are updated"},
		},
		Files:       []string{"api/handler.go"},
		Status:      TaskStatusCompleted,
		CreatedAt:   now,
		CompletedAt: &now,
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Task
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != task.ID {
		t.Errorf("ID mismatch")
	}
	if decoded.Status != TaskStatusCompleted {
		t.Errorf("Status = %q, want %q", decoded.Status, TaskStatusCompleted)
	}
	if len(decoded.AcceptanceCriteria) != 2 {
		t.Errorf("AcceptanceCriteria length = %d, want 2", len(decoded.AcceptanceCriteria))
	}
	if decoded.AcceptanceCriteria[0].Given != "tests exist" {
		t.Errorf("AcceptanceCriteria[0].Given = %q, want %q", decoded.AcceptanceCriteria[0].Given, "tests exist")
	}
	if decoded.Type != TaskTypeImplement {
		t.Errorf("Type = %q, want %q", decoded.Type, TaskTypeImplement)
	}
	if decoded.CompletedAt == nil {
		t.Error("CompletedAt should not be nil")
	}
}

func TestContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// All context-aware operations should fail
	_, err := CreatePlan(ctx, nil, "test", "Test")
	if err == nil {
		t.Error("CreatePlan should fail with cancelled context")
	}

	_, err = LoadPlan(ctx, nil, "test")
	if err == nil {
		t.Error("LoadPlan should fail with cancelled context")
	}

}

// ============================================================================
// Additional edge case tests for Plan/Task mutations and validation
// ============================================================================

// TestManager_SavePlan_Direct tests saving a modified plan directly.
func TestManager_SavePlan_Direct(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	// Create plan
	plan, err := CreatePlan(ctx, nil, "test-direct-save", "Test Direct Save")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Modify fields
	plan.Goal = "Updated goal"
	plan.Context = "Updated context"

	// Save directly
	err = SavePlan(ctx, nil, plan)
	if err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	// Reload and verify
	loaded, err := LoadPlan(ctx, nil, "test-direct-save")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if loaded.Goal != "Updated goal" {
		t.Errorf("Goal = %q, want %q", loaded.Goal, "Updated goal")
	}
	if loaded.Context != "Updated context" {
		t.Errorf("Context = %q, want %q", loaded.Context, "Updated context")
	}
}

// TestManager_SavePlan_ContextCancellation tests that SavePlan respects context.
func TestManager_SavePlan_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	plan := &Plan{
		ID:    PlanEntityID("test"),
		Slug:  "test",
		Title: "Test",
	}

	err := SavePlan(ctx, nil, plan)
	if err == nil {
		t.Error("SavePlan should fail with cancelled context")
	}
}

// TestPlan_FieldMutations tests modifying plan fields, save, reload, verify.
// Tests both new (Goal/Context/Scope) and legacy fields for backwards compatibility.
func TestPlan_FieldMutations(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	// Create plan
	plan, err := CreatePlan(ctx, nil, "mutations", "Mutations Test")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Test Goal/Context/Scope fields
	plan.Goal = "Implement OAuth 2.0 refresh token flow"
	plan.Context = "Complex multi-service architecture with auth issues"
	plan.Scope = Scope{
		Include:    []string{"api/auth/*", "internal/token/*"},
		Exclude:    []string{"vendor/*"},
		DoNotTouch: []string{"config.yaml"},
	}

	// Save
	if err := SavePlan(ctx, nil, plan); err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	// Reload
	loaded, err := LoadPlan(ctx, nil, "mutations")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}

	// Verify all fields
	if loaded.Goal != plan.Goal {
		t.Errorf("Goal = %q, want %q", loaded.Goal, plan.Goal)
	}
	if loaded.Context != plan.Context {
		t.Errorf("Context = %q, want %q", loaded.Context, plan.Context)
	}
	if len(loaded.Scope.Include) != len(plan.Scope.Include) {
		t.Errorf("Scope.Include length = %d, want %d", len(loaded.Scope.Include), len(plan.Scope.Include))
	}
	if len(loaded.Scope.Exclude) != len(plan.Scope.Exclude) {
		t.Errorf("Scope.Exclude length = %d, want %d", len(loaded.Scope.Exclude), len(plan.Scope.Exclude))
	}
	if len(loaded.Scope.DoNotTouch) != len(plan.Scope.DoNotTouch) {
		t.Errorf("Scope.DoNotTouch length = %d, want %d", len(loaded.Scope.DoNotTouch), len(plan.Scope.DoNotTouch))
	}
}

// TestScope_Mutations tests modifying Include/Exclude/DoNotTouch arrays.
func TestScope_Mutations(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	plan, err := CreatePlan(ctx, nil, "scope-mut", "Scope Mutation")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Verify initial empty state
	if len(plan.Scope.Include) != 0 {
		t.Errorf("initial Include = %v, want empty", plan.Scope.Include)
	}

	// Modify scope
	plan.Scope.Include = []string{"api/", "lib/auth/", "internal/middleware/"}
	plan.Scope.Exclude = []string{"vendor/", "third_party/"}
	plan.Scope.DoNotTouch = []string{"config.yaml", ".env", "secrets/"}

	// Save and reload
	if err := SavePlan(ctx, nil, plan); err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	loaded, err := LoadPlan(ctx, nil, "scope-mut")
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}

	// Verify scope
	if len(loaded.Scope.Include) != 3 {
		t.Errorf("Include length = %d, want 3", len(loaded.Scope.Include))
	}
	if len(loaded.Scope.Exclude) != 2 {
		t.Errorf("Exclude length = %d, want 2", len(loaded.Scope.Exclude))
	}
	if len(loaded.Scope.DoNotTouch) != 3 {
		t.Errorf("DoNotTouch length = %d, want 3", len(loaded.Scope.DoNotTouch))
	}

	// Verify specific values
	if loaded.Scope.Include[0] != "api/" {
		t.Errorf("Include[0] = %q, want %q", loaded.Scope.Include[0], "api/")
	}
	if loaded.Scope.DoNotTouch[2] != "secrets/" {
		t.Errorf("DoNotTouch[2] = %q, want %q", loaded.Scope.DoNotTouch[2], "secrets/")
	}
}

// TestManager_SavePlan_InvalidSlug tests that SavePlan validates slug.
func TestManager_SavePlan_InvalidSlug(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	plan := &Plan{
		ID:    PlanEntityID("test"),
		Slug:  "../invalid",
		Title: "Test",
	}

	err := SavePlan(ctx, nil, plan)
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug, got %v", err)
	}
}

// TestManager_UpdatePlan tests updating plan fields.
func TestManager_UpdatePlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	plan, err := CreatePlan(ctx, nil, "test-update", "Original Title")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	plan.Goal = "Original goal"
	plan.Context = "Original context"
	if err := SavePlan(ctx, nil, plan); err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	// Single-field updates
	updated := assertUpdatePlanTitle(t, ctx, plan.Slug, "Updated Title", "Original goal")
	assertUpdatePlanGoal(t, ctx, plan.Slug, "New goal", updated.Title)
	assertUpdatePlanContext(t, ctx, plan.Slug, "New context")

	// Multi-field update
	title2, goal2, context2 := "Title 2", "Goal 2", "Context 2"
	updated, err = UpdatePlan(ctx, nil, plan.Slug, UpdatePlanRequest{
		Title:   &title2,
		Goal:    &goal2,
		Context: &context2,
	})
	if err != nil {
		t.Fatalf("UpdatePlan (multi-field) failed: %v", err)
	}
	if updated.Title != title2 || updated.Goal != goal2 || updated.Context != context2 {
		t.Error("Multiple field update failed")
	}

	// Verify changes persist across a fresh load.
	assertPlanFieldsPersisted(t, ctx, plan.Slug, title2, goal2, context2)
}

// assertUpdatePlanTitle updates only the title and verifies the other fields are unchanged.
// Returns the updated plan.
func assertUpdatePlanTitle(t *testing.T, ctx context.Context, slug, newTitle, wantGoal string) *Plan {
	t.Helper()
	updated, err := UpdatePlan(ctx, nil, slug, UpdatePlanRequest{Title: &newTitle})
	if err != nil {
		t.Fatalf("UpdatePlan (title) failed: %v", err)
	}
	if updated.Title != newTitle {
		t.Errorf("Title = %q, want %q", updated.Title, newTitle)
	}
	if updated.Goal != wantGoal {
		t.Errorf("Goal changed unexpectedly: %q", updated.Goal)
	}
	return updated
}

// assertUpdatePlanGoal updates only the goal and verifies the title is unchanged.
func assertUpdatePlanGoal(t *testing.T, ctx context.Context, slug, newGoal, wantTitle string) {
	t.Helper()
	updated, err := UpdatePlan(ctx, nil, slug, UpdatePlanRequest{Goal: &newGoal})
	if err != nil {
		t.Fatalf("UpdatePlan (goal) failed: %v", err)
	}
	if updated.Goal != newGoal {
		t.Errorf("Goal = %q, want %q", updated.Goal, newGoal)
	}
	if updated.Title != wantTitle {
		t.Error("Title should remain unchanged")
	}
}

// assertUpdatePlanContext updates only the context field and verifies the result.
func assertUpdatePlanContext(t *testing.T, ctx context.Context, slug, newContext string) {
	t.Helper()
	updated, err := UpdatePlan(ctx, nil, slug, UpdatePlanRequest{Context: &newContext})
	if err != nil {
		t.Fatalf("UpdatePlan (context) failed: %v", err)
	}
	if updated.Context != newContext {
		t.Errorf("Context = %q, want %q", updated.Context, newContext)
	}
}

// assertPlanFieldsPersisted loads the plan from disk and verifies the given field values.
func assertPlanFieldsPersisted(t *testing.T, ctx context.Context, slug, wantTitle, wantGoal, wantContext string) {
	t.Helper()
	loaded, err := LoadPlan(ctx, nil, slug)
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if loaded.Title != wantTitle {
		t.Errorf("Persisted title = %q, want %q", loaded.Title, wantTitle)
	}
	if loaded.Goal != wantGoal {
		t.Errorf("Persisted goal = %q, want %q", loaded.Goal, wantGoal)
	}
	if loaded.Context != wantContext {
		t.Errorf("Persisted context = %q, want %q", loaded.Context, wantContext)
	}
}

// TestManager_UpdatePlan_NotFound tests updating non-existent plan.
func TestManager_UpdatePlan_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	title := "New Title"
	req := UpdatePlanRequest{Title: &title}
	_, err := UpdatePlan(ctx, nil, "nonexistent", req)
	if !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("expected ErrPlanNotFound, got %v", err)
	}
}

// TestManager_UpdatePlan_StateGuards tests state guards for updating plans.
func TestManager_UpdatePlan_StateGuards(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	tests := []struct {
		name    string
		status  Status
		wantErr error
	}{
		{"created-allowed", StatusCreated, nil},
		{"drafted-allowed", StatusDrafted, nil},
		{"reviewed-allowed", StatusReviewed, nil},
		{"approved-allowed", StatusApproved, nil},
		{"requirements-generated-allowed", StatusRequirementsGenerated, nil},
		{"scenarios-generated-allowed", StatusScenariosGenerated, nil},
		{"implementing-blocked", StatusImplementing, ErrPlanNotUpdatable},
		{"complete-blocked", StatusComplete, ErrPlanNotUpdatable},
		{"archived-blocked", StatusArchived, ErrPlanNotUpdatable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create plan with specific status
			slug := "plan-" + strings.ReplaceAll(tt.name, "_", "-")
			plan, err := CreatePlan(ctx, nil, slug, "Test Plan")
			if err != nil {
				t.Fatalf("CreatePlan failed: %v", err)
			}

			plan.Status = tt.status
			err = SavePlan(ctx, nil, plan)
			if err != nil {
				t.Fatalf("SavePlan failed: %v", err)
			}

			// Try to update
			newTitle := "Updated Title"
			req := UpdatePlanRequest{Title: &newTitle}
			_, err = UpdatePlan(ctx, nil, plan.Slug, req)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestManager_DeletePlan tests deleting a plan.
func TestManager_DeletePlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	// Create plan
	plan, err := CreatePlan(ctx, nil, "test-delete", "Test Delete")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Verify plan exists
	planPath := filepath.Join(tmpDir, ".semspec", "projects", "default", "plans", plan.Slug)
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		t.Fatal("plan directory should exist")
	}

	// Delete plan
	err = DeletePlan(ctx, nil, plan.Slug)
	if err != nil {
		t.Fatalf("DeletePlan failed: %v", err)
	}

	// Verify plan directory removed
	if _, err := os.Stat(planPath); !os.IsNotExist(err) {
		t.Error("plan directory should be removed")
	}

	// Verify LoadPlan returns not found
	_, err = LoadPlan(ctx, nil, plan.Slug)
	if !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("expected ErrPlanNotFound after delete, got: %v", err)
	}
}

// TestManager_DeletePlan_NotFound tests deleting non-existent plan.
func TestManager_DeletePlan_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	err := DeletePlan(ctx, nil, "nonexistent")
	if !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("expected ErrPlanNotFound, got %v", err)
	}
}

// TestManager_DeletePlan_StateGuards tests state guards for deleting plans.
func TestManager_DeletePlan_StateGuards(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	tests := []struct {
		name    string
		status  Status
		wantErr error
	}{
		{"created-allowed", StatusCreated, nil},
		{"drafted-allowed", StatusDrafted, nil},
		{"reviewed-allowed", StatusReviewed, nil},
		{"approved-allowed", StatusApproved, nil},
		{"requirements-generated-allowed", StatusRequirementsGenerated, nil},
		{"scenarios-generated-allowed", StatusScenariosGenerated, nil},
		{"implementing-blocked", StatusImplementing, ErrPlanNotDeletable},
		{"complete-blocked", StatusComplete, ErrPlanNotDeletable},
		{"archived-blocked", StatusArchived, ErrPlanNotDeletable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create plan with specific status
			slug := "delplan-" + strings.ReplaceAll(tt.name, "_", "-")
			plan, err := CreatePlan(ctx, nil, slug, "Test Plan")
			if err != nil {
				t.Fatalf("CreatePlan failed: %v", err)
			}

			plan.Status = tt.status
			err = SavePlan(ctx, nil, plan)
			if err != nil {
				t.Fatalf("SavePlan failed: %v", err)
			}

			// Try to delete
			err = DeletePlan(ctx, nil, plan.Slug)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				// Verify plan still exists when delete should fail
				if _, statErr := os.Stat(ProjectPlanPath(tmpDir, "default", plan.Slug)); os.IsNotExist(statErr) {
					t.Error("plan should still exist after failed delete")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestManager_ArchivePlan_StatusUpdate tests archiving a plan via status update.
func TestManager_ArchivePlan_StatusUpdate(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	// Create plan
	plan, err := CreatePlan(ctx, nil, "test-archive", "Test Archive")
	if err != nil {
		t.Fatalf("CreatePlan failed: %v", err)
	}

	// Archive plan
	err = ArchivePlan(ctx, nil, plan.Slug)
	if err != nil {
		t.Fatalf("ArchivePlan failed: %v", err)
	}

	// Verify plan still exists but status is archived
	loaded, err := LoadPlan(ctx, nil, plan.Slug)
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}

	if loaded.Status != StatusArchived {
		t.Errorf("Status = %q, want %q", loaded.Status, StatusArchived)
	}

	// Verify plan directory still exists
	planPath := ProjectPlanPath(tmpDir, "default", plan.Slug)
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		t.Error("plan directory should still exist after archive")
	}
}

// TestManager_ArchivePlan_NotFound tests archiving non-existent plan.
func TestManager_ArchivePlan_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
	
	err := ArchivePlan(ctx, nil, "nonexistent")
	if !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestExtractProjectSlug(t *testing.T) {
	tests := []struct {
		name      string
		projectID string
		want      string
	}{
		{"valid", "semspec.local.wf.project.project.my-project", "my-project"},
		{"default project", "semspec.local.wf.project.project.default", "default"},
		{"empty string", "", ""},
		{"malformed", "random.string", ""},
		{"partial prefix", "semspec.local.wf.project.project.", ""},
		{"wrong format", "c360.semspec.workflow.project.project.old", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractProjectSlug(tt.projectID)
			if got != tt.want {
				t.Errorf("ExtractProjectSlug(%q) = %q, want %q", tt.projectID, got, tt.want)
			}
		})
	}
}
