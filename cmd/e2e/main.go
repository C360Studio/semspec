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
		workspacePath string
		binaryPath    string
		configPath    string
		outputJSON    bool
		timeout       time.Duration
		globalTimeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "e2e [scenario]",
		Short: "Run semspec e2e tests",
		Long: `Run end-to-end tests for semspec workflow system.

Available scenarios:
  status-command   - Tests /status command via HTTP gateway
  propose-workflow - Tests /propose with graph entity creation
  full-workflow    - Tests complete propose → design → spec → tasks → check → approve
  help-command     - Tests /help command lists available commands
  context-command  - Tests /context command with graph query
  graph-publishing - Tests /propose publishes entities to graph.ingest.entity
  rdf-export       - Tests /export command with RDF formats and profiles
  debug-command    - Tests /debug command for trace correlation
  workflow-basic   - Tests the full propose → approve workflow (NATS direct)
  constitution     - Tests constitution enforcement during approval
  ast-go           - Tests Go AST processor entity extraction
  ast-typescript   - Tests TypeScript AST processor entity extraction
  brownfield       - Tests workflow on existing codebase with history
  greenfield       - Tests workflow on new empty project
  all              - Run all scenarios (default)

Examples:
  e2e                          # Run all scenarios
  e2e workflow-basic           # Run specific scenario
  e2e --json                   # Output results as JSON
  e2e --nats nats://host:4222  # Custom NATS URL
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scenarioName := "all"
			if len(args) > 0 {
				scenarioName = args[0]
			}

			// Derive fixtures path from workspace path
			fixturesPath := workspacePath[:strings.LastIndex(workspacePath, "/")] + "/fixtures"

			cfg := &config.Config{
				NATSURL:        natsURL,
				HTTPBaseURL:    httpURL,
				WorkspacePath:  workspacePath,
				FixturesPath:   fixturesPath,
				BinaryPath:     binaryPath,
				ConfigPath:     configPath,
				CommandTimeout: timeout,
				SetupTimeout:   timeout * 2,
				StageTimeout:   timeout,
			}

			return run(scenarioName, cfg, outputJSON, globalTimeout)
		},
	}

	cmd.Flags().StringVar(&natsURL, "nats", config.DefaultNATSURL, "NATS server URL")
	cmd.Flags().StringVar(&httpURL, "http", config.DefaultHTTPURL, "HTTP gateway URL")
	cmd.Flags().StringVar(&workspacePath, "workspace", "/workspace", "Workspace path for test files")
	cmd.Flags().StringVar(&binaryPath, "binary", "./bin/semspec", "Path to semspec binary")
	cmd.Flags().StringVar(&configPath, "config", "./configs/e2e.json", "Path to E2E config file")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output results as JSON")
	cmd.Flags().DurationVar(&timeout, "timeout", config.DefaultCommandTimeout, "Per-command timeout")
	cmd.Flags().DurationVar(&globalTimeout, "global-timeout", 10*time.Minute, "Global timeout for all scenarios")

	// Add list subcommand
	cmd.AddCommand(listCmd())

	return cmd
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available scenarios",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Available scenarios:")
			fmt.Println()
			fmt.Println("  HTTP Gateway Tests (recommended):")
			fmt.Println("  status-command    Tests /status command via HTTP gateway")
			fmt.Println("  propose-workflow  Tests /propose with graph entity creation")
			fmt.Println("  full-workflow     Tests complete propose → design → spec → tasks → check → approve")
			fmt.Println("  help-command      Tests /help command lists available commands")
			fmt.Println("  context-command   Tests /context command with graph query")
			fmt.Println("  graph-publishing  Tests /propose publishes entities to graph.ingest.entity")
			fmt.Println("  rdf-export        Tests /export command with RDF formats and profiles")
			fmt.Println("  debug-command     Tests /debug command for trace correlation")
			fmt.Println()
			fmt.Println("  Legacy NATS Direct Tests:")
			fmt.Println("  workflow-basic    Tests the full propose → approve workflow")
			fmt.Println("  constitution      Tests constitution enforcement during approval")
			fmt.Println()
			fmt.Println("  AST Processor Tests:")
			fmt.Println("  ast-go            Tests Go AST processor entity extraction")
			fmt.Println("  ast-typescript    Tests TypeScript AST processor entity extraction")
			fmt.Println()
			fmt.Println("  Integration Tests:")
			fmt.Println("  brownfield        Tests workflow on existing codebase with history")
			fmt.Println("  greenfield        Tests workflow on new empty project")
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
		// HTTP Gateway scenarios (recommended)
		scenarios.NewStatusCommandScenario(cfg),
		scenarios.NewProposeWorkflowScenario(cfg),
		scenarios.NewFullWorkflowScenario(cfg),
		scenarios.NewHelpCommandScenario(cfg),
		scenarios.NewContextCommandScenario(cfg),
		scenarios.NewGraphPublishingScenario(cfg),
		scenarios.NewRDFExportScenario(cfg),
		scenarios.NewDebugCommandScenario(cfg),
		// Legacy NATS direct scenarios
		scenarios.NewWorkflowBasicScenario(cfg),
		scenarios.NewConstitutionScenario(cfg),
		// AST processor scenarios
		scenarios.NewASTGoScenario(cfg),
		scenarios.NewASTTypeScriptScenario(cfg),
		// Integration scenarios
		scenarios.NewBrownfieldScenario(cfg),
		scenarios.NewGreenfieldScenario(cfg),
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
			fmt.Printf("  %s %s (%dms)\n", status, stage.Name, stage.Duration.Milliseconds())
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
		fmt.Printf("  %s  %s (%dms)\n", status, r.ScenarioName, r.Duration.Milliseconds())
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
