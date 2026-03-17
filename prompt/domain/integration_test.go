package domain

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
	"github.com/c360studio/semspec/workflow"
)

// Integration tests using REAL production fragments from domain.Software() and domain.Research().
// These catch regressions in actual fragment content, conditions, and role gating that the
// test-double-based integration tests in prompt/integration_test.go cannot.

// allTools simulates the full tool list from agentictools.ListRegisteredTools().
var allTools = []string{
	"file_read", "file_write", "file_list",
	"git_status", "git_diff", "git_commit",
	"workflow_query_graph", "workflow_read_document",
	"workflow_get_codebase_summary", "workflow_traverse_relationships",
	"decompose_task", "spawn_agent", "create_tool", "query_agent_tree",
}

func buildProductionPipeline(fragments []*prompt.Fragment) *prompt.Assembler {
	r := prompt.NewRegistry()
	r.RegisterAll(fragments...)
	r.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
	return prompt.NewAssembler(r)
}

// TestProductionSoftwareAllRoles exercises every role through the full pipeline
// with real Software() fragments, verifying identity, format, and isolation.
func TestProductionSoftwareAllRoles(t *testing.T) {
	t.Parallel()
	assembler := buildProductionPipeline(Software())

	tests := []struct {
		role         prompt.Role
		provider     prompt.Provider
		wantContains []string // content that must appear
		wantAbsent   []string // content that must NOT appear
	}{
		{
			prompt.RoleDeveloper, prompt.ProviderAnthropic,
			[]string{"developer implementing code changes", "<identity>", "file_write"},
			[]string{"plan reviewer validating"},
		},
		{
			prompt.RolePlanner, prompt.ProviderOpenAI,
			[]string{"planner exploring a problem space", "## Identity", "committed"},
			[]string{"MUST call file_write"},
		},
		{
			prompt.RoleReviewer, prompt.ProviderOllama,
			[]string{"code reviewer validating implementation quality", "## Identity"},
			[]string{"MUST call file_write"},
		},
		{
			prompt.RolePlanReviewer, prompt.ProviderAnthropic,
			[]string{"plan reviewer validating", "<identity>", "needs_changes"},
			[]string{"developer implementing"},
		},
		{
			prompt.RoleTaskReviewer, prompt.ProviderOpenAI,
			[]string{"task reviewer validating", "## Identity"},
			[]string{"developer implementing"},
		},
		{
			prompt.RoleRequirementGenerator, prompt.ProviderAnthropic,
			[]string{"requirement writer extracting", "<identity>"},
			[]string{"code reviewer"},
		},
		{
			prompt.RoleScenarioGenerator, prompt.ProviderOpenAI,
			[]string{"scenario writer generating", "## Identity"},
			[]string{"code reviewer"},
		},
		{
			prompt.RoleTaskGenerator, prompt.ProviderOllama,
			[]string{"task decomposer breaking", "## Identity"},
			[]string{"code reviewer"},
		},
		{
			prompt.RolePhaseGenerator, prompt.ProviderAnthropic,
			[]string{"decomposing plans into logical execution phases", "<identity>"},
			[]string{"code reviewer"},
		},
		{
			prompt.RolePlanCoordinator, prompt.ProviderOpenAI,
			[]string{"planning coordinator", "## Identity"},
			[]string{"code reviewer"},
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.role)+"/"+string(tt.provider), func(t *testing.T) {
			t.Parallel()
			tools := prompt.FilterTools(allTools, tt.role)
			result := assembler.Assemble(&prompt.AssemblyContext{
				Role:           tt.role,
				Provider:       tt.provider,
				AvailableTools: tools,
				SupportsTools:  true,
			})

			if result.SystemMessage == "" {
				t.Fatal("expected non-empty system message")
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(result.SystemMessage, want) {
					t.Errorf("expected %q in prompt for %s/%s", want, tt.role, tt.provider)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(result.SystemMessage, absent) {
					t.Errorf("unexpected %q in prompt for %s (fragment leakage)", absent, tt.role)
				}
			}
			if len(result.FragmentsUsed) == 0 {
				t.Error("expected at least one fragment used")
			}
		})
	}
}

// TestProductionSoftwareGapDetectionAllRoles verifies the real gap detection
// fragment reaches every role.
func TestProductionSoftwareGapDetectionAllRoles(t *testing.T) {
	t.Parallel()
	assembler := buildProductionPipeline(Software())

	roles := []prompt.Role{
		prompt.RoleDeveloper, prompt.RolePlanner, prompt.RoleReviewer,
		prompt.RolePlanReviewer, prompt.RoleTaskReviewer,
		prompt.RoleRequirementGenerator, prompt.RoleScenarioGenerator,
		prompt.RoleTaskGenerator, prompt.RolePhaseGenerator,
		prompt.RolePlanCoordinator,
	}

	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()
			result := assembler.Assemble(&prompt.AssemblyContext{
				Role:     role,
				Provider: prompt.ProviderOpenAI,
			})

			if !strings.Contains(result.SystemMessage, "Knowledge Gaps") {
				t.Errorf("role %s missing gap detection from production fragments", role)
			}
		})
	}
}

// TestProductionSoftwareDeveloperRetryCondition verifies the real retry
// fragment condition and content with production fragments.
func TestProductionSoftwareDeveloperRetryCondition(t *testing.T) {
	t.Parallel()
	assembler := buildProductionPipeline(Software())
	tools := prompt.FilterTools(allTools, prompt.RoleDeveloper)

	t.Run("no feedback: retry absent", func(t *testing.T) {
		t.Parallel()
		result := assembler.Assemble(&prompt.AssemblyContext{
			Role:           prompt.RoleDeveloper,
			Provider:       prompt.ProviderAnthropic,
			AvailableTools: tools,
			SupportsTools:  true,
		})

		if strings.Contains(result.SystemMessage, "Previous Feedback") {
			t.Error("retry directive should not appear without feedback")
		}
	})

	t.Run("with feedback: retry present with content", func(t *testing.T) {
		t.Parallel()
		result := assembler.Assemble(&prompt.AssemblyContext{
			Role:           prompt.RoleDeveloper,
			Provider:       prompt.ProviderAnthropic,
			AvailableTools: tools,
			SupportsTools:  true,
			TaskContext: &prompt.TaskContext{
				Task:     workflow.Task{ID: "task.fix.1"},
				Feedback: "Error wrapping missing in handler",
			},
		})

		if !strings.Contains(result.SystemMessage, "Error wrapping missing in handler") {
			t.Error("expected feedback content interpolated in retry prompt")
		}
	})
}

// TestProductionSoftwareTaskContextCondition verifies the real task context
// fragment activates correctly with production fragments.
func TestProductionSoftwareTaskContextCondition(t *testing.T) {
	t.Parallel()
	assembler := buildProductionPipeline(Software())
	tools := prompt.FilterTools(allTools, prompt.RoleDeveloper)

	t.Run("no task context: domain context absent", func(t *testing.T) {
		t.Parallel()
		result := assembler.Assemble(&prompt.AssemblyContext{
			Role:           prompt.RoleDeveloper,
			Provider:       prompt.ProviderOpenAI,
			AvailableTools: tools,
			SupportsTools:  true,
		})

		// The task-context fragment only fires with TaskContext set.
		if strings.Contains(result.SystemMessage, "Acceptance Criteria") {
			t.Error("should not have acceptance criteria without TaskContext")
		}
	})

	t.Run("with task context: task details shown", func(t *testing.T) {
		t.Parallel()
		result := assembler.Assemble(&prompt.AssemblyContext{
			Role:           prompt.RoleDeveloper,
			Provider:       prompt.ProviderOpenAI,
			AvailableTools: tools,
			SupportsTools:  true,
			TaskContext: &prompt.TaskContext{
				Task: workflow.Task{
					ID:          "task.auth.1",
					Description: "Add JWT validation middleware",
					Type:        "implement",
					Files:       []string{"middleware/auth.go"},
					AcceptanceCriteria: []workflow.AcceptanceCriterion{
						{Given: "an expired token", When: "request is made", Then: "returns 401"},
					},
				},
				PlanGoal: "Add authentication layer",
			},
		})

		if !strings.Contains(result.SystemMessage, "task.auth.1") {
			t.Error("should contain task ID")
		}
		if !strings.Contains(result.SystemMessage, "JWT validation middleware") {
			t.Error("should contain task description")
		}
		if !strings.Contains(result.SystemMessage, "middleware/auth.go") {
			t.Error("should contain scope file")
		}
		if !strings.Contains(result.SystemMessage, "expired token") {
			t.Error("should contain acceptance criteria")
		}
	})
}

// TestProductionSoftwareProviderFormatting verifies production fragments
// produce correct formatting for each provider.
func TestProductionSoftwareProviderFormatting(t *testing.T) {
	t.Parallel()
	assembler := buildProductionPipeline(Software())

	tests := []struct {
		provider  prompt.Provider
		wantTag   string
		absentTag string
	}{
		{prompt.ProviderAnthropic, "<identity>", "## Identity"},
		{prompt.ProviderOpenAI, "## Identity", "<identity>"},
		{prompt.ProviderOllama, "## Identity", "<identity>"},
	}

	for _, tt := range tests {
		t.Run(string(tt.provider), func(t *testing.T) {
			t.Parallel()
			result := assembler.Assemble(&prompt.AssemblyContext{
				Role:           prompt.RoleDeveloper,
				Provider:       tt.provider,
				AvailableTools: prompt.FilterTools(allTools, prompt.RoleDeveloper),
				SupportsTools:  true,
			})

			if !strings.Contains(result.SystemMessage, tt.wantTag) {
				t.Errorf("expected %q for provider %s", tt.wantTag, tt.provider)
			}
			if strings.Contains(result.SystemMessage, tt.absentTag) {
				t.Errorf("unexpected %q for provider %s", tt.absentTag, tt.provider)
			}
		})
	}
}

// TestProductionDomainSwitching verifies software vs research domain produces
// different prompts for the same role using real fragments.
func TestProductionDomainSwitching(t *testing.T) {
	t.Parallel()
	swAssembler := buildProductionPipeline(Software())
	resAssembler := buildProductionPipeline(Research())

	tools := prompt.FilterTools(allTools, prompt.RoleDeveloper)

	swResult := swAssembler.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderAnthropic,
		AvailableTools: tools,
		SupportsTools:  true,
	})
	resResult := resAssembler.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderAnthropic,
		AvailableTools: tools,
		SupportsTools:  true,
	})

	if swResult.SystemMessage == resResult.SystemMessage {
		t.Fatal("software and research domains must produce different prompts")
	}

	// Software should have developer identity; research should have analyst identity.
	if !strings.Contains(swResult.SystemMessage, "developer implementing code changes") {
		t.Error("software domain should contain developer identity")
	}
	if !strings.Contains(resResult.SystemMessage, "research analyst") {
		t.Error("research domain should contain analyst identity")
	}

	// Research should have evidence-based output format.
	if !strings.Contains(resResult.SystemMessage, "confidence") {
		t.Error("research domain should reference confidence levels")
	}
}

// TestProductionSoftwareCategoryOrdering verifies the full category ordering
// chain with real production fragments.
func TestProductionSoftwareCategoryOrdering(t *testing.T) {
	t.Parallel()
	assembler := buildProductionPipeline(Software())

	// Developer has the most categories populated, so best for ordering checks.
	tools := prompt.FilterTools(allTools, prompt.RoleDeveloper)
	result := assembler.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: tools,
		SupportsTools:  true,
	})

	msg := result.SystemMessage

	identityIdx := strings.Index(msg, "## Identity")
	toolDirIdx := strings.Index(msg, "## Tool Directives")
	roleIdx := strings.Index(msg, "## Role")
	toolGuideIdx := strings.Index(msg, "## Tool Guidance")
	outputIdx := strings.Index(msg, "## Output Format")
	gapIdx := strings.Index(msg, "## Gap Detection")

	if identityIdx < 0 {
		t.Fatal("expected Identity section")
	}

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
}
