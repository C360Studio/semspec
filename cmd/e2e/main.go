// Package main provides the e2e test runner CLI.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/c360studio/semspec/test/e2e/config"
	"github.com/c360studio/semspec/test/e2e/scenarios"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var (
		natsURL       string
		httpURL       string
		graphURL      string
		mockLLMURL    string
		workspacePath string
		binaryPath    string
		configPath    string
		outputJSON    bool
		fastTimeouts  bool
		timeout       time.Duration
		globalTimeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "e2e [scenario]",
		Short: "Run semspec e2e tests",
		Long: `Run end-to-end tests for semspec workflow system.

Tier 1 — Component Tests (no LLM):
  plan-workflow       - Tests CreatePlan, PromotePlan, ExecutePlan via REST API (ADR-003)
  task-dispatcher     - Tests parallel context building and dependency-aware task dispatch
  questions-api       - Tests Q&A HTTP API endpoints (list, get, answer)
  scenario-execution  - Tests Requirement/Scenario CRUD and scenario-execution+DAG reactive workflow trigger
  reactive-execution  - Tests full reactive execution lifecycle: decomposition → node dispatch → completion
  change-proposal     - Tests ChangeProposal CRUD, status transitions, cascade response, and error handling
  sandbox-lifecycle   - Tests sandbox server lifecycle: worktree CRUD, file ops, git, exec, merge, cleanup
  agent-roster        - Tests persistent agent roster: agent selection, error tracking, dispatch verification
  team-roster         - Tests team-based agent infrastructure: team entities, member linkage, bidirectional refs
  team-knowledge      - Tests always-on team knowledge infrastructure: auto-seeding, HTTP endpoints, error categories
  graph-sources       - Tests graph source registry: semsource readiness, GraphQL entity indexing
  graph-tools         - Tests all graph tool paths: graph_summary, graph_query, graph_search
  doc-ingest          - Tests document ingestion: markdown, RST parsing and chunking
  openspec-ingest     - Tests OpenSpec specification ingestion with requirements and scenarios
  plan-state-machine  - Tests plan retry/complete/reject endpoint guard clauses

Tier 2 — Pipeline Tests (mock LLM):
  hello-world                    - Greenfield Python+JS: add /goodbye endpoint with semantic validation
  hello-world-code-execution     - Hello-world with full TDD pipeline execution
  hello-world-requirement-retry  - Hello-world with requirement rejection → dirty-node retry → approval
  hello-world-plan-rejection     - Hello-world with plan rejection → revision → approval
  hello-world-plan-exhaustion    - Hello-world with plan review exhaustion → escalation
  plan-phase                   - Full plan pipeline: plan → requirements → scenarios → review → approved
  execution-phase              - Full execution pipeline: plan → approve → decompose → TDD → complete
  team-execution               - Team pipeline: plan → requirements → scenarios → execute → dispatch
  context-pressure             - Claims verification: context truncation, model routing, revision quality
  plan-stall-retry             - Execution stall: failed requirement → retry → re-execution succeeds
  plan-stall-complete          - Execution stall: failed requirement → force-complete
  plan-stall-reject            - Execution stall: failed requirement → reject → retry from rejected

Real-LLM scenarios (health-check, rest-api, todo-app, epic-meshtastic) have moved
to Playwright E2E — see docs/e2e-scenario-archive.md for details.

  all                 - Run all scenarios (default)

Examples:
  e2e                          # Run all scenarios
  e2e plan-workflow            # Run specific scenario
  e2e --json                   # Output results as JSON
  e2e --nats nats://host:4222  # Custom NATS URL
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			scenarioName := "all"
			if len(args) > 0 {
				scenarioName = args[0]
			}

			// Derive fixtures path from workspace path
			fixturesPath := workspacePath[:strings.LastIndex(workspacePath, "/")] + "/fixtures"

			cfg := &config.Config{
				NATSURL:        natsURL,
				HTTPBaseURL:    httpURL,
				GraphURL:       graphURL,
				MockLLMURL:     mockLLMURL,
				WorkspacePath:  workspacePath,
				FixturesPath:   fixturesPath,
				BinaryPath:     binaryPath,
				ConfigPath:     configPath,
				CommandTimeout: timeout,
				SetupTimeout:   timeout * 2,
				StageTimeout:   timeout,
				FastTimeouts:   fastTimeouts,
			}

			return run(scenarioName, cfg, outputJSON, globalTimeout)
		},
	}

	cmd.Flags().StringVar(&natsURL, "nats", config.DefaultNATSURL, "NATS server URL")
	cmd.Flags().StringVar(&httpURL, "http", config.DefaultHTTPURL, "HTTP gateway URL")
	cmd.Flags().StringVar(&graphURL, "graph", config.DefaultGraphURL, "Graph gateway URL")
	cmd.Flags().StringVar(&mockLLMURL, "mock-llm", "", "Mock LLM server URL (enables mock stats verification)")
	cmd.Flags().StringVar(&workspacePath, "workspace", "/workspace", "Workspace path for test files")
	cmd.Flags().StringVar(&binaryPath, "binary", "./bin/semspec", "Path to semspec binary")
	cmd.Flags().StringVar(&configPath, "config", "./configs/e2e.json", "Path to E2E config file")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output results as JSON")
	cmd.Flags().DurationVar(&timeout, "timeout", config.DefaultCommandTimeout, "Per-command timeout")
	cmd.Flags().DurationVar(&globalTimeout, "global-timeout", 25*time.Minute, "Global timeout for all scenarios")
	cmd.Flags().BoolVar(&fastTimeouts, "fast-timeouts", false, "Use aggressive timeouts for mock/fast LLM backends")

	// Add list subcommand
	cmd.AddCommand(listCmd())

	return cmd
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available scenarios",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println("Available scenarios:")
			fmt.Println()
			fmt.Println("  Tier 1 — Component Tests (no LLM):")
			fmt.Println("  plan-workflow       Tests CreatePlan, PromotePlan, ExecutePlan (ADR-003)")
			fmt.Println("  task-dispatcher     Tests parallel context building and dependency-aware dispatch")
			fmt.Println("  questions-api       Tests Q&A HTTP API endpoints (list, get, answer)")
			fmt.Println("  scenario-execution  Tests Requirement/Scenario CRUD and reactive workflow trigger")
			fmt.Println("  reactive-execution  Tests full reactive execution: decomposition → dispatch → completion")
			fmt.Println("  change-proposal     Tests ChangeProposal CRUD, status transitions, cascade, and error handling")
			fmt.Println("  sandbox-lifecycle   Tests sandbox worktree CRUD, file ops, git, exec, merge, cleanup")
			fmt.Println("  agent-roster        Tests persistent agent roster: selection, error tracking, dispatch")
			fmt.Println("  team-roster         Tests team-based agent infrastructure: team entities, member linkage, bidirectional refs")
			fmt.Println("  team-knowledge      Tests always-on team knowledge: auto-seeding, HTTP endpoints, error categories")
			fmt.Println("  graph-sources       Tests graph source registry: semsource readiness, GraphQL entities")
			fmt.Println("  graph-tools         Tests all graph tool paths: summary, query, search")
			fmt.Println("  doc-ingest          Tests document ingestion: markdown, RST parsing and chunking")
			fmt.Println("  openspec-ingest     Tests OpenSpec specification ingestion")
			fmt.Println("  plan-state-machine  Tests plan retry/complete/reject guard clauses")
			fmt.Println()
			fmt.Println("  Tier 2 — Pipeline Tests (mock LLM):")
			fmt.Println("  hello-world                    Greenfield Python+JS: /goodbye endpoint")
			fmt.Println("  hello-world-code-execution     Full TDD pipeline execution variant")
			fmt.Println("  hello-world-requirement-retry  Requirement rejection → dirty-node retry variant")
			fmt.Println("  hello-world-plan-rejection     Plan rejection → revision → approval variant")
			fmt.Println("  hello-world-plan-exhaustion    Plan review exhaustion → escalation variant")
			fmt.Println("  plan-phase                   Full plan pipeline: plan → requirements → scenarios → review")
			fmt.Println("  execution-phase              Full execution pipeline: plan → approve → decompose → TDD → complete")
			fmt.Println("  team-execution               Team pipeline: plan → requirements → scenarios → execute → dispatch")
			fmt.Println("  context-pressure             Context truncation, model routing, revision quality")
			fmt.Println("  plan-stall-retry             Execution stall: failed req → retry → re-execute")
			fmt.Println("  plan-stall-complete          Execution stall: failed req → force-complete")
			fmt.Println("  plan-stall-reject            Execution stall: failed req → reject → retry")
			fmt.Println()
			fmt.Println("  Real-LLM scenarios (health-check, rest-api, todo-app, epic-meshtastic)")
			fmt.Println("  have moved to Playwright E2E — see docs/e2e-scenario-archive.md")
			fmt.Println()
			fmt.Println("Use 'e2e all' to run all scenarios.")
		},
	}
}

func run(scenarioName string, cfg *config.Config, outputJSON bool, globalTimeout time.Duration) error {
	// Create context with global timeout and signal handling
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Handle OS signals for graceful shutdown
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Create scenario registry
	scenarioList := []scenarios.Scenario{
		// REST API scenarios
		scenarios.NewPlanWorkflowScenario(cfg),
		scenarios.NewQuestionsAPIScenario(cfg),
		scenarios.NewScenarioExecutionScenario(cfg),
		scenarios.NewReactiveExecutionScenario(cfg),
		scenarios.NewChangeProposalScenario(cfg),
		scenarios.NewSandboxLifecycleScenario(cfg),
		scenarios.NewTeamKnowledgeScenario(cfg),
		// Graph source registry (ADR-032)
		scenarios.NewGraphSourcesScenario(cfg),
		scenarios.NewGraphToolsScenario(cfg),
		// Document processing scenarios (require source-ingester enabled)
		scenarios.NewDocIngestScenario(cfg),
		scenarios.NewOpenSpecIngestScenario(cfg),
		// Tier 1: State machine guard clauses
		scenarios.NewPlanStateMachineScenario(cfg),
		// Tier 2: Pipeline tests (mock LLM)
		scenarios.NewPlanPhaseScenario(cfg),
		scenarios.NewExecutionPhaseScenario(cfg),
		scenarios.NewHelloWorldScenario(cfg),
		scenarios.NewHelloWorldScenario(cfg, scenarios.WithCodeExecution()),
		scenarios.NewHelloWorldScenario(cfg, scenarios.WithRequirementRetry()),
		scenarios.NewHelloWorldScenario(cfg, scenarios.WithPlanRejections(1)),
		scenarios.NewHelloWorldScenario(cfg, scenarios.WithPlanExhaustion()),
		scenarios.NewContextPressureScenario(cfg),
		// Tier 2: Stall recovery variants (state machine refactor)
		scenarios.NewPlanStallRecoveryScenario(cfg, scenarios.StallRecoveryRetry),
		scenarios.NewPlanStallRecoveryScenario(cfg, scenarios.StallRecoveryComplete),
		scenarios.NewPlanStallRecoveryScenario(cfg, scenarios.StallRecoveryReject),
	}

	scenarioMap := make(map[string]scenarios.Scenario)
	for _, s := range scenarioList {
		scenarioMap[s.Name()] = s
	}

	// Determine which scenarios to run
	var toRun []scenarios.Scenario
	if scenarioName == "all" {
		toRun = scenarioList
	} else {
		s, ok := scenarioMap[scenarioName]
		if !ok {
			return fmt.Errorf("unknown scenario: %s", scenarioName)
		}
		toRun = []scenarios.Scenario{s}
	}

	// Run scenarios
	results := make([]*scenarios.Result, 0, len(toRun))
	allPassed := true

	for _, scenario := range toRun {
		// Check if context was cancelled
		if ctx.Err() != nil {
			if !outputJSON {
				fmt.Println("\nTest run interrupted!")
			}
			break
		}

		result := runScenario(ctx, scenario, outputJSON)
		results = append(results, result)
		if !result.Success {
			allPassed = false
		}
	}

	// Output final results
	if outputJSON {
		outputJSONResults(results)
	} else {
		outputTextSummary(results)
	}

	if !allPassed {
		return fmt.Errorf("some scenarios failed")
	}
	return nil
}

func runScenario(ctx context.Context, scenario scenarios.Scenario, quietMode bool) *scenarios.Result {
	if !quietMode {
		fmt.Printf("\n═══════════════════════════════════════════════════════════════\n")
		fmt.Printf("Running: %s\n", scenario.Name())
		fmt.Printf("Description: %s\n", scenario.Description())
		fmt.Printf("═══════════════════════════════════════════════════════════════\n\n")
	}

	// Setup
	if !quietMode {
		fmt.Print("Setup... ")
	}
	if err := scenario.Setup(ctx); err != nil {
		result := scenarios.NewResult(scenario.Name())
		result.Error = fmt.Sprintf("setup failed: %v", err)
		result.AddError(result.Error)
		result.Complete()
		if !quietMode {
			fmt.Printf("FAILED: %v\n", err)
		}
		return result
	}
	if !quietMode {
		fmt.Println("OK")
	}

	// Execute
	if !quietMode {
		fmt.Print("Execute... ")
	}
	result, err := scenario.Execute(ctx)
	if err != nil {
		result = scenarios.NewResult(scenario.Name())
		result.Error = fmt.Sprintf("execution error: %v", err)
		result.AddError(result.Error)
		result.Complete()
		if !quietMode {
			fmt.Printf("ERROR: %v\n", err)
		}
	} else if result.Success {
		if !quietMode {
			fmt.Println("PASSED")
		}
	} else {
		if !quietMode {
			fmt.Printf("FAILED: %s\n", result.Error)
		}
	}

	// Teardown
	if !quietMode {
		fmt.Print("Teardown... ")
	}
	if err := scenario.Teardown(ctx); err != nil {
		result.AddWarning(fmt.Sprintf("teardown failed: %v", err))
		if !quietMode {
			fmt.Printf("WARNING: %v\n", err)
		}
	} else if !quietMode {
		fmt.Println("OK")
	}

	// Print stage details
	if !quietMode && len(result.Stages) > 0 {
		fmt.Println("\nStages:")
		for _, stage := range result.Stages {
			status := "✓"
			if !stage.Success {
				status = "✗"
			}
			fmt.Printf("  %s %s (%s)\n", status, stage.Name, formatDuration(stage.Duration))
			if stage.Error != "" {
				fmt.Printf("      Error: %s\n", stage.Error)
			}
		}
	}

	return result
}

func outputJSONResults(results []*scenarios.Result) {
	output := struct {
		Timestamp time.Time           `json:"timestamp"`
		Results   []*scenarios.Result `json:"results"`
		Summary   struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		} `json:"summary"`
	}{
		Timestamp: time.Now(),
		Results:   results,
	}

	output.Summary.Total = len(results)
	for _, r := range results {
		if r.Success {
			output.Summary.Passed++
		} else {
			output.Summary.Failed++
		}
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling results: %v\n", err)
		return
	}
	fmt.Println(string(data))
}

func outputTextSummary(results []*scenarios.Result) {
	fmt.Println("\n═══════════════════════════════════════════════════════════════")
	fmt.Println("                          SUMMARY")
	fmt.Println("═══════════════════════════════════════════════════════════════")

	passed := 0
	failed := 0
	for _, r := range results {
		status := "✓ PASSED"
		if !r.Success {
			status = "✗ FAILED"
			failed++
		} else {
			passed++
		}
		fmt.Printf("  %s  %s (%s)\n", status, r.ScenarioName, formatDuration(r.Duration))
		if !r.Success && r.Error != "" {
			// Truncate long error messages
			errMsg := r.Error
			if len(errMsg) > 80 {
				errMsg = errMsg[:77] + "..."
			}
			fmt.Printf("           %s\n", errMsg)
		}
	}

	fmt.Println(strings.Repeat("─", 65))
	fmt.Printf("  Total: %d | Passed: %d | Failed: %d\n", len(results), passed, failed)
	fmt.Println("═══════════════════════════════════════════════════════════════")

	if failed > 0 {
		fmt.Println("\nSome tests failed. Run with --json for detailed output.")
	}
}

// formatDuration formats a duration with appropriate precision.
// Sub-millisecond durations show microseconds, longer ones show milliseconds.
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}
