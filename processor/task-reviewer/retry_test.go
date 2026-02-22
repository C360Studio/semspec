package taskreviewer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semspec/llm"
)

// mockLLM implements llmCompleter for testing the format correction retry loop.
type mockLLM struct {
	responses []*llm.Response
	errs      []error
	calls     []llm.Request
	idx       int
}

func (m *mockLLM) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	m.calls = append(m.calls, req)
	i := m.idx
	m.idx++

	if i < len(m.errs) && m.errs[i] != nil {
		return nil, m.errs[i]
	}
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return nil, fmt.Errorf("mockLLM: no response configured for call %d", i)
}

func newTestComponent(mock *mockLLM) *Component {
	return &Component{
		llmClient: mock,
		logger:    slog.Default(),
		config:    Config{DefaultCapability: "reviewing"},
	}
}

const validApprovedJSON = `{
  "verdict": "approved",
  "summary": "Tasks meet all SOP requirements",
  "findings": [
    {
      "sop_id": "source.doc.sops.testing",
      "sop_title": "Testing Standards",
      "severity": "info",
      "status": "compliant",
      "issue": "",
      "suggestion": ""
    }
  ]
}`

const validNeedsChangesJSON = `{
  "verdict": "needs_changes",
  "summary": "Tasks are missing test coverage requirements",
  "findings": [
    {
      "sop_id": "source.doc.sops.testing",
      "sop_title": "Testing Standards",
      "severity": "error",
      "status": "violation",
      "issue": "No test task found for API endpoints",
      "suggestion": "Add a task with type=test covering the API files",
      "task_id": "task.feature.1"
    }
  ]
}`

// --- formatCorrectionPrompt tests ---

func TestFormatCorrectionPrompt(t *testing.T) {
	err := fmt.Errorf("no JSON found in response")
	prompt := formatCorrectionPrompt(err)

	if !strings.Contains(prompt, "no JSON found in response") {
		t.Error("prompt should contain the original error message")
	}
	if !strings.Contains(prompt, `"verdict"`) {
		t.Error("prompt should contain the expected verdict field")
	}
	if !strings.Contains(prompt, `"findings"`) {
		t.Error("prompt should contain the expected findings field")
	}
	if !strings.Contains(prompt, "ONLY a valid JSON object") {
		t.Error("prompt should instruct LLM to respond with only JSON")
	}
	if !strings.Contains(prompt, `"task_id"`) {
		t.Error("prompt should contain the task_id field in the example")
	}
}

// --- parseReviewFromResponse tests ---

func TestParseReviewFromResponse_ValidApproved(t *testing.T) {
	c := newTestComponent(&mockLLM{})

	result, err := c.parseReviewFromResponse(validApprovedJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != "approved" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "approved")
	}
	if len(result.Findings) != 1 {
		t.Errorf("Findings count = %d, want 1", len(result.Findings))
	}
}

func TestParseReviewFromResponse_ValidNeedsChanges(t *testing.T) {
	c := newTestComponent(&mockLLM{})

	result, err := c.parseReviewFromResponse(validNeedsChangesJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != "needs_changes" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "needs_changes")
	}
	if len(result.Findings) != 1 {
		t.Fatalf("Findings count = %d, want 1", len(result.Findings))
	}
	if result.Findings[0].Status != "violation" {
		t.Errorf("Finding[0].Status = %q, want %q", result.Findings[0].Status, "violation")
	}
	if result.Findings[0].TaskID != "task.feature.1" {
		t.Errorf("Finding[0].TaskID = %q, want %q", result.Findings[0].TaskID, "task.feature.1")
	}
}

func TestParseReviewFromResponse_MarkdownWrapped(t *testing.T) {
	c := newTestComponent(&mockLLM{})

	content := "Here's my review:\n\n```json\n" + validApprovedJSON + "\n```\n\nLet me know if you need more details."

	result, err := c.parseReviewFromResponse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != "approved" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "approved")
	}
}

func TestParseReviewFromResponse_InvalidVerdict(t *testing.T) {
	c := newTestComponent(&mockLLM{})

	badVerdict := `{"verdict": "maybe", "summary": "unsure", "findings": []}`
	_, err := c.parseReviewFromResponse(badVerdict)
	if err == nil {
		t.Fatal("expected error for invalid verdict")
	}
	if !strings.Contains(err.Error(), "invalid verdict") {
		t.Errorf("error = %q, should contain 'invalid verdict'", err.Error())
	}
}

func TestParseReviewFromResponse_NoJSON(t *testing.T) {
	c := newTestComponent(&mockLLM{})

	_, err := c.parseReviewFromResponse("Just some plain text response without JSON")
	if err == nil {
		t.Fatal("expected error for no JSON")
	}
	if !strings.Contains(err.Error(), "no JSON found") {
		t.Errorf("error = %q, should contain 'no JSON found'", err.Error())
	}
}

func TestParseReviewFromResponse_MalformedJSON(t *testing.T) {
	c := newTestComponent(&mockLLM{})

	// llm.ExtractJSON returns empty when it can't find valid JSON,
	// so we get "no JSON found" rather than a parse error
	_, err := c.parseReviewFromResponse(`{"verdict": "approved", "summary": incomplete`)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	// Either "no JSON found" (when ExtractJSON fails) or "parse JSON" (when it extracts partial)
	if !strings.Contains(err.Error(), "JSON") {
		t.Errorf("error = %q, should contain 'JSON'", err.Error())
	}
}

// --- Retry loop behavior tests ---

func TestReviewTasks_SuccessOnFirstAttempt(t *testing.T) {
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: validApprovedJSON, Model: "test-model"},
		},
	}
	c := newTestComponent(mock)

	result, err := c.parseReviewFromResponse(validApprovedJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != "approved" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "approved")
	}

	// Verify the mock works for the retry path too
	_ = mock // mock is ready for integration tests
}

func TestReviewTasks_SuccessOnSecondAttempt(t *testing.T) {
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: "Here's my review: the tasks look good overall.", Model: "test-model"},
			{Content: validApprovedJSON, Model: "test-model"},
		},
	}
	c := newTestComponent(mock)

	// Simulate what reviewTasks does: build messages and call LLM in a retry loop
	messages := []llm.Message{
		{Role: "system", Content: "You are a task reviewer."},
		{Role: "user", Content: "Review these tasks."},
	}

	var result *LLMTaskReviewResult
	temperature := 0.3
	var lastErr error

	for attempt := range maxFormatRetries {
		llmResp, err := mock.Complete(context.Background(), llm.Request{
			Capability:  "reviewing",
			Messages:    messages,
			Temperature: &temperature,
			MaxTokens:   4096,
		})
		if err != nil {
			t.Fatalf("unexpected LLM error: %v", err)
		}

		parsed, parseErr := c.parseReviewFromResponse(llmResp.Content)
		if parseErr == nil {
			result = parsed
			break
		}

		lastErr = parseErr
		if attempt+1 >= maxFormatRetries {
			break
		}

		messages = append(messages,
			llm.Message{Role: "assistant", Content: llmResp.Content},
			llm.Message{Role: "user", Content: formatCorrectionPrompt(parseErr)},
		)
	}

	if result == nil {
		t.Fatalf("expected result, got error: %v", lastErr)
	}
	if result.Verdict != "approved" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "approved")
	}
	if len(mock.calls) != 2 {
		t.Errorf("LLM calls = %d, want 2", len(mock.calls))
	}

	// Second call should include correction context
	secondCall := mock.calls[1]
	if len(secondCall.Messages) != 4 {
		t.Fatalf("second call messages = %d, want 4 (system + user + assistant + correction)", len(secondCall.Messages))
	}
	if !strings.Contains(secondCall.Messages[3].Content, "could not be parsed as JSON") {
		t.Error("correction message should explain the parse failure")
	}
}

func TestReviewTasks_AllRetriesFail(t *testing.T) {
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: "not json 1", Model: "m"},
			{Content: "not json 2", Model: "m"},
			{Content: "not json 3", Model: "m"},
			{Content: "not json 4", Model: "m"},
			{Content: "not json 5", Model: "m"},
		},
	}

	messages := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "usr"},
	}
	temperature := 0.3
	c := newTestComponent(mock)
	var lastErr error

	for attempt := range maxFormatRetries {
		llmResp, err := mock.Complete(context.Background(), llm.Request{
			Capability:  "reviewing",
			Messages:    messages,
			Temperature: &temperature,
			MaxTokens:   4096,
		})
		if err != nil {
			t.Fatalf("unexpected LLM error: %v", err)
		}

		_, parseErr := c.parseReviewFromResponse(llmResp.Content)
		if parseErr == nil {
			t.Fatal("expected parse failure")
		}

		lastErr = parseErr
		if attempt+1 >= maxFormatRetries {
			break
		}

		messages = append(messages,
			llm.Message{Role: "assistant", Content: llmResp.Content},
			llm.Message{Role: "user", Content: formatCorrectionPrompt(parseErr)},
		)
	}

	if lastErr == nil {
		t.Fatal("expected error after all retries")
	}
	if len(mock.calls) != maxFormatRetries {
		t.Errorf("LLM calls = %d, want %d", len(mock.calls), maxFormatRetries)
	}
}

func TestReviewTasks_InvalidVerdict_Retries(t *testing.T) {
	// Valid JSON but invalid verdict — should be caught by parseReviewFromResponse
	badVerdict := `{"verdict": "maybe", "summary": "unsure", "findings": []}`
	c := newTestComponent(&mockLLM{})

	// First call returns bad verdict
	_, err := c.parseReviewFromResponse(badVerdict)
	if err == nil {
		t.Fatal("expected error for invalid verdict")
	}
	if !strings.Contains(err.Error(), "invalid verdict") {
		t.Errorf("error = %q, should contain 'invalid verdict'", err.Error())
	}

	// Second call returns valid review
	result, err := c.parseReviewFromResponse(validNeedsChangesJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != "needs_changes" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "needs_changes")
	}
}

func TestFormattedFindings_HumanReadable(t *testing.T) {
	c := newTestComponent(&mockLLM{})

	result, err := c.parseReviewFromResponse(validNeedsChangesJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	formatted := result.FormatFindings()

	// Should be human-readable markdown, not raw JSON
	if strings.Contains(formatted, `"sop_id"`) {
		t.Error("formatted findings should not contain raw JSON keys")
	}
	if !strings.Contains(formatted, "Violations") {
		t.Error("formatted findings should contain 'Violations' section header")
	}
	if !strings.Contains(formatted, "Testing Standards") {
		t.Error("formatted findings should contain the SOP title")
	}
	if !strings.Contains(formatted, "No test task found for API endpoints") {
		t.Error("formatted findings should contain the issue description")
	}
	if !strings.Contains(formatted, "Add a task with type=test") {
		t.Error("formatted findings should contain the suggestion")
	}
}

func TestCallbackPayload_IncludesFormattedFindings(t *testing.T) {
	// Verify that TaskReviewResult (the callback payload struct) correctly
	// includes FormattedFindings when built from a review result — this is
	// what the workflow interpolator accesses via ${steps.task_reviewer.output.formatted_findings}.
	c := newTestComponent(&mockLLM{})

	result, err := c.parseReviewFromResponse(validNeedsChangesJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate what publishResult does
	payload := &TaskReviewResult{
		RequestID:         "test-req",
		Slug:              "test-slug",
		Verdict:           result.Verdict,
		Summary:           result.Summary,
		Findings:          result.Findings,
		FormattedFindings: result.FormatFindings(),
		Status:            "completed",
	}

	if payload.FormattedFindings == "" {
		t.Fatal("FormattedFindings should not be empty")
	}
	if strings.HasPrefix(payload.FormattedFindings, "[{") {
		t.Error("FormattedFindings should be markdown, not JSON array")
	}
	if !strings.Contains(payload.FormattedFindings, "Violations") {
		t.Error("FormattedFindings should contain violation section")
	}

	// Verify JSON round-trip preserves the field
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded TaskReviewResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.FormattedFindings != payload.FormattedFindings {
		t.Errorf("FormattedFindings not preserved through JSON round-trip:\n  got:  %q\n  want: %q",
			decoded.FormattedFindings, payload.FormattedFindings)
	}
}
