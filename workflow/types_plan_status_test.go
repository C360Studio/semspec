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
		{StatusPhasesGenerated, true},
		{StatusPhasesApproved, true},
		{StatusTasksGenerated, true},
		{StatusTasksApproved, true},
		{StatusImplementing, true},
		{StatusComplete, true},
		{StatusArchived, true},
		{StatusRejected, true},
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
		// approved -> phases_generated (legacy direct flow still valid)
		{StatusApproved, StatusPhasesGenerated, true},

		// requirements_generated -> scenarios_generated
		{StatusRequirementsGenerated, StatusScenariosGenerated, true},
		// requirements_generated -> rejected
		{StatusRequirementsGenerated, StatusRejected, true},
		// requirements_generated -> phases_generated (invalid, must go through scenarios)
		{StatusRequirementsGenerated, StatusPhasesGenerated, false},

		// scenarios_generated -> reviewed (review happens after scenario generation)
		{StatusScenariosGenerated, StatusReviewed, true},
		// scenarios_generated -> phases_generated (static mode, review skipped)
		{StatusScenariosGenerated, StatusPhasesGenerated, true},
		// scenarios_generated -> ready_for_execution (reactive mode, review skipped)
		{StatusScenariosGenerated, StatusReadyForExecution, true},
		// scenarios_generated -> rejected
		{StatusScenariosGenerated, StatusRejected, true},
		// scenarios_generated -> requirements_generated (invalid)
		{StatusScenariosGenerated, StatusRequirementsGenerated, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			if got := tt.from.CanTransitionTo(tt.to); got != tt.want {
				t.Errorf("(%q).CanTransitionTo(%q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}
