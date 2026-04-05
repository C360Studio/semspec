// Package github provides a thin GitHub API client using stdlib net/http.
// No external dependencies (no go-github). The API surface is intentionally
// small: issues, PRs, comments, reviews, and branch refs.
package github

import "time"

// Issue represents a GitHub issue.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"` // open, closed
	Labels    []Label   `json:"labels"`
	User      User      `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Label represents a GitHub label.
type Label struct {
	Name string `json:"name"`
}

// User represents a GitHub user.
type User struct {
	Login string `json:"login"`
}

// PR represents a GitHub pull request.
type PR struct {
	Number  int       `json:"number"`
	Title   string    `json:"title"`
	Body    string    `json:"body"`
	State   string    `json:"state"` // open, closed
	HTMLURL string    `json:"html_url"`
	Head    BranchRef `json:"head"`
	Base    BranchRef `json:"base"`
	Merged  bool      `json:"merged"`
}

// BranchRef represents a branch reference in a PR.
type BranchRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// Review represents a GitHub PR review.
type Review struct {
	ID          int64     `json:"id"`
	State       string    `json:"state"` // APPROVED, CHANGES_REQUESTED, COMMENTED
	Body        string    `json:"body"`
	User        User      `json:"user"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// ReviewComment represents an inline comment on a PR review.
type ReviewComment struct {
	ID       int64  `json:"id"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Body     string `json:"body"`
	DiffHunk string `json:"diff_hunk"`
}

// CreatePRRequest is the request body for creating a pull request.
type CreatePRRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Draft bool   `json:"draft"`
}

// CreateCommentRequest is the request body for creating an issue comment.
type CreateCommentRequest struct {
	Body string `json:"body"`
}

// CreateRefRequest is the request body for creating a git reference.
type CreateRefRequest struct {
	Ref string `json:"ref"` // refs/heads/<branch>
	SHA string `json:"sha"`
}

// HasLabel returns true if the issue has a label with the given name.
func (i *Issue) HasLabel(name string) bool {
	for _, l := range i.Labels {
		if l.Name == name {
			return true
		}
	}
	return false
}
