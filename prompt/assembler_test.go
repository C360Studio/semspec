package prompt

import (
	"fmt"
	"strings"
	"testing"
)

func TestAssemblerBasic(t *testing.T) {
	r := NewRegistry()
	r.RegisterAll(
		&Fragment{ID: "identity", Category: CategorySystemBase, Content: "You are a developer."},
		&Fragment{ID: "tools", Category: CategoryToolDirective, Content: "You must use tools."},
	)

	a := NewAssembler(r)
	result := a.Assemble(&AssemblyContext{Role: RoleDeveloper})

	if !strings.Contains(result.SystemMessage, "You are a developer.") {
		t.Error("expected system message to contain identity")
	}
	if !strings.Contains(result.SystemMessage, "You must use tools.") {
		t.Error("expected system message to contain tool directive")
	}
	if len(result.FragmentsUsed) != 2 {
		t.Errorf("expected 2 fragments used, got %d", len(result.FragmentsUsed))
	}
}

func TestAssemblerAnthropicXML(t *testing.T) {
	r := NewRegistry()
	r.Register(&Fragment{ID: "id", Category: CategorySystemBase, Content: "You are a developer."})

	a := NewAssembler(r)
	result := a.Assemble(&AssemblyContext{Provider: ProviderAnthropic})

	if !strings.Contains(result.SystemMessage, "<identity>") {
		t.Error("expected XML tags for Anthropic")
	}
	if !strings.Contains(result.SystemMessage, "</identity>") {
		t.Error("expected closing XML tag for Anthropic")
	}
}

func TestAssemblerOpenAIMarkdown(t *testing.T) {
	r := NewRegistry()
	r.Register(&Fragment{ID: "id", Category: CategorySystemBase, Content: "You are a developer."})

	a := NewAssembler(r)
	result := a.Assemble(&AssemblyContext{Provider: ProviderOpenAI})

	if !strings.Contains(result.SystemMessage, "## Identity") {
		t.Error("expected markdown header for OpenAI")
	}
}

func TestAssemblerDefaultFormat(t *testing.T) {
	r := NewRegistry()
	r.Register(&Fragment{ID: "id", Category: CategorySystemBase, Content: "You are a developer."})

	a := NewAssembler(r)
	result := a.Assemble(&AssemblyContext{Provider: Provider("unknown")})

	if !strings.Contains(result.SystemMessage, "Identity:\n") {
		t.Error("expected plain label for unknown provider")
	}
}

func TestAssemblerContentFunc(t *testing.T) {
	r := NewRegistry()
	r.Register(&Fragment{
		ID:       "dynamic",
		Category: CategoryToolGuidance,
		ContentFunc: func(ctx *AssemblyContext) string {
			return "Tools: " + strings.Join(ctx.AvailableTools, ", ")
		},
	})

	a := NewAssembler(r)
	result := a.Assemble(&AssemblyContext{
		AvailableTools: []string{"file_read", "file_write"},
	})

	if !strings.Contains(result.SystemMessage, "Tools: file_read, file_write") {
		t.Error("expected dynamic content from ContentFunc")
	}
}

func TestAssemblerEmptyContentFunc(t *testing.T) {
	r := NewRegistry()
	r.Register(&Fragment{
		ID:       "empty",
		Category: CategoryToolGuidance,
		ContentFunc: func(_ *AssemblyContext) string {
			return ""
		},
	})

	a := NewAssembler(r)
	result := a.Assemble(&AssemblyContext{})

	if result.SystemMessage != "" {
		t.Error("expected empty system message when all content is empty")
	}
}

func TestAssemblerCategoryOrdering(t *testing.T) {
	r := NewRegistry()
	r.RegisterAll(
		&Fragment{ID: "gap", Category: CategoryGapDetection, Content: "GAPS"},
		&Fragment{ID: "base", Category: CategorySystemBase, Content: "BASE"},
		&Fragment{ID: "output", Category: CategoryOutputFormat, Content: "OUTPUT"},
	)

	a := NewAssembler(r)
	result := a.Assemble(&AssemblyContext{Provider: ProviderOpenAI})

	baseIdx := strings.Index(result.SystemMessage, "BASE")
	outputIdx := strings.Index(result.SystemMessage, "OUTPUT")
	gapIdx := strings.Index(result.SystemMessage, "GAPS")

	if baseIdx > outputIdx || outputIdx > gapIdx {
		t.Error("fragments should appear in category order: base < output < gap")
	}
}

func TestAssemblerRoleFiltering(t *testing.T) {
	r := NewRegistry()
	r.RegisterAll(
		&Fragment{ID: "shared", Category: CategorySystemBase, Content: "shared"},
		&Fragment{ID: "dev", Category: CategoryRoleContext, Content: "dev only", Roles: []Role{RoleDeveloper}},
		&Fragment{ID: "rev", Category: CategoryRoleContext, Content: "rev only", Roles: []Role{RoleReviewer}},
	)

	a := NewAssembler(r)
	result := a.Assemble(&AssemblyContext{Role: RoleDeveloper, Provider: ProviderOpenAI})

	if !strings.Contains(result.SystemMessage, "dev only") {
		t.Error("expected developer fragment")
	}
	if strings.Contains(result.SystemMessage, "rev only") {
		t.Error("should not contain reviewer fragment")
	}
}

func TestAssemblerErrorTrendInjection(t *testing.T) {
	r := NewRegistry()
	// Register the error trends fragment with the same pattern as software.go.
	r.Register(&Fragment{
		ID:       "error-trends",
		Category: CategoryPeerFeedback,
		Roles:    []Role{RoleDeveloper},
		Condition: func(ctx *AssemblyContext) bool {
			return ctx.TaskContext != nil && len(ctx.TaskContext.ErrorTrends) > 0
		},
		ContentFunc: func(ctx *AssemblyContext) string {
			var sb strings.Builder
			sb.WriteString("RECURRING ISSUES:\n")
			for _, trend := range ctx.TaskContext.ErrorTrends {
				sb.WriteString(fmt.Sprintf("- %s (%d): %s\n", trend.Label, trend.Count, trend.Guidance))
			}
			return sb.String()
		},
	})

	a := NewAssembler(r)

	t.Run("trends present", func(t *testing.T) {
		result := a.Assemble(&AssemblyContext{
			Role:     RoleDeveloper,
			Provider: ProviderOpenAI,
			TaskContext: &TaskContext{
				ErrorTrends: []ErrorTrend{
					{CategoryID: "missing_tests", Label: "Missing Tests", Count: 3, Guidance: "Create test files."},
					{CategoryID: "wrong_pattern", Label: "Wrong Pattern", Count: 2, Guidance: "Follow conventions."},
				},
			},
		})

		if !strings.Contains(result.SystemMessage, "RECURRING ISSUES") {
			t.Error("expected error trends section in system message")
		}
		if !strings.Contains(result.SystemMessage, "Missing Tests (3)") {
			t.Error("expected 'Missing Tests (3)' in system message")
		}
		if !strings.Contains(result.SystemMessage, "Wrong Pattern (2)") {
			t.Error("expected 'Wrong Pattern (2)' in system message")
		}
		if !strings.Contains(result.SystemMessage, "Create test files.") {
			t.Error("expected guidance text in system message")
		}
	})

	t.Run("no trends", func(t *testing.T) {
		result := a.Assemble(&AssemblyContext{
			Role:     RoleDeveloper,
			Provider: ProviderOpenAI,
			TaskContext: &TaskContext{
				ErrorTrends: nil,
			},
		})

		if strings.Contains(result.SystemMessage, "RECURRING ISSUES") {
			t.Error("should NOT contain error trends section when no trends")
		}
	})

	t.Run("nil task context", func(t *testing.T) {
		result := a.Assemble(&AssemblyContext{
			Role:     RoleDeveloper,
			Provider: ProviderOpenAI,
		})

		if strings.Contains(result.SystemMessage, "RECURRING ISSUES") {
			t.Error("should NOT contain error trends section with nil TaskContext")
		}
	})
}

func TestFormatSection(t *testing.T) {
	tests := []struct {
		name     string
		style    ProviderStyle
		contains string
	}{
		{"xml", ProviderStyle{PreferXML: true}, "<test>\ncontent\n</test>"},
		{"markdown", ProviderStyle{PreferMarkdown: true}, "## Test\ncontent"},
		{"default", ProviderStyle{}, "Test:\ncontent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatSection("Test", "content", tt.style)
			if result != tt.contains {
				t.Errorf("expected %q, got %q", tt.contains, result)
			}
		})
	}
}
