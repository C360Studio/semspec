package workflowapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/nats-io/nats.go/jetstream"
)

// RegisterHTTPHandlers registers HTTP handlers for the workflow-api component.
// The prefix includes the trailing slash (e.g., "/workflow-api/").
// This includes both workflow endpoints and Q&A endpoints.
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	// Workflow endpoints
	mux.HandleFunc(prefix+"plans/", c.handleGetPlanReviews)

	// Q&A endpoints (delegated to question handler)
	// These are registered at /workflow-api/questions/* instead of /questions/*
	// to keep them scoped under this component's prefix
	c.mu.RLock()
	questionHandler := c.questionHandler
	c.mu.RUnlock()

	if questionHandler != nil {
		questionHandler.RegisterHTTPHandlers(prefix+"questions", mux)
	}
}

// WorkflowExecution represents a workflow execution from the KV bucket.
// This mirrors the semstreams workflow execution structure.
type WorkflowExecution struct {
	ID           string                 `json:"id"`
	WorkflowID   string                 `json:"workflow_id"`
	WorkflowName string                 `json:"workflow_name"`
	State        string                 `json:"state"`
	StepResults  map[string]*StepResult `json:"step_results"`
	Trigger      json.RawMessage        `json:"trigger"`
	Error        string                 `json:"error,omitempty"`
	CreatedAt    string                 `json:"created_at"`
	UpdatedAt    string                 `json:"updated_at"`
}

// StepResult represents a single step's result within an execution.
type StepResult struct {
	StepName  string          `json:"step_name"`
	Status    string          `json:"status"`
	Output    json.RawMessage `json:"output"`
	Error     string          `json:"error,omitempty"`
	Iteration int             `json:"iteration"`
}

// TriggerPayload represents the trigger data structure.
type TriggerPayload struct {
	WorkflowID string       `json:"workflow_id"`
	Data       *TriggerData `json:"data,omitempty"`
}

// TriggerData contains semspec-specific fields.
type TriggerData struct {
	Slug        string `json:"slug,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// handleGetPlanReviews handles GET /plans/{slug}/reviews
// Returns the review synthesis result for the given plan slug.
func (c *Component) handleGetPlanReviews(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract slug from path: /workflow-api/plans/{slug}/reviews
	slug, endpoint := extractSlugAndEndpoint(r.URL.Path)
	if slug == "" {
		http.Error(w, "Plan slug required", http.StatusBadRequest)
		return
	}

	// Only handle /reviews endpoint
	if endpoint != "reviews" {
		http.Error(w, "Unknown endpoint", http.StatusNotFound)
		return
	}

	// Get execution bucket
	bucket, err := c.getExecBucket(r.Context())
	if err != nil {
		c.logger.Error("Failed to get execution bucket", "error", err)
		http.Error(w, "Workflow executions not available", http.StatusServiceUnavailable)
		return
	}

	// Find execution by slug
	exec, err := c.findExecutionBySlug(r.Context(), bucket, slug)
	if err != nil {
		c.logger.Error("Failed to find execution", "slug", slug, "error", err)
		http.Error(w, "Failed to retrieve execution", http.StatusInternalServerError)
		return
	}

	if exec == nil {
		http.Error(w, "Review not found", http.StatusNotFound)
		return
	}

	// Get review step result
	reviewResult := c.findReviewResult(exec)
	if reviewResult == nil {
		http.Error(w, "No completed review", http.StatusNotFound)
		return
	}

	// Return the review output directly (it's already SynthesisResult JSON)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(reviewResult.Output); err != nil {
		c.logger.Warn("Failed to write response", "error", err)
	}
}

// maxExecutionsToScan limits the number of executions to scan to prevent unbounded iteration.
const maxExecutionsToScan = 500

// findExecutionBySlug searches for a completed workflow execution with the given slug.
func (c *Component) findExecutionBySlug(ctx context.Context, bucket jetstream.KeyValue, slug string) (*WorkflowExecution, error) {
	if bucket == nil {
		return nil, nil
	}

	// List all keys
	keys, err := bucket.Keys(ctx)
	if err != nil {
		// No keys or empty bucket - return nil
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return nil, nil
		}
		return nil, err
	}

	var latestExec *WorkflowExecution

	for i, key := range keys {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Limit iterations to prevent unbounded scanning
		if i >= maxExecutionsToScan {
			c.logger.Warn("Execution scan limit reached", "limit", maxExecutionsToScan, "slug", slug)
			break
		}

		// Skip secondary index keys (e.g., TASK_xxx)
		if strings.HasPrefix(key, "TASK_") {
			continue
		}

		entry, err := bucket.Get(ctx, key)
		if err != nil {
			continue
		}

		var exec WorkflowExecution
		if err := json.Unmarshal(entry.Value(), &exec); err != nil {
			continue
		}

		// Parse trigger to check slug
		var trigger TriggerPayload
		if err := json.Unmarshal(exec.Trigger, &trigger); err != nil {
			continue
		}

		// Check if slug matches
		if trigger.Data != nil && trigger.Data.Slug == slug {
			// Check if this is a review workflow with completed state
			if exec.State == "completed" || exec.State == "running" {
				// Check if it has a review result
				if c.findReviewResult(&exec) != nil {
					// Return most recent completed one
					if latestExec == nil || exec.UpdatedAt > latestExec.UpdatedAt {
						execCopy := exec
						latestExec = &execCopy
					}
				}
			}
		}
	}

	return latestExec, nil
}

// findReviewResult looks for a completed review step result in the execution.
func (c *Component) findReviewResult(exec *WorkflowExecution) *StepResult {
	if exec.StepResults == nil {
		return nil
	}

	// Look for a step named "review" with success status
	if result, ok := exec.StepResults["review"]; ok && result.Status == "success" {
		return result
	}

	// Also check for "review-synthesis" or similar variants
	for name, result := range exec.StepResults {
		if strings.Contains(strings.ToLower(name), "review") && result.Status == "success" {
			// Verify it has output that looks like SynthesisResult
			if len(result.Output) > 0 {
				return result
			}
		}
	}

	return nil
}

// extractSlugAndEndpoint extracts slug and endpoint from path like /workflow-api/plans/{slug}/reviews
func extractSlugAndEndpoint(path string) (slug, endpoint string) {
	// Find /plans/ in the path
	idx := strings.Index(path, "/plans/")
	if idx == -1 {
		return "", ""
	}

	// Get everything after /plans/
	remainder := path[idx+len("/plans/"):]

	// Split by / to get slug and endpoint
	parts := strings.SplitN(remainder, "/", 2)
	if len(parts) == 0 {
		return "", ""
	}

	slug = parts[0]
	if len(parts) > 1 {
		endpoint = strings.TrimSuffix(parts[1], "/")
	}

	return slug, endpoint
}
