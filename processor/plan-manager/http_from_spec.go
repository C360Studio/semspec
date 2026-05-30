package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/graph"
	"github.com/c360studio/semspec/pkg/paths"
	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/specimport"
)

// repoPathFromConfig returns the component's configured repo path, falling
// back to SEMSPEC_REPO_PATH or "." per the config.RepoPath documented
// default. Lives here (not on Component) because repo path is only needed
// by the from-spec handler; the rest of plan-manager works against
// PLAN_STATES keys, not the filesystem.
func (c *Component) repoPathForFromSpec() string {
	if c.config.RepoPath != "" {
		return c.config.RepoPath
	}
	if env := os.Getenv("SEMSPEC_REPO_PATH"); env != "" {
		return env
	}
	wd, _ := os.Getwd()
	return wd
}

// writeFromSpecJSON encodes a JSON response with the given status. Local
// helper because plan-manager's package doesn't expose a generic writeJSON.
func writeFromSpecJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// isSafeChangeName rejects path-traversal vectors. A safe change_name is
// a single non-empty segment with no slashes, no parent-directory tokens,
// and no leading dot (which would target hidden dirs). Per go-reviewer PR
// 4 audit blocker #1.
func isSafeChangeName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	// Reject explicit parent-dir tokens anywhere in the string for paranoia.
	if strings.Contains(name, "..") {
		return false
	}
	return true
}

// CreatePlanFromSpecRequest is the body of POST /plan-manager/plans/from-spec.
// At minimum the request specifies which change to import. When RepoPath is
// empty, the handler resolves it from SEMSPEC_REPO_PATH or the working
// directory at startup.
type CreatePlanFromSpecRequest struct {
	// ChangeName is the openspec/changes/<name>/ directory to import.
	// Required.
	ChangeName string `json:"change_name"`

	// RepoPath is the absolute path to the repository root containing
	// openspec/. Optional — handler falls back to component config.
	RepoPath string `json:"repo_path,omitempty"`

	// Slug overrides the slug used for the imported Plan. Optional —
	// defaults to the change name. Slug uniqueness is enforced by
	// plan-manager.
	Slug string `json:"slug,omitempty"`

	// Title overrides the imported Plan's title. Optional.
	Title string `json:"title,omitempty"`

	// GraphReadinessBudgetMs lets callers tune the wait for semsource
	// indexing. Default 30000 (30s).
	GraphReadinessBudgetMs int `json:"graph_readiness_budget_ms,omitempty"`
}

// CreatePlanFromSpecResponse is the success body of POST /plan-manager/plans/from-spec.
type CreatePlanFromSpecResponse struct {
	Slug             string                       `json:"slug"`
	PlanID           string                       `json:"plan_id"`
	CapabilityCount  int                          `json:"capability_count"`
	RequirementCount int                          `json:"requirement_count"`
	ScenarioCount    int                          `json:"scenario_count"`
	StructuralResult *specimport.StructuralResult `json:"structural_result"`
	ExternalRefs     map[string]string            `json:"external_refs,omitempty"`
	// Warnings flags non-fatal import issues the operator should know about
	// (e.g. tripleWriter unavailable → round-trip identity not persisted).
	// Per go-reviewer PR 4 audit should-fix #5.
	Warnings []string `json:"warnings,omitempty"`
	Message  string   `json:"message"`
}

// FromSpecErrorResponse is the structured error body for failures. Mirrors
// the StructuralFinding shape so the UI can render findings inline.
type FromSpecErrorResponse struct {
	Error            string                       `json:"error"`
	StructuralResult *specimport.StructuralResult `json:"structural_result,omitempty"`
}

// handleCreatePlanFromSpec handles POST /plan-manager/plans/from-spec.
// Imports an openspec/changes/<name>/ directory as a semspec Plan
// (ADR-040 Move 4 / folded ADR-038).
func (c *Component) handleCreatePlanFromSpec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req CreatePlanFromSpecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ChangeName == "" {
		http.Error(w, "change_name is required", http.StatusBadRequest)
		return
	}
	// Path-traversal guard: change_name must be a single kebab-case-safe
	// segment with no path separators or upward references. Without this,
	// "../../../etc" would resolve outside openspec/changes/ via
	// filepath.Join (which cleans but does not reject upward escape).
	if !isSafeChangeName(req.ChangeName) {
		http.Error(w, "change_name must be a single path segment (no /, \\, or ..)", http.StatusBadRequest)
		return
	}

	repoPath := req.RepoPath
	if repoPath == "" {
		repoPath = c.repoPathForFromSpec()
	}
	if repoPath == "" {
		http.Error(w, "repo_path required (no SEMSPEC_REPO_PATH set on component)", http.StatusBadRequest)
		return
	}

	changePath := filepath.Join(repoPath, "openspec", "changes", req.ChangeName)

	// Layer 1: structural pre-check.
	sr, err := specimport.StructuralCheck(changePath)
	if err != nil {
		writeFromSpecJSON(w, http.StatusBadRequest, FromSpecErrorResponse{
			Error: fmt.Sprintf("structural check failed: %v", err),
		})
		return
	}
	if !sr.OK {
		writeFromSpecJSON(w, http.StatusBadRequest, FromSpecErrorResponse{
			Error:            "change directory failed structural pre-check",
			StructuralResult: sr,
		})
		return
	}

	// Resolve graph querier. Fail 503 when graph is unavailable so
	// the UI can prompt the operator to wait for semsource readiness.
	q := c.resolveGraphQuerier()
	if q == nil {
		writeFromSpecJSON(w, http.StatusServiceUnavailable, FromSpecErrorResponse{
			Error:            "graph querier unavailable — semsource may not be running",
			StructuralResult: sr,
		})
		return
	}

	// Translate.
	budget := time.Duration(req.GraphReadinessBudgetMs) * time.Millisecond
	tr, err := specimport.Translate(r.Context(), q, sr, specimport.TranslateOptions{
		Slug:                 req.Slug,
		Title:                req.Title,
		GraphReadinessBudget: budget,
	})
	if err != nil {
		writeFromSpecJSON(w, http.StatusServiceUnavailable, FromSpecErrorResponse{
			Error:            fmt.Sprintf("translation failed: %v", err),
			StructuralResult: sr,
		})
		return
	}

	// Persist via planStore (single-writer pattern).
	ps := c.planStoreOrFail(w)
	if ps == nil {
		return
	}
	if tr.Plan.Slug == "" {
		tr.Plan.Slug = paths.Slugify(sr.ChangeName)
	}
	if ps.exists(tr.Plan.Slug) {
		writeFromSpecJSON(w, http.StatusConflict, FromSpecErrorResponse{
			Error: fmt.Sprintf("plan %q already exists — pick a different slug or archive the existing plan", tr.Plan.Slug),
		})
		return
	}

	ctx := r.Context()
	// Single-shot persist via createImported — guarantees the first KV
	// write carries Status=StatusExplored so the planner component's
	// PLAN_STATES watcher routes the imported plan to routeExplored
	// (planner sub-phase) rather than routeCreated (analyst sub-phase).
	// Per go-reviewer PR 4 blocker #2.
	if err := ps.createImported(ctx, tr.Plan, c.resolveProjectQALevel(), nil); err != nil {
		http.Error(w, fmt.Sprintf("create imported plan: %v", err), http.StatusInternalServerError)
		return
	}
	plan := tr.Plan

	// Emit external_spec triples for round-trip identity. Best-effort —
	// the import succeeds even when the triple writer is unavailable, but
	// the response surfaces a warning so the operator knows the round-trip
	// backing wasn't persisted (PR 4 review #5).
	var warnings []string
	if c.tripleWriter == nil {
		warnings = append(warnings, "tripleWriter unavailable — external_spec triples not persisted; PR 3 outbound emitter will not recover source-graph identity until the next ENTITY_STATES write")
	} else {
		c.emitExternalSpecTriples(ctx, plan.Slug, tr.ExternalRefs)
	}

	resp := CreatePlanFromSpecResponse{
		Slug:             plan.Slug,
		PlanID:           plan.ID,
		CapabilityCount:  len(plan.Exploration.Capabilities),
		RequirementCount: len(plan.Requirements),
		ScenarioCount:    len(plan.Scenarios),
		StructuralResult: sr,
		ExternalRefs:     tr.ExternalRefs,
		Warnings:         warnings,
		Message:          fmt.Sprintf("Imported OpenSpec change %q as plan %q", sr.ChangeName, plan.Slug),
	}
	c.logger.Info("Imported OpenSpec change",
		"change", sr.ChangeName,
		"slug", plan.Slug,
		"capabilities", resp.CapabilityCount,
		"requirements", resp.RequirementCount)
	writeFromSpecJSON(w, http.StatusCreated, resp)
}

// resolveGraphQuerier returns the active graph querier or nil when graph
// access isn't configured (e.g. test environment). Wraps the global
// sources registry so the handler can be exercised without spinning up
// a real graph backend.
func (c *Component) resolveGraphQuerier() graph.Querier {
	reg := graph.GlobalSources()
	if reg == nil {
		return nil
	}
	return graph.NewFederatedGraphGatherer(reg, c.logger)
}

// emitExternalSpecTriples writes the round-trip identity triples that link
// imported entities back to their source-graph entity IDs. Per ADR-040
// rev 5 predicate table: semspec.capability.external_spec +
// semspec.requirement.external_spec.
func (c *Component) emitExternalSpecTriples(ctx context.Context, slug string, externalRefs map[string]string) {
	if c.tripleWriter == nil || len(externalRefs) == 0 {
		return
	}
	for key, externalID := range externalRefs {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		writeExternalSpecTriple(ctx, c.tripleWriter, parts[0], parts[1], slug, externalID)
	}
}

// writeExternalSpecTriple emits one external_spec triple. Best-effort —
// graph writes fail silently to keep the import path resilient.
func writeExternalSpecTriple(ctx context.Context, tw *graphutil.TripleWriter, kind, identifier, slug, externalID string) {
	if tw == nil || externalID == "" {
		return
	}
	switch kind {
	case "capability":
		entityID := workflow.CapabilityEntityID(slug, identifier)
		_ = tw.WriteTriple(ctx, entityID, semspec.CapabilityExternalSpec, externalID)
	case "requirement":
		entityID := workflow.RequirementEntityID(identifier)
		_ = tw.WriteTriple(ctx, entityID, semspec.RequirementExternalSpec, externalID)
	}
}
