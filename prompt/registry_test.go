package prompt

import (
	"testing"
)

func TestRegistryRegister(t *testing.T) {
	r := NewRegistry()
	r.Register(&Fragment{ID: "test.fragment", Category: CategorySystemBase, Content: "hello"})

	if r.FragmentCount() != 1 {
		t.Errorf("expected 1 fragment, got %d", r.FragmentCount())
	}
}

func TestRegistryRegisterAll(t *testing.T) {
	r := NewRegistry()
	r.RegisterAll(
		&Fragment{ID: "f1", Category: CategorySystemBase, Content: "a"},
		&Fragment{ID: "f2", Category: CategoryToolDirective, Content: "b"},
	)

	if r.FragmentCount() != 2 {
		t.Errorf("expected 2 fragments, got %d", r.FragmentCount())
	}
}

func TestRegistryRoleFiltering(t *testing.T) {
	r := NewRegistry()
	r.RegisterAll(
		&Fragment{ID: "all-roles", Category: CategorySystemBase, Content: "shared"},
		&Fragment{ID: "dev-only", Category: CategoryRoleContext, Content: "developer stuff", Roles: []Role{RoleDeveloper}},
		&Fragment{ID: "review-only", Category: CategoryRoleContext, Content: "reviewer stuff", Roles: []Role{RoleReviewer}},
	)

	devCtx := &AssemblyContext{Role: RoleDeveloper}
	devFragments := r.GetFragmentsForContext(devCtx)

	if len(devFragments) != 2 {
		t.Fatalf("expected 2 fragments for developer, got %d", len(devFragments))
	}
	if devFragments[0].ID != "all-roles" {
		t.Errorf("expected 'all-roles' first, got %q", devFragments[0].ID)
	}
	if devFragments[1].ID != "dev-only" {
		t.Errorf("expected 'dev-only' second, got %q", devFragments[1].ID)
	}
}

func TestRegistryProviderFiltering(t *testing.T) {
	r := NewRegistry()
	r.RegisterAll(
		&Fragment{ID: "all-providers", Category: CategorySystemBase, Content: "shared"},
		&Fragment{ID: "anthropic-only", Category: CategoryProviderHints, Content: "xml stuff", Providers: []Provider{ProviderAnthropic}},
	)

	ollamaCtx := &AssemblyContext{Role: RoleDeveloper, Provider: ProviderOllama}
	fragments := r.GetFragmentsForContext(ollamaCtx)

	if len(fragments) != 1 {
		t.Fatalf("expected 1 fragment for ollama, got %d", len(fragments))
	}
	if fragments[0].ID != "all-providers" {
		t.Errorf("expected 'all-providers', got %q", fragments[0].ID)
	}
}

func TestRegistryConditionFiltering(t *testing.T) {
	r := NewRegistry()
	r.Register(&Fragment{
		ID:       "tools-only",
		Category: CategoryToolDirective,
		Content:  "you must use tools",
		Condition: func(ctx *AssemblyContext) bool {
			return ctx.SupportsTools
		},
	})

	noTools := &AssemblyContext{SupportsTools: false}
	if len(r.GetFragmentsForContext(noTools)) != 0 {
		t.Error("expected no fragments when tools not supported")
	}

	withTools := &AssemblyContext{SupportsTools: true}
	if len(r.GetFragmentsForContext(withTools)) != 1 {
		t.Error("expected 1 fragment when tools supported")
	}
}

func TestRegistrySortOrder(t *testing.T) {
	r := NewRegistry()
	r.RegisterAll(
		&Fragment{ID: "c", Category: CategoryGapDetection, Content: "gap"},
		&Fragment{ID: "a", Category: CategorySystemBase, Content: "base"},
		&Fragment{ID: "b", Category: CategoryToolDirective, Content: "tools", Priority: 1},
		&Fragment{ID: "b0", Category: CategoryToolDirective, Content: "tools first", Priority: 0},
	)

	ctx := &AssemblyContext{}
	frags := r.GetFragmentsForContext(ctx)

	expected := []string{"a", "b0", "b", "c"}
	if len(frags) != len(expected) {
		t.Fatalf("expected %d fragments, got %d", len(expected), len(frags))
	}
	for i, id := range expected {
		if frags[i].ID != id {
			t.Errorf("position %d: expected %q, got %q", i, id, frags[i].ID)
		}
	}
}

func TestRegistryGetStyle(t *testing.T) {
	r := NewRegistry()

	style := r.GetStyle(ProviderAnthropic)
	if !style.PreferXML {
		t.Error("expected Anthropic to prefer XML")
	}

	unknown := r.GetStyle(Provider("unknown"))
	if unknown.PreferXML || unknown.PreferMarkdown {
		t.Error("expected zero-value style for unknown provider")
	}
}
