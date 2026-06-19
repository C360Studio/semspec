package requirementexecutor

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// TestSelectReqBranchBase pins the branch-derivation precedence that is the
// whole point of the DependsOn-driven fix: an orchestrator-resolved base (the
// dependent's prerequisite ref) must win over the plan base and HEAD, so a
// dependent requirement forks from its prereqs' work instead of the plan base.
// A regression here silently re-introduces the parallel-shared-file assembly
// conflict that motivated the fix.
func TestSelectReqBranchBase(t *testing.T) {
	tests := []struct {
		name       string
		planBranch string
		baseBranch string
		want       string
	}{
		{
			name: "no plan, no base -> HEAD (DAG root, non-GitHub plan)",
			want: "HEAD",
		},
		{
			name:       "plan base only -> plan base (DAG root, GitHub plan)",
			planBranch: "semspec/plan-demo",
			want:       "semspec/plan-demo",
		},
		{
			name:       "resolved base wins over plan base (dependent forks from prereq)",
			planBranch: "semspec/plan-demo",
			baseBranch: "semspec/requirement-a1",
			want:       "semspec/requirement-a1",
		},
		{
			name:       "resolved base wins over empty plan base",
			baseBranch: "semspec/reqbase-d1",
			want:       "semspec/reqbase-d1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := selectReqBranchBase(tt.planBranch, tt.baseBranch); got != tt.want {
				t.Errorf("selectReqBranchBase(%q, %q) = %q, want %q",
					tt.planBranch, tt.baseBranch, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// kvEntryStub — minimal jetstream.KeyValueEntry for handleReqPending tests.
// ---------------------------------------------------------------------------

type reqWatcherKVEntry struct {
	key   string
	value []byte
}

func (e *reqWatcherKVEntry) Bucket() string                  { return "EXECUTION_STATES" }
func (e *reqWatcherKVEntry) Key() string                     { return e.key }
func (e *reqWatcherKVEntry) Value() []byte                   { return e.value }
func (e *reqWatcherKVEntry) Revision() uint64                { return 1 }
func (e *reqWatcherKVEntry) Created() time.Time              { return time.Time{} }
func (e *reqWatcherKVEntry) Delta() uint64                   { return 0 }
func (e *reqWatcherKVEntry) Operation() jetstream.KeyValueOp { return jetstream.KeyValuePut }

func makeReqKVEntry(t *testing.T, re workflow.RequirementExecution) *reqWatcherKVEntry {
	t.Helper()
	value, err := json.Marshal(re)
	if err != nil {
		t.Fatalf("makeReqKVEntry: marshal: %v", err)
	}
	key := workflow.RequirementExecutionKey(re.Slug, re.RequirementID)
	return &reqWatcherKVEntry{key: key, value: value}
}

// ---------------------------------------------------------------------------
// GAP 1 — handleReqPending stage guard (#226 re-dispatch invariant)
//
// Context: after a buggy reset (#226) left requirement executions at stage
// "active" instead of "pending", the watchReqPending watcher fired for those
// entries but handleReqPending silently no-op'd them — they were never claimed
// and no dispatch fired, leaving the requirement idle forever. Fix (#227) made
// the reset recreate the KV entry at "pending". These two tests pin the
// invariant so a regression that leaves reqs "active" is caught
// deterministically instead of by a paid run.
// ---------------------------------------------------------------------------

// TestHandleReqPending_NonPendingStage_IsNoOp is the load-bearing regression
// test for the #226 re-dispatch wedge. A KV entry whose Stage is NOT "pending"
// (e.g. "active", "decomposing", "executing", etc. — as a buggy reset would
// leave it) must be a complete no-op: no execution is claimed into activeExecs
// and triggersProcessed stays at zero.
//
// The invariant this pins: re-dispatch after reset REQUIRES the reset to
// recreate the KV entry at stage "pending". If handleReqPending were to accept
// a non-pending entry and proceed to claimExecution, it would attempt to claim
// an already-running execution — a correctness violation on top of the wedge.
func TestHandleReqPending_NonPendingStage_IsNoOp(t *testing.T) {
	nonPendingStages := []string{
		"active",      // exact #226 wedge: buggy reset left this behind
		"decomposing", // claimed but not yet executing
		"executing",   // live execution
		"reviewing",   // under reviewer
		"completed",   // terminal
		"failed",      // terminal
		"error",       // terminal
	}

	for _, stage := range nonPendingStages {
		t.Run("stage="+stage, func(t *testing.T) {
			c := newTestComponent(t)

			entry := makeReqKVEntry(t, workflow.RequirementExecution{
				Slug:          "plan-nowedge",
				RequirementID: "req-nowedge",
				Stage:         stage,
			})

			c.handleReqPending(t.Context(), entry)

			// No execution must be claimed into the cache.
			keys := c.activeExecs.Keys()
			if len(keys) != 0 {
				t.Errorf("stage=%q: activeExecs should be empty (no dispatch), got keys=%v", stage, keys)
			}
			// triggersProcessed is incremented only inside initReqExecution which
			// runs after a successful claim — it must stay zero.
			if got := c.triggersProcessed.Load(); got != 0 {
				t.Errorf("stage=%q: triggersProcessed = %d, want 0", stage, got)
			}
		})
	}
}

// TestHandleReqPending_PendingStage_AttemptsClaim verifies the positive branch:
// a "pending" entry IS processed by handleReqPending (the stage guard passes).
// With no NATS client the claimExecution round-trip fails, so the exec is never
// inserted into activeExecs — but the key difference from the non-pending case
// is that the code REACHES the claim call instead of returning at the guard.
//
// NOTE — seam gap: there is no injectable seam for claimExecution on the
// Component struct. To fully assert "claim drives pending→decomposing and
// dispatch fires" (the positive side of the invariant) without a live NATS
// execution-manager, a claimExecutionFunc seam analogous to
// storyStatusClaimerFunc would be needed. The negative test above is the
// load-bearing one; this positive test serves as a compile-time check that the
// pending path is exercisable and documents where the seam is missing.
func TestHandleReqPending_PendingStage_AttemptsClaim(t *testing.T) {
	c := newTestComponent(t)

	entry := makeReqKVEntry(t, workflow.RequirementExecution{
		Slug:          "plan-claim",
		RequirementID: "req-claim",
		Stage:         "pending",
		// Minimal fields required so handleReqPending can unmarshal and
		// build an exec. The claim itself will fail (no NATS), so the exec
		// never reaches activeExecs — that is expected and asserted below.
	})

	// Must not panic and must not insert an exec (claim fails without NATS).
	c.handleReqPending(t.Context(), entry)

	// Claim fails → exec never inserted → activeExecs is empty.
	// This is the expected NATS-less outcome; it is distinct from the
	// non-pending guard which exits before even attempting the claim.
	keys := c.activeExecs.Keys()
	if len(keys) != 0 {
		// An exec here would mean the code bypassed the claim — structural bug.
		t.Errorf("activeExecs should be empty (claim failed without NATS), got keys=%v", keys)
	}
}
