package requirementgenerator

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestFindRejectionReasons(t *testing.T) {
	tests := []struct {
		name          string
		proposals     []workflow.ChangeProposal
		deprecatedIDs []string
		want          map[string]string
	}{
		{
			name:          "no proposals returns empty map",
			proposals:     nil,
			deprecatedIDs: []string{"req-1"},
			want:          map[string]string{},
		},
		{
			name: "per-requirement reasons from accepted proposal",
			proposals: []workflow.ChangeProposal{
				{
					ID:             "cp-1",
					Status:         workflow.ChangeProposalStatusAccepted,
					AffectedReqIDs: []string{"req-1", "req-2"},
					RejectionReasons: map[string]string{
						"req-1": "too broad",
						"req-2": "duplicates req-3",
					},
				},
			},
			deprecatedIDs: []string{"req-1", "req-2"},
			want: map[string]string{
				"req-1": "too broad",
				"req-2": "duplicates req-3",
			},
		},
		{
			name: "fallback to rationale when no per-requirement reasons",
			proposals: []workflow.ChangeProposal{
				{
					ID:             "cp-1",
					Status:         workflow.ChangeProposalStatusAccepted,
					AffectedReqIDs: []string{"req-1"},
					Rationale:      "requirements are unclear",
				},
			},
			deprecatedIDs: []string{"req-1"},
			want: map[string]string{
				"req-1": "requirements are unclear",
			},
		},
		{
			name: "filters to only deprecated IDs",
			proposals: []workflow.ChangeProposal{
				{
					ID:             "cp-1",
					Status:         workflow.ChangeProposalStatusAccepted,
					AffectedReqIDs: []string{"req-1", "req-2", "req-3"},
					RejectionReasons: map[string]string{
						"req-1": "reason-1",
						"req-2": "reason-2",
						"req-3": "reason-3",
					},
				},
			},
			deprecatedIDs: []string{"req-2"},
			want: map[string]string{
				"req-2": "reason-2",
			},
		},
		{
			name: "skips non-accepted proposals",
			proposals: []workflow.ChangeProposal{
				{
					ID:             "cp-1",
					Status:         workflow.ChangeProposalStatusRejected,
					AffectedReqIDs: []string{"req-1"},
					RejectionReasons: map[string]string{
						"req-1": "should not appear",
					},
				},
				{
					ID:             "cp-2",
					Status:         workflow.ChangeProposalStatusAccepted,
					AffectedReqIDs: []string{"req-1"},
					RejectionReasons: map[string]string{
						"req-1": "correct reason",
					},
				},
			},
			deprecatedIDs: []string{"req-1"},
			want: map[string]string{
				"req-1": "correct reason",
			},
		},
		{
			name: "most recent accepted proposal wins",
			proposals: []workflow.ChangeProposal{
				{
					ID:             "cp-1",
					Status:         workflow.ChangeProposalStatusAccepted,
					AffectedReqIDs: []string{"req-1"},
					RejectionReasons: map[string]string{
						"req-1": "old reason",
					},
				},
				{
					ID:             "cp-2",
					Status:         workflow.ChangeProposalStatusAccepted,
					AffectedReqIDs: []string{"req-1"},
					RejectionReasons: map[string]string{
						"req-1": "newer reason",
					},
				},
			},
			deprecatedIDs: []string{"req-1"},
			want: map[string]string{
				"req-1": "newer reason",
			},
		},
		{
			name: "collects reasons across multiple proposals",
			proposals: []workflow.ChangeProposal{
				{
					ID:             "cp-1",
					Status:         workflow.ChangeProposalStatusAccepted,
					AffectedReqIDs: []string{"req-1"},
					RejectionReasons: map[string]string{
						"req-1": "reason for req-1",
					},
				},
				{
					ID:             "cp-2",
					Status:         workflow.ChangeProposalStatusAccepted,
					AffectedReqIDs: []string{"req-2"},
					RejectionReasons: map[string]string{
						"req-2": "reason for req-2",
					},
				},
			},
			deprecatedIDs: []string{"req-1", "req-2"},
			want: map[string]string{
				"req-1": "reason for req-1",
				"req-2": "reason for req-2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &workflow.Plan{
				ChangeProposals: tt.proposals,
			}
			got := findRejectionReasons(plan, tt.deprecatedIDs)

			if len(got) != len(tt.want) {
				t.Fatalf("got %d reasons, want %d: %v", len(got), len(tt.want), got)
			}
			for id, wantReason := range tt.want {
				if got[id] != wantReason {
					t.Errorf("reason for %s = %q, want %q", id, got[id], wantReason)
				}
			}
		})
	}
}
