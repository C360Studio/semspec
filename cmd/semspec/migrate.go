package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/spf13/cobra"
)

// migrateCmd returns the `semspec migrate` parent command.
func migrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Data migration utilities",
		Long:  "Run-once data migration commands for upgrading semspec data structures.",
	}
	cmd.AddCommand(extractScenariosCmd())
	return cmd
}

// extractScenariosCmd returns the `semspec migrate extract-scenarios` command.
//
// Per ADR-024 Phase 3: extracts AcceptanceCriteria from Tasks into first-class
// Requirement and Scenario nodes. Run once against a clean state. Not idempotent —
// rerun only against a clean state on failure.
func extractScenariosCmd() *cobra.Command {
	var repoPath string

	cmd := &cobra.Command{
		Use:   "extract-scenarios",
		Short: "Extract AcceptanceCriteria from Tasks into Requirement/Scenario nodes",
		Long: `Migrates existing Tasks that have AcceptanceCriteria into the ADR-024
graph model by creating:
  - One Requirement per Task (placeholder, titled from Task description)
  - One Scenario per AcceptanceCriterion (Given/When/Then)
  - Task.ScenarioIDs linking the Task to its new Scenarios
  - Task.AcceptanceCriteria cleared after migration

Run once against a clean state. Not idempotent.
Validates that every migrated Task has at least one ScenarioID before writing.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			absRepoPath, err := filepath.Abs(repoPath)
			if err != nil {
				return fmt.Errorf("resolve repo path: %w", err)
			}
			info, err := os.Stat(absRepoPath)
			if err != nil {
				return fmt.Errorf("stat repo path: %w", err)
			}
			if !info.IsDir() {
				return fmt.Errorf("not a directory: %s", absRepoPath)
			}

			return runExtractScenarios(context.Background(), absRepoPath)
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", ".", "Repository path (contains .semspec/)")

	return cmd
}

// MigrationResult records per-plan migration statistics.
type MigrationResult struct {
	TasksMigrated       int
	RequirementsCreated int
	ScenariosCreated    int
	TasksSkipped        int
}

// runExtractScenarios executes the migration for every plan in the default project.
func runExtractScenarios(ctx context.Context, repoPath string) error {
	manager := workflow.NewManager(repoPath)

	listResult, err := manager.ListPlans(ctx)
	if err != nil {
		return fmt.Errorf("list plans: %w", err)
	}
	if len(listResult.Errors) > 0 {
		for _, e := range listResult.Errors {
			fmt.Fprintf(os.Stderr, "warning: error loading a plan: %v\n", e)
		}
	}

	if len(listResult.Plans) == 0 {
		fmt.Println("No plans found — nothing to migrate.")
		return nil
	}

	var totalReqs, totalScenarios, totalMigrated int

	for _, plan := range listResult.Plans {
		result, err := migrateExtractScenarios(manager, plan.Slug)
		if err != nil {
			return fmt.Errorf("migrate plan %q: %w", plan.Slug, err)
		}

		if result.TasksMigrated == 0 {
			fmt.Printf("  Plan %q: no tasks with AcceptanceCriteria — skipped\n", plan.Slug)
			continue
		}

		totalReqs += result.RequirementsCreated
		totalScenarios += result.ScenariosCreated
		totalMigrated += result.TasksMigrated

		fmt.Printf("  Plan %q: migrated %d tasks, created %d requirements, %d scenarios\n",
			plan.Slug,
			result.TasksMigrated,
			result.RequirementsCreated,
			result.ScenariosCreated,
		)
	}

	fmt.Printf("\nMigration complete: %d task(s) migrated, %d requirement(s) created, %d scenario(s) created\n",
		totalMigrated, totalReqs, totalScenarios)

	return nil
}

// migrateExtractScenarios migrates a single plan: creates Requirements+Scenarios from
// Task.AcceptanceCriteria, sets Task.ScenarioIDs, and clears Task.AcceptanceCriteria.
// Validates before writing. Context is derived from Background.
func migrateExtractScenarios(manager *workflow.Manager, slug string) (*MigrationResult, error) {
	return migrateExtractScenariosCtx(context.Background(), manager, slug)
}

// migrationState holds the loaded data needed for a single-plan migration.
type migrationState struct {
	tasks        []workflow.Task
	requirements []workflow.Requirement
	scenarios    []workflow.Scenario
}

// loadMigrationState loads the current tasks, requirements, and scenarios for a plan.
func loadMigrationState(ctx context.Context, manager *workflow.Manager, slug string) (*migrationState, error) {
	tasks, err := manager.LoadTasks(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("load tasks: %w", err)
	}

	// Load existing requirements and scenarios so we append rather than overwrite.
	requirements, err := manager.LoadRequirements(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("load requirements: %w", err)
	}

	scenarios, err := manager.LoadScenarios(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("load scenarios: %w", err)
	}

	return &migrationState{tasks: tasks, requirements: requirements, scenarios: scenarios}, nil
}

// migrateTask converts a task's AcceptanceCriteria into a Requirement and Scenarios,
// appending to the provided slices. It mutates the task in place: ScenarioIDs are set
// and AcceptanceCriteria is cleared. Returns updated slices and the new scenario IDs.
func migrateTask(
	task *workflow.Task,
	slug string,
	reqSeq, scenarioSeq int,
	now time.Time,
	requirements []workflow.Requirement,
	scenarios []workflow.Scenario,
) (updatedReqs []workflow.Requirement, updatedScens []workflow.Scenario, scenarioIDs []string) {
	reqID := fmt.Sprintf("requirement.%s.%d", slug, reqSeq)

	title := "Requirement for: " + task.Description
	if len(title) > 80 {
		title = title[:77] + "..."
	}

	newReq := workflow.Requirement{
		ID:          reqID,
		PlanID:      workflow.PlanEntityID(slug),
		Title:       title,
		Description: task.Description,
		Status:      workflow.RequirementStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	requirements = append(requirements, newReq)

	for _, ac := range task.AcceptanceCriteria {
		scenarioID := fmt.Sprintf("scenario.%s.%d", slug, scenarioSeq)
		newScenario := workflow.Scenario{
			ID:            scenarioID,
			RequirementID: reqID,
			Given:         ac.Given,
			When:          ac.When,
			Then:          []string{ac.Then},
			Status:        workflow.ScenarioStatusPending,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		scenarios = append(scenarios, newScenario)
		scenarioIDs = append(scenarioIDs, scenarioID)
		scenarioSeq++
	}

	task.ScenarioIDs = append(task.ScenarioIDs, scenarioIDs...)
	task.AcceptanceCriteria = nil

	return requirements, scenarios, scenarioIDs
}

// persistMigrationResults writes requirements, scenarios, and tasks in dependency order.
// Fail-fast: stops before writing tasks if an earlier write fails.
func persistMigrationResults(
	ctx context.Context,
	manager *workflow.Manager,
	slug string,
	requirements []workflow.Requirement,
	scenarios []workflow.Scenario,
	tasks []workflow.Task,
) error {
	if err := manager.SaveRequirements(ctx, requirements, slug); err != nil {
		return fmt.Errorf("save requirements: %w", err)
	}
	if err := manager.SaveScenarios(ctx, scenarios, slug); err != nil {
		return fmt.Errorf("save scenarios: %w", err)
	}
	if err := manager.SaveTasks(ctx, tasks, slug); err != nil {
		return fmt.Errorf("save tasks: %w", err)
	}
	return nil
}

// migrateExtractScenariosCtx is the context-aware implementation called by both the
// CLI runner and tests.
func migrateExtractScenariosCtx(ctx context.Context, manager *workflow.Manager, slug string) (*MigrationResult, error) {
	result := &MigrationResult{}

	state, err := loadMigrationState(ctx, manager, slug)
	if err != nil {
		return nil, err
	}

	// Sequence counters start after existing entries.
	reqSeq := len(state.requirements) + 1
	scenarioSeq := len(state.scenarios) + 1
	now := time.Now()

	// migratedTaskIDs tracks which task IDs were migrated for post-loop validation.
	migratedTaskIDs := make(map[string]struct{})

	for i := range state.tasks {
		task := &state.tasks[i]

		if len(task.AcceptanceCriteria) == 0 {
			result.TasksSkipped++
			continue
		}

		var newScenarioIDs []string
		state.requirements, state.scenarios, newScenarioIDs = migrateTask(
			task, slug, reqSeq, scenarioSeq, now, state.requirements, state.scenarios,
		)
		reqSeq++
		scenarioSeq += len(newScenarioIDs)
		migratedTaskIDs[task.ID] = struct{}{}
		result.RequirementsCreated++
		result.ScenariosCreated += len(newScenarioIDs)
		result.TasksMigrated++
	}

	if result.TasksMigrated == 0 {
		return result, nil
	}

	// Validate: every migrated task must have at least one ScenarioID.
	for i := range state.tasks {
		if _, wasMigrated := migratedTaskIDs[state.tasks[i].ID]; !wasMigrated {
			continue
		}
		if len(state.tasks[i].ScenarioIDs) == 0 {
			return nil, fmt.Errorf("validation failed: task %q has no ScenarioIDs after migration", state.tasks[i].ID)
		}
	}

	if err := persistMigrationResults(ctx, manager, slug, state.requirements, state.scenarios, state.tasks); err != nil {
		return nil, err
	}

	return result, nil
}
