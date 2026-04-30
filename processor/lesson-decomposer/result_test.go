package lessondecomposer

import (
	"strings"
	"testing"
)

func TestParseDecomposerResult_PlainJSON(t *testing.T) {
	raw := `{
		"summary": "Run full tests before submit_work.",
		"detail": "On step [12] the developer submitted without running ./tests.",
		"injection_form": "Always run go test ./... before calling submit_work.",
		"category_ids": ["incomplete_verification"],
		"root_cause_role": "developer",
		"evidence_steps": [{"loop_id": "abc", "step_index": 12}],
		"evidence_files": [{"path": "main.go", "line_start": 10, "line_end": 20, "commit_sha": "deadbeef"}]
	}`
	got, err := parseDecomposerResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Summary == "" || got.Detail == "" || got.InjectionForm == "" {
		t.Errorf("missing fields: %+v", got)
	}
	if len(got.EvidenceSteps) != 1 || got.EvidenceSteps[0].LoopID != "abc" {
		t.Errorf("unexpected evidence_steps: %+v", got.EvidenceSteps)
	}
	if len(got.EvidenceFiles) != 1 || got.EvidenceFiles[0].Path != "main.go" {
		t.Errorf("unexpected evidence_files: %+v", got.EvidenceFiles)
	}
}

func TestParseDecomposerResult_MarkdownFenced(t *testing.T) {
	raw := "```json\n" + `{"summary":"x","detail":"y","injection_form":"z","root_cause_role":"developer","evidence_steps":[{"loop_id":"a","step_index":0}]}` + "\n```"
	got, err := parseDecomposerResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Summary != "x" {
		t.Errorf("summary = %q", got.Summary)
	}
}

func TestParseDecomposerResult_Empty(t *testing.T) {
	if _, err := parseDecomposerResult(""); err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseDecomposerResult_NotJSON(t *testing.T) {
	if _, err := parseDecomposerResult("this is not json at all"); err == nil {
		t.Error("expected error for non-JSON input")
	}
}

func TestBuildLesson_Success(t *testing.T) {
	r := &decomposerResult{
		Summary:       "  Run tests  ",
		Detail:        "Detailed narrative",
		InjectionForm: "Always run tests",
		CategoryIDs:   []string{"missing_tests", "  ", "missing_tests"}, // dup + empty filtered
		RootCauseRole: "developer",
		EvidenceSteps: []decomposerStepRef{{LoopID: "abc", StepIndex: 1}},
		EvidenceFiles: []decomposerFileRef{{Path: "main.go", LineStart: 1, LineEnd: 10, CommitSHA: "xyz"}},
	}
	got, err := buildLesson(r, "scn-1", "developer", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Summary != "Run tests" {
		t.Errorf("summary not trimmed: %q", got.Summary)
	}
	if got.Source != "decomposer" {
		t.Errorf("source = %q, want decomposer", got.Source)
	}
	if got.ScenarioID != "scn-1" {
		t.Errorf("scenarioID = %q", got.ScenarioID)
	}
	if got.Role != "developer" {
		t.Errorf("role = %q", got.Role)
	}
	if len(got.CategoryIDs) != 1 || got.CategoryIDs[0] != "missing_tests" {
		t.Errorf("categories not deduped: %v", got.CategoryIDs)
	}
	if len(got.EvidenceSteps) != 1 || got.EvidenceSteps[0].LoopID != "abc" {
		t.Errorf("evidence_steps wrong: %+v", got.EvidenceSteps)
	}
	if len(got.EvidenceFiles) != 1 || got.EvidenceFiles[0].Path != "main.go" {
		t.Errorf("evidence_files wrong: %+v", got.EvidenceFiles)
	}
}

func TestBuildLesson_RootCauseRoleDefault(t *testing.T) {
	r := &decomposerResult{
		Summary:       "x",
		Detail:        "y",
		InjectionForm: "z",
		EvidenceSteps: []decomposerStepRef{{LoopID: "a", StepIndex: 0}},
	}
	got, err := buildLesson(r, "", "developer", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RootCauseRole != "developer" {
		t.Errorf("RootCauseRole should default to target role when empty, got %q", got.RootCauseRole)
	}
}

func TestBuildLesson_RoleDefault(t *testing.T) {
	r := &decomposerResult{
		Summary: "x", Detail: "y", InjectionForm: "z",
		EvidenceFiles: []decomposerFileRef{{Path: "main.go"}},
	}
	got, err := buildLesson(r, "", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Role != "developer" {
		t.Errorf("Role should default to developer when empty, got %q", got.Role)
	}
}

func TestBuildLesson_RejectMissingRequired(t *testing.T) {
	cases := []decomposerResult{
		{Detail: "y", InjectionForm: "z", EvidenceSteps: []decomposerStepRef{{LoopID: "a"}}},               // missing summary
		{Summary: "x", InjectionForm: "z", EvidenceSteps: []decomposerStepRef{{LoopID: "a"}}},              // missing detail
		{Summary: "x", Detail: "y", EvidenceSteps: []decomposerStepRef{{LoopID: "a"}}},                     // missing injection_form
		{Summary: " ", Detail: "y", InjectionForm: "z", EvidenceSteps: []decomposerStepRef{{LoopID: "a"}}}, // whitespace-only summary
	}
	for i, c := range cases {
		_, err := buildLesson(&c, "", "developer", false)
		if err == nil {
			t.Errorf("case %d: expected error for missing required field", i)
		}
	}
}

func TestBuildLesson_RejectNoEvidence(t *testing.T) {
	r := &decomposerResult{Summary: "x", Detail: "y", InjectionForm: "z"}
	_, err := buildLesson(r, "", "developer", false)
	if err == nil {
		t.Fatal("expected error when no evidence supplied")
	}
	if !strings.Contains(err.Error(), "evidence") {
		t.Errorf("error should mention evidence, got %v", err)
	}
}

func TestBuildLesson_RejectAllEmptyEvidence(t *testing.T) {
	// Evidence arrays present but every entry blank — must fail just like
	// "no evidence". Otherwise the writer would record a citation-free
	// lesson with empty StepRef/FileRef values.
	r := &decomposerResult{
		Summary: "x", Detail: "y", InjectionForm: "z",
		EvidenceSteps: []decomposerStepRef{{}},
		EvidenceFiles: []decomposerFileRef{{Path: " "}},
	}
	_, err := buildLesson(r, "", "developer", false)
	if err == nil {
		t.Fatal("expected error when evidence entries are all blank")
	}
}

func TestBuildLesson_NilResult(t *testing.T) {
	if _, err := buildLesson(nil, "", "developer", false); err == nil {
		t.Fatal("expected error for nil result")
	}
}

func TestBuildLesson_PositiveFlagPropagates(t *testing.T) {
	r := &decomposerResult{
		Summary:       "Read the existing test framework before writing tests.",
		Detail:        "Step [3] showed the developer reading existing patterns first.",
		InjectionForm: "Read the existing test framework before writing the first test.",
		EvidenceSteps: []decomposerStepRef{{LoopID: "abc", StepIndex: 3}},
	}
	got, err := buildLesson(r, "scn-1", "developer", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Positive {
		t.Error("positive=true should propagate to the recorded lesson")
	}
	if got.Source != "decomposer" {
		t.Errorf("Source = %q, want decomposer", got.Source)
	}
}

func TestSanitiseCategoryIDs(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{nil, nil},
		{[]string{}, nil},
		{[]string{"  ", ""}, nil},
		{[]string{"a", "a", "  "}, []string{"a"}},
		{[]string{"a", "b", "a"}, []string{"a", "b"}},
	}
	for i, c := range cases {
		got := sanitiseCategoryIDs(c.in)
		if len(got) != len(c.want) {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
			continue
		}
		for j := range got {
			if got[j] != c.want[j] {
				t.Errorf("case %d index %d: got %q, want %q", i, j, got[j], c.want[j])
			}
		}
	}
}
