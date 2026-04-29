package lessondecomposer

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/jsonutil"
)

// decomposerResult is the JSON shape the lesson-decomposer agent returns
// via submit_work. Mirrors the lessonSchema in tools/terminal/schemas.go.
//
// The decomposer is required to populate at least one of evidence_steps or
// evidence_files — buildLesson rejects when both are empty so the caller
// can surface a parse failure instead of silently writing an unaudited
// lesson.
type decomposerResult struct {
	Summary       string              `json:"summary"`
	Detail        string              `json:"detail"`
	InjectionForm string              `json:"injection_form"`
	CategoryIDs   []string            `json:"category_ids,omitempty"`
	RootCauseRole string              `json:"root_cause_role"`
	EvidenceSteps []decomposerStepRef `json:"evidence_steps,omitempty"`
	EvidenceFiles []decomposerFileRef `json:"evidence_files,omitempty"`
}

type decomposerStepRef struct {
	LoopID    string `json:"loop_id"`
	StepIndex int    `json:"step_index"`
}

type decomposerFileRef struct {
	Path      string `json:"path"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
	CommitSHA string `json:"commit_sha,omitempty"`
}

// parseDecomposerResult extracts the structured lesson payload from the
// agent's submit_work raw output. Strips markdown fences and code blocks
// the way other reviewers do.
func parseDecomposerResult(raw string) (*decomposerResult, error) {
	cleaned := jsonutil.ExtractJSON(raw)
	if strings.TrimSpace(cleaned) == "" {
		return nil, fmt.Errorf("decomposer result empty after JSON extraction")
	}
	var out decomposerResult
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return nil, fmt.Errorf("unmarshal decomposer result: %w", err)
	}
	return &out, nil
}

// buildLesson translates the decomposer's structured result into a
// workflow.Lesson ready for lessons.Writer.RecordLesson. Validates the
// minimum shape: summary + detail + injection_form must be non-empty,
// and at least one evidence pointer must be populated. Returns an error
// when the agent produced an under-specified lesson — the caller should
// not silently write a half-baked lesson to the graph.
//
// scenarioID is the request's scenario ID, used as the lesson's
// ScenarioID so consumers can filter by the surface that surfaced the
// failure. role is the target role (typically "developer").
func buildLesson(r *decomposerResult, scenarioID, role string) (workflow.Lesson, error) {
	if r == nil {
		return workflow.Lesson{}, fmt.Errorf("nil decomposer result")
	}
	summary := strings.TrimSpace(r.Summary)
	detail := strings.TrimSpace(r.Detail)
	injection := strings.TrimSpace(r.InjectionForm)
	if summary == "" || detail == "" || injection == "" {
		return workflow.Lesson{}, fmt.Errorf("decomposer result missing required fields (summary=%t detail=%t injection_form=%t)",
			summary != "", detail != "", injection != "")
	}
	if len(r.EvidenceSteps) == 0 && len(r.EvidenceFiles) == 0 {
		return workflow.Lesson{}, fmt.Errorf("decomposer result has no evidence (at least one of evidence_steps or evidence_files required)")
	}

	steps := make([]workflow.StepRef, 0, len(r.EvidenceSteps))
	for _, s := range r.EvidenceSteps {
		if s.LoopID == "" {
			continue
		}
		steps = append(steps, workflow.StepRef{LoopID: s.LoopID, StepIndex: s.StepIndex})
	}

	files := make([]workflow.FileRef, 0, len(r.EvidenceFiles))
	for _, f := range r.EvidenceFiles {
		if strings.TrimSpace(f.Path) == "" {
			continue
		}
		files = append(files, workflow.FileRef{
			Path:      f.Path,
			LineStart: f.LineStart,
			LineEnd:   f.LineEnd,
			CommitSHA: f.CommitSHA,
		})
	}

	if len(steps) == 0 && len(files) == 0 {
		// All entries had empty LoopID/Path — reject as if no evidence was
		// supplied at all.
		return workflow.Lesson{}, fmt.Errorf("decomposer result evidence entries are all empty after sanitisation")
	}

	role = strings.TrimSpace(role)
	if role == "" {
		role = "developer"
	}
	rootCause := strings.TrimSpace(r.RootCauseRole)
	if rootCause == "" {
		rootCause = role
	}

	return workflow.Lesson{
		Source:        "decomposer",
		ScenarioID:    scenarioID,
		Summary:       summary,
		Detail:        detail,
		InjectionForm: injection,
		CategoryIDs:   sanitiseCategoryIDs(r.CategoryIDs),
		Role:          role,
		RootCauseRole: rootCause,
		EvidenceSteps: steps,
		EvidenceFiles: files,
	}, nil
}

func sanitiseCategoryIDs(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
