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

Available scenarios:
  plan-workflow       - Tests CreatePlan, PromotePlan, ExecutePlan via REST API (ADR-003)
  task-dispatcher     - Tests parallel context building and dependency-aware task dispatch
  rdf-export          - Tests /export command with RDF formats and profiles
  debug-command       - Tests trajectory-api endpoints for trace correlation
  trajectory          - Tests trajectory tracking via trajectory-api endpoints
  questions-api       - Tests Q&A HTTP API endpoints (list, get, answer)
  doc-ingest          - Tests document ingestion: markdown, RST parsing and chunking
  openspec-ingest     - Tests OpenSpec specification ingestion with requirements and scenarios
  hello-world                  - Greenfield Python+JS: add /goodbye endpoint with semantic validation
  hello-world-plan-rejection   - Hello-world with plan rejection → revision → approval
  hello-world-task-rejection   - Hello-world with task rejection → revision → approval
  hello-world-double-rejection - Hello-world with both plan and task rejections
  todo-app                     - Brownfield Go+Svelte: add due dates with semantic validation
  all                 - Run all scenarios (default)

Examples:
  e2e                          # Run all scenarios
  e2e workflow-basic           # Run specific scenario
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
	cmd.Flags().StringVar(&mockLLMURL, "mock-llm", "", "Mock LLM server URL (enables mock stats verification)")
	cmd.Flags().StringVar(&workspacePath, "workspace", "/workspace", "Workspace path for test files")
	cmd.Flags().StringVar(&binaryPath, "binary", "./bin/semspec", "Path to semspec binary")
	cmd.Flags().StringVar(&configPath, "config", "./configs/e2e.json", "Path to E2E config file")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output results as JSON")
	cmd.Flags().DurationVar(&timeout, "timeout", config.DefaultCommandTimeout, "Per-command timeout")
	cmd.Flags().DurationVar(&globalTimeout, "global-timeout", 20*time.Minute, "Global timeout for all scenarios")
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
			fmt.Println("  REST API Tests:")
			fmt.Println("  plan-workflow     Tests CreatePlan, PromotePlan, ExecutePlan (ADR-003)")
			fmt.Println("  task-dispatcher   Tests parallel context building and dependency-aware dispatch")
			fmt.Println("  rdf-export        Tests /export command with RDF formats and profiles")
			fmt.Println("  debug-command     Tests trajectory-api endpoints for trace correlation")
			fmt.Println("  trajectory        Tests trajectory tracking via trajectory-api endpoints")
			fmt.Println("  questions-api     Tests Q&A HTTP API endpoints (list, get, answer)")
			fmt.Println()
			fmt.Println("  Document Processing Tests (require source-ingester enabled):")
			fmt.Println("  doc-ingest        Tests document ingestion: markdown, RST parsing and chunking")
			fmt.Println("  openspec-ingest   Tests OpenSpec specification ingestion")
			fmt.Println()
			fmt.Println("  Semantic Validation Scenarios (require LLM):")
			fmt.Println("  hello-world                  Greenfield Python+JS: /goodbye endpoint")
			fmt.Println("  hello-world-plan-rejection   Plan rejection → revision → approval variant")
			fmt.Println("  hello-world-task-rejection   Task rejection → revision → approval variant")
			fmt.Println("  hello-world-double-rejection Both plan and task rejection variant")
			fmt.Println("  todo-app                     Brownfield Go+Svelte: due dates")
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
		scenarios.NewTaskDispatcherScenario(cfg),
		scenarios.NewRDFExportScenario(cfg),
		scenarios.NewDebugCommandScenario(cfg),
		scenarios.NewTrajectoryScenario(cfg),
		scenarios.NewQuestionsAPIScenario(cfg),
		// Document processing scenarios (require source-ingester enabled)
		scenarios.NewDocIngestScenario(cfg),
		scenarios.NewOpenSpecIngestScenario(cfg),
		// Semantic validation scenarios (require LLM)
		scenarios.NewHelloWorldScenario(cfg),
		scenarios.NewHelloWorldScenario(cfg, scenarios.WithPlanRejections(1)),
		scenarios.NewHelloWorldScenario(cfg, scenarios.WithTaskRejections(1)),
		scenarios.NewHelloWorldScenario(cfg, scenarios.WithPlanRejections(1), scenarios.WithTaskRejections(1)),
		scenarios.NewTodoAppScenario(cfg),
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
