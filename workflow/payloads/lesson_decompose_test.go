package payloads

import (
	"encoding/json"
	"testing"
)

func TestLessonDecomposeRequested_Validate(t *testing.T) {
	cases := []struct {
		name    string
		payload LessonDecomposeRequested
		wantErr bool
	}{
		{
			name: "valid",
			payload: LessonDecomposeRequested{
				Slug:    "p1",
				LoopID:  "loop-123",
				Verdict: "rejected",
				Source:  "execution-manager",
			},
		},
		{
			name:    "missing slug",
			payload: LessonDecomposeRequested{LoopID: "l", Verdict: "rejected", Source: "x"},
			wantErr: true,
		},
		{
			name:    "missing loop_id",
			payload: LessonDecomposeRequested{Slug: "p", Verdict: "rejected", Source: "x"},
			wantErr: true,
		},
		{
			name:    "missing verdict",
			payload: LessonDecomposeRequested{Slug: "p", LoopID: "l", Source: "x"},
			wantErr: true,
		},
		{
			name:    "missing source",
			payload: LessonDecomposeRequested{Slug: "p", LoopID: "l", Verdict: "rejected"},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.payload.Validate()
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestLessonDecomposeRequested_RoundTripJSON(t *testing.T) {
	in := LessonDecomposeRequested{
		Slug:          "plan-abc",
		TaskID:        "task-1",
		RequirementID: "req-1",
		ScenarioID:    "scn-1",
		LoopID:        "loop-xyz",
		Verdict:       "rejected",
		Feedback:      "missing test for nil case",
		Source:        "execution-manager",
	}
	data, err := json.Marshal(&in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out LessonDecomposeRequested
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round-trip mismatch:\n  in  = %+v\n  out = %+v", in, out)
	}
}

func TestLessonDecomposeRequested_Schema(t *testing.T) {
	r := &LessonDecomposeRequested{}
	got := r.Schema()
	if got.Domain != "workflow" {
		t.Errorf("Domain = %q", got.Domain)
	}
	if got.Category != "lesson-decompose-requested" {
		t.Errorf("Category = %q", got.Category)
	}
	if got.Version != "v1" {
		t.Errorf("Version = %q", got.Version)
	}
}

func TestLessonDecomposeRequestedSubject(t *testing.T) {
	got := LessonDecomposeRequestedSubject("plan-x")
	want := "workflow.events.lesson.decompose.requested.plan-x"
	if got != want {
		t.Errorf("subject = %q, want %q", got, want)
	}
}
