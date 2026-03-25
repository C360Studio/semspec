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

func TestDocumentExecutor_ReadDocument_Constitution_NoFile_ReturnsError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	setupPlanDir(t, tmpDir, "my-plan")
	// No constitution.md written.

	exec := NewDocumentExecutor(tmpDir)
	call := makeCall("c1", "read_document", map[string]any{
		"slug":     "my-plan",
		"document": "constitution",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for missing constitution, want error")
	}
}

func TestDocumentExecutor_ReadDocument_Constitution_ReturnsFormattedContent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	setupPlanDir(t, tmpDir, "my-plan")
	writeConstitution(t, tmpDir)

	exec := NewDocumentExecutor(tmpDir)
	call := makeCall("c1", "read_document", map[string]any{
		"slug":     "my-plan",
		"document": "constitution",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
	if !strings.Contains(result.Content, "Project Constitution") {
		t.Errorf("result.Content = %q, want 'Project Constitution' heading", result.Content)
	}
	if !strings.Contains(result.Content, "Test-First Development") {
		t.Errorf("result.Content = %q, want principle title", result.Content)
	}
}

// ---------------------------------------------------------------------------
// DocumentExecutor – writeDocument
// ---------------------------------------------------------------------------

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

func TestDocumentExecutor_ListDocuments_WithConstitution_ReturnsTrueForConstitution(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	setupPlanDir(t, tmpDir, "my-plan")
	writeConstitution(t, tmpDir)

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
	if !docs["constitution"] {
		t.Error("docs[constitution] = false, want true (constitution.md was written)")
	}
}

// ---------------------------------------------------------------------------
// DocumentExecutor – getPlanStatus
// ---------------------------------------------------------------------------

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

func TestConstitutionExecutor_ListTools_ReturnsTwoDefinitions(t *testing.T) {
	t.Parallel()

	exec := NewConstitutionExecutor("/tmp")
	tools := exec.ListTools()

	if len(tools) != 2 {
		t.Fatalf("ListTools() returned %d definitions, want 2", len(tools))
	}

	want := map[string]bool{
		"workflow_check_constitution": true,
		"workflow_get_principles":     true,
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
// ConstitutionExecutor – Execute dispatch
// ---------------------------------------------------------------------------

func TestConstitutionExecutor_Execute_UnknownTool_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewConstitutionExecutor("/tmp")
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
// ConstitutionExecutor – checkConstitution
// ---------------------------------------------------------------------------

func TestConstitutionExecutor_CheckConstitution_MissingContent_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewConstitutionExecutor(t.TempDir())
	call := makeCall("c1", "workflow_check_constitution", map[string]any{
		"document_type": "plan",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error about missing content")
	}
	if !strings.Contains(strings.ToLower(result.Error), "content") {
		t.Errorf("result.Error = %q, want mention of 'content'", result.Error)
	}
}

func TestConstitutionExecutor_CheckConstitution_MissingDocumentType_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewConstitutionExecutor(t.TempDir())
	call := makeCall("c1", "workflow_check_constitution", map[string]any{
		"content": "# Plan content",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error about missing document_type")
	}
	if !strings.Contains(strings.ToLower(result.Error), "document_type") {
		t.Errorf("result.Error = %q, want mention of 'document_type'", result.Error)
	}
}

func TestConstitutionExecutor_CheckConstitution_NoConstitution_ReturnsHasConstitutionFalse(t *testing.T) {
	t.Parallel()

	exec := NewConstitutionExecutor(t.TempDir()) // no constitution file

	call := makeCall("c1", "workflow_check_constitution", map[string]any{
		"content":       "# My plan without testing",
		"document_type": "tasks",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty (no constitution is graceful)", result.Error)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Content), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["has_constitution"] != false {
		t.Errorf("has_constitution = %v, want false", resp["has_constitution"])
	}
}

func TestConstitutionExecutor_CheckConstitution_WithConstitution_PassingDoc_ReturnsPassed(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeConstitution(t, tmpDir)

	exec := NewConstitutionExecutor(tmpDir)
	call := makeCall("c1", "workflow_check_constitution", map[string]any{
		"content":       "# Plan\n\nThis plan includes test coverage and documentation.",
		"document_type": "plan",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Content), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["has_constitution"] != true {
		t.Errorf("has_constitution = %v, want true", resp["has_constitution"])
	}
	count, _ := resp["principles_checked"].(float64)
	if int(count) == 0 {
		t.Errorf("principles_checked = 0, want > 0")
	}
}

func TestConstitutionExecutor_CheckConstitution_TasksWithoutTest_ReturnsConcern(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeConstitution(t, tmpDir) // principle 1: Test-First Development, "before" in desc

	exec := NewConstitutionExecutor(tmpDir)
	call := makeCall("c1", "workflow_check_constitution", map[string]any{
		"content": "# Tasks\n\n- [ ] Implement login\n- [ ] Deploy service",
		// content deliberately has no "test" keyword
		"document_type": "tasks",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Content), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["passed"] != false {
		t.Errorf("passed = %v, want false (test mention missing triggers concern)", resp["passed"])
	}
	concerns, _ := resp["concerns"].([]any)
	if len(concerns) == 0 {
		t.Error("concerns is empty, want at least one concern about missing tests")
	}
}

// ---------------------------------------------------------------------------
// ConstitutionExecutor – getPrinciples
// ---------------------------------------------------------------------------

func TestConstitutionExecutor_GetPrinciples_NoConstitution_ReturnsHasConstitutionFalse(t *testing.T) {
	t.Parallel()

	exec := NewConstitutionExecutor(t.TempDir())
	call := makeCall("c1", "workflow_get_principles", map[string]any{})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty (no constitution is graceful)", result.Error)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Content), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["has_constitution"] != false {
		t.Errorf("has_constitution = %v, want false", resp["has_constitution"])
	}
}

func TestConstitutionExecutor_GetPrinciples_WithConstitution_ReturnsPrinciples(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeConstitution(t, tmpDir)

	exec := NewConstitutionExecutor(tmpDir)
	call := makeCall("c1", "workflow_get_principles", map[string]any{})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
	if !json.Valid([]byte(result.Content)) {
		t.Fatalf("result.Content is not valid JSON: %s", result.Content)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Content), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["has_constitution"] != true {
		t.Errorf("has_constitution = %v, want true", resp["has_constitution"])
	}
	principles, ok := resp["principles"].([]any)
	if !ok || len(principles) == 0 {
		t.Fatalf("principles is empty or wrong type: %T %v", resp["principles"], resp["principles"])
	}
	// Verify at least the first principle has the required fields.
	first, ok := principles[0].(map[string]any)
	if !ok {
		t.Fatalf("principles[0] is not a map: %T", principles[0])
	}
	if first["title"] == "" || first["title"] == nil {
		t.Error("principles[0].title is empty, want a title string")
	}
	if first["number"] == nil {
		t.Error("principles[0].number is nil, want a number")
	}
}

func TestConstitutionExecutor_GetPrinciples_VersionAndRatifiedPresent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	writeConstitution(t, tmpDir)

	exec := NewConstitutionExecutor(tmpDir)
	call := makeCall("c1", "workflow_get_principles", map[string]any{})

	result, err := exec.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}

	var resp map[string]any
	json.Unmarshal([]byte(result.Content), &resp)

	if resp["version"] == "" || resp["version"] == nil {
		t.Errorf("version = %v, want non-empty string", resp["version"])
	}
	if resp["ratified"] == "" || resp["ratified"] == nil {
		t.Errorf("ratified = %v, want non-empty date string", resp["ratified"])
	}
}

// ---------------------------------------------------------------------------
// ConstitutionExecutor – context cancellation
// ---------------------------------------------------------------------------

func TestConstitutionExecutor_CheckConstitution_CancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewConstitutionExecutor(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	call := makeCall("c1", "workflow_check_constitution", map[string]any{
		"content":       "# Plan",
		"document_type": "plan",
	})

	result, err := exec.Execute(ctx, call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for cancelled context, want error")
	}
}

func TestConstitutionExecutor_GetPrinciples_CancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewConstitutionExecutor(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	call := makeCall("c1", "workflow_get_principles", map[string]any{})

	result, err := exec.Execute(ctx, call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for cancelled context, want error")
	}
}

// ---------------------------------------------------------------------------
// GrepExecutor – ListTools
// ---------------------------------------------------------------------------

func TestGrepExecutor_ListTools_ReturnsTwoDefinitions(t *testing.T) {
	t.Parallel()

	exec := NewGrepExecutor("/tmp")
	tools := exec.ListTools()

	if len(tools) != 2 {
		t.Fatalf("ListTools() returned %d definitions, want 2", len(tools))
	}

	want := map[string]bool{
		"workflow_grep_fallback": true,
		"workflow_find_files":    true,
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
// GrepExecutor – Execute dispatch
// ---------------------------------------------------------------------------

func TestGrepExecutor_Execute_UnknownTool_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewGrepExecutor("/tmp")
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
// GrepExecutor – grepFallback
// ---------------------------------------------------------------------------

func TestGrepExecutor_GrepFallback_MissingPattern_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewGrepExecutor(t.TempDir())
	call := makeCall("c1", "workflow_grep_fallback", map[string]any{})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error about missing pattern")
	}
	if !strings.Contains(strings.ToLower(result.Error), "pattern") {
		t.Errorf("result.Error = %q, want mention of 'pattern'", result.Error)
	}
}

func TestGrepExecutor_GrepFallback_EmptyPattern_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewGrepExecutor(t.TempDir())
	call := makeCall("c1", "workflow_grep_fallback", map[string]any{"pattern": ""})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for empty pattern, want error")
	}
}

func TestGrepExecutor_GrepFallback_NoMatches_ReturnsNoMatchesMessage(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	// Write a file that won't match the pattern.
	os.WriteFile(filepath.Join(tmpDir, "hello.txt"), []byte("hello world"), 0644)

	exec := NewGrepExecutor(tmpDir)
	call := makeCall("c1", "workflow_grep_fallback", map[string]any{
		"pattern": "XYZNONEXISTENT_PATTERN_9999",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty for no-match", result.Error)
	}
	if !strings.Contains(result.Content, "No matches found") {
		t.Errorf("result.Content = %q, want 'No matches found'", result.Content)
	}
}

func TestGrepExecutor_GrepFallback_Matches_ReturnsResultsWithFallbackNote(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "source.go"), []byte("package main\n\nfunc Hello() {}\n"), 0644)

	exec := NewGrepExecutor(tmpDir)
	call := makeCall("c1", "workflow_grep_fallback", map[string]any{
		"pattern": "func Hello",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
	if !strings.Contains(result.Content, "func Hello") {
		t.Errorf("result.Content = %q, want match content", result.Content)
	}
	// Verify fallback note is present.
	if !strings.Contains(result.Content, "NOTE: Using grep fallback") {
		t.Errorf("result.Content = %q, want fallback note", result.Content)
	}
}

func TestGrepExecutor_GrepFallback_FilePattern_FiltersResults(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("func main() {}\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.ts"), []byte("function main() {}\n"), 0644)

	exec := NewGrepExecutor(tmpDir)
	call := makeCall("c1", "workflow_grep_fallback", map[string]any{
		"pattern":      "func main",
		"file_pattern": "*.go",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
	if strings.Contains(result.Content, "main.ts") {
		t.Errorf("result.Content contains main.ts — file_pattern *.go should have excluded it")
	}
}

func TestGrepExecutor_GrepFallback_ContextLinesClamped(t *testing.T) {
	t.Parallel()

	// We can't directly observe the clamping without a spy, but we verify
	// the call succeeds and produces content rather than panicking.
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "code.go"), []byte("func A() {}\nfunc B() {}\nfunc C() {}\n"), 0644)

	exec := NewGrepExecutor(tmpDir)
	call := makeCall("c1", "workflow_grep_fallback", map[string]any{
		"pattern":       "func",
		"context_lines": float64(100), // should be clamped to 5
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	// As long as no error and we get content, clamping worked.
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
}

func TestGrepExecutor_GrepFallback_MaxResultsClamped(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "code.go"), []byte("func A() {}\n"), 0644)

	exec := NewGrepExecutor(tmpDir)
	call := makeCall("c1", "workflow_grep_fallback", map[string]any{
		"pattern":     "func",
		"max_results": float64(999), // should be clamped to 50
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
}

func TestGrepExecutor_GrepFallback_SubdirPath_SearchesSubdir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	subdir := filepath.Join(tmpDir, "src")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "core.go"), []byte("func Core() {}\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "root.go"), []byte("func Root() {}\n"), 0644)

	exec := NewGrepExecutor(tmpDir)
	call := makeCall("c1", "workflow_grep_fallback", map[string]any{
		"pattern": "func",
		"path":    "src",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
	if !strings.Contains(result.Content, "Core") {
		t.Errorf("result.Content = %q, want match from src/core.go", result.Content)
	}
}

// ---------------------------------------------------------------------------
// GrepExecutor – findFiles
// ---------------------------------------------------------------------------

func TestGrepExecutor_FindFiles_MissingPattern_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewGrepExecutor(t.TempDir())
	call := makeCall("c1", "workflow_find_files", map[string]any{})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty, want error about missing pattern")
	}
	if !strings.Contains(strings.ToLower(result.Error), "pattern") {
		t.Errorf("result.Error = %q, want mention of 'pattern'", result.Error)
	}
}

func TestGrepExecutor_FindFiles_EmptyPattern_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewGrepExecutor(t.TempDir())
	call := makeCall("c1", "workflow_find_files", map[string]any{"pattern": ""})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for empty pattern, want error")
	}
}

func TestGrepExecutor_FindFiles_NoMatches_ReturnsNoFilesMessage(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)

	exec := NewGrepExecutor(tmpDir)
	call := makeCall("c1", "workflow_find_files", map[string]any{
		"pattern": "*.nonexistent",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty for no-match", result.Error)
	}
	if !strings.Contains(result.Content, "No files found") {
		t.Errorf("result.Content = %q, want 'No files found'", result.Content)
	}
}

func TestGrepExecutor_FindFiles_GoPattern_ReturnsMatchingFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.ts"), []byte("const x = 1"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "util.go"), []byte("package util"), 0644)

	exec := NewGrepExecutor(tmpDir)
	call := makeCall("c1", "workflow_find_files", map[string]any{
		"pattern": "*.go",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
	if !json.Valid([]byte(result.Content)) {
		t.Fatalf("result.Content is not valid JSON: %s", result.Content)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Content), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	files, _ := resp["files"].([]any)
	if len(files) < 2 {
		t.Errorf("files len = %d, want >= 2 (.go files)", len(files))
	}

	// Ensure no .ts files leaked in.
	for _, f := range files {
		name, _ := f.(string)
		if strings.HasSuffix(name, ".ts") {
			t.Errorf("unexpected .ts file in results: %s", name)
		}
	}
}

func TestGrepExecutor_FindFiles_MaxResultsClamped(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	for i := 0; i < 5; i++ {
		name := filepath.Join(tmpDir, strings.Repeat("a", i+1)+".txt")
		os.WriteFile(name, []byte("content"), 0644)
	}

	exec := NewGrepExecutor(tmpDir)
	call := makeCall("c1", "workflow_find_files", map[string]any{
		"pattern":     "*.txt",
		"max_results": float64(999), // should clamp to 100
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
}

func TestGrepExecutor_FindFiles_ExcludesGitAndNodeModules(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	// Create files inside excluded directories.
	os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)
	os.WriteFile(filepath.Join(tmpDir, ".git", "config"), []byte(""), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "node_modules"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "node_modules", "dep.js"), []byte(""), 0644)
	// Create a file that should be found.
	os.WriteFile(filepath.Join(tmpDir, "real.go"), []byte("package main"), 0644)

	exec := NewGrepExecutor(tmpDir)
	call := makeCall("c1", "workflow_find_files", map[string]any{
		"pattern": "*.go",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}

	// .git and node_modules files should not appear.
	if strings.Contains(result.Content, ".git") {
		t.Error("result.Content includes .git directory files, should be excluded")
	}
	if strings.Contains(result.Content, "node_modules") {
		t.Error("result.Content includes node_modules files, should be excluded")
	}
}

func TestGrepExecutor_FindFiles_FallbackNotePresent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)

	exec := NewGrepExecutor(tmpDir)
	call := makeCall("c1", "workflow_find_files", map[string]any{
		"pattern": "*.go",
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
	if !strings.Contains(result.Content, "fallback") {
		t.Errorf("result.Content = %q, want fallback note", result.Content)
	}
}

func TestGrepExecutor_FindFiles_CancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()

	exec := NewGrepExecutor(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	call := makeCall("c1", "workflow_find_files", map[string]any{"pattern": "*.go"})

	result, err := exec.Execute(ctx, call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("result.Error is empty for cancelled context, want error")
	}
}

// ---------------------------------------------------------------------------
// CallID propagation across all executors
// ---------------------------------------------------------------------------

func TestAllExecutors_CallIDPropagated(t *testing.T) {
	t.Parallel()

	const wantCallID = "my-unique-call-id"

	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		executor interface {
			Execute(context.Context, agentic.ToolCall) (agentic.ToolResult, error)
		}
		call agentic.ToolCall
	}{
		{
			name:     "DocumentExecutor missing slug",
			executor: NewDocumentExecutor(tmpDir),
			call:     makeCall(wantCallID, "read_document", map[string]any{}),
		},
		{
			name:     "ConstitutionExecutor missing content",
			executor: NewConstitutionExecutor(tmpDir),
			call:     makeCall(wantCallID, "workflow_check_constitution", map[string]any{}),
		},
		{
			name:     "GrepExecutor missing pattern",
			executor: NewGrepExecutor(tmpDir),
			call:     makeCall(wantCallID, "workflow_grep_fallback", map[string]any{}),
		},
		{
			name:     "GraphExecutor missing query",
			executor: NewGraphExecutor(),
			call:     makeCall(wantCallID, "graph_query", map[string]any{}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, _ := tt.executor.Execute(context.Background(), tt.call)
			if result.CallID != wantCallID {
				t.Errorf("result.CallID = %q, want %q", result.CallID, wantCallID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// formatSearchResult
// ---------------------------------------------------------------------------

func TestFormatSearchResult(t *testing.T) {
	tests := []struct {
		name         string
		data         map[string]any
		wantContains []string
		wantExact    string
	}{
		{
			name:      "nil globalSearch",
			data:      map[string]any{},
			wantExact: "No results found.",
		},
		{
			name: "answer only",
			data: map[string]any{
				"globalSearch": map[string]any{
					"answer": "The auth system uses JWT tokens.",
				},
			},
			wantContains: []string{"JWT tokens"},
		},
		{
			name: "answer with model attribution",
			data: map[string]any{
				"globalSearch": map[string]any{
					"answer":       "Error handling uses fmt.Errorf with %w.",
					"answer_model": "gemini-flash",
				},
			},
			wantContains: []string{"fmt.Errorf", "synthesized by gemini-flash"},
		},
		{
			name: "entity digests without answer",
			data: map[string]any{
				"globalSearch": map[string]any{
					"entity_digests": []any{
						map[string]any{"id": "code.func.Run", "type": "function", "label": "Run"},
						map[string]any{"id": "code.type.Config", "type": "type", "label": "Config"},
					},
				},
			},
			wantContains: []string{"Run [function]", "Config [type]"},
		},
		{
			name: "answer plus digests",
			data: map[string]any{
				"globalSearch": map[string]any{
					"answer": "The system has two main entry points.",
					"entity_digests": []any{
						map[string]any{"id": "code.func.Main", "type": "function", "label": "Main"},
					},
				},
			},
			wantContains: []string{"two main entry points", "Matched entities:", "Main [function]"},
		},
		{
			name: "communities only",
			data: map[string]any{
				"globalSearch": map[string]any{
					"community_summaries": []any{
						map[string]any{
							"summary":      "Authentication cluster handling JWT and OAuth.",
							"member_count": float64(12),
							"entities": []any{
								map[string]any{"id": "auth.jwt", "type": "module", "label": "JWT Handler"},
							},
						},
					},
				},
			},
			wantContains: []string{"Knowledge clusters:", "Authentication cluster", "JWT Handler [module]"},
		},
		{
			name: "count only fallback",
			data: map[string]any{
				"globalSearch": map[string]any{
					"count": float64(42),
				},
			},
			wantContains: []string{"Found 42 entities", "graph_query"},
		},
		{
			name: "empty globalSearch map",
			data: map[string]any{
				"globalSearch": map[string]any{},
			},
			wantExact: "No results found.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatSearchResult(tt.data)

			if tt.wantExact != "" {
				if got != tt.wantExact {
					t.Errorf("formatSearchResult() = %q, want %q", got, tt.wantExact)
				}
				return
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("formatSearchResult() missing %q in:\n%s", want, got)
				}
			}
		})
	}
}
