package executionmanager

import (
	"encoding/json"
	"net/http"

	"github.com/c360studio/semspec/workflow"
)

// handleListTasks returns all active task executions for a plan slug.
//
// GET /execution-manager/plans/{slug}/tasks
func (c *Component) handleListTasks(w http.ResponseWriter, r *http.Request, slug string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	c.mu.RLock()
	store := c.store
	c.mu.RUnlock()

	if store == nil {
		http.Error(w, "Execution store not available", http.StatusServiceUnavailable)
		return
	}

	tasks := store.listTasksForSlug(r.Context(), slug)
	if tasks == nil {
		tasks = []*workflow.TaskExecution{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks) //nolint:errcheck
}

// handleListRequirements returns all active requirement executions for a plan slug.
//
// GET /execution-manager/plans/{slug}/requirements
func (c *Component) handleListRequirements(w http.ResponseWriter, r *http.Request, slug string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	c.mu.RLock()
	store := c.store
	c.mu.RUnlock()

	if store == nil {
		http.Error(w, "Execution store not available", http.StatusServiceUnavailable)
		return
	}

	reqs := store.listReqsForSlug(r.Context(), slug)
	if reqs == nil {
		reqs = []*workflow.RequirementExecution{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reqs) //nolint:errcheck
}
