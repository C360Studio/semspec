package workflow

import (
	"testing"
)

func TestPlanStatus_IsValid_NewStatuses(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
	}{
		{StatusRequirementsGenerated, true},
		{StatusScenariosGenerated, true},
		// Existing statuses still valid
		{StatusCreated, true},
		{StatusDrafted, true},
		{StatusReviewed, true},
		{StatusApproved, true},
		{StatusImplementing, true},
		{StatusComplete, true},
		{StatusArchived, true},
		{StatusRejected, true},
		// In-progress statuses
		{StatusDrafting, true},
		{StatusReviewingDraft, true},
		{StatusGeneratingRequirements, true},
		{StatusGeneratingScenarios, true},
		{StatusReviewingScenarios, true},
		// New statuses
		{StatusAwaitingReview, true},
		{StatusChanged, true},
		// QA phase statuses
		{StatusReadyForQA, true},
		{StatusReviewingQA, true},
		// ADR-040: analyst sub-phase statuses
		{StatusExploring, true},
		{StatusExplored, true},
		// ADR-051 Slice 3: architecture-review statuses
		{StatusReviewingArchitecture, true},
		{StatusArchitectureReviewed, true},
		// Invalid
		{"", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("Status(%q).IsValid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestPlanStatus_CanTransitionTo_NewStatuses(t *testing.T) {
	tests := []struct {
		from Status
		to   Status
		want bool
	}{
		// drafted -> requirements_generated (new flow: req/scenario gen before review)
		{StatusDrafted, StatusRequirementsGenerated, true},
		// drafted -> reviewed (legacy: review directly after drafting)
		{StatusDrafted, StatusReviewed, true},
		// drafted -> rejected
		{StatusDrafted, StatusRejected, true},
		// drafted -> approved (invalid, must go through reviewed first)
		{StatusDrafted, StatusApproved, false},

		// approved -> requirements_generated (backwards compat)
		{StatusApproved, StatusRequirementsGenerated, true},
		// approved -> ready_for_execution (auto-approve skips req/scenario step)
		{StatusApproved, StatusReadyForExecution, true},
		// approved -> rejected (review loop escalation)
		{StatusApproved, StatusRejected, true},

		// requirements_generated -> generating_architecture (architecture-generator claims)
		{StatusRequirementsGenerated, StatusGeneratingArchitecture, true},
		// requirements_generated -> architecture_generated (skip path)
		{StatusRequirementsGenerated, StatusArchitectureGenerated, true},
		// requirements_generated -> scenarios_generated (invalid — must go through architecture)
		{StatusRequirementsGenerated, StatusScenariosGenerated, false},
		// requirements_generated -> rejected
		{StatusRequirementsGenerated, StatusRejected, true},

		// architecture_generated → preparing_stories (review-disabled direct path)
		// or → reviewing_architecture (ADR-051 Slice 3, review enabled). Legacy
		// direct paths to scenarios_* remain invalid.
		{StatusArchitectureGenerated, StatusPreparingStories, true},
		{StatusArchitectureGenerated, StatusReviewingArchitecture, true},
		{StatusArchitectureGenerated, StatusGeneratingScenarios, false},
		{StatusArchitectureGenerated, StatusScenariosGenerated, false},
		{StatusArchitectureGenerated, StatusArchitectureReviewed, false}, // must pass through reviewing_architecture
		// architecture_generated -> rejected
		{StatusArchitectureGenerated, StatusRejected, true},

		// ADR-051 Slice 3: architecture review round transitions.
		{StatusReviewingArchitecture, StatusArchitectureReviewed, true},  // approved
		{StatusReviewingArchitecture, StatusRequirementsGenerated, true}, // rejected → re-architect
		{StatusReviewingArchitecture, StatusRejected, true},
		{StatusReviewingArchitecture, StatusPreparingStories, false}, // must pass through architecture_reviewed
		{StatusArchitectureReviewed, StatusPreparingStories, true},
		{StatusArchitectureReviewed, StatusRejected, true},
		{StatusArchitectureReviewed, StatusScenariosGenerated, false},

		// scenarios_generated -> reviewed (review happens after scenario generation)
		{StatusScenariosGenerated, StatusReviewed, true},
		// scenarios_generated -> ready_for_execution (reactive mode, review skipped)
		{StatusScenariosGenerated, StatusReadyForExecution, true},
		// scenarios_generated -> rejected
		{StatusScenariosGenerated, StatusRejected, true},
		// scenarios_generated -> requirements_generated (invalid)
		{StatusScenariosGenerated, StatusRequirementsGenerated, false},

		// In-progress claim transitions
		// created -> drafting (planner claims)
		{StatusCreated, StatusDrafting, true},
		// drafting -> drafted (planner finishes)
		{StatusDrafting, StatusDrafted, true},
		// drafting -> rejected (planner fails)
		{StatusDrafting, StatusRejected, true},
		// drafting -> drafting (second claim — invalid)
		{StatusDrafting, StatusDrafting, false},

		// drafted -> reviewing_draft (plan-reviewer R1 claims)
		{StatusDrafted, StatusReviewingDraft, true},
		// reviewing_draft -> reviewed (review finishes)
		{StatusReviewingDraft, StatusReviewed, true},
		// reviewing_draft -> rejected (review fails)
		{StatusReviewingDraft, StatusRejected, true},
		// reviewing_draft -> reviewing_draft (second claim — invalid)
		{StatusReviewingDraft, StatusReviewingDraft, false},

		// approved -> generating_requirements (requirement-generator claims)
		{StatusApproved, StatusGeneratingRequirements, true},
		// generating_requirements -> requirements_generated
		{StatusGeneratingRequirements, StatusRequirementsGenerated, true},
		// generating_requirements -> rejected
		{StatusGeneratingRequirements, StatusRejected, true},
		// generating_requirements -> generating_requirements (second claim — invalid)
		{StatusGeneratingRequirements, StatusGeneratingRequirements, false},

		// requirements_generated -> generating_architecture (architecture-generator claims)
		{StatusRequirementsGenerated, StatusGeneratingArchitecture, true},
		// generating_architecture -> architecture_generated
		{StatusGeneratingArchitecture, StatusArchitectureGenerated, true},
		// generating_architecture -> rejected
		{StatusGeneratingArchitecture, StatusRejected, true},
		// requirements_generated -> generating_scenarios (invalid — must go through architecture)
		{StatusRequirementsGenerated, StatusGeneratingScenarios, false},

		// ADR-043 PR 4l — Bob claims from stories_generated, not architecture_generated.
		{StatusStoriesGenerated, StatusGeneratingScenarios, true},
		// generating_scenarios -> scenarios_generated
		{StatusGeneratingScenarios, StatusScenariosGenerated, true},
		// generating_scenarios -> rejected
		{StatusGeneratingScenarios, StatusRejected, true},
		// generating_scenarios -> generating_scenarios (second claim — invalid)
		{StatusGeneratingScenarios, StatusGeneratingScenarios, false},

		// scenarios_generated -> reviewing_scenarios (plan-reviewer R2 claims)
		{StatusScenariosGenerated, StatusReviewingScenarios, true},
		// reviewing_scenarios -> reviewed
		{StatusReviewingScenarios, StatusReviewed, true},
		// reviewing_scenarios -> ready_for_execution
		{StatusReviewingScenarios, StatusReadyForExecution, true},
		// reviewing_scenarios -> rejected
		{StatusReviewingScenarios, StatusRejected, true},
		// reviewing_scenarios -> reviewing_scenarios (second claim — invalid)
		{StatusReviewingScenarios, StatusReviewingScenarios, false},

		// ADR-029: Revision loop transitions
		// reviewing_draft -> created (R1 retry)
		{StatusReviewingDraft, StatusCreated, true},
		// reviewing_scenarios -> approved (R2 retry — clear everything)
		{StatusReviewingScenarios, StatusApproved, true},
		// reviewing_scenarios -> created (R2 phase-targeted retry — plan phase)
		{StatusReviewingScenarios, StatusCreated, true},
		// reviewing_scenarios -> requirements_generated (R2 phase-targeted retry — architecture)
		{StatusReviewingScenarios, StatusRequirementsGenerated, true},
		// reviewing_scenarios -> architecture_generated (R2 phase-targeted retry — scenarios only)
		{StatusReviewingScenarios, StatusArchitectureGenerated, true},
		// rejected -> created (manual R1 restart after escalation)
		{StatusRejected, StatusCreated, true},
		// rejected -> approved (manual R2 restart — pre-existing)
		{StatusRejected, StatusApproved, true},
		// rejected -> requirements_generated (accepted post-QA architecture recovery)
		{StatusRejected, StatusRequirementsGenerated, true},

		// Negative: reviewing can't skip to wrong re-entry
		{StatusReviewingDraft, StatusApproved, false},

		// StatusAwaitingReview transitions — human review gate before completion
		// Transitions INTO awaiting_review
		{StatusReviewingRollup, StatusAwaitingReview, true},
		{StatusImplementing, StatusAwaitingReview, true},
		// Transitions FROM awaiting_review
		{StatusAwaitingReview, StatusComplete, true},
		{StatusAwaitingReview, StatusReadyForExecution, true},
		{StatusAwaitingReview, StatusRejected, true},
		{StatusAwaitingReview, StatusArchived, true},
		// Invalid transitions from awaiting_review
		{StatusAwaitingReview, StatusImplementing, false},
		{StatusAwaitingReview, StatusApproved, false},
		// Invalid transitions into awaiting_review
		{StatusComplete, StatusAwaitingReview, false},
		{StatusCreated, StatusAwaitingReview, false},

		// StatusChanged transitions — auto-accept change proposal partial regen
		// Transitions INTO changed (from 7 states)
		{StatusRequirementsGenerated, StatusChanged, true},
		{StatusArchitectureGenerated, StatusChanged, true},
		{StatusScenariosGenerated, StatusChanged, true},
		{StatusScenariosReviewed, StatusChanged, true},
		{StatusReadyForExecution, StatusChanged, true},
		{StatusImplementing, StatusChanged, true},
		{StatusComplete, StatusChanged, true},
		// Transitions FROM changed
		{StatusChanged, StatusGeneratingRequirements, true},
		{StatusChanged, StatusRejected, true},
		// Invalid transitions from changed
		{StatusChanged, StatusApproved, false},
		{StatusChanged, StatusComplete, false},
		{StatusChanged, StatusImplementing, false},
		// Invalid transitions into changed (early pipeline states and rollup)
		{StatusCreated, StatusChanged, false},
		{StatusDrafted, StatusChanged, false},
		{StatusApproved, StatusChanged, false},
		{StatusReviewingRollup, StatusChanged, false}, // don't interrupt rollup

		// story_reprepare recovery back-edge: implementing →
		// preparing_stories (Sarah re-runs with Story.RecoveryHint).
		{StatusImplementing, StatusPreparingStories, true},
		// architecture_revise recovery back-edge: implementing →
		// requirements_generated (the architect re-runs after a wipe).
		{StatusImplementing, StatusRequirementsGenerated, true},
		// ...but the symmetric forward states must NOT gain a free back-edge:
		// only implementing carries these recovery exceptions.
		{StatusReadyForQA, StatusPreparingStories, false},
		{StatusReviewingQA, StatusPreparingStories, false},
		{StatusAwaitingReview, StatusPreparingStories, false},
		{StatusReadyForQA, StatusRequirementsGenerated, false},
		{StatusReviewingQA, StatusRequirementsGenerated, false},
		{StatusAwaitingReview, StatusRequirementsGenerated, false},

		// QA phase transitions (Phase 2f branch-point move target)
		// implementing → ready_for_qa (level=unit|integration|full)
		{StatusImplementing, StatusReadyForQA, true},
		// implementing → reviewing_qa (level=synthesis once branch-point moves)
		{StatusImplementing, StatusReviewingQA, true},
		// ready_for_qa → reviewing_qa (executor finished, qa-reviewer picks up)
		{StatusReadyForQA, StatusReviewingQA, true},
		// ready_for_qa → rejected (executor infra failure)
		{StatusReadyForQA, StatusRejected, true},
		// ready_for_qa → implementing (invalid — no going back)
		{StatusReadyForQA, StatusImplementing, false},
		// reviewing_qa → complete (verdict approved, auto-approve-review=true)
		{StatusReviewingQA, StatusComplete, true},
		// reviewing_qa → awaiting_review (verdict approved, gated)
		{StatusReviewingQA, StatusAwaitingReview, true},
		// reviewing_qa → rejected (verdict rejected/needs_changes)
		{StatusReviewingQA, StatusRejected, true},
		// reviewing_qa → implementing (invalid — verdict is terminal decision)
		{StatusReviewingQA, StatusImplementing, false},

		// ADR-040: analyst sub-phase transitions
		// created → exploring (planner component claims for analyst sub-phase)
		{StatusCreated, StatusExploring, true},
		// exploring → explored (analyst sub-phase done)
		{StatusExploring, StatusExplored, true},
		// exploring → rejected (analyst exhausted retries)
		{StatusExploring, StatusRejected, true},
		// exploring → drafted (invalid — must pass through explored)
		{StatusExploring, StatusDrafted, false},
		// exploring → exploring (second claim — invalid)
		{StatusExploring, StatusExploring, false},
		// explored → drafting (planner sub-phase claims)
		{StatusExplored, StatusDrafting, true},
		// explored → drafted (legacy/skip path)
		{StatusExplored, StatusDrafted, true},
		// explored → rejected (escalation)
		{StatusExplored, StatusRejected, true},
		// explored → exploring (invalid — no going back)
		{StatusExplored, StatusExploring, false},
		// explored → created (invalid — no implicit reset)
		{StatusExplored, StatusCreated, false},
		// Legacy paths still valid after additions
		{StatusCreated, StatusDrafting, true},
		{StatusCreated, StatusDrafted, true},

		// --- Transitions missing from prior coverage ---

		// created → rejected (escalation)
		{StatusCreated, StatusRejected, true},

		// reviewed → approved / rejected
		{StatusReviewed, StatusApproved, true},
		{StatusReviewed, StatusRejected, true},

		// reviewing_scenarios → scenarios_reviewed (R2 approved, auto_approve=false)
		{StatusReviewingScenarios, StatusScenariosReviewed, true},

		// scenarios_reviewed → ready_for_execution (human "Approve & Continue")
		{StatusScenariosReviewed, StatusReadyForExecution, true},
		// scenarios_reviewed → rejected (escalation)
		{StatusScenariosReviewed, StatusRejected, true},

		// preparing_stories → stories_generated (Sarah happy path)
		{StatusPreparingStories, StatusStoriesGenerated, true},
		// preparing_stories → rejected (escalation)
		{StatusPreparingStories, StatusRejected, true},

		// stories_generated → preparing_stories (R3 retry — accepted story_reprepare PlanDecision)
		{StatusStoriesGenerated, StatusPreparingStories, true},

		// ready_for_execution → implementing (scenario orchestrator picks up)
		{StatusReadyForExecution, StatusImplementing, true},

		// implementing → complete (level=none; direct terminal with no review)
		{StatusImplementing, StatusComplete, true},
		// implementing → rejected (execution escalation)
		{StatusImplementing, StatusRejected, true},

		// complete → archived (shelve)
		{StatusComplete, StatusArchived, true},
		// complete → ready_for_execution (re-execute all requirements)
		{StatusComplete, StatusReadyForExecution, true},

		// archived → complete (unarchive)
		{StatusArchived, StatusComplete, true},
		// archived → ready_for_execution (unarchive + retry)
		{StatusArchived, StatusReadyForExecution, true},

		// rejected → ready_for_execution (retry failed requirements)
		{StatusRejected, StatusReadyForExecution, true},

		// Legal today but NO production performer (audit 2026-06-19,
		// docs/audit/e2e-flow-accuracy-and-coverage.md §dead edges) —
		// candidates for tightening:
		// implementing → reviewing_rollup (legacy alias; new code emits reviewing_qa)
		{StatusImplementing, StatusReviewingRollup, true},
		// implementing → reviewing_qa already covered above (line 236)
		// ready_for_qa → rejected already covered above (line 240)
		// rejected → created already covered above (line 170)
		// rejected → implementing (resume stalled plan — no orchestrator path today)
		{StatusRejected, StatusImplementing, true},
		// preparing_stories → architecture_generated (R3 phase-targeted retry — no performer today)
		{StatusPreparingStories, StatusArchitectureGenerated, true},

		// Negative: confirm a sample of genuinely-illegal transitions stay false
		// archived cannot transition to implementing or drafting
		{StatusArchived, StatusImplementing, false},
		{StatusArchived, StatusDrafted, false},
		// complete cannot go directly to implementing
		{StatusComplete, StatusImplementing, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			if got := tt.from.CanTransitionTo(tt.to); got != tt.want {
				t.Errorf("(%q).CanTransitionTo(%q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestPlanStatus_IsInProgress(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
	}{
		{StatusDrafting, true},
		{StatusReviewingDraft, true},
		{StatusGeneratingRequirements, true},
		{StatusGeneratingArchitecture, true},
		{StatusGeneratingScenarios, true},
		{StatusReviewingScenarios, true},
		// ADR-040: analyst sub-phase in-progress
		{StatusExploring, true},
		// Non-in-progress statuses
		{StatusCreated, false},
		{StatusDrafted, false},
		// ADR-040: explored is terminal of analyst sub-phase, not in-progress
		{StatusExplored, false},
		{StatusApproved, false},
		{StatusRequirementsGenerated, false},
		{StatusArchitectureGenerated, false},
		{StatusScenariosGenerated, false},
		{StatusReadyForExecution, false},
		{StatusImplementing, false},
		{StatusComplete, false},
		{StatusAwaitingReview, false},
		{StatusChanged, false},
		{StatusRejected, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsInProgress(); got != tt.want {
				t.Errorf("Status(%q).IsInProgress() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
