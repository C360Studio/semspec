package payloads

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/message"
)

// LessonDecomposeRequestedType is the message type for lesson decomposition
// requests published by execution-manager (and later plan-reviewer/qa-reviewer)
// after a reviewer rejection. The lesson-decomposer component subscribes and
// produces an evidence-cited Lesson via lessons.Writer (ADR-033 Phase 2+).
var LessonDecomposeRequestedType = message.Type{
	Domain:   "workflow",
	Category: "lesson-decompose-requested",
	Version:  "v1",
}

// LessonDecomposeRequested carries the full context the decomposer needs to
// fetch the trajectory, scenario AC, and worktree diff for a single rejection.
//
// Wire semantics:
//   - Published on workflow.events.lesson.decompose.requested.<slug> via the
//     WORKFLOW JetStream stream.
//   - Consumed by processor/lesson-decomposer with a durable consumer.
//   - Phase 2a: handler logs receipt only. Phase 2b adds trajectory fetch +
//     LLM dispatch + Lesson emission with evidence pointers.
type LessonDecomposeRequested struct {
	// Slug is the plan slug — used for routing and trajectory deep-links.
	Slug string `json:"slug"`

	// TaskID is the execution task ID (a single TDD cycle within a requirement).
	TaskID string `json:"task_id"`

	// RequirementID identifies the requirement being executed.
	RequirementID string `json:"requirement_id"`

	// ScenarioID identifies the scenario whose AC the rejection failed against.
	ScenarioID string `json:"scenario_id,omitempty"`

	// LoopID is the agentic-loop ID stamped on the task at trigger time —
	// usually the requirement-executor's loop that dispatched the task.
	// Carried for back-compat and trace-deep-link routing; the decomposer
	// prefers DeveloperLoopID + ReviewerLoopID for trajectory evidence.
	LoopID string `json:"loop_id"`

	// DeveloperLoopID is the agentic-loop ID for the most recent developer
	// dispatch that produced the code under review. Required for the
	// decomposer's trajectory fetch — this is the loop where the failure
	// actually manifests in tool calls and model output. Empty when no
	// developer dispatch has completed yet (cannot happen at code-review
	// rejection time; included as a safety net).
	DeveloperLoopID string `json:"developer_loop_id,omitempty"`

	// ReviewerLoopID is the agentic-loop ID for the reviewer that emitted
	// this verdict. Useful for the decomposer to read the reviewer's chain
	// of reasoning when classifying the failure.
	ReviewerLoopID string `json:"reviewer_loop_id,omitempty"`

	// Verdict is the reviewer verdict that triggered this request.
	// Always "rejected" in Phase 2 (smallest blast radius); approval-on-first-try
	// with rating >= 4 will trigger positive lessons in Phase 6.
	Verdict string `json:"verdict"`

	// Feedback is the reviewer feedback string. Carried so the decomposer can
	// reconcile the trajectory with what the reviewer actually said.
	Feedback string `json:"feedback,omitempty"`

	// Source identifies the producer site for routing/metrics: "execution-manager"
	// for code-review rejections (Phase 2), "plan-reviewer" or "qa-reviewer"
	// when those producers migrate (Phase 3).
	Source string `json:"source"`
}

// Schema implements message.Payload.
func (r *LessonDecomposeRequested) Schema() message.Type {
	return LessonDecomposeRequestedType
}

// Validate implements message.Payload.
func (r *LessonDecomposeRequested) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if r.LoopID == "" && r.DeveloperLoopID == "" && r.ReviewerLoopID == "" {
		return fmt.Errorf("at least one of loop_id, developer_loop_id, or reviewer_loop_id is required (decomposer cannot fetch trajectory without it)")
	}
	if r.Verdict == "" {
		return fmt.Errorf("verdict is required")
	}
	if r.Source == "" {
		return fmt.Errorf("source is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *LessonDecomposeRequested) MarshalJSON() ([]byte, error) {
	type Alias LessonDecomposeRequested
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *LessonDecomposeRequested) UnmarshalJSON(data []byte) error {
	type Alias LessonDecomposeRequested
	return json.Unmarshal(data, (*Alias)(r))
}

// LessonDecomposeRequestedSubject builds the NATS subject for a given plan slug.
// The lesson-decomposer subscribes on workflow.events.lesson.decompose.requested.>
// and routes by slug from the payload.
func LessonDecomposeRequestedSubject(slug string) string {
	return fmt.Sprintf("workflow.events.lesson.decompose.requested.%s", slug)
}
