package domain

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
)

// TestRenderPlannerPrompt_ProjectFileTreeInjection pins the bug-#7 fix from
// the 2026-05-03 /health postmortem: when a project file tree is provided,
// it MUST appear at the top of the user prompt with the grounding rule, on
// both fresh and revision paths. Greenfield-safe: empty tree is silently
// omitted.
func TestRenderPlannerPrompt_ProjectFileTreeInjection(t *testing.T) {
	tests := []struct {
		name        string
		ctx         *prompt.PlannerPromptContext
		mustContain []string
		mustNotHave []string
	}{
		{
			name: "fresh creation with tree",
			ctx: &prompt.PlannerPromptContext{
				Title:           "Add /health endpoint",
				ProjectFileTree: "main.go\ngo.mod\ninternal/auth/auth.go",
			},
			mustContain: []string{
				"## Project Files",
				"git ls-files",
				"main.go\ngo.mod\ninternal/auth/auth.go",
				"Hallucinating directories",
				"**Title:** Add /health endpoint",
			},
		},
		{
			name: "revision with tree",
			ctx: &prompt.PlannerPromptContext{
				IsRevision:       true,
				PreviousPlanJSON: `{"goal":"X"}`,
				RevisionPrompt:   "## REVISION REQUEST\n\nFix scope.",
				ProjectFileTree:  "main.go\ngo.mod",
			},
			mustContain: []string{
				"## Project Files",
				"main.go\ngo.mod",
				"## Your Previous Plan Output",
				"## REVISION REQUEST",
			},
		},
		{
			name: "fresh creation without tree (greenfield)",
			ctx: &prompt.PlannerPromptContext{
				Title:           "Bootstrap new service",
				ProjectFileTree: "",
			},
			mustContain: []string{
				"**Title:** Bootstrap new service",
			},
			mustNotHave: []string{
				"## Project Files",
				"git ls-files",
			},
		},
		{
			name: "revision without tree",
			ctx: &prompt.PlannerPromptContext{
				IsRevision:     true,
				RevisionPrompt: "## REVISION REQUEST\n\nFix scope.",
			},
			mustContain: []string{
				"## REVISION REQUEST",
			},
			mustNotHave: []string{
				"## Project Files",
			},
		},
		{
			name: "tree appears BEFORE title (so model reads grounding first)",
			ctx: &prompt.PlannerPromptContext{
				Title:           "Add /health",
				ProjectFileTree: "main.go",
			},
			mustContain: []string{},
			// Order assertion below.
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderPlannerPrompt(tt.ctx)
			for _, want := range tt.mustContain {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\n--- got ---\n%s", want, got)
				}
			}
			for _, banned := range tt.mustNotHave {
				if strings.Contains(got, banned) {
					t.Errorf("output should NOT contain %q\n--- got ---\n%s", banned, got)
				}
			}
		})
	}

	// Order check: tree must precede the title section.
	t.Run("tree precedes title", func(t *testing.T) {
		got := renderPlannerPrompt(&prompt.PlannerPromptContext{
			Title:           "Add /health",
			ProjectFileTree: "main.go",
		})
		treeIdx := strings.Index(got, "## Project Files")
		titleIdx := strings.Index(got, "**Title:**")
		if treeIdx < 0 || titleIdx < 0 || treeIdx > titleIdx {
			t.Errorf("tree (%d) must precede title (%d)\n--- got ---\n%s", treeIdx, titleIdx, got)
		}
	})
}

func TestRenderRequirementGeneratorPrompt_FreshGeneration(t *testing.T) {
	got := renderRequirementGeneratorPrompt(&prompt.RequirementGeneratorContext{
		Title:           "Add /goodbye endpoint",
		Goal:            "Implement a /goodbye HTTP endpoint that returns JSON.",
		Context:         "The service currently has /hello but no /goodbye.",
		ScopeInclude:    []string{"api/app.py", "api/test_app.py"},
		ScopeExclude:    []string{"docs/"},
		ScopeDoNotTouch: []string{"README.md"},
	})

	mustContain := []string{
		"## Plan to Decompose",
		"**Title**: Add /goodbye endpoint",
		"**Goal**: Implement a /goodbye HTTP endpoint",
		"**Context**: The service currently has /hello",
		"**Scope Include**: api/app.py, api/test_app.py",
		"**Scope Exclude**: docs/",
		"**Do Not Touch**: README.md",
		"Extract testable requirements from the above plan",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("rendered prompt missing %q\n--- prompt ---\n%s", want, got)
		}
	}

	mustNotContain := []string{
		"Existing Approved Requirements",
		"Rejected Requirements",
		"Previous Attempt Failed",
		"Previous Review Findings",
	}
	for _, dont := range mustNotContain {
		if strings.Contains(got, dont) {
			t.Errorf("fresh-generation prompt should not contain %q\n--- prompt ---\n%s", dont, got)
		}
	}
}

func TestRenderRequirementGeneratorPrompt_PartialRegen(t *testing.T) {
	got := renderRequirementGeneratorPrompt(&prompt.RequirementGeneratorContext{
		Title: "Add /goodbye endpoint",
		Goal:  "Implement a /goodbye HTTP endpoint that returns JSON.",
		ExistingRequirements: []prompt.ExistingRequirementSummary{
			{
				ID:         "requirement.foo.1",
				Title:      "Goodbye endpoint returns JSON",
				Status:     "active",
				FilesOwned: []string{"api/handlers/goodbye.go"},
				DependsOn:  []string{"requirement.foo.2"},
			},
			{
				// Inactive requirements must NOT be surfaced to the LLM —
				// matches the legacy builder's status filter so the agent
				// can't accidentally depend on a deprecated requirement.
				ID:     "requirement.foo.deprecated",
				Title:  "Old approach",
				Status: "deprecated",
			},
		},
		ReplaceRequirementIDs: []string{"requirement.foo.3", "requirement.foo.4"},
		RejectionReasons: map[string]string{
			"requirement.foo.3": "scope was too broad",
			// requirement.foo.4 has no reason → falls through to placeholder
		},
	})

	mustContain := []string{
		"## Existing Approved Requirements",
		"requirement.foo.1",
		"Goodbye endpoint returns JSON",
		"files_owned: api/handlers/goodbye.go",
		"depends_on: requirement.foo.2",
		"## Rejected Requirements",
		"requirement.foo.3: rejected because: scope was too broad",
		"requirement.foo.4: rejected because: no reason provided",
		"Generate ONLY replacement requirements for the rejected IDs above",
		"do NOT claim a path already in any kept requirement",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("partial-regen prompt missing %q\n--- prompt ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "requirement.foo.deprecated") {
		t.Errorf("inactive requirement leaked into prompt — status filter regressed\n--- prompt ---\n%s", got)
	}
	if strings.Contains(got, "Old approach") {
		t.Errorf("inactive requirement title leaked\n--- prompt ---\n%s", got)
	}
}

func TestRenderRequirementGeneratorPrompt_PreviousErrorAndReviewFindings(t *testing.T) {
	got := renderRequirementGeneratorPrompt(&prompt.RequirementGeneratorContext{
		Title:          "x",
		Goal:           "y",
		PreviousError:  "could not parse JSON: unexpected token",
		ReviewFindings: "- requirement 2 missing scenarios",
	})
	if !strings.Contains(got, "## Previous Attempt Failed") {
		t.Errorf("previous-error section missing")
	}
	if !strings.Contains(got, "could not parse JSON: unexpected token") {
		t.Errorf("previous error text missing")
	}
	if !strings.Contains(got, "## Previous Review Findings") {
		t.Errorf("review-findings section missing")
	}
	if !strings.Contains(got, "requirement 2 missing scenarios") {
		t.Errorf("review findings text missing")
	}
}

// TestAssemblerEndToEnd_RequirementGenerator pins the full pipeline: register
// the Software() fragments, set the typed RequirementGenerator context, and
// confirm Assemble produces the expected user message via the registry path.
// This is the contract the requirement-generator component is about to
// migrate to.
func TestAssemblerEndToEnd_RequirementGenerator(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	a := prompt.NewAssembler(r)

	out := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleRequirementGenerator,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"submit_work", "graph_summary"},
		RequirementGenerator: &prompt.RequirementGeneratorContext{
			Title: "Add /goodbye endpoint",
			Goal:  "Return a goodbye message as JSON",
		},
	})

	if out.RenderError != nil {
		t.Fatalf("unexpected RenderError: %v", out.RenderError)
	}
	if out.UserPromptID != "software.requirement-generator.user-prompt" {
		t.Errorf("UserPromptID = %q, want the registry's requirement-generator user-prompt fragment", out.UserPromptID)
	}
	if !strings.Contains(out.UserMessage, "Add /goodbye endpoint") {
		t.Errorf("user message missing plan title: %q", out.UserMessage)
	}
	if !strings.Contains(out.SystemMessage, "files_owned") {
		t.Errorf("system message missing dial-#1 partition guidance — fragment ordering regressed")
	}
	// Belt-and-suspenders: user-message content must NOT leak into system
	// message (the whole reason CategoryUserPrompt exists).
	if strings.Contains(out.SystemMessage, "Add /goodbye endpoint") {
		t.Errorf("user-prompt content leaked into system message — assembler bug")
	}
}
