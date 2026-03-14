package prompt

import (
	"testing"
)

func TestGraphManifestFragment_ConditionFalseWhenEmpty(t *testing.T) {
	frag := GraphManifestFragment(func() string { return "" })

	ctx := &AssemblyContext{}
	if frag.Condition(ctx) {
		t.Error("expected Condition to return false when fetchFn returns empty string")
	}
}

func TestGraphManifestFragment_ConditionTrueWhenContent(t *testing.T) {
	frag := GraphManifestFragment(func() string { return "predicate: source.doc" })

	ctx := &AssemblyContext{}
	if !frag.Condition(ctx) {
		t.Error("expected Condition to return true when fetchFn returns non-empty string")
	}
}

func TestGraphManifestFragment_ContentFuncPassthrough(t *testing.T) {
	want := "## Knowledge Graph\n- source.doc: 12 entities"
	frag := GraphManifestFragment(func() string { return want })

	ctx := &AssemblyContext{}
	got := frag.ContentFunc(ctx)
	if got != want {
		t.Errorf("ContentFunc = %q, want %q", got, want)
	}
}

func TestGraphManifestFragment_Metadata(t *testing.T) {
	frag := GraphManifestFragment(func() string { return "" })

	if frag.ID != "core.knowledge-manifest" {
		t.Errorf("ID = %q, want %q", frag.ID, "core.knowledge-manifest")
	}
	if frag.Category != CategoryKnowledgeManifest {
		t.Errorf("Category = %d, want %d", frag.Category, CategoryKnowledgeManifest)
	}
	if frag.Priority != 0 {
		t.Errorf("Priority = %d, want 0", frag.Priority)
	}
}
