package recoveryagent

import (
	"reflect"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow/payloads"
)

// TestBuildRecoveryPlanDecision_ThreadsAffectedStoryIDsThrough is the
// Train C step 2 wire-shape contract: RecoveryRequested.AffectedStoryIDs
// (populated by requirement-executor from the wedged exec's Story
// cursor) MUST propagate through buildRecoveryPlanDecision into
// PlanDecision.AffectedStoryIDs. plan-manager downstream consumes that
// list to scope the story_reprepare cascade + applyRecoveryHint to the
// specific Stories rather than the whole Requirement.
func TestBuildRecoveryPlanDecision_ThreadsAffectedStoryIDsThrough(t *testing.T) {
	req := &payloads.RecoveryRequested{
		RecoveryID:       "12345678-aaaa-bbbb-cccc-dddddddddddd",
		Slug:             "demo",
		Layer:            payloads.RecoveryLayerPhaseLocal,
		RequirementID:    "req.demo.1",
		AffectedStoryIDs: []string{"story.demo.1.1", "story.demo.1.2"},
		EscalationReason: "wedge analysis points at Sarah's story-shaping",
	}

	dec := buildRecoveryPlanDecision(req, nil, payloads.RecoveryActionStoryReprepare, "Story 2's files_owned missed src/x.go", true, time.Now())

	want := []string{"story.demo.1.1", "story.demo.1.2"}
	if !reflect.DeepEqual(dec.AffectedStoryIDs, want) {
		t.Errorf("AffectedStoryIDs = %v, want %v", dec.AffectedStoryIDs, want)
	}
}

// TestBuildRecoveryPlanDecision_EmptyAffectedStoryIDsOmitsField pins the
// omitempty contract: legacy / single-Story / pre-ADR-043 wedges leave
// AffectedStoryIDs nil. The downstream cascade interprets nil/empty
// as "operate at Requirement granularity"; setting an empty slice
// explicitly here would be indistinguishable but is wasteful.
func TestBuildRecoveryPlanDecision_EmptyAffectedStoryIDsOmitsField(t *testing.T) {
	req := &payloads.RecoveryRequested{
		RecoveryID:       "12345678-aaaa-bbbb-cccc-dddddddddddd",
		Slug:             "legacy",
		Layer:            payloads.RecoveryLayerPhaseLocal,
		RequirementID:    "req.legacy.1",
		EscalationReason: "legacy req without Stories",
		// AffectedStoryIDs intentionally empty
	}

	dec := buildRecoveryPlanDecision(req, nil, payloads.RecoveryActionRefinePrompt, "refine the prompt", true, time.Now())

	if dec.AffectedStoryIDs != nil {
		t.Errorf("AffectedStoryIDs = %v, want nil (legacy wedge has no Stories in scope)", dec.AffectedStoryIDs)
	}
}
