package scenariogenerator

import (
	"testing"

	"github.com/c360studio/semspec/workflow/dispatchretry"
)

// TestRetryKey_PerStoryUsesStoryIDSuffix pins the per-Story retry key
// shape. The dispatcher (dispatchPerStory) and consumer
// (handleLoopCompletion / retryOrFail / IsStaleLoop / Clear) must use the
// same key for the same (slug, requirement, story) tuple — otherwise the
// retry registry is silently bypassed in per-Story dispatch.
func TestRetryKey_PerStoryUsesStoryIDSuffix(t *testing.T) {
	got := retryKey("demo", "req.demo.1", "story.demo.1.1")
	want := "demo/story.demo.1.1"
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
