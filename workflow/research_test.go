package workflow

import (
	"strings"
	"testing"
)

func TestNewResearch_CreatesValidPendingRecord(t *testing.T) {
	r := NewResearch("loop-1", "call-1", "What is the constructor signature for AbstractSensorModule?", []string{"github.com/opensensorhub/osh-core"})

	if r.ID == "" || !strings.HasPrefix(r.ID, "research-") {
		t.Errorf("ID = %q; want research-<uuid>", r.ID)
	}
	if r.Status != ResearchStatusPending {
		t.Errorf("Status = %q; want %q", r.Status, ResearchStatusPending)
	}
	if r.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if err := r.Validate(); err != nil {
		t.Errorf("freshly-created research failed validation: %v", err)
	}
}

func TestResearch_Validate_RejectsMissingFields(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Research)
		wantErr string
	}{
		{
			name:    "missing id",
			mutate:  func(r *Research) { r.ID = "" },
			wantErr: "research.id is required",
		},
		{
			name:    "missing asking_loop_id",
			mutate:  func(r *Research) { r.AskingLoopID = "" },
			wantErr: "research.asking_loop_id is required",
		},
		{
			name:    "missing asking_call_id",
			mutate:  func(r *Research) { r.AskingCallID = "" },
			wantErr: "research.asking_call_id is required",
		},
		{
			name:    "missing question",
			mutate:  func(r *Research) { r.Question = "" },
			wantErr: "research.question is required",
		},
		{
			name:    "unknown status",
			mutate:  func(r *Research) { r.Status = ResearchStatus("weird") },
			wantErr: `research.status "weird" is not a known status`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewResearch("loop-1", "call-1", "Q?", nil)
			tc.mutate(r)
			err := r.Validate()
			if err == nil {
				t.Fatalf("Validate returned nil; want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Validate error = %q; want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestResearch_Validate_AnsweredRequiresAnswerAndCitations(t *testing.T) {
	makeAnswered := func() *Research {
		r := NewResearch("loop-1", "call-1", "Q?", nil)
		r.Status = ResearchStatusAnswered
		r.Answer = "concrete answer with structure"
		r.Citations = []Citation{{URL: "https://example.test/a", Lines: "10-20"}}
		return r
	}

	if err := makeAnswered().Validate(); err != nil {
		t.Errorf("happy-path answered failed validation: %v", err)
	}

	t.Run("missing answer", func(t *testing.T) {
		r := makeAnswered()
		r.Answer = ""
		if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "answer is required when status=answered") {
			t.Errorf("want missing-answer error; got %v", err)
		}
	})

	t.Run("missing citations", func(t *testing.T) {
		r := makeAnswered()
		r.Citations = nil
		if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "citations is required when status=answered") {
			t.Errorf("want missing-citations error; got %v", err)
		}
	})

	t.Run("oversize answer", func(t *testing.T) {
		r := makeAnswered()
		r.Answer = strings.Repeat("x", MaxResearchAnswerBytes+1)
		err := r.Validate()
		if err == nil || !strings.Contains(err.Error(), "researcher must distill further") {
			t.Errorf("want oversize-answer error; got %v", err)
		}
	})

	t.Run("answer exactly at cap is ok", func(t *testing.T) {
		r := makeAnswered()
		r.Answer = strings.Repeat("y", MaxResearchAnswerBytes)
		if err := r.Validate(); err != nil {
			t.Errorf("answer at cap should validate; got %v", err)
		}
	})
}

func TestResearch_Validate_TimeoutAndErrorRequireErrorField(t *testing.T) {
	cases := []ResearchStatus{ResearchStatusTimeout, ResearchStatusError}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			r := NewResearch("loop-1", "call-1", "Q?", nil)
			r.Status = status

			// Missing error → reject
			if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "error is required when status=") {
				t.Errorf("want missing-error rejection; got %v", err)
			}

			// With error → accept
			r.Error = "researcher loop failed: max iterations"
			if err := r.Validate(); err != nil {
				t.Errorf("with error field should validate; got %v", err)
			}
		})
	}
}

func TestCitation_Validate_RequiresExactlyOneOfURLOrFile(t *testing.T) {
	cases := []struct {
		name    string
		c       Citation
		wantErr string
	}{
		{"both set", Citation{URL: "https://a", File: "/b"}, "only one of url or file"},
		{"neither set", Citation{Lines: "1-5"}, "one of url or file is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.c.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("want error containing %q; got %v", tc.wantErr, err)
			}
		})
	}

	t.Run("url only", func(t *testing.T) {
		c := Citation{URL: "https://example.test", Lines: "10"}
		if err := c.Validate(); err != nil {
			t.Errorf("url-only citation should validate; got %v", err)
		}
	})

	t.Run("file only", func(t *testing.T) {
		c := Citation{File: "/sources/foo.go", Lines: "10"}
		if err := c.Validate(); err != nil {
			t.Errorf("file-only citation should validate; got %v", err)
		}
	})
}

func TestResearch_Validate_PropagatesBadCitationInAnsweredState(t *testing.T) {
	r := NewResearch("loop-1", "call-1", "Q?", nil)
	r.Status = ResearchStatusAnswered
	r.Answer = "answer"
	r.Citations = []Citation{
		{URL: "https://ok"},
		{URL: "https://bad", File: "/also/bad"}, // invalid — both set
	}
	err := r.Validate()
	if err == nil || !strings.Contains(err.Error(), "citations[1]") {
		t.Errorf("want bad-citation propagation with index; got %v", err)
	}
}

func TestResearchBucket_Constant(t *testing.T) {
	if ResearchBucket != "RESEARCH" {
		t.Errorf("ResearchBucket = %q; want RESEARCH", ResearchBucket)
	}
}
