package reactive

import (
	"context"
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type mockDiscarder struct {
	called     bool
	calledWith string
	err        error
}

func (m *mockDiscarder) Discard(_ context.Context, worktreePath string) error {
	m.called = true
	m.calledWith = worktreePath
	return m.err
}

type mockCleaner struct {
	called     bool
	calledWith string
	err        error
}

func (m *mockCleaner) DeleteEntitiesByLoop(_ context.Context, loopID string) error {
	m.called = true
	m.calledWith = loopID
	return m.err
}

// ---------------------------------------------------------------------------
// TestRollbackLoop_BothSucceed
// ---------------------------------------------------------------------------

func TestRollbackLoop_BothSucceed(t *testing.T) {
	discarder := &mockDiscarder{}
	cleaner := &mockCleaner{}
	rp := NewResetProtocol(discarder, cleaner)

	result := rp.RollbackLoop(context.Background(), "loop-001", "/tmp/worktrees/loop-001")

	if result.LoopID != "loop-001" {
		t.Errorf("expected LoopID 'loop-001', got %q", result.LoopID)
	}
	if !result.WorktreeDiscarded {
		t.Error("expected WorktreeDiscarded=true")
	}
	if result.WorktreeError != "" {
		t.Errorf("expected no WorktreeError, got %q", result.WorktreeError)
	}
	if !result.GraphCleaned {
		t.Error("expected GraphCleaned=true")
	}
	if result.GraphError != "" {
		t.Errorf("expected no GraphError, got %q", result.GraphError)
	}
	if !result.Success() {
		t.Error("expected Success()=true when both steps succeed")
	}
	if result.Error() != "" {
		t.Errorf("expected empty Error(), got %q", result.Error())
	}

	// Verify mock interactions.
	if !discarder.called {
		t.Error("expected Discard to be called")
	}
	if discarder.calledWith != "/tmp/worktrees/loop-001" {
		t.Errorf("expected Discard called with '/tmp/worktrees/loop-001', got %q", discarder.calledWith)
	}
	if !cleaner.called {
		t.Error("expected DeleteEntitiesByLoop to be called")
	}
	if cleaner.calledWith != "loop-001" {
		t.Errorf("expected DeleteEntitiesByLoop called with 'loop-001', got %q", cleaner.calledWith)
	}
}

// ---------------------------------------------------------------------------
// TestRollbackLoop_WorktreeFailsGraphSucceeds
// ---------------------------------------------------------------------------

func TestRollbackLoop_WorktreeFailsGraphSucceeds(t *testing.T) {
	discarder := &mockDiscarder{err: errors.New("worktree locked")}
	cleaner := &mockCleaner{}
	rp := NewResetProtocol(discarder, cleaner)

	result := rp.RollbackLoop(context.Background(), "loop-002", "/tmp/worktrees/loop-002")

	if result.WorktreeDiscarded {
		t.Error("expected WorktreeDiscarded=false when discard fails")
	}
	if result.WorktreeError == "" {
		t.Error("expected WorktreeError to be set")
	}

	// Graph cleanup must still run even though worktree failed.
	if !cleaner.called {
		t.Error("graph cleaner must be called even when worktree discard fails")
	}
	if !result.GraphCleaned {
		t.Error("expected GraphCleaned=true")
	}
	if result.GraphError != "" {
		t.Errorf("expected no GraphError, got %q", result.GraphError)
	}

	if result.Success() {
		t.Error("expected Success()=false when worktree step fails")
	}
	if result.Error() == "" {
		t.Error("expected non-empty Error() when worktree step fails")
	}
}

// ---------------------------------------------------------------------------
// TestRollbackLoop_GraphFailsWorktreeSucceeds
// ---------------------------------------------------------------------------

func TestRollbackLoop_GraphFailsWorktreeSucceeds(t *testing.T) {
	discarder := &mockDiscarder{}
	cleaner := &mockCleaner{err: errors.New("graph unavailable")}
	rp := NewResetProtocol(discarder, cleaner)

	result := rp.RollbackLoop(context.Background(), "loop-003", "/tmp/worktrees/loop-003")

	// Worktree must have been discarded successfully.
	if !discarder.called {
		t.Error("expected Discard to be called")
	}
	if !result.WorktreeDiscarded {
		t.Error("expected WorktreeDiscarded=true")
	}
	if result.WorktreeError != "" {
		t.Errorf("expected no WorktreeError, got %q", result.WorktreeError)
	}

	// Graph failed.
	if result.GraphCleaned {
		t.Error("expected GraphCleaned=false when graph cleanup fails")
	}
	if result.GraphError == "" {
		t.Error("expected GraphError to be set")
	}

	if result.Success() {
		t.Error("expected Success()=false when graph step fails")
	}
	if result.Error() == "" {
		t.Error("expected non-empty Error() when graph step fails")
	}
}

// ---------------------------------------------------------------------------
// TestRollbackLoop_BothFail
// ---------------------------------------------------------------------------

func TestRollbackLoop_BothFail(t *testing.T) {
	discarder := &mockDiscarder{err: errors.New("disk full")}
	cleaner := &mockCleaner{err: errors.New("connection refused")}
	rp := NewResetProtocol(discarder, cleaner)

	result := rp.RollbackLoop(context.Background(), "loop-004", "/tmp/worktrees/loop-004")

	if result.WorktreeDiscarded {
		t.Error("expected WorktreeDiscarded=false")
	}
	if result.WorktreeError == "" {
		t.Error("expected WorktreeError to be set")
	}
	if result.GraphCleaned {
		t.Error("expected GraphCleaned=false")
	}
	if result.GraphError == "" {
		t.Error("expected GraphError to be set")
	}

	if result.Success() {
		t.Error("expected Success()=false when both steps fail")
	}

	// Error() must mention both failures.
	errMsg := result.Error()
	if errMsg == "" {
		t.Error("expected non-empty Error()")
	}
	// The combined error must reference both individual errors.
	if result.WorktreeError != "disk full" {
		t.Errorf("expected WorktreeError 'disk full', got %q", result.WorktreeError)
	}
	if result.GraphError != "connection refused" {
		t.Errorf("expected GraphError 'connection refused', got %q", result.GraphError)
	}
}

// ---------------------------------------------------------------------------
// TestRollbackLoop_EmptyWorktreePath
// ---------------------------------------------------------------------------

func TestRollbackLoop_EmptyWorktreePath(t *testing.T) {
	discarder := &mockDiscarder{}
	cleaner := &mockCleaner{}
	rp := NewResetProtocol(discarder, cleaner)

	// Empty worktreePath means no worktree was allocated — skip discard.
	result := rp.RollbackLoop(context.Background(), "loop-005", "")

	if discarder.called {
		t.Error("Discard must NOT be called when worktreePath is empty")
	}
	// Worktree is considered discarded (nothing to do).
	if !result.WorktreeDiscarded {
		t.Error("expected WorktreeDiscarded=true when no worktree path is provided")
	}
	if result.WorktreeError != "" {
		t.Errorf("expected no WorktreeError, got %q", result.WorktreeError)
	}

	// Graph cleanup still runs.
	if !cleaner.called {
		t.Error("graph cleaner must still be called when worktree path is empty")
	}
	if !result.GraphCleaned {
		t.Error("expected GraphCleaned=true")
	}
	if !result.Success() {
		t.Error("expected Success()=true when no worktree and graph succeeds")
	}
}
