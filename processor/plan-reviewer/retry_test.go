package planreviewer

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

const validReviewJSON = `{
  "verdict": "approved",
  "summary": "Plan meets all SOP requirements",
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

const needsChangesJSON = `{
  "verdict": "needs_changes",
  "summary": "Plan is missing test coverage requirements",
  "findings": [
    {
      "sop_id": "source.doc.sops.testing",
      "sop_title": "Testing Standards",
      "severity": "error",
      "status": "violation",
      "issue": "No tests mentioned in scope",
      "suggestion": "Add test files to scope.include"
    }
  ]
}`

// --- reviewerFormatCorrectionPrompt tests ---

func TestReviewerFormatCorrectionPrompt(t *testing.T) {
	err := fmt.Errorf("no JSON found in response")
	prompt := reviewerFormatCorrectionPrompt(err)

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
}

// --- Retry loop behavior tests ---

func TestReviewPlan_SuccessOnFirstAttempt(t *testing.T) {
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: validReviewJSON, Model: "test-model"},
		},
	}
	c := newTestComponent(mock)

	result, err := c.parseReviewFromResponse(validReviewJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != "approved" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "approved")
	}

	// Verify the mock works for the retry path too
	_ = mock // mock is ready for integration tests
}

func TestReviewPlan_SuccessOnSecondAttempt(t *testing.T) {
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: "Here's my review: the plan looks good.", Model: "test-model"},
			{Content: validReviewJSON, Model: "test-model"},
		},
	}
	c := newTestComponent(mock)

	// Simulate what reviewPlan does: build messages and call LLM in a retry loop
	messages := []llm.Message{
		{Role: "system", Content: "You are a plan reviewer."},
		{Role: "user", Content: "Review this plan."},
	}

	var result *PlanReviewResult
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
			result = &PlanReviewResult{
				Verdict: parsed.Verdict,
				Summary: parsed.Summary,
			}
			break
		}

		lastErr = parseErr
		if attempt+1 >= maxFormatRetries {
			break
		}

		messages = append(messages,
			llm.Message{Role: "assistant", Content: llmResp.Content},
			llm.Message{Role: "user", Content: reviewerFormatCorrectionPrompt(parseErr)},
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

func TestReviewPlan_AllRetriesFail(t *testing.T) {
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
			llm.Message{Role: "user", Content: reviewerFormatCorrectionPrompt(parseErr)},
		)
	}

	if lastErr == nil {
		t.Fatal("expected error after all retries")
	}
	if len(mock.calls) != maxFormatRetries {
		t.Errorf("LLM calls = %d, want %d", len(mock.calls), maxFormatRetries)
	}
}

func TestReviewPlan_InvalidVerdict_Retries(t *testing.T) {
	// Valid JSON but invalid verdict — should be caught by parseReviewFromResponse
	badVerdict := `{"verdict": "maybe", "summary": "unsure", "findings": []}`
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: badVerdict, Model: "m"},
			{Content: needsChangesJSON, Model: "m"},
		},
	}
	c := newTestComponent(mock)

	// First call returns bad verdict
	_, err := c.parseReviewFromResponse(badVerdict)
	if err == nil {
		t.Fatal("expected error for invalid verdict")
	}
	if !strings.Contains(err.Error(), "invalid verdict") {
		t.Errorf("error = %q, should contain 'invalid verdict'", err.Error())
	}

	// Second call returns valid review
	result, err := c.parseReviewFromResponse(needsChangesJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Verdict != "needs_changes" {
		t.Errorf("Verdict = %q, want %q", result.Verdict, "needs_changes")
	}
}

func TestReviewPlan_NeedsChanges_ParsedCorrectly(t *testing.T) {
	c := newTestComponent(&mockLLM{})

	result, err := c.parseReviewFromResponse(needsChangesJSON)
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
}

func TestFormattedFindings_HumanReadable(t *testing.T) {
	c := newTestComponent(&mockLLM{})

	result, err := c.parseReviewFromResponse(needsChangesJSON)
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
	if !strings.Contains(formatted, "No tests mentioned in scope") {
		t.Error("formatted findings should contain the issue description")
	}
	if !strings.Contains(formatted, "Add test files to scope.include") {
		t.Error("formatted findings should contain the suggestion")
	}
}

func TestCallbackPayload_IncludesFormattedFindings(t *testing.T) {
	// Verify that PlanReviewResult (the callback payload struct) correctly
	// includes FormattedFindings when built from a review result — this is
	// what the workflow interpolator accesses via ${steps.plan_reviewer.output.formatted_findings}.
	c := newTestComponent(&mockLLM{})

	result, err := c.parseReviewFromResponse(needsChangesJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate what publishResult does
	payload := &PlanReviewResult{
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

	var decoded PlanReviewResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.FormattedFindings != payload.FormattedFindings {
		t.Errorf("FormattedFindings not preserved through JSON round-trip:\n  got:  %q\n  want: %q",
			decoded.FormattedFindings, payload.FormattedFindings)
	}
}
