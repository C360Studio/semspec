package prompt

import (
	"errors"
	"strings"
	"testing"
)

// TestUserPromptCategory_NotInSystemMessage pins the contract that
// CategoryUserPrompt fragments contribute to UserMessage only — never to
// SystemMessage. A regression here would put user-prompt content in the
// wrong message and silently break every role.
func TestUserPromptCategory_NotInSystemMessage(t *testing.T) {
	r := NewRegistry()
	r.Register(&Fragment{
		ID:       "test.system",
		Category: CategorySystemBase,
		Roles:    []Role{RoleDeveloper},
		Content:  "SYSTEM-CONTENT",
	})
	r.Register(&Fragment{
		ID:       "test.user",
		Category: CategoryUserPrompt,
		Roles:    []Role{RoleDeveloper},
		UserPrompt: func(_ *AssemblyContext) (string, error) {
			return "USER-CONTENT", nil
		},
	})

	a := NewAssembler(r)
	out := a.Assemble(&AssemblyContext{
		Role:     RoleDeveloper,
		Provider: ProviderOpenAI,
	})

	if !strings.Contains(out.SystemMessage, "SYSTEM-CONTENT") {
		t.Errorf("system message missing system fragment: %q", out.SystemMessage)
	}
	if strings.Contains(out.SystemMessage, "USER-CONTENT") {
		t.Errorf("user-prompt content leaked into system message: %q", out.SystemMessage)
	}
	if out.UserMessage != "USER-CONTENT" {
		t.Errorf("UserMessage = %q, want %q", out.UserMessage, "USER-CONTENT")
	}
	if out.UserPromptID != "test.user" {
		t.Errorf("UserPromptID = %q, want %q", out.UserPromptID, "test.user")
	}
	if out.RenderError != nil {
		t.Errorf("unexpected RenderError: %v", out.RenderError)
	}
}

// TestUserPrompt_NoFragmentEmptyMessage covers the back-compat path: roles
// without a registered user-prompt fragment still get a system message and
// their callers (which build their own user prompt today) keep working.
func TestUserPrompt_NoFragmentEmptyMessage(t *testing.T) {
	r := NewRegistry()
	r.Register(&Fragment{
		ID:       "test.system-only",
		Category: CategorySystemBase,
		Roles:    []Role{RolePlanner},
		Content:  "system-only role",
	})

	a := NewAssembler(r)
	out := a.Assemble(&AssemblyContext{
		Role:     RolePlanner,
		Provider: ProviderOpenAI,
	})

	if out.UserMessage != "" {
		t.Errorf("expected empty UserMessage when no fragment registered, got %q", out.UserMessage)
	}
	if out.UserPromptID != "" {
		t.Errorf("expected empty UserPromptID, got %q", out.UserPromptID)
	}
	if out.RenderError != nil {
		t.Errorf("unexpected RenderError on no-fragment path: %v", out.RenderError)
	}
	if out.SystemMessage == "" {
		t.Error("system message should still render for system-only roles")
	}
}

// TestUserPrompt_RenderError surfaces render failures via AssembledPrompt.RenderError
// instead of panicking — callers that opt into the user-prompt path can fail
// loudly, but the assembler itself stays panic-free for a render-time mistake.
func TestUserPrompt_RenderError(t *testing.T) {
	r := NewRegistry()
	r.Register(&Fragment{
		ID:       "test.broken",
		Category: CategoryUserPrompt,
		Roles:    []Role{RoleDeveloper},
		UserPrompt: func(_ *AssemblyContext) (string, error) {
			return "", errors.New("boom")
		},
	})

	a := NewAssembler(r)
	out := a.Assemble(&AssemblyContext{Role: RoleDeveloper, Provider: ProviderOpenAI})

	if out.RenderError == nil {
		t.Fatal("expected RenderError to surface, got nil")
	}
	if !strings.Contains(out.RenderError.Error(), "boom") {
		t.Errorf("RenderError should wrap the original cause, got %q", out.RenderError)
	}
	if out.UserMessage != "" {
		t.Errorf("UserMessage should be empty when render fails, got %q", out.UserMessage)
	}
}

// TestUserPrompt_NilUserPromptFunc enforces that fragments in
// CategoryUserPrompt MUST implement UserPrompt. A nil render function is a
// registration bug — surfacing it as RenderError keeps the assembler total
// while making the bug discoverable.
func TestUserPrompt_NilUserPromptFunc(t *testing.T) {
	r := NewRegistry()
	r.Register(&Fragment{
		ID:       "test.no-render",
		Category: CategoryUserPrompt,
		Roles:    []Role{RoleDeveloper},
		// UserPrompt deliberately nil
	})

	a := NewAssembler(r)
	out := a.Assemble(&AssemblyContext{Role: RoleDeveloper, Provider: ProviderOpenAI})

	if out.RenderError == nil {
		t.Fatal("expected RenderError for nil UserPrompt func")
	}
	if !strings.Contains(out.RenderError.Error(), "nil UserPrompt") {
		t.Errorf("error should name the missing render func, got %q", out.RenderError)
	}
}

// TestRegistry_DuplicateUserPromptPanics is the structural guarantee. Two
// CategoryUserPrompt fragments for the same role must panic at registration
// — that's the dial-#1 footgun (silent dual-pattern user-prompt builders)
// made structurally impossible.
func TestRegistry_DuplicateUserPromptPanics(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected panic on duplicate CategoryUserPrompt for same role")
		}
		msg, ok := recovered.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", recovered, recovered)
		}
		if !strings.Contains(msg, "duplicate CategoryUserPrompt") {
			t.Errorf("panic message should name the duplicate-user-prompt rule, got %q", msg)
		}
	}()

	r := NewRegistry()
	r.Register(&Fragment{
		ID:         "test.first",
		Category:   CategoryUserPrompt,
		Roles:      []Role{RoleDeveloper},
		UserPrompt: func(*AssemblyContext) (string, error) { return "first", nil },
	})
	r.Register(&Fragment{
		ID:         "test.second",
		Category:   CategoryUserPrompt,
		Roles:      []Role{RoleDeveloper},
		UserPrompt: func(*AssemblyContext) (string, error) { return "second", nil },
	})
}

// TestRegistry_DuplicateUserPromptDifferentRoles_OK confirms the constraint
// is *per role*, not global — two different roles can each have their own
// user-prompt fragment.
func TestRegistry_DuplicateUserPromptDifferentRoles_OK(t *testing.T) {
	r := NewRegistry()
	r.Register(&Fragment{
		ID:         "test.dev",
		Category:   CategoryUserPrompt,
		Roles:      []Role{RoleDeveloper},
		UserPrompt: func(*AssemblyContext) (string, error) { return "dev", nil },
	})
	r.Register(&Fragment{
		ID:         "test.plan",
		Category:   CategoryUserPrompt,
		Roles:      []Role{RolePlanner},
		UserPrompt: func(*AssemblyContext) (string, error) { return "plan", nil },
	})
	if got := r.UserPromptFragmentFor(RoleDeveloper); got == nil || got.ID != "test.dev" {
		t.Errorf("expected dev user-prompt fragment, got %+v", got)
	}
	if got := r.UserPromptFragmentFor(RolePlanner); got == nil || got.ID != "test.plan" {
		t.Errorf("expected planner user-prompt fragment, got %+v", got)
	}
	if got := r.UserPromptFragmentFor(RoleArchitect); got != nil {
		t.Errorf("unregistered role should return nil, got %+v", got)
	}
}

// TestRegistry_RegisterAllUserPromptDuplicate covers the same uniqueness
// guarantee through the bulk-registration API. RegisterAll is the primary
// path domain registries use; it must enforce the same rule.
func TestRegistry_RegisterAllUserPromptDuplicate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected RegisterAll to panic on duplicate user-prompt")
		}
	}()
	r := NewRegistry()
	r.RegisterAll(
		&Fragment{
			ID: "test.a", Category: CategoryUserPrompt, Roles: []Role{RoleDeveloper},
			UserPrompt: func(*AssemblyContext) (string, error) { return "", nil },
		},
		&Fragment{
			ID: "test.b", Category: CategoryUserPrompt, Roles: []Role{RoleDeveloper},
			UserPrompt: func(*AssemblyContext) (string, error) { return "", nil },
		},
	)
}

// TestRegistry_ReregisterSameIDReplaces lets a domain re-register the same
// fragment ID without tripping the uniqueness check — that's how prompt
// edits reload during dev. Different IDs claiming the same user-prompt slot
// is the actual bug; same-ID replacement is fine.
func TestRegistry_ReregisterSameIDReplaces(t *testing.T) {
	r := NewRegistry()
	r.Register(&Fragment{
		ID: "test.replaceable", Category: CategoryUserPrompt, Roles: []Role{RoleDeveloper},
		UserPrompt: func(*AssemblyContext) (string, error) { return "v1", nil },
	})
	// Same ID, new content — not a duplicate, just an update.
	r.Register(&Fragment{
		ID: "test.replaceable", Category: CategoryUserPrompt, Roles: []Role{RoleDeveloper},
		UserPrompt: func(*AssemblyContext) (string, error) { return "v2", nil },
	})

	a := NewAssembler(r)
	out := a.Assemble(&AssemblyContext{Role: RoleDeveloper, Provider: ProviderOpenAI})
	if out.UserMessage != "v2" {
		t.Errorf("re-registration should replace, got %q", out.UserMessage)
	}
}
