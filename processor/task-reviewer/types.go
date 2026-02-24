// Package taskreviewer provides a processor that reviews generated tasks against SOPs
// before approval using LLM analysis.
package taskreviewer

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
)

// TaskReviewTrigger is the trigger payload for task review.
type TaskReviewTrigger struct {
	workflow.CallbackFields

	RequestID     string          `json:"request_id"`
	Slug          string          `json:"slug"`
	ProjectID     string          `json:"project_id,omitempty"`
	Tasks         []workflow.Task `json:"tasks"`
	ScopePatterns []string        `json:"scope_patterns,omitempty"`
	SOPContext    string          `json:"sop_context,omitempty"` // Pre-built SOP context

	// Trace context for trajectory tracking
	TraceID string `json:"trace_id,omitempty"`
	LoopID  string `json:"loop_id,omitempty"`
}

// Schema implements message.Payload.
func (t *TaskReviewTrigger) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "task-review-trigger", Version: "v1"}
}

// Validate implements message.Payload.
func (t *TaskReviewTrigger) Validate() error {
	if t.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if t.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if len(t.Tasks) == 0 {
		return fmt.Errorf("tasks are required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (t *TaskReviewTrigger) MarshalJSON() ([]byte, error) {
	type Alias TaskReviewTrigger
	return json.Marshal((*Alias)(t))
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *TaskReviewTrigger) UnmarshalJSON(data []byte) error {
	type Alias TaskReviewTrigger
	return json.Unmarshal(data, (*Alias)(t))
}

// TaskReviewResult is the result payload for task review.
type TaskReviewResult struct {
	RequestID string              `json:"request_id"`
	Slug      string              `json:"slug"`
	Verdict   string              `json:"verdict"` // "approved" or "needs_changes"
	Summary   string              `json:"summary"`
	Findings  []TaskReviewFinding `json:"findings"`
	// FormattedFindings is a human-readable markdown rendering of the
	// findings array. Workflow templates should reference this field
	// (not the raw findings array) when embedding review feedback in
	// LLM prompts, because semstreams interpolation JSON-stringifies
	// arrays — producing unreadable output for local LLMs.
	FormattedFindings string   `json:"formatted_findings"`
	Status            string   `json:"status"`
	LLMRequestIDs     []string `json:"llm_request_ids,omitempty"`
}

// Schema implements message.Payload.
func (r *TaskReviewResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "task-review-result", Version: "v1"}
}

// Validate implements message.Payload.
func (r *TaskReviewResult) Validate() error {
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *TaskReviewResult) MarshalJSON() ([]byte, error) {
	type Alias TaskReviewResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *TaskReviewResult) UnmarshalJSON(data []byte) error {
	type Alias TaskReviewResult
	return json.Unmarshal(data, (*Alias)(r))
}

// TaskReviewFinding represents a single finding from task review.
type TaskReviewFinding struct {
	SOPID      string `json:"sop_id"`
	SOPTitle   string `json:"sop_title,omitempty"`
	Severity   string `json:"severity"` // "error", "warning", or "info"
	Status     string `json:"status"`   // "compliant", "violation", or "not_applicable"
	Issue      string `json:"issue,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
	TaskID     string `json:"task_id,omitempty"` // Specific task if applicable
}

// LLMTaskReviewResult is the structured output expected from the LLM.
type LLMTaskReviewResult struct {
	Verdict  string              `json:"verdict"`
	Summary  string              `json:"summary"`
	Findings []TaskReviewFinding `json:"findings"`
}

// IsApproved returns true if the verdict is "approved".
func (r *LLMTaskReviewResult) IsApproved() bool {
	return r.Verdict == "approved"
}

// ErrorFindings returns only error-severity findings that are violations.
func (r *LLMTaskReviewResult) ErrorFindings() []TaskReviewFinding {
	var errors []TaskReviewFinding
	for _, f := range r.Findings {
		if f.Severity == "error" && f.Status == "violation" {
			errors = append(errors, f)
		}
	}
	return errors
}

// FormatFindings formats findings for display in human-readable markdown.
func (r *LLMTaskReviewResult) FormatFindings() string {
	if len(r.Findings) == 0 {
		return "No findings."
	}

	var sb strings.Builder

	// Group by status
	var violations, compliant, notApplicable []TaskReviewFinding
	for _, f := range r.Findings {
		switch f.Status {
		case "violation":
			violations = append(violations, f)
		case "compliant":
			compliant = append(compliant, f)
		default:
			notApplicable = append(notApplicable, f)
		}
	}

	// Show violations first
	if len(violations) > 0 {
		sb.WriteString("### Violations\n\n")
		for _, f := range violations {
			sb.WriteString(fmt.Sprintf("- **[%s]** %s\n", strings.ToUpper(f.Severity), f.SOPID))
			if f.SOPTitle != "" {
				sb.WriteString(fmt.Sprintf("  - SOP: %s\n", f.SOPTitle))
			}
			if f.TaskID != "" {
				sb.WriteString(fmt.Sprintf("  - Task: %s\n", f.TaskID))
			}
			if f.Issue != "" {
				sb.WriteString(fmt.Sprintf("  - Issue: %s\n", f.Issue))
			}
			if f.Suggestion != "" {
				sb.WriteString(fmt.Sprintf("  - Suggestion: %s\n", f.Suggestion))
			}
		}
		sb.WriteString("\n")
	}

	// Show compliant items
	if len(compliant) > 0 {
		sb.WriteString("### Compliant\n\n")
		for _, f := range compliant {
			sb.WriteString(fmt.Sprintf("- %s", f.SOPID))
			if f.SOPTitle != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", f.SOPTitle))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
