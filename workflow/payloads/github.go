package payloads

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/message"
)

// ---------------------------------------------------------------------------
// GitHub integration payloads (ADR-031)
// ---------------------------------------------------------------------------

// GitHubPlanCreationRequest is the typed payload published by github-watcher
// when a validated issue is ready for plan creation.
type GitHubPlanCreationRequest struct {
	IssueNumber int    `json:"issue_number"`
	IssueURL    string `json:"issue_url"`
	Repository  string `json:"repository"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Scope       string `json:"scope,omitempty"`
	Constraints string `json:"constraints,omitempty"`
	Priority    string `json:"priority,omitempty"`
	TraceID     string `json:"trace_id,omitempty"`
}

// Schema implements message.Payload.
func (r *GitHubPlanCreationRequest) Schema() message.Type { return GitHubPlanCreationRequestType }

// Validate implements message.Payload.
func (r *GitHubPlanCreationRequest) Validate() error {
	if r.IssueNumber == 0 {
		return fmt.Errorf("issue_number is required")
	}
	if r.Title == "" {
		return fmt.Errorf("title is required")
	}
	if r.Repository == "" {
		return fmt.Errorf("repository is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *GitHubPlanCreationRequest) MarshalJSON() ([]byte, error) {
	type Alias GitHubPlanCreationRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *GitHubPlanCreationRequest) UnmarshalJSON(data []byte) error {
	type Alias GitHubPlanCreationRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// GitHubPlanCreationRequestType is the message type for GitHub plan creation requests.
var GitHubPlanCreationRequestType = message.Type{
	Domain:   "workflow",
	Category: "github-plan-creation-request",
	Version:  "v1",
}

// GitHubPRCreatedEvent is published by github-submitter after creating a PR.
type GitHubPRCreatedEvent struct {
	Slug       string `json:"slug"`
	PRNumber   int    `json:"pr_number"`
	PRURL      string `json:"pr_url"`
	Repository string `json:"repository"`
	TraceID    string `json:"trace_id,omitempty"`
}

// Schema implements message.Payload.
func (r *GitHubPRCreatedEvent) Schema() message.Type { return GitHubPRCreatedEventType }

// Validate implements message.Payload.
func (r *GitHubPRCreatedEvent) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if r.PRNumber == 0 {
		return fmt.Errorf("pr_number is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *GitHubPRCreatedEvent) MarshalJSON() ([]byte, error) {
	type Alias GitHubPRCreatedEvent
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *GitHubPRCreatedEvent) UnmarshalJSON(data []byte) error {
	type Alias GitHubPRCreatedEvent
	return json.Unmarshal(data, (*Alias)(r))
}

// GitHubPRCreatedEventType is the message type for PR creation events.
var GitHubPRCreatedEventType = message.Type{
	Domain:   "workflow",
	Category: "github-pr-created-event",
	Version:  "v1",
}

// GitHubPRFeedbackRequest is published by github-submitter when a PR receives
// a CHANGES_REQUESTED review. plan-manager consumes this to create ChangeProposals
// and re-trigger execution for affected requirements.
type GitHubPRFeedbackRequest struct {
	Slug     string            `json:"slug"`
	PRNumber int               `json:"pr_number"`
	ReviewID int64             `json:"review_id"`
	Reviewer string            `json:"reviewer"`
	State    string            `json:"state"` // CHANGES_REQUESTED
	Body     string            `json:"body"`
	Comments []PRReviewComment `json:"comments,omitempty"`
	TraceID  string            `json:"trace_id,omitempty"`
}

// PRReviewComment is an inline comment from a PR review.
type PRReviewComment struct {
	ID       int64  `json:"id"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	Body     string `json:"body"`
	DiffHunk string `json:"diff_hunk,omitempty"`
}

// Schema implements message.Payload.
func (r *GitHubPRFeedbackRequest) Schema() message.Type { return GitHubPRFeedbackRequestType }

// Validate implements message.Payload.
func (r *GitHubPRFeedbackRequest) Validate() error {
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if r.PRNumber == 0 {
		return fmt.Errorf("pr_number is required")
	}
	if r.ReviewID == 0 {
		return fmt.Errorf("review_id is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *GitHubPRFeedbackRequest) MarshalJSON() ([]byte, error) {
	type Alias GitHubPRFeedbackRequest
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *GitHubPRFeedbackRequest) UnmarshalJSON(data []byte) error {
	type Alias GitHubPRFeedbackRequest
	return json.Unmarshal(data, (*Alias)(r))
}

// GitHubPRFeedbackRequestType is the message type for PR feedback requests.
var GitHubPRFeedbackRequestType = message.Type{
	Domain:   "workflow",
	Category: "github-pr-feedback-request",
	Version:  "v1",
}
