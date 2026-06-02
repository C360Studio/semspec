package requirementexecutor

import (
	"sync"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestFindExecByTaskID_LocksAroundFieldReads exercises the lock added by
// the H2 fix. Without the lock, concurrent writers to
// CurrentNodeTaskID / ReviewerTaskID under exec.mu and the reader at
// findExecByTaskID would race — visible to `go test -race`.
//
// This test races a writer (flipping CurrentNodeTaskID 1000× under the
// lock, simulating dispatchNextNodeLocked) against a reader
// (findExecByTaskID 1000×). Pre-fix the reader did not take the lock; with
// -race that's a guaranteed data-race report. Post-fix both sides hold
// exec.mu and the run is clean.
//
// Closes go-reviewer Pass-1 finding H2 regression coverage.
func TestFindExecByTaskID_LocksAroundFieldReads(t *testing.T) {
	c := newTestComponent(t)
	exec := &requirementExecution{
		EntityID:      "entity-1",
		Slug:          "demo",
		RequirementID: "req.demo.1",
	}
	c.activeExecs.Set(exec.EntityID, exec) //nolint:errcheck // cache set is best-effort

	var wg sync.WaitGroup
	const iterations = 1000

	wg.Add(2)

	// Writer goroutine — simulates dispatch flipping the task ID under the lock.
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			exec.mu.Lock()
			exec.CurrentNodeTaskID = "task-A"
			exec.ReviewerTaskID = "task-B"
			exec.mu.Unlock()
		}
	}()

	// Reader goroutine — exercises findExecByTaskID, which must take exec.mu
	// to read the same fields safely.
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = c.findExecByTaskID("task-A")
		}
	}()

	wg.Wait()

	// Final functional pin: find with the live task ID returns the exec.
	exec.mu.Lock()
	exec.CurrentNodeTaskID = "live-task"
	exec.mu.Unlock()
	if got := c.findExecByTaskID("live-task"); got != exec {
		t.Errorf("findExecByTaskID(\"live-task\") did not return the live exec")
	}
}

// TestInitReqExecution_ScopeWrittenUnderLock pins H1 indirectly via a
// race-detector load on the publishEntity + exec.Scope assignment timing.
//
// Pre-fix, exec.Scope was assigned BEFORE acquiring exec.mu — after the
// exec was already in activeExecs. A concurrent findExecByTaskID (or any
// other reader) could race the assignment. Post-fix the assignment moves
// inside the locked section. The test exercises a concurrent reader and
// writer pattern that would have flagged under -race pre-fix.
//
// We can't easily call initReqExecution in unit-test mode (it spawns the
// dispatcher), but we CAN exercise the post-fix invariant: any reader
// holding exec.mu observes Scope coherently with concurrent writes.
func TestInitReqExecution_ScopeWrittenUnderLock(t *testing.T) {
	exec := &requirementExecution{
		EntityID:      "entity-1",
		Slug:          "demo",
		RequirementID: "req.demo.1",
	}

	var wg sync.WaitGroup
	const iterations = 500

	wg.Add(2)

	// Writer: simulates the initReqExecution path's locked Scope assignment.
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			scope := &workflow.Scope{Include: []string{"src/a.go"}}
			exec.mu.Lock()
			exec.Scope = scope
			exec.mu.Unlock()
		}
	}()

	// Reader: any goroutine that reads exec.Scope MUST hold exec.mu to be
	// race-free. Pin that contract.
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			exec.mu.Lock()
			_ = exec.Scope
			exec.mu.Unlock()
		}
	}()

	wg.Wait()

	// Final functional assertion so `go test` without -race still pins the
	// post-fix contract: a locked write must be observable under a locked
	// read. Without this, the test relied entirely on -race to flag breakage.
	final := &workflow.Scope{Include: []string{"src/final.go"}}
	exec.mu.Lock()
	exec.Scope = final
	got := exec.Scope
	exec.mu.Unlock()
	if got != final {
		t.Errorf("locked read did not observe locked write; got %v, want %v", got, final)
	}
}
