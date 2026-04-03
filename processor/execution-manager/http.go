package executionmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// RegisterHTTPHandlers registers execution-manager HTTP endpoints under prefix.
//
// Registered routes:
//
//	GET {prefix}plans/{slug}/stream        — SSE stream of all execution updates
//	GET {prefix}plans/{slug}/tasks         — list active task executions
//	GET {prefix}plans/{slug}/requirements  — list active requirement executions
//	GET {prefix}lessons                    — list recent lessons
//	GET {prefix}lessons/counts             — per-role per-category error counts
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	mux.HandleFunc(prefix+"plans/", c.handlePlanExecutions)
	mux.HandleFunc(prefix+"lessons", c.handleLessons)
}

// handlePlanExecutions routes plan-scoped execution requests by subpath.
func (c *Component) handlePlanExecutions(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	plansIdx := strings.Index(path, "plans/")
	if plansIdx < 0 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	remainder := path[plansIdx+len("plans/"):]

	parts := strings.SplitN(remainder, "/", 2)
	slug := parts[0]
	if slug == "" {
		http.Error(w, "slug required", http.StatusBadRequest)
		return
	}

	subpath := ""
	if len(parts) > 1 {
		subpath = parts[1]
	}

	switch subpath {
	case "stream":
		c.handleExecutionStream(w, r, slug)
	case "tasks":
		c.handleListTasks(w, r, slug)
	case "requirements":
		c.handleListRequirements(w, r, slug)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// handleLessons routes lesson-related requests.
// GET {prefix}lessons        — list recent lessons (optional ?role= filter)
// GET {prefix}lessons/counts — per-role lesson counts (optional ?role= filter)
func (c *Component) handleLessons(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if c.lessonWriter == nil {
		http.Error(w, "lesson store not available", http.StatusServiceUnavailable)
		return
	}

	path := r.URL.Path
	if strings.HasSuffix(path, "/counts") {
		c.handleLessonCounts(w, r)
		return
	}
	c.handleListLessons(w, r)
}

// handleListLessons returns recent lessons, optionally filtered by role.
func (c *Component) handleListLessons(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	role := r.URL.Query().Get("role")
	lessons, err := c.lessonWriter.ListLessonsForRole(ctx, role, 50)
	if err != nil {
		c.logger.Error("Failed to list lessons", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if lessons == nil {
		lessons = []workflow.Lesson{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lessons) //nolint:errcheck
}

// handleLessonCounts returns per-category error counts for a role.
func (c *Component) handleLessonCounts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	role := r.URL.Query().Get("role")
	if role == "" {
		role = "developer"
	}
	counts, err := c.lessonWriter.GetRoleLessonCounts(ctx, role)
	if err != nil {
		c.logger.Error("Failed to get lesson counts", "role", role, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(counts) //nolint:errcheck
}
