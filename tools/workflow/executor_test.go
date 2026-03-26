package workflow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeCall is a convenience builder for agentic.ToolCall.
func makeCall(id, name string, args map[string]any) agentic.ToolCall {
	return agentic.ToolCall{ID: id, Name: name, Arguments: args}
}

// setupPlanDir creates the .semspec/projects/default/plans/{slug} directory
// structure expected by DocumentExecutor and ConstitutionExecutor.
// It also writes a minimal plan.json so LoadPlan succeeds.
func setupPlanDir(t *testing.T, repoRoot, slug string) string {
	t.Helper()
	planPath := filepath.Join(repoRoot, ".semspec", "projects", "default", "plans", slug)
	if err := os.MkdirAll(planPath, 0755); err != nil {
		t.Fatalf("setupPlanDir: %v", err)
	}
	// Write a minimal plan.json so LoadPlan works for getPlanStatus tests.
	planJSON := map[string]any{
		"slug":       slug,
		"title":      "Test plan: " + slug,
		"project_id": "semspec.local.project.default",
		"approved":   false,
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(planJSON)
	if err := os.WriteFile(filepath.Join(planPath, "plan.json"), data, 0644); err != nil {
		t.Fatalf("setupPlanDir write plan.json: %v", err)
	}
	return planPath
}

// writeConstitution writes a valid constitution.md to repoRoot/.semspec/.
func writeConstitution(t *testing.T, repoRoot string) {
	t.Helper()
	dir := filepath.Join(repoRoot, ".semspec")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("writeConstitution mkdir: %v", err)
	}
	content := `# Project Constitution

Version: 1.0.0
Ratified: 2025-01-01

## Principles

### 1. Test-First Development

All code must have tests written before implementation.

Rationale: Tests clarify intent and prevent regressions.

### 2. Documentation Required

Every public API must be documented.

Rationale: Documentation enables collaboration.
`
	if err := os.WriteFile(filepath.Join(dir, "constitution.md"), []byte(content), 0644); err != nil {
		t.Fatalf("writeConstitution write: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GraphExecutor – ListTools
// ---------------------------------------------------------------------------

func TestGraphExecutor_ListTools_ReturnsThreeDefinitions(t *testing.T) {
	t.Parallel()

	exec := NewGraphExecutor()
	tools := exec.ListTools()

	if len(tools) != 3 {
		t.Fatalf("ListTools() returned %d definitions, want 3", len(tools))
	}

	want := map[string]bool{
		"graph_summary": true,
		"graph_search":  true,
		"graph_query":   true,
	}
	for _, tool := range tools {
		if !want[tool.Name] {
			t.Errorf("unexpected tool name %q", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		if tool.Parameters == nil {
			t.Errorf("tool %q has nil parameters", tool.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// GraphExecutor – Execute dispatch
// ---------------------------------------------------------------------------

func TestGraphExecutor_Execute_UnknownTool_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewGraphExecutor()
	call := makeCall("c1", "workflow_no_such_tool", nil)

	result, err := exec.Execute(context.Background(), call)

	if err == nil {
		t.Fatal("Execute() returned nil error for unknown tool, want non-nil")
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error message")
	}
	if !strings.Contains(result.Error, "workflow_no_such_tool") {
		t.Errorf("result.Error = %q, want mention of tool name", result.Error)
	}
}

// ---------------------------------------------------------------------------
// GraphExecutor – queryGraph argument validation
// ---------------------------------------------------------------------------

func TestGraphExecutor_QueryGraph_MissingQuery_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewGraphExecutor()
	call := makeCall("c1", "graph_query", map[string]any{})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error about missing query argument")
	}
	if !strings.Contains(strings.ToLower(result.Error), "query") {
		t.Errorf("result.Error = %q, want mention of 'query'", result.Error)
	}
}

func TestGraphExecutor_QueryGraph_EmptyQuery_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewGraphExecutor()
	call := makeCall("c1", "graph_query", map[string]any{"query": ""})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for empty query, want error")
	}
}

// ---------------------------------------------------------------------------
// GraphExecutor – queryGraph with mock HTTP server
// ---------------------------------------------------------------------------

func TestGraphExecutor_QueryGraph_HTTPError_ReturnsError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	exec := &GraphExecutor{gatewayURL: srv.URL}
	call := makeCall("c1", "graph_query", map[string]any{
		"query": "{ entitiesByPredicate(predicate: \"code.function\") }",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for HTTP 500, want error")
	}
}

func TestGraphExecutor_QueryGraph_GraphQLError_ReturnsError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"errors": []map[string]any{
				{"message": "field 'bad' not found"},
			},
		})
	}))
	defer srv.Close()

	exec := &GraphExecutor{gatewayURL: srv.URL}
	call := makeCall("c1", "graph_query", map[string]any{
		"query": "{ bad }",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for GraphQL error, want error")
	}
	if !strings.Contains(result.Error, "field 'bad' not found") {
		t.Errorf("result.Error = %q, want GraphQL error message", result.Error)
	}
}

func TestGraphExecutor_QueryGraph_Success_ReturnsJSONContent(t *testing.T) {
	t.Parallel()

	responseData := map[string]any{
		"data": map[string]any{
			"entitiesByPredicate": []string{"entity.1", "entity.2"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responseData)
	}))
	defer srv.Close()

	exec := &GraphExecutor{gatewayURL: srv.URL}
	call := makeCall("c1", "graph_query", map[string]any{
		"query": "{ entitiesByPredicate(predicate: \"code.function\") }",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
	if result.Content == "" {
		t.Fatal("result.Content is empty, want JSON")
	}
	if !json.Valid([]byte(result.Content)) {
		t.Errorf("result.Content is not valid JSON: %s", result.Content)
	}
}

// Tests for graph_entity and graph_traverse removed — tools dropped in Phase 2.
// Agents use graph_search for discovery and graph_query for specific lookups.

// ---------------------------------------------------------------------------
// GraphExecutor – context cancellation
// ---------------------------------------------------------------------------

func TestGraphExecutor_QueryGraph_CancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()

	// Slow server that outlives the cancelled context.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exec := &GraphExecutor{gatewayURL: srv.URL}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	call := makeCall("c1", "graph_query", map[string]any{
		"query": "{ entitiesByPredicate(predicate: \"code.function\") }",
	})

	result, err := exec.Execute(ctx, call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for cancelled context, want error")
	}
}

// ---------------------------------------------------------------------------
// DocumentExecutor – ListTools
// ---------------------------------------------------------------------------

func TestDocumentExecutor_ListTools_ReturnsFourDefinitions(t *testing.T) {
	t.Parallel()

	exec := NewDocumentExecutor("/tmp")
	tools := exec.ListTools()

	if len(tools) != 4 {
		t.Fatalf("ListTools() returned %d definitions, want 4", len(tools))
	}

	want := map[string]bool{
		"read_document":            true,
		"workflow_write_document":  true,
		"workflow_list_documents":  true,
		"workflow_get_plan_status": true,
	}
	for _, tool := range tools {
		if !want[tool.Name] {
			t.Errorf("unexpected tool name %q", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// DocumentExecutor – Execute dispatch
// ---------------------------------------------------------------------------

func TestDocumentExecutor_Execute_UnknownTool_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewDocumentExecutor("/tmp")
	call := makeCall("c1", "workflow_no_such_tool", nil)

	result, err := exec.Execute(context.Background(), call)

	if err == nil {
		t.Fatal("Execute() returned nil error for unknown tool, want non-nil")
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error message")
	}
}

// ---------------------------------------------------------------------------
// DocumentExecutor – readDocument
// ---------------------------------------------------------------------------

func TestDocumentExecutor_ReadDocument_MissingSlug_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewDocumentExecutor(t.TempDir())
	call := makeCall("c1", "read_document", map[string]any{
		"document": "plan",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error about missing slug")
	}
	if !strings.Contains(strings.ToLower(result.Error), "slug") {
		t.Errorf("result.Error = %q, want mention of 'slug'", result.Error)
	}
}

func TestDocumentExecutor_ReadDocument_MissingDocumentType_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewDocumentExecutor(t.TempDir())
	call := makeCall("c1", "read_document", map[string]any{
		"slug": "my-plan",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error about missing document type")
	}
	if !strings.Contains(strings.ToLower(result.Error), "document") {
		t.Errorf("result.Error = %q, want mention of 'document'", result.Error)
	}
}

func TestDocumentExecutor_ReadDocument_UnknownDocumentType_ReturnsError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	setupPlanDir(t, tmpDir, "my-plan")

	exec := NewDocumentExecutor(tmpDir)
	call := makeCall("c1", "read_document", map[string]any{
		"slug":     "my-plan",
		"document": "invalid-type",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error about unknown document type")
	}
	if !strings.Contains(result.Error, "invalid-type") {
		t.Errorf("result.Error = %q, want mention of the bad document type", result.Error)
	}
}

func TestDocumentExecutor_ReadDocument_NonExistentPlanDoc_ReturnsError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	setupPlanDir(t, tmpDir, "my-plan") // dir exists but no plan.md

	exec := NewDocumentExecutor(tmpDir)
	call := makeCall("c1", "read_document", map[string]any{
		"slug":     "my-plan",
		"document": "plan",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for missing plan.md, want error")
	}
}

func TestDocumentExecutor_ReadDocument_Plan_ReturnsContent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	planDir := setupPlanDir(t, tmpDir, "my-plan")

	wantContent := "# My Plan\n\nThis is the plan."
	if err := os.WriteFile(filepath.Join(planDir, "plan.md"), []byte(wantContent), 0644); err != nil {
		t.Fatalf("write plan.md: %v", err)
	}

	exec := NewDocumentExecutor(tmpDir)
	call := makeCall("c1", "read_document", map[string]any{
		"slug":     "my-plan",
		"document": "plan",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
	if result.Content != wantContent {
		t.Errorf("result.Content = %q, want %q", result.Content, wantContent)
	}
}

func TestDocumentExecutor_ReadDocument_Tasks_ReturnsContent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	planDir := setupPlanDir(t, tmpDir, "my-plan")

	wantContent := "# Tasks\n\n- Task 1\n- Task 2"
	if err := os.WriteFile(filepath.Join(planDir, "tasks.md"), []byte(wantContent), 0644); err != nil {
		t.Fatalf("write tasks.md: %v", err)
	}

	exec := NewDocumentExecutor(tmpDir)
	call := makeCall("c1", "read_document", map[string]any{
		"slug":     "my-plan",
		"document": "tasks",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
	if result.Content != wantContent {
		t.Errorf("result.Content = %q, want %q", result.Content, wantContent)
	}
}

func TestDocumentExecutor_WriteDocument_MissingSlug_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewDocumentExecutor(t.TempDir())
	call := makeCall("c1", "workflow_write_document", map[string]any{
		"document": "plan",
		"content":  "# My Plan",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error about missing slug")
	}
}

func TestDocumentExecutor_WriteDocument_MissingDocumentType_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewDocumentExecutor(t.TempDir())
	call := makeCall("c1", "workflow_write_document", map[string]any{
		"slug":    "my-plan",
		"content": "# My Plan",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error about missing document type")
	}
}

func TestDocumentExecutor_WriteDocument_MissingContent_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewDocumentExecutor(t.TempDir())
	call := makeCall("c1", "workflow_write_document", map[string]any{
		"slug":     "my-plan",
		"document": "plan",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error about missing content")
	}
}

func TestDocumentExecutor_WriteDocument_PlanDirNotFound_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewDocumentExecutor(t.TempDir()) // plan directory never created
	call := makeCall("c1", "workflow_write_document", map[string]any{
		"slug":     "nonexistent-plan",
		"document": "plan",
		"content":  "# Plan",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for non-existent plan directory, want error")
	}
	if !strings.Contains(result.Error, "nonexistent-plan") {
		t.Errorf("result.Error = %q, want mention of slug", result.Error)
	}
}

func TestDocumentExecutor_WriteDocument_InvalidDocType_ReturnsError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	setupPlanDir(t, tmpDir, "my-plan")

	exec := NewDocumentExecutor(tmpDir)
	call := makeCall("c1", "workflow_write_document", map[string]any{
		"slug":     "my-plan",
		"document": "constitution", // constitution is read-only
		"content":  "# Something",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for read-only doc type, want error")
	}
}

func TestDocumentExecutor_WriteDocument_Plan_WritesFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	planDir := setupPlanDir(t, tmpDir, "my-plan")

	content := "# My Plan\n\nFull plan content here."
	exec := NewDocumentExecutor(tmpDir)
	call := makeCall("c1", "workflow_write_document", map[string]any{
		"slug":     "my-plan",
		"document": "plan",
		"content":  content,
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
	if !strings.Contains(result.Content, "Successfully wrote") {
		t.Errorf("result.Content = %q, want success message", result.Content)
	}

	// Verify file was actually written.
	data, err := os.ReadFile(filepath.Join(planDir, "plan.md"))
	if err != nil {
		t.Fatalf("read written plan.md: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content = %q, want %q", string(data), content)
	}
}

func TestDocumentExecutor_WriteDocument_Tasks_WritesFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	planDir := setupPlanDir(t, tmpDir, "my-plan")

	content := "# Tasks\n\n- [ ] Task A\n- [ ] Task B"
	exec := NewDocumentExecutor(tmpDir)
	call := makeCall("c1", "workflow_write_document", map[string]any{
		"slug":     "my-plan",
		"document": "tasks",
		"content":  content,
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}

	data, err := os.ReadFile(filepath.Join(planDir, "tasks.md"))
	if err != nil {
		t.Fatalf("read written tasks.md: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content = %q, want %q", string(data), content)
	}
}

// ---------------------------------------------------------------------------
// DocumentExecutor – listDocuments
// ---------------------------------------------------------------------------

func TestDocumentExecutor_ListDocuments_MissingSlug_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewDocumentExecutor(t.TempDir())
	call := makeCall("c1", "workflow_list_documents", map[string]any{})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error about missing slug")
	}
}

func TestDocumentExecutor_ListDocuments_NoDocs_ReturnsFalseForAll(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	setupPlanDir(t, tmpDir, "my-plan") // no plan.md or tasks.md

	exec := NewDocumentExecutor(tmpDir)
	call := makeCall("c1", "workflow_list_documents", map[string]any{"slug": "my-plan"})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}

	var docs map[string]bool
	if err := json.Unmarshal([]byte(result.Content), &docs); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if docs["plan"] {
		t.Error("docs[plan] = true, want false (no plan.md written)")
	}
	if docs["tasks"] {
		t.Error("docs[tasks] = true, want false (no tasks.md written)")
	}
}

func TestDocumentExecutor_ListDocuments_WithPlanFile_ReturnsTrueForPlan(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	planDir := setupPlanDir(t, tmpDir, "my-plan")
	os.WriteFile(filepath.Join(planDir, "plan.md"), []byte("# Plan"), 0644)

	exec := NewDocumentExecutor(tmpDir)
	call := makeCall("c1", "workflow_list_documents", map[string]any{"slug": "my-plan"})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}

	var docs map[string]bool
	if err := json.Unmarshal([]byte(result.Content), &docs); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !docs["plan"] {
		t.Error("docs[plan] = false, want true (plan.md was written)")
	}
	if docs["tasks"] {
		t.Error("docs[tasks] = true, want false (no tasks.md written)")
	}
}

func TestDocumentExecutor_GetPlanStatus_MissingSlug_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewDocumentExecutor(t.TempDir())
	call := makeCall("c1", "workflow_get_plan_status", map[string]any{})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error about missing slug")
	}
}

func TestDocumentExecutor_GetPlanStatus_NonExistentPlan_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewDocumentExecutor(t.TempDir())
	call := makeCall("c1", "workflow_get_plan_status", map[string]any{
		"slug": "ghost-plan",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for missing plan, want error")
	}
}

// ---------------------------------------------------------------------------
// DocumentExecutor – context cancellation
// ---------------------------------------------------------------------------

func TestDocumentExecutor_ReadDocument_CancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewDocumentExecutor(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	call := makeCall("c1", "read_document", map[string]any{
		"slug":     "my-plan",
		"document": "plan",
	})

	result, err := exec.Execute(ctx, call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for cancelled context, want error")
	}
}

func TestDocumentExecutor_WriteDocument_CancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewDocumentExecutor(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	call := makeCall("c1", "workflow_write_document", map[string]any{
		"slug":     "my-plan",
		"document": "plan",
		"content":  "# Plan",
	})

	result, err := exec.Execute(ctx, call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for cancelled context, want error")
	}
}

func TestDocumentExecutor_ListDocuments_CancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewDocumentExecutor(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	call := makeCall("c1", "workflow_list_documents", map[string]any{"slug": "my-plan"})

	result, err := exec.Execute(ctx, call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for cancelled context, want error")
	}
}

// ---------------------------------------------------------------------------
// ConstitutionExecutor – ListTools
// ---------------------------------------------------------------------------
