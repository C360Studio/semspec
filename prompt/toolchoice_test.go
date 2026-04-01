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
	tc := ResolveToolChoice(RoleDeveloper, []string{"bash"})
	if tc == nil {
		t.Fatal("expected non-nil for single tool")
	}
	if tc.Mode != "function" {
		t.Errorf("expected mode 'function', got %q", tc.Mode)
	}
	if tc.FunctionName != "bash" {
		t.Errorf("expected function_name 'bash', got %q", tc.FunctionName)
	}
}

func TestResolveToolChoice_DeveloperRequired(t *testing.T) {
	tc := ResolveToolChoice(RoleDeveloper, []string{"bash", "submit_work", "graph_search"})
	if tc == nil {
		t.Fatal("expected non-nil for developer with tools")
	}
	if tc.Mode != "required" {
		t.Errorf("expected mode 'required', got %q", tc.Mode)
	}
}

func TestResolveToolChoice_ReviewerNil(t *testing.T) {
	tools := []string{"bash", "submit_work"}

	for _, role := range []Role{RoleReviewer, RolePlanReviewer, RoleTaskReviewer} {
		tc := ResolveToolChoice(role, tools)
		if tc != nil {
			t.Errorf("expected nil for %s with tools", role)
		}
	}
}

func TestResolveToolChoice_BuilderRequired(t *testing.T) {
	tools := []string{"bash", "submit_work", "ask_question"}

	for _, role := range []Role{RoleDeveloper, RoleValidator} {
		tc := ResolveToolChoice(role, tools)
		if tc == nil {
			t.Fatalf("expected non-nil for %s with tools", role)
		}
		if tc.Mode != "required" {
			t.Errorf("expected mode 'required' for %s, got %q", role, tc.Mode)
		}
	}
}

func TestResolveToolChoice_PlannerNil(t *testing.T) {
	tools := []string{"bash", "graph_search"}

	for _, role := range []Role{RolePlanner, RolePlanCoordinator} {
		tc := ResolveToolChoice(role, tools)
		if tc != nil {
			t.Errorf("expected nil for %s with tools", role)
		}
	}
}
