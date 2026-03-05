package reactive

import (
	"context"
	"fmt"
	"log/slog"
)

// ---------------------------------------------------------------------------
// WorktreeDiscarder / GraphCleaner interfaces
// ---------------------------------------------------------------------------

// WorktreeDiscarder is the subset of spawn.WorktreeManager needed for rollback.
// The full WorktreeManager satisfies this interface; the narrow interface lets
// ResetProtocol avoid a direct dependency on the spawn package.
type WorktreeDiscarder interface {
	Discard(ctx context.Context, worktreePath string) error
}

// GraphCleaner removes graph entities that were created by a specific agent
// loop run. Implementations are responsible for identifying which entities
// belong to the given loop (typically via a provenance predicate or loop-ID
// attribute recorded at creation time).
type GraphCleaner interface {
	// DeleteEntitiesByLoop removes all entities attributed to loopID.
	// This includes code entities, provenance records, and any other triples
	// written during the loop's execution.
	DeleteEntitiesByLoop(ctx context.Context, loopID string) error
}

// ---------------------------------------------------------------------------
// RollbackResult
// ---------------------------------------------------------------------------

// RollbackResult captures what happened during a rollback attempt.
// Both cleanup operations are best-effort; partial success is valid and
// recorded here for observability.
type RollbackResult struct {
	LoopID            string `json:"loop_id"`
	WorktreeDiscarded bool   `json:"worktree_discarded"`
	WorktreeError     string `json:"worktree_error,omitempty"`
	GraphCleaned      bool   `json:"graph_cleaned"`
	GraphError        string `json:"graph_error,omitempty"`
}

// Success returns true only when both cleanup steps completed without error.
func (r *RollbackResult) Success() bool {
	return r.WorktreeDiscarded && r.GraphCleaned
}

// Error returns a combined error message when one or both steps failed, or an
// empty string when both succeeded.
func (r *RollbackResult) Error() string {
	switch {
	case r.WorktreeError != "" && r.GraphError != "":
		return fmt.Sprintf("worktree: %s; graph: %s", r.WorktreeError, r.GraphError)
	case r.WorktreeError != "":
		return r.WorktreeError
	case r.GraphError != "":
		return r.GraphError
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// ResetProtocol
// ---------------------------------------------------------------------------

// ResetProtocol encapsulates the cleanup actions needed when an agent loop
// fails. It coordinates worktree disposal and graph cleanup, running both
// steps as best-effort so that a failure in one does not prevent the other.
//
// The protocol is intentionally passive: it performs cleanup when called and
// returns a result. It does not publish events or transition workflow state —
// that responsibility belongs to the reactive engine rules that invoke it.
type ResetProtocol struct {
	worktrees WorktreeDiscarder
	graph     GraphCleaner
}

// NewResetProtocol constructs a ResetProtocol.
func NewResetProtocol(w WorktreeDiscarder, g GraphCleaner) *ResetProtocol {
	return &ResetProtocol{
		worktrees: w,
		graph:     g,
	}
}

// RollbackLoop performs full cleanup for a failed agent loop:
//  1. Discard the worktree at worktreePath (file rollback) — best effort.
//     Skipped when worktreePath is empty (loop never claimed a worktree).
//  2. Delete graph entities attributed to loopID — best effort.
//
// Both steps are attempted regardless of whether the other succeeds.
// The returned RollbackResult captures what succeeded and what failed for
// structured logging and observability downstream.
func (r *ResetProtocol) RollbackLoop(ctx context.Context, loopID, worktreePath string) *RollbackResult {
	result := &RollbackResult{LoopID: loopID}

	// Step 1: Discard the worktree if a path was recorded for this loop.
	if worktreePath == "" {
		// No worktree was created for this loop — mark as discarded (nothing to do).
		result.WorktreeDiscarded = true
	} else {
		if err := r.worktrees.Discard(ctx, worktreePath); err != nil {
			result.WorktreeError = err.Error()
			slog.WarnContext(ctx, "reset: worktree discard failed",
				"loop_id", loopID,
				"worktree_path", worktreePath,
				"error", err,
			)
		} else {
			result.WorktreeDiscarded = true
		}
	}

	// Step 2: Delete graph entities attributed to this loop.
	if err := r.graph.DeleteEntitiesByLoop(ctx, loopID); err != nil {
		result.GraphError = err.Error()
		slog.WarnContext(ctx, "reset: graph cleanup failed",
			"loop_id", loopID,
			"error", err,
		)
	} else {
		result.GraphCleaned = true
	}

	return result
}
