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

Scenarios are organized in two tiers:
  Tier 1 — Component tests: REST/CRUD/state-machine coverage (most need no LLM).
  Tier 2 — Pipeline tests: drive the mock-LLM agent pipeline end to end.

Run 'e2e list' for the current scenario set. The list is derived from the
scenario registry, so it cannot drift from what 'e2e <scenario>' accepts.
Real-LLM scenarios run via Playwright E2E (task e2e:ui).

  all  - Run all scenarios (default)

Examples:
  e2e                          # Run all scenarios
  e2e plan-workflow            # Run specific scenario
  e2e list                     # List available scenarios
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
			// Derive the catalogue from the registry so it can never drift from
			// the runnable set. Config is irrelevant to Name()/Description().
			scenarioList := buildScenarioList(&config.Config{})
			width := 0
			for _, s := range scenarioList {
				if n := len(s.Name()); n > width {
					width = n
				}
			}
			fmt.Println("Available scenarios:")
			fmt.Println()
			for _, s := range scenarioList {
				fmt.Printf("  %-*s  %s\n", width, s.Name(), s.Description())
			}
			fmt.Println()
			fmt.Println("Real-LLM scenarios run via Playwright E2E (task e2e:ui).")
			fmt.Println("Use 'e2e all' to run all scenarios.")
		},
	}
}

// buildScenarioList is the single source of truth for the runnable scenario
// registry. run() executes from it and listCmd() derives the help output from
// it, so the documented list can never drift from what `e2e <scenario>`
// actually accepts. The order is the canonical run order.
func buildScenarioList(cfg *config.Config) []scenarios.Scenario {
	return []scenarios.Scenario{
		// Component / API — REST, CRUD, and state-machine coverage. Most need
		// no LLM; plan-workflow and requirement-crud use the mock planner only
		// to bootstrap a plan to operate on.
		scenarios.NewPlanWorkflowScenario(cfg),          // plan-workflow
		scenarios.NewScenarioExecutionScenario(cfg),     // requirement-crud
		scenarios.NewQuestionsAPIScenario(cfg),          // questions-api
		scenarios.NewPlanDecisionScenario(cfg),          // plan-decision
		scenarios.NewReactiveExecutionScenario(cfg),     // reactive-execution
		scenarios.NewSandboxLifecycleScenario(cfg),      // sandbox-lifecycle
		scenarios.NewGraphSourcesScenario(cfg),          // graph-sources (ADR-032)
		scenarios.NewGraphToolsScenario(cfg),            // graph-tools
		scenarios.NewDocIngestScenario(cfg),             // doc-ingest
		scenarios.NewOpenSpecIngestScenario(cfg),        // openspec-ingest
		scenarios.NewPlanStateMachineScenario(cfg),      // plan-state-machine
		scenarios.NewStaleMutationScenario(cfg),         // stale-mutation
		scenarios.NewContractObservabilityScenario(cfg), // contract-observability

		// Mock pipeline — plan phase
		scenarios.NewPlanPhaseScenario(cfg),                                   // plan-phase
		scenarios.NewPlanPhaseScenario(cfg, scenarios.WithReqReview()),        // req-review (ADR-051 Slice 4)
		scenarios.NewPlanPhaseScenario(cfg, scenarios.WithArchReview()),       // arch-review (ADR-051 Slice 3)
		scenarios.NewHelloWorldScenario(cfg),                                  // plan-smoke
		scenarios.NewHelloWorldScenario(cfg, scenarios.WithPlanRejections(1)), // plan-reject
		scenarios.NewHelloWorldScenario(cfg, scenarios.WithPlanExhaustion()),  // plan-exhaust

		// Mock pipeline — execution phase
		scenarios.NewExecutionPhaseScenario(cfg),                               // execution-phase
		scenarios.NewHelloWorldScenario(cfg, scenarios.WithCodeExecution()),    // exec-smoke
		scenarios.NewHelloWorldScenario(cfg, scenarios.WithRequirementRetry()), // exec-requirement-retry
		scenarios.NewParallelAssemblyScenario(cfg),                             // exec-ownership-gate (ADR-049 move-3)

		// Mock pipeline — stall recovery
		scenarios.NewHelloWorldScenario(cfg, scenarios.WithIterationExhaustion()),    // stall-iteration
		scenarios.NewPlanStallRecoveryScenario(cfg, scenarios.StallRecoveryRetry),    // stall-retry
		scenarios.NewPlanStallRecoveryScenario(cfg, scenarios.StallRecoveryComplete), // stall-complete
		scenarios.NewPlanStallRecoveryScenario(cfg, scenarios.StallRecoveryReject),   // stall-reject

		// Mock pipeline — QA phase
		scenarios.NewQACycleScenario(cfg),            // qa-unit
		scenarios.NewQAIntegrationCycleScenario(cfg), // qa-integration
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
	scenarioList := buildScenarioList(cfg)

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
