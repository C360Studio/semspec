package cascade

import (
	"context"
	"fmt"

	"github.com/c360studio/semspec/workflow"
)

// CascadeResult summarizes the effect of accepting a ChangeProposal.
type CascadeResult struct {
	AffectedRequirementIDs []string
	AffectedScenarioIDs    []string
	AffectedTaskIDs        []string
	TasksDirtied           int
}

// CascadeChangeProposal executes the dirty cascade when a ChangeProposal is accepted.
//
// Steps:
//  1. Load all scenarios for the plan; filter to those whose RequirementID is in proposal.AffectedReqIDs.
//  2. Load all tasks for the plan; filter to those whose ScenarioIDs overlap with affected scenario IDs.
//  3. Set each matching task's status to dirty (unless terminal: completed or failed).
//  4. Persist updated tasks.
//  5. Return a CascadeResult describing what changed.
//
// The function is deliberately free of NATS or reactive-engine dependencies so it can be called
// directly from HTTP handlers and tested without infrastructure.
func CascadeChangeProposal(ctx context.Context, manager *workflow.Manager, slug string, proposal *workflow.ChangeProposal) (*CascadeResult, error) {
	if proposal == nil {
		return nil, fmt.Errorf("proposal is nil")
	}

	result := &CascadeResult{
		AffectedRequirementIDs: make([]string, 0, len(proposal.AffectedReqIDs)),
		AffectedScenarioIDs:    make([]string, 0),
		AffectedTaskIDs:        make([]string, 0),
	}

	// Copy affected requirement IDs into result.
	affectedReqs := make(map[string]bool, len(proposal.AffectedReqIDs))
	for _, id := range proposal.AffectedReqIDs {
		affectedReqs[id] = true
		result.AffectedRequirementIDs = append(result.AffectedRequirementIDs, id)
	}

	if len(affectedReqs) == 0 {
		// No requirements affected — nothing to cascade.
		return result, nil
	}

	// Step 1: Find scenarios belonging to affected requirements.
	allScenarios, err := manager.LoadScenarios(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("load scenarios: %w", err)
	}

	affectedScenarioIDs := make(map[string]bool)
	for _, sc := range allScenarios {
		if affectedReqs[sc.RequirementID] {
			affectedScenarioIDs[sc.ID] = true
			result.AffectedScenarioIDs = append(result.AffectedScenarioIDs, sc.ID)
		}
	}

	if len(affectedScenarioIDs) == 0 {
		// No scenarios linked to affected requirements — no tasks to dirty.
		return result, nil
	}

	// Step 2: Find tasks that satisfy any of the affected scenarios.
	tasks, err := manager.LoadTasks(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("load tasks: %w", err)
	}

	dirty := false
	for i := range tasks {
		if isTerminalTaskStatus(tasks[i].Status) {
			continue
		}
		if taskOverlapsScenarios(&tasks[i], affectedScenarioIDs) {
			if tasks[i].Status != workflow.TaskStatusDirty {
				tasks[i].Status = workflow.TaskStatusDirty
				result.TasksDirtied++
			}
			result.AffectedTaskIDs = append(result.AffectedTaskIDs, tasks[i].ID)
			dirty = true
		}
	}

	// Step 3: Persist only if any task was changed.
	if dirty {
		if err := manager.SaveTasks(ctx, tasks, slug); err != nil {
			return nil, fmt.Errorf("save tasks: %w", err)
		}
	}

	return result, nil
}

// isTerminalTaskStatus returns true for statuses that cannot transition to dirty.
func isTerminalTaskStatus(s workflow.TaskStatus) bool {
	return s == workflow.TaskStatusCompleted || s == workflow.TaskStatusFailed
}

// taskOverlapsScenarios returns true if any of the task's ScenarioIDs are in the affected set.
func taskOverlapsScenarios(task *workflow.Task, affectedScenarioIDs map[string]bool) bool {
	for _, sid := range task.ScenarioIDs {
		if affectedScenarioIDs[sid] {
			return true
		}
	}
	return false
}
