//go:build integration

package workflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDocumentExecutor_GetPlanStatus_ExistingPlan_ReturnsStatus(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	planDir := setupPlanDir(t, tmpDir, "my-plan")
	os.WriteFile(filepath.Join(planDir, "plan.md"), []byte("# Plan"), 0644)

	exec := NewDocumentExecutor(tmpDir)
	call := makeCall("c1", "workflow_get_plan_status", map[string]any{"slug": "my-plan"})

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

	var status map[string]any
	if err := json.Unmarshal([]byte(result.Content), &status); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if status["slug"] != "my-plan" {
		t.Errorf("status[slug] = %v, want %q", status["slug"], "my-plan")
	}
	docs, ok := status["documents"].(map[string]any)
	if !ok {
		t.Fatalf("status[documents] is not a map: %T", status["documents"])
	}
	if docs["plan"] != true {
		t.Errorf("documents[plan] = %v, want true", docs["plan"])
	}
	if docs["tasks"] != false {
		t.Errorf("documents[tasks] = %v, want false", docs["tasks"])
	}
}
