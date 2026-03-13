package prompt

import (
	"testing"
)

func TestResolveToolChoice_NoTools(t *testing.T) {
	tc := ResolveToolChoice(RoleDeveloper, nil)
	if tc != nil {
		t.Error("expected nil for no tools")
	}
}

func TestResolveToolChoice_SingleTool(t *testing.T) {
	tc := ResolveToolChoice(RoleDeveloper, []string{"file_write"})
	if tc == nil {
		t.Fatal("expected non-nil for single tool")
	}
	if tc.Mode != "function" {
		t.Errorf("expected mode 'function', got %q", tc.Mode)
	}
	if tc.FunctionName != "file_write" {
		t.Errorf("expected function_name 'file_write', got %q", tc.FunctionName)
	}
}

func TestResolveToolChoice_DeveloperRequired(t *testing.T) {
	tc := ResolveToolChoice(RoleDeveloper, []string{"file_read", "file_write", "git_diff"})
	if tc == nil {
		t.Fatal("expected non-nil for developer with tools")
	}
	if tc.Mode != "required" {
		t.Errorf("expected mode 'required', got %q", tc.Mode)
	}
}

func TestResolveToolChoice_ReviewerNil(t *testing.T) {
	tools := []string{"file_read", "git_diff"}

	for _, role := range []Role{RoleReviewer, RolePlanReviewer, RoleTaskReviewer} {
		tc := ResolveToolChoice(role, tools)
		if tc != nil {
			t.Errorf("expected nil for %s with tools", role)
		}
	}
}

func TestResolveToolChoice_PlannerNil(t *testing.T) {
	tools := []string{"file_read", "workflow_query_graph"}

	for _, role := range []Role{RolePlanner, RolePlanCoordinator} {
		tc := ResolveToolChoice(role, tools)
		if tc != nil {
			t.Errorf("expected nil for %s with tools", role)
		}
	}
}
