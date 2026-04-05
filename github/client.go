package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is a thin GitHub API client using stdlib net/http.
type Client struct {
	token      string
	baseURL    string
	owner      string
	repo       string
	httpClient *http.Client
}

// NewClient creates a new GitHub API client.
func NewClient(token, repository string) (*Client, error) {
	owner, repo, err := parseRepository(repository)
	if err != nil {
		return nil, err
	}
	return &Client{
		token:   token,
		baseURL: "https://api.github.com",
		owner:   owner,
		repo:    repo,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// parseRepository splits "owner/repo" into its parts.
// Rejects URL-style inputs (containing ":" or "//").
func parseRepository(repository string) (string, string, error) {
	if strings.Contains(repository, "://") || strings.Contains(repository, ":") {
		return "", "", fmt.Errorf("invalid repository format %q: expected owner/repo, not a URL", repository)
	}
	parts := strings.SplitN(repository, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repository format %q: expected owner/repo", repository)
	}
	if strings.Contains(parts[1], "/") {
		return "", "", fmt.Errorf("invalid repository format %q: expected owner/repo, not a path", repository)
	}
	return parts[0], parts[1], nil
}

// ListIssues returns open issues with the given label, updated since the given time.
// TODO: Follow Link header pagination for repos with >100 matching issues.
func (c *Client) ListIssues(ctx context.Context, since time.Time, label string) ([]Issue, error) {
	params := url.Values{
		"state":     {"open"},
		"sort":      {"updated"},
		"direction": {"asc"},
		"per_page":  {"100"},
	}
	if !since.IsZero() {
		params.Set("since", since.Format(time.RFC3339))
	}
	if label != "" {
		params.Set("labels", label)
	}

	path := fmt.Sprintf("/repos/%s/%s/issues?%s", c.owner, c.repo, params.Encode())
	var issues []Issue
	if err := c.get(ctx, path, &issues); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	return issues, nil
}

// GetIssue returns a single issue by number.
func (c *Client) GetIssue(ctx context.Context, number int) (*Issue, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", c.owner, c.repo, number)
	var issue Issue
	if err := c.get(ctx, path, &issue); err != nil {
		return nil, fmt.Errorf("get issue %d: %w", number, err)
	}
	return &issue, nil
}

// CreateComment posts a comment on an issue or PR.
func (c *Client) CreateComment(ctx context.Context, number int, body string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", c.owner, c.repo, number)
	req := CreateCommentRequest{Body: body}
	if err := c.post(ctx, path, req, nil); err != nil {
		return fmt.Errorf("create comment on #%d: %w", number, err)
	}
	return nil
}

// CreatePR creates a pull request.
func (c *Client) CreatePR(ctx context.Context, head, base, title, body string, draft bool) (*PR, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls", c.owner, c.repo)
	req := CreatePRRequest{
		Title: title,
		Body:  body,
		Head:  head,
		Base:  base,
		Draft: draft,
	}
	var pr PR
	if err := c.post(ctx, path, req, &pr); err != nil {
		return nil, fmt.Errorf("create PR: %w", err)
	}
	return &pr, nil
}

// GetPR returns a single pull request by number.
func (c *Client) GetPR(ctx context.Context, number int) (*PR, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", c.owner, c.repo, number)
	var pr PR
	if err := c.get(ctx, path, &pr); err != nil {
		return nil, fmt.Errorf("get PR %d: %w", number, err)
	}
	return &pr, nil
}

// ListReviews returns reviews for a pull request.
func (c *Client) ListReviews(ctx context.Context, prNumber int) ([]Review, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", c.owner, c.repo, prNumber)
	var reviews []Review
	if err := c.get(ctx, path, &reviews); err != nil {
		return nil, fmt.Errorf("list reviews for PR %d: %w", prNumber, err)
	}
	return reviews, nil
}

// ListReviewComments returns inline comments for a specific review.
func (c *Client) ListReviewComments(ctx context.Context, prNumber int, reviewID int64) ([]ReviewComment, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews/%d/comments", c.owner, c.repo, prNumber, reviewID)
	var comments []ReviewComment
	if err := c.get(ctx, path, &comments); err != nil {
		return nil, fmt.Errorf("list review comments for PR %d review %d: %w", prNumber, reviewID, err)
	}
	return comments, nil
}

// CreateBranchRef creates a new branch reference pointing to the given SHA.
func (c *Client) CreateBranchRef(ctx context.Context, branch, sha string) error {
	path := fmt.Sprintf("/repos/%s/%s/git/refs", c.owner, c.repo)
	req := CreateRefRequest{
		Ref: "refs/heads/" + branch,
		SHA: sha,
	}
	if err := c.post(ctx, path, req, nil); err != nil {
		return fmt.Errorf("create branch ref %s: %w", branch, err)
	}
	return nil
}

// get performs a GET request and decodes the JSON response.
func (c *Client) get(ctx context.Context, path string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, result)
}

// post performs a POST request with a JSON body and optionally decodes the response.
func (c *Client) post(ctx context.Context, path string, body, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, result)
}

// do executes the request with auth headers and handles errors/rate limiting.
func (c *Client) do(req *http.Request, result any) error {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Rate limit handling.
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		resetStr := resp.Header.Get("X-RateLimit-Reset")
		if resetStr != "" {
			if resetUnix, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
				resetTime := time.Unix(resetUnix, 0)
				return &RateLimitError{
					ResetAt:   resetTime,
					Remaining: 0,
				}
			}
		}
		return &RateLimitError{ResetAt: time.Now().Add(60 * time.Second)}
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// RateLimitError is returned when the GitHub API rate limit is exceeded.
type RateLimitError struct {
	ResetAt   time.Time
	Remaining int
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("GitHub API rate limit exceeded, resets at %s", e.ResetAt.Format(time.RFC3339))
}

// APIError is returned for non-2xx responses from the GitHub API.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("GitHub API error %d: %s", e.StatusCode, e.Message)
}
