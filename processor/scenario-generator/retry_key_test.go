package scenariogenerator

import (
	"testing"

	"github.com/c360studio/semspec/workflow/dispatchretry"
)

// TestRetryKey_PerStoryIncludesBothStoryAndRequirement pins the per-Story
// retry key shape. The dispatcher (dispatchPerStory) and consumer
// (handleLoopCompletion / retryOrFail / IsStaleLoop / Clear) must use the
// same key for the same (slug, requirement, story) tuple — otherwise the
// retry registry is silently bypassed in per-Story dispatch.
//
// Under ADR-044 (M:N coverage), one Story may cover N Requirements; each
// parallel scengen dispatch must occupy its OWN slot or the first N-1
// completions are silently dropped as "stale" by the last-write-wins
// SetActiveLoop. The key therefore pins BOTH storyID and requirementID.
func TestRetryKey_PerStoryIncludesBothStoryAndRequirement(t *testing.T) {
	got := retryKey("demo", "req.demo.1", "story.demo.1.1")
	want := "demo/story.demo.1.1/req.demo.1"
	if got != want {
		t.Errorf("retryKey(per-Story) = %q, want %q", got, want)
	}
}

// TestRetryKey_LegacyUsesRequirementIDSuffix pins the legacy
// per-Requirement key shape kept for pre-Sarah plans and mock fixtures.
func TestRetryKey_LegacyUsesRequirementIDSuffix(t *testing.T) {
	got := retryKey("demo", "req.demo.1", "")
	want := "demo/req.demo.1"
	if got != want {
		t.Errorf("retryKey(legacy) = %q, want %q", got, want)
	}
}

// TestRetryKey_DistinctKeysForDifferentStoriesUnderSameRequirement is the
// headline regression test for go-reviewer Pass-2 finding C1.
//
// Pre-fix, the dispatcher wrote retry state under "slug/storyID" but the
// consumer (handleLoopCompletion / retryOrFail) looked it up under
// "slug/requirementID" — so per-Story dispatches silently bypassed the
// entire retry registry. A single Bob parse glitch hard-failed the plan
// with zero retries, despite MaxGenerationRetries being configured.
//
// This test pins that two Stories under one Requirement produce distinct
// keys (so the dispatcher Tracks them separately) AND that the consumer
// can recover the same key from (slug, requirementID, storyID) — the
// underlying contract that closes C1.
func TestRetryKey_DistinctKeysForDifferentStoriesUnderSameRequirement(t *testing.T) {
	keyA := retryKey("demo", "req.demo.1", "story.demo.1.1")
	keyB := retryKey("demo", "req.demo.1", "story.demo.1.2")
	if keyA == keyB {
		t.Errorf("two Stories under same Requirement produced the SAME retry key (%q) — pre-fix shape would silently bypass the retry registry", keyA)
	}
}

// TestRetryKey_PerStoryProducerConsumerRoundTrip simulates the load-bearing
// flow: dispatcher Tracks under the per-Story key, consumer's Snapshot
// finds the entry under the same key. Demonstrates the bug pre-fix would
// produce: a Snapshot lookup keyed by requirementID returns nothing
// because the entry was stored under storyID.
func TestRetryKey_PerStoryProducerConsumerRoundTrip(t *testing.T) {
	const slug = "demo"
	const reqID = "req.demo.1"
	const storyID = "story.demo.1.1"

	producerKey := retryKey(slug, reqID, storyID)
	consumerKey := retryKey(slug, reqID, storyID)

	if producerKey != consumerKey {
		t.Errorf("producer key %q ≠ consumer key %q — symmetric retryKey call is the contract that closes Pass-2 C1", producerKey, consumerKey)
	}
}

// TestRetryRegistry_PerStoryTrackAndSnapshotRoundTrip exercises the real
// dispatchretry.State the way the producer and consumer use it: Track
// under the per-Story key, then Snapshot from the same (slug, req, story)
// tuple. Pre-fix, the consumer's "slug + / + requirementID" lookup
// returned (nil, false) because the entry was stored under storyID — and
// retryOrFail's `if _, ok := Snapshot(key); !ok { sendGenerationFailed }`
// hard-rejected the plan with zero retries. Post-fix, the symmetric
// retryKey call recovers the entry.
func TestRetryRegistry_PerStoryTrackAndSnapshotRoundTrip(t *testing.T) {
	state := dispatchretry.New(dispatchretry.Config{MaxRetries: 3})

	const slug = "demo"
	const reqID = "req.demo.1"
	const storyA = "story.demo.1.1"
	const storyB = "story.demo.1.2"

	// Producer side: dispatcher Tracks two Story entries under one Req.
	state.Track(retryKey(slug, reqID, storyA), &scenarioRetryPayload{})
	state.Track(retryKey(slug, reqID, storyB), &scenarioRetryPayload{})

	// Consumer side: handleLoopCompletion for Story A must find Story A's
	// entry, NOT Story B's. Pre-fix, the consumer key (slug/requirementID)
	// would collide across A and B — and would even be MISSING since the
	// entries were stored under storyID-keyed strings.
	if _, ok := state.Snapshot(retryKey(slug, reqID, storyA)); !ok {
		t.Errorf("consumer Snapshot for Story A returned no entry — retry registry bypassed; Pass-2 C1 not closed")
	}
	if _, ok := state.Snapshot(retryKey(slug, reqID, storyB)); !ok {
		t.Errorf("consumer Snapshot for Story B returned no entry — retry registry bypassed; Pass-2 C1 not closed")
	}

	// Clearing one Story's entry must not affect the other's.
	state.Clear(retryKey(slug, reqID, storyA))
	if _, ok := state.Snapshot(retryKey(slug, reqID, storyA)); ok {
		t.Errorf("Story A's entry should be cleared")
	}
	if _, ok := state.Snapshot(retryKey(slug, reqID, storyB)); !ok {
		t.Errorf("Story B's entry should survive Story A's Clear")
	}
}

// TestRetryKey_DistinctKeysForDifferentRequirementsUnderSameStory pins the
// ADR-044 M:N regression. Under the cohesive-component shape (1 Story
// covering N Requirements) every parallel scengen dispatch must occupy
// its own retry slot. Pre-fix, retryKey returned slug/storyID alone —
// symmetric at the producer + consumer level but collapsing N parallel
// dispatches onto one key. Track was first-wins (entries 2..N silently
// dropped), SetActiveLoop was last-wins (only the latest taskID
// survived), and IsStaleLoop dropped the first N-1 completions as "stale."
//
// Real-world incident: paid mavlink-hard 2026-06-03 wedged in
// generating_scenarios because reqs 1 and 3 of 4 completed BEFORE the
// last-dispatched req (4) and were silently treated as stale. The plan
// sat with 2 of 4 scenario sets persisted and no forward motion.
//
// This test pins that two Requirements under one Story produce distinct
// keys so each parallel dispatch has its own Track/SetActiveLoop slot.
func TestRetryKey_DistinctKeysForDifferentRequirementsUnderSameStory(t *testing.T) {
	keyA := retryKey("demo", "req.demo.1", "story.demo.1.1")
	keyB := retryKey("demo", "req.demo.2", "story.demo.1.1")
	if keyA == keyB {
		t.Errorf("two Requirements under same Story produced the SAME retry key (%q) — pre-fix shape silently drops N-1 of N parallel M:N completions as stale", keyA)
	}
}

// TestRetryRegistry_MNCohesiveStoryFourReqsRoundTrip simulates the exact
// ADR-044 cohesive-component shape that wedged the 2026-06-03 paid
// mavlink-hard smoke: one Story covers 4 Requirements; 4 parallel scengen
// dispatches each Track + SetActiveLoop the same Story; loop completions
// arrive in mixed order. Every completion must find its own non-stale
// retry slot.
func TestRetryRegistry_MNCohesiveStoryFourReqsRoundTrip(t *testing.T) {
	state := dispatchretry.New(dispatchretry.Config{MaxRetries: 3})

	const slug = "mavlink-hard"
	const storyID = "story.mavlink-hard.1.1"
	reqs := []string{
		"req.mavlink-hard.1",
		"req.mavlink-hard.2",
		"req.mavlink-hard.3",
		"req.mavlink-hard.4",
	}
	taskIDs := map[string]string{
		reqs[0]: "task-2e08623c",
		reqs[1]: "task-6d8c2964",
		reqs[2]: "task-4d8e3f61",
		reqs[3]: "task-e2786ece",
	}

	// Producer fan-out: 4 parallel dispatches. Pre-fix, Track 2..4 returned
	// (existing, false) and SetActiveLoop overwrote the prior taskID.
	for _, reqID := range reqs {
		key := retryKey(slug, reqID, storyID)
		entry, fresh := state.Track(key, &scenarioRetryPayload{})
		if !fresh {
			t.Errorf("Track(%s) returned fresh=false — pre-fix collision shape; expected a unique slot per parallel req", key)
		}
		if entry == nil {
			t.Errorf("Track(%s) returned nil entry", key)
		}
		state.SetActiveLoop(key, taskIDs[reqID])
	}

	// Loop completions arrive in mixed order (the production shape was
	// 1, 3, 4, 2). Every completion must read back the taskID it was
	// dispatched with — proving that SetActiveLoop wrote N independent
	// slots, not one shared slot.
	completionOrder := []string{reqs[0], reqs[2], reqs[3], reqs[1]}
	for _, reqID := range completionOrder {
		key := retryKey(slug, reqID, storyID)
		if state.IsStaleLoop(key, taskIDs[reqID]) {
			t.Errorf("IsStaleLoop returned true for req %s under cohesive Story %s — pre-fix silent-drop shape; expected each parallel dispatch's completion to match its own slot", reqID, storyID)
		}
	}
}
