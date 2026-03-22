package prompt

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// Integration tests exercise the full prompt assembly pipeline:
// domain registration → tool filtering → tool guidance → assembler → formatted output.
// These complement the unit tests in assembler_test.go, tools_test.go, etc.
//
// Run only integration tests: go test ./prompt/ -run TestIntegration

// allSemspecTools simulates the full tool list from agentictools.ListRegisteredTools().
var allSemspecTools = []string{
	"bash", "submit_work", "ask_question",
	"graph_search", "graph_query", "graph_summary",
	"web_search", "http_request",
	"review_scenario",
	"decompose_task", "spawn_agent",
}

// softwareFragments returns the software domain fragment set.
// This is a test helper; in production, processor code calls domain.Software().
// See prompt/domain/integration_test.go for tests using real production fragments.
func softwareFragments() []*Fragment {
	// Minimal set covering all roles. Matches structure of domain/software.go.
	return []*Fragment{
		{ID: "sw.developer.system-base", Category: CategorySystemBase, Roles: []Role{RoleDeveloper}, Content: "You are a developer implementing code changes."},
		{ID: "sw.developer.tool-directive", Category: CategoryToolDirective, Roles: []Role{RoleDeveloper}, Content: "CRITICAL: You MUST use bash to create or modify files."},
		{ID: "sw.developer.role-context", Category: CategoryRoleContext, Roles: []Role{RoleDeveloper}, Content: "Gather context before writing code. Follow SOPs."},
		{ID: "sw.developer.output-format", Category: CategoryOutputFormat, Roles: []Role{RoleDeveloper}, Content: `Output JSON: {"result": "...", "files_modified": [...]}`},
		{ID: "sw.developer.retry-directive", Category: CategoryToolDirective, Priority: 1, Roles: []Role{RoleDeveloper},
			Condition: func(ctx *AssemblyContext) bool { return ctx.TaskContext != nil && ctx.TaskContext.Feedback != "" },
			ContentFunc: func(ctx *AssemblyContext) string {
				return "RETRY: Address feedback: " + ctx.TaskContext.Feedback
			},
		},
		{ID: "sw.developer.task-context", Category: CategoryDomainContext, Roles: []Role{RoleDeveloper},
			Condition:   func(ctx *AssemblyContext) bool { return ctx.TaskContext != nil },
			ContentFunc: func(ctx *AssemblyContext) string { return "Task: " + ctx.TaskContext.Task.ID },
		},
		{ID: "sw.planner.system-base", Category: CategorySystemBase, Roles: []Role{RolePlanner}, Content: "You are finalizing a development plan."},
		{ID: "sw.planner.output-format", Category: CategoryOutputFormat, Roles: []Role{RolePlanner}, Content: `Respond with JSON: {"status": "committed", ...}`},
		{ID: "sw.planner.role-context", Category: CategoryRoleContext, Roles: []Role{RolePlanner}, Content: "Validate plan completeness. Ask questions if critical info missing."},
		{ID: "sw.planner.plan-context", Category: CategoryDomainContext, Roles: []Role{RolePlanner},
			Condition: func(ctx *AssemblyContext) bool { return ctx.PlanContext != nil },
			ContentFunc: func(ctx *AssemblyContext) string {
				var sb strings.Builder
				sb.WriteString("Plan: " + ctx.PlanContext.Title)
				if ctx.PlanContext.IsRevision {
					sb.WriteString("\nREVISION: " + ctx.PlanContext.ReviewSummary)
				}
				return sb.String()
			},
		},
		{ID: "sw.plan-reviewer.system-base", Category: CategorySystemBase, Roles: []Role{RolePlanReviewer}, Content: "You are a plan reviewer validating plans against SOPs."},
		{ID: "sw.plan-reviewer.role-context", Category: CategoryRoleContext, Roles: []Role{RolePlanReviewer}, Content: "Review each SOP. Produce verdict with findings."},
		{ID: "sw.plan-reviewer.output-format", Category: CategoryOutputFormat, Roles: []Role{RolePlanReviewer}, Content: `{"verdict": "approved" | "needs_changes"}`},
		{ID: "sw.plan-reviewer.review-context", Category: CategoryDomainContext, Roles: []Role{RolePlanReviewer},
			Condition: func(ctx *AssemblyContext) bool { return ctx.ReviewContext != nil },
			ContentFunc: func(ctx *AssemblyContext) string {
				return "Reviewing plan: " + ctx.ReviewContext.PlanSlug
			},
		},
		{ID: "sw.task-reviewer.system-base", Category: CategorySystemBase, Roles: []Role{RoleTaskReviewer}, Content: "You are a task reviewer validating generated tasks."},
		{ID: "sw.task-reviewer.output-format", Category: CategoryOutputFormat, Roles: []Role{RoleTaskReviewer}, Content: `{"verdict": "approved" | "needs_changes"}`},
		{ID: "sw.reviewer.system-base", Category: CategorySystemBase, Roles: []Role{RoleReviewer}, Content: "You are a code reviewer checking production readiness."},
		{ID: "sw.reviewer.role-context", Category: CategoryRoleContext, Roles: []Role{RoleReviewer}, Content: "Gather SOPs. Check every requirement. READ-ONLY access."},
		{ID: "sw.reviewer.output-format", Category: CategoryOutputFormat, Roles: []Role{RoleReviewer}, Content: `{"verdict": "approved" | "rejected"}`},
		{ID: "sw.req-gen.system-base", Category: CategorySystemBase, Roles: []Role{RoleRequirementGenerator}, Content: "You are extracting requirements from a plan."},
		{ID: "sw.scenario-gen.system-base", Category: CategorySystemBase, Roles: []Role{RoleScenarioGenerator}, Content: "You are generating BDD scenarios."},
		{ID: "sw.task-gen.system-base", Category: CategorySystemBase, Roles: []Role{RoleTaskGenerator}, Content: "You are generating development tasks."},
		{ID: "sw.plan-coordinator.system-base", Category: CategorySystemBase, Roles: []Role{RolePlanCoordinator}, Content: "You are a planning coordinator spawning focused planners."},
		{ID: "sw.gap-detection", Category: CategoryGapDetection, Content: "If you encounter knowledge gaps, use <gap> XML blocks."},
	}
}

// researchFragments returns the research domain fragment set.
func researchFragments() []*Fragment {
	return []*Fragment{
		{ID: "res.analyst.system-base", Category: CategorySystemBase, Roles: []Role{RoleDeveloper}, Content: "You are a research analyst investigating through evidence."},
		{ID: "res.analyst.output-format", Category: CategoryOutputFormat, Roles: []Role{RoleDeveloper}, Content: `{"findings": [...], "confidence": "HIGH"}`},
		{ID: "res.synthesizer.system-base", Category: CategorySystemBase, Roles: []Role{RolePlanner}, Content: "You are synthesizing findings from multiple sources."},
		{ID: "res.reviewer.system-base", Category: CategorySystemBase, Roles: []Role{RoleReviewer}, Content: "You are peer-reviewing research findings."},
		{ID: "res.gap-detection", Category: CategoryGapDetection, Content: "Flag evidence gaps with <gap> blocks."},
	}
}

// buildPipeline constructs the full prompt pipeline for a domain.
func buildPipeline(fragments []*Fragment) *Assembler {
	r := NewRegistry()
	r.RegisterAll(fragments...)
	r.Register(ToolGuidanceFragment(DefaultToolGuidance()))
	return NewAssembler(r)
}

// TestIntegrationAllRoles verifies each role gets a non-empty, correctly formatted
// prompt through the full pipeline with tool filtering and tool guidance.
func TestIntegrationAllRoles(t *testing.T) {
	t.Parallel()
	assembler := buildPipeline(softwareFragments())

	tests := []struct {
		role          Role
		provider      Provider
		wantIdentity  string
		wantFormat    string // provider formatting marker
		wantNoLeak    string // content that must NOT appear
		toolsExpected int    // minimum tools after filtering
	}{
		{RoleDeveloper, ProviderAnthropic, "developer implementing code", "<identity>", "plan reviewer", 2},
		{RolePlanner, ProviderOpenAI, "finalizing a development plan", "## Identity", "file_write", 1},
		{RoleReviewer, ProviderOllama, "code reviewer", "## Identity", "file_write", 2},
		{RolePlanReviewer, ProviderAnthropic, "plan reviewer", "<identity>", "developer implementing", 0},
		{RoleTaskReviewer, ProviderOpenAI, "task reviewer", "## Identity", "developer implementing", 0},
		{RoleRequirementGenerator, ProviderAnthropic, "extracting requirements", "<identity>", "code reviewer", 0},
		{RoleScenarioGenerator, ProviderOpenAI, "BDD scenarios", "## Identity", "code reviewer", 0},
		{RoleTaskGenerator, ProviderOllama, "generating development tasks", "## Identity", "code reviewer", 0},
		{RolePlanCoordinator, ProviderOpenAI, "planning coordinator", "## Identity", "code reviewer", 0},
	}

	for _, tt := range tests {
		t.Run(string(tt.role)+"/"+string(tt.provider), func(t *testing.T) {
			t.Parallel()
			tools := FilterTools(allSemspecTools, tt.role)
			if len(tools) < tt.toolsExpected {
				t.Errorf("expected at least %d tools for %s, got %d", tt.toolsExpected, tt.role, len(tools))
			}

			result := assembler.Assemble(&AssemblyContext{
				Role:           tt.role,
				Provider:       tt.provider,
				AvailableTools: tools,
				SupportsTools:  true,
			})

			if result.SystemMessage == "" {
				t.Fatal("expected non-empty system message")
			}
			if !strings.Contains(result.SystemMessage, tt.wantIdentity) {
				t.Errorf("expected identity %q in prompt for %s", tt.wantIdentity, tt.role)
			}
			if !strings.Contains(result.SystemMessage, tt.wantFormat) {
				t.Errorf("expected provider format marker %q for %s/%s", tt.wantFormat, tt.role, tt.provider)
			}
			if tt.wantNoLeak != "" && strings.Contains(result.SystemMessage, tt.wantNoLeak) {
				t.Errorf("prompt for %s should not contain %q (fragment leakage)", tt.role, tt.wantNoLeak)
			}
			if len(result.FragmentsUsed) == 0 {
				t.Error("expected at least one fragment used")
			}
		})
	}
}

// TestIntegrationDomainSwitching verifies the same role gets different prompts
// when assembled under software vs research domain.
func TestIntegrationDomainSwitching(t *testing.T) {
	t.Parallel()
	swAssembler := buildPipeline(softwareFragments())
	resAssembler := buildPipeline(researchFragments())

	tools := FilterTools(allSemspecTools, RoleDeveloper)

	swResult := swAssembler.Assemble(&AssemblyContext{
		Role:           RoleDeveloper,
		Provider:       ProviderAnthropic,
		AvailableTools: tools,
		SupportsTools:  true,
	})
	resResult := resAssembler.Assemble(&AssemblyContext{
		Role:           RoleDeveloper,
		Provider:       ProviderAnthropic,
		AvailableTools: tools,
		SupportsTools:  true,
	})

	if swResult.SystemMessage == resResult.SystemMessage {
		t.Fatal("software and research domains should produce different prompts")
	}
	if !strings.Contains(swResult.SystemMessage, "developer implementing code") {
		t.Error("software domain should use developer identity")
	}
	if !strings.Contains(resResult.SystemMessage, "research analyst") {
		t.Error("research domain should use analyst identity")
	}

	// Both should have gap detection
	if !strings.Contains(swResult.SystemMessage, "gap") {
		t.Error("software domain should include gap detection")
	}
	if !strings.Contains(resResult.SystemMessage, "gap") {
		t.Error("research domain should include gap detection")
	}
}

// TestIntegrationProviderFormatComparison verifies the same role+domain produces
// structurally different output for each provider (XML, Markdown, plain).
func TestIntegrationProviderFormatComparison(t *testing.T) {
	t.Parallel()
	assembler := buildPipeline(softwareFragments())

	providers := []struct {
		provider    Provider
		xmlMarker   string
		mdMarker    string
		plainMarker string
	}{
		{ProviderAnthropic, "<identity>", "", ""},
		{ProviderOpenAI, "", "## Identity", ""},
		{ProviderOllama, "", "## Identity", ""},
		{Provider("custom"), "", "", "Identity:\n"},
	}

	for _, p := range providers {
		t.Run(string(p.provider), func(t *testing.T) {
			t.Parallel()
			result := assembler.Assemble(&AssemblyContext{
				Role:           RoleDeveloper,
				Provider:       p.provider,
				AvailableTools: FilterTools(allSemspecTools, RoleDeveloper),
				SupportsTools:  true,
			})

			if p.xmlMarker != "" && !strings.Contains(result.SystemMessage, p.xmlMarker) {
				t.Errorf("expected XML marker %q for %s", p.xmlMarker, p.provider)
			}
			if p.mdMarker != "" && !strings.Contains(result.SystemMessage, p.mdMarker) {
				t.Errorf("expected markdown marker %q for %s", p.mdMarker, p.provider)
			}
			if p.plainMarker != "" && !strings.Contains(result.SystemMessage, p.plainMarker) {
				t.Errorf("expected plain marker %q for %s", p.plainMarker, p.provider)
			}
		})
	}
}

// TestIntegrationCategoryOrdering verifies fragments always appear in category order
// across all role/provider combinations. Checks the full category chain:
// Identity(0) < Tool Directives(100) < Role(300) < Tool Guidance(500) < Output Format(600) < Gap Detection(700).
func TestIntegrationCategoryOrdering(t *testing.T) {
	t.Parallel()
	assembler := buildPipeline(softwareFragments())

	roles := []Role{RoleDeveloper, RolePlanner, RoleReviewer, RolePlanReviewer}

	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()
			tools := FilterTools(allSemspecTools, role)
			result := assembler.Assemble(&AssemblyContext{
				Role:           role,
				Provider:       ProviderOpenAI,
				AvailableTools: tools,
				SupportsTools:  true,
			})

			msg := result.SystemMessage

			// Collect section positions; -1 means absent (which is fine for optional sections).
			identityIdx := strings.Index(msg, "## Identity")
			toolDirIdx := strings.Index(msg, "## Tool Directives")
			roleIdx := strings.Index(msg, "## Role")
			toolGuideIdx := strings.Index(msg, "## Tool Guidance")
			outputIdx := strings.Index(msg, "## Output Format")
			gapIdx := strings.Index(msg, "## Gap Detection")

			if identityIdx < 0 {
				t.Fatal("expected Identity section")
			}

			// Verify ordering for each adjacent pair of present sections.
			pairs := []struct {
				name   string
				before int
				after  int
			}{
				{"Identity < Tool Directives", identityIdx, toolDirIdx},
				{"Tool Directives < Role", toolDirIdx, roleIdx},
				{"Role < Tool Guidance", roleIdx, toolGuideIdx},
				{"Tool Guidance < Output Format", toolGuideIdx, outputIdx},
				{"Output Format < Gap Detection", outputIdx, gapIdx},
			}
			for _, p := range pairs {
				if p.before >= 0 && p.after >= 0 && p.before > p.after {
					t.Errorf("ordering violation: %s (positions %d > %d)", p.name, p.before, p.after)
				}
			}
		})
	}
}

// TestIntegrationToolFilteringAndGuidance verifies tool filtering restricts
// available tools, and tool guidance only shows guidance for available tools.
func TestIntegrationToolFilteringAndGuidance(t *testing.T) {
	t.Parallel()
	assembler := buildPipeline(softwareFragments())

	t.Run("developer sees all tool guidance", func(t *testing.T) {
		t.Parallel()
		tools := FilterTools(allSemspecTools, RoleDeveloper)
		result := assembler.Assemble(&AssemblyContext{
			Role:           RoleDeveloper,
			Provider:       ProviderOpenAI,
			AvailableTools: tools,
			SupportsTools:  true,
		})

		// Use bare tool names (not markdown-formatted) for consistent assertions.
		if !strings.Contains(result.SystemMessage, "bash") {
			t.Error("developer should see bash guidance")
		}
		if !strings.Contains(result.SystemMessage, "decompose_task") {
			t.Error("developer should see decompose_task guidance")
		}
	})

	t.Run("reviewer sees only read-only tool guidance", func(t *testing.T) {
		t.Parallel()
		tools := FilterTools(allSemspecTools, RoleReviewer)
		result := assembler.Assemble(&AssemblyContext{
			Role:           RoleReviewer,
			Provider:       ProviderOpenAI,
			AvailableTools: tools,
			SupportsTools:  true,
		})

		// file_write is not in the reviewer's filtered tool list, so no guidance appears.
		// Check using bare name — consistent with the developer assertion above.
		if strings.Contains(result.SystemMessage, "file_write") {
			t.Error("reviewer should not see file_write guidance")
		}
		if !strings.Contains(result.SystemMessage, "graph_search") {
			t.Error("reviewer should see graph_search guidance")
		}
	})

	t.Run("no guidance when single tool", func(t *testing.T) {
		t.Parallel()
		result := assembler.Assemble(&AssemblyContext{
			Role:           RoleDeveloper,
			Provider:       ProviderOpenAI,
			AvailableTools: []string{"file_read"},
			SupportsTools:  true,
		})

		// ToolGuidanceFragment condition requires > 1 tool
		if strings.Contains(result.SystemMessage, "Available tools and when to use them") {
			t.Error("should not show tool guidance with single tool")
		}
	})
}

// TestIntegrationConditionalFragmentInteractions verifies that multiple
// conditional fragments (retry, task context, plan context, review context)
// compose correctly together.
func TestIntegrationConditionalFragmentInteractions(t *testing.T) {
	t.Parallel()
	assembler := buildPipeline(softwareFragments())
	tools := FilterTools(allSemspecTools, RoleDeveloper)

	t.Run("no task context: minimal developer prompt", func(t *testing.T) {
		t.Parallel()
		result := assembler.Assemble(&AssemblyContext{
			Role:           RoleDeveloper,
			Provider:       ProviderAnthropic,
			AvailableTools: tools,
			SupportsTools:  true,
		})

		if strings.Contains(result.SystemMessage, "RETRY") {
			t.Error("should not contain retry directive without feedback")
		}
		if strings.Contains(result.SystemMessage, "Task:") {
			t.Error("should not contain task context without TaskContext")
		}
	})

	t.Run("task context without feedback: task shown, no retry", func(t *testing.T) {
		t.Parallel()
		result := assembler.Assemble(&AssemblyContext{
			Role:           RoleDeveloper,
			Provider:       ProviderAnthropic,
			AvailableTools: tools,
			SupportsTools:  true,
			TaskContext: &TaskContext{
				Task: workflow.Task{ID: "task.auth.1", Description: "Add JWT validation"},
			},
		})

		if !strings.Contains(result.SystemMessage, "Task: task.auth.1") {
			t.Error("should contain task ID")
		}
		if strings.Contains(result.SystemMessage, "RETRY") {
			t.Error("should not contain retry directive without feedback")
		}
	})

	t.Run("task context with feedback: both task and retry shown", func(t *testing.T) {
		t.Parallel()
		result := assembler.Assemble(&AssemblyContext{
			Role:           RoleDeveloper,
			Provider:       ProviderAnthropic,
			AvailableTools: tools,
			SupportsTools:  true,
			TaskContext: &TaskContext{
				Task:     workflow.Task{ID: "task.auth.1", Description: "Add JWT validation"},
				Feedback: "Missing error handling in auth middleware",
			},
		})

		if !strings.Contains(result.SystemMessage, "Task: task.auth.1") {
			t.Error("should contain task ID")
		}
		if !strings.Contains(result.SystemMessage, "RETRY") {
			t.Error("should contain retry directive with feedback")
		}
		if !strings.Contains(result.SystemMessage, "Missing error handling") {
			t.Error("should contain actual feedback text")
		}
	})

	t.Run("planner with plan context: plan details shown", func(t *testing.T) {
		t.Parallel()
		plannerTools := FilterTools(allSemspecTools, RolePlanner)
		result := assembler.Assemble(&AssemblyContext{
			Role:           RolePlanner,
			Provider:       ProviderOpenAI,
			AvailableTools: plannerTools,
			SupportsTools:  true,
			PlanContext: &PlanContext{
				Title: "Add OAuth2 support",
			},
		})

		if !strings.Contains(result.SystemMessage, "Plan: Add OAuth2 support") {
			t.Error("should contain plan title")
		}
		if strings.Contains(result.SystemMessage, "REVISION") {
			t.Error("should not contain revision marker without IsRevision")
		}
	})

	t.Run("planner with revision context: revision details shown", func(t *testing.T) {
		t.Parallel()
		plannerTools := FilterTools(allSemspecTools, RolePlanner)
		result := assembler.Assemble(&AssemblyContext{
			Role:           RolePlanner,
			Provider:       ProviderOpenAI,
			AvailableTools: plannerTools,
			SupportsTools:  true,
			PlanContext: &PlanContext{
				Title:         "Add OAuth2 support",
				IsRevision:    true,
				ReviewSummary: "Missing migration strategy for token storage",
			},
		})

		if !strings.Contains(result.SystemMessage, "Plan: Add OAuth2 support") {
			t.Error("should contain plan title")
		}
		if !strings.Contains(result.SystemMessage, "REVISION") {
			t.Error("should contain revision marker when IsRevision is true")
		}
		if !strings.Contains(result.SystemMessage, "Missing migration strategy") {
			t.Error("should contain review summary in revision")
		}
	})

	t.Run("plan-reviewer with review context: review details shown", func(t *testing.T) {
		t.Parallel()
		reviewTools := FilterTools(allSemspecTools, RolePlanReviewer)
		result := assembler.Assemble(&AssemblyContext{
			Role:           RolePlanReviewer,
			Provider:       ProviderAnthropic,
			AvailableTools: reviewTools,
			SupportsTools:  true,
			ReviewContext: &ReviewContext{
				PlanSlug: "add-oauth2-support",
			},
		})

		if !strings.Contains(result.SystemMessage, "Reviewing plan: add-oauth2-support") {
			t.Error("should contain plan slug in review context")
		}
	})
}

// TestIntegrationToolChoiceAlignment verifies that ToolChoice resolution
// produces results consistent with the tool filter output.
func TestIntegrationToolChoiceAlignment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role         Role
		wantMode     string
		wantNil      bool
		wantFunction string
	}{
		{RoleDeveloper, "required", false, ""},
		{RoleReviewer, "", true, ""},
		{RolePlanReviewer, "", true, ""},
		{RolePlanner, "", true, ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			t.Parallel()
			tools := FilterTools(allSemspecTools, tt.role)
			tc := ResolveToolChoice(tt.role, tools)

			if tt.wantNil {
				if tc != nil {
					t.Errorf("expected nil ToolChoice for %s, got %+v", tt.role, tc)
				}
				return
			}
			if tc == nil {
				t.Fatalf("expected non-nil ToolChoice for %s", tt.role)
			}
			if tc.Mode != tt.wantMode {
				t.Errorf("expected mode %q for %s, got %q", tt.wantMode, tt.role, tc.Mode)
			}
			if tt.wantFunction != "" && tc.FunctionName != tt.wantFunction {
				t.Errorf("expected function %q for %s, got %q", tt.wantFunction, tt.role, tc.FunctionName)
			}
		})
	}
}

// TestIntegrationNoFragmentLeakage systematically verifies that no role
// receives fragments intended exclusively for another role.
func TestIntegrationNoFragmentLeakage(t *testing.T) {
	t.Parallel()
	assembler := buildPipeline(softwareFragments())

	// Deterministic iteration via slice of structs (not map).
	leakChecks := []struct {
		role      Role
		forbidden []string
	}{
		{RoleDeveloper, []string{"plan reviewer", "task reviewer", "code reviewer checking", "extracting requirements", "BDD scenarios"}},
		{RolePlanner, []string{"code reviewer", "plan reviewer", "task reviewer"}},
		{RoleReviewer, []string{"plan reviewer", "finalizing a development plan"}},
		{RolePlanReviewer, []string{"code reviewer checking", "developer implementing"}},
		{RoleTaskReviewer, []string{"code reviewer checking", "developer implementing"}},
	}

	for _, lc := range leakChecks {
		t.Run(string(lc.role), func(t *testing.T) {
			t.Parallel()
			tools := FilterTools(allSemspecTools, lc.role)
			result := assembler.Assemble(&AssemblyContext{
				Role:           lc.role,
				Provider:       ProviderOpenAI,
				AvailableTools: tools,
				SupportsTools:  true,
			})

			for _, snippet := range lc.forbidden {
				if strings.Contains(result.SystemMessage, snippet) {
					t.Errorf("role %s prompt should not contain %q", lc.role, snippet)
				}
			}
		})
	}
}

// TestIntegrationGapDetectionShared verifies gap detection is present
// in every role's assembled prompt (it's not role-gated).
func TestIntegrationGapDetectionShared(t *testing.T) {
	t.Parallel()
	assembler := buildPipeline(softwareFragments())

	roles := []Role{
		RoleDeveloper, RolePlanner, RoleReviewer, RolePlanReviewer,
		RoleTaskReviewer, RoleRequirementGenerator, RoleScenarioGenerator,
		RoleTaskGenerator, RolePlanCoordinator,
	}

	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()
			result := assembler.Assemble(&AssemblyContext{
				Role:     role,
				Provider: ProviderOpenAI,
			})

			if !strings.Contains(result.SystemMessage, "gap") {
				t.Errorf("role %s should receive gap detection fragment", role)
			}
		})
	}
}

// TestIntegrationFragmentsUsedTracking verifies the observability field
// accurately tracks which fragments were included.
func TestIntegrationFragmentsUsedTracking(t *testing.T) {
	t.Parallel()
	assembler := buildPipeline(softwareFragments())

	t.Run("developer without task context", func(t *testing.T) {
		t.Parallel()
		result := assembler.Assemble(&AssemblyContext{
			Role:           RoleDeveloper,
			Provider:       ProviderOpenAI,
			AvailableTools: FilterTools(allSemspecTools, RoleDeveloper),
			SupportsTools:  true,
		})

		used := make(map[string]bool)
		for _, id := range result.FragmentsUsed {
			used[id] = true
		}

		// Must include these
		mustHave := []string{"sw.developer.system-base", "sw.developer.tool-directive", "sw.gap-detection"}
		for _, id := range mustHave {
			if !used[id] {
				t.Errorf("expected %s in FragmentsUsed", id)
			}
		}

		// Must NOT include conditional fragments
		mustNotHave := []string{"sw.developer.retry-directive", "sw.developer.task-context"}
		for _, id := range mustNotHave {
			if used[id] {
				t.Errorf("did not expect %s in FragmentsUsed (condition not met)", id)
			}
		}
	})

	t.Run("developer with task context and feedback", func(t *testing.T) {
		t.Parallel()
		result := assembler.Assemble(&AssemblyContext{
			Role:           RoleDeveloper,
			Provider:       ProviderOpenAI,
			AvailableTools: FilterTools(allSemspecTools, RoleDeveloper),
			SupportsTools:  true,
			TaskContext: &TaskContext{
				Task:     workflow.Task{ID: "task.1"},
				Feedback: "fix the tests",
			},
		})

		used := make(map[string]bool)
		for _, id := range result.FragmentsUsed {
			used[id] = true
		}

		// Conditional fragments should now be included
		if !used["sw.developer.retry-directive"] {
			t.Error("expected retry-directive in FragmentsUsed when feedback present")
		}
		if !used["sw.developer.task-context"] {
			t.Error("expected task-context in FragmentsUsed when TaskContext present")
		}
	})
}

// TestIntegrationSingleToolForcesFunction verifies that when a role is
// filtered down to exactly one tool, ToolChoice forces that function.
func TestIntegrationSingleToolForcesFunction(t *testing.T) {
	t.Parallel()
	singleTool := []string{"file_read"}
	tc := ResolveToolChoice(RoleDeveloper, singleTool)
	if tc == nil {
		t.Fatal("expected non-nil ToolChoice for single tool")
	}
	if tc.Mode != "function" {
		t.Errorf("expected mode 'function', got %q", tc.Mode)
	}
	if tc.FunctionName != "file_read" {
		t.Errorf("expected function name 'file_read', got %q", tc.FunctionName)
	}
}

// TestIntegrationEmptyToolsNilChoice verifies no ToolChoice when tools empty.
func TestIntegrationEmptyToolsNilChoice(t *testing.T) {
	t.Parallel()
	tc := ResolveToolChoice(RoleDeveloper, nil)
	if tc != nil {
		t.Errorf("expected nil ToolChoice for empty tools, got %+v", tc)
	}
}
