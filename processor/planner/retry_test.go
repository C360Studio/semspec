package planner

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semspec/llm"
)

// mockLLM implements llmCompleter for testing the format correction retry loop.
type mockLLM struct {
	// responses is the ordered list of responses to return.
	// Each call to Complete pops from the front.
	responses []*llm.Response
	// errs parallels responses — if set, the error is returned instead.
	errs []error
	// calls records every request for assertion.
	calls []llm.Request
	idx   int
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

// newTestComponent creates a minimal Component with a mock LLM for retry tests.
func newTestComponent(mock *mockLLM) *Component {
	return &Component{
		llmClient: mock,
		logger:    slog.Default(),
		config:    Config{DefaultCapability: "planning"},
	}
}

// validPlanJSON is a well-formed plan response.
const validPlanJSON = `{
  "goal": "Add a goodbye endpoint",
  "context": "The API needs a farewell route",
  "scope": {
    "include": ["api/routes/"],
    "exclude": ["api/internal/"]
  }
}`

// --- formatCorrectionPrompt tests ---

func TestFormatCorrectionPrompt(t *testing.T) {
	err := fmt.Errorf("no JSON found in response")
	prompt := formatCorrectionPrompt(err)

	if !strings.Contains(prompt, "no JSON found in response") {
		t.Error("prompt should contain the original error message")
	}
	if !strings.Contains(prompt, `"goal"`) {
		t.Error("prompt should contain the expected JSON schema")
	}
	if !strings.Contains(prompt, "ONLY a valid JSON object") {
		t.Error("prompt should instruct LLM to respond with only JSON")
	}
}

// --- Retry loop behavior tests ---

func TestGeneratePlan_SuccessOnFirstAttempt(t *testing.T) {
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: validPlanJSON, Model: "test-model"},
		},
	}
	c := newTestComponent(mock)

	plan, err := c.generatePlanFromMessages(context.Background(), "planning", "You are a planner.", "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Goal != "Add a goodbye endpoint" {
		t.Errorf("Goal = %q, want %q", plan.Goal, "Add a goodbye endpoint")
	}

	// Should have made exactly 1 LLM call
	if len(mock.calls) != 1 {
		t.Errorf("LLM calls = %d, want 1", len(mock.calls))
	}
}

func TestGeneratePlan_SuccessOnSecondAttempt(t *testing.T) {
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: "Here's the plan: it's a great one!", Model: "test-model"},
			{Content: validPlanJSON, Model: "test-model"},
		},
	}
	c := newTestComponent(mock)

	plan, err := c.generatePlanFromMessages(context.Background(), "planning", "You are a planner.", "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Goal != "Add a goodbye endpoint" {
		t.Errorf("Goal = %q, want %q", plan.Goal, "Add a goodbye endpoint")
	}

	// Should have made 2 LLM calls
	if len(mock.calls) != 2 {
		t.Errorf("LLM calls = %d, want 2", len(mock.calls))
	}

	// Second call should include correction context
	// Messages: system, user, assistant(bad), correction(user)
	secondCall := mock.calls[1]
	if len(secondCall.Messages) != 4 {
		t.Fatalf("second call messages = %d, want 4 (system + user + assistant + correction)", len(secondCall.Messages))
	}
	if secondCall.Messages[0].Role != "system" {
		t.Errorf("messages[0].Role = %q, want %q", secondCall.Messages[0].Role, "system")
	}
	if secondCall.Messages[2].Role != "assistant" {
		t.Errorf("messages[2].Role = %q, want %q", secondCall.Messages[2].Role, "assistant")
	}
	if secondCall.Messages[2].Content != "Here's the plan: it's a great one!" {
		t.Errorf("messages[2] should echo the failed LLM response")
	}
	if secondCall.Messages[3].Role != "user" {
		t.Errorf("messages[3].Role = %q, want %q", secondCall.Messages[3].Role, "user")
	}
	if !strings.Contains(secondCall.Messages[3].Content, "could not be parsed as JSON") {
		t.Error("correction message should explain the parse failure")
	}
}

func TestGeneratePlan_AllRetriesFail(t *testing.T) {
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: "not json 1", Model: "test-model"},
			{Content: "not json 2", Model: "test-model"},
			{Content: "not json 3", Model: "test-model"},
			{Content: "not json 4", Model: "test-model"},
			{Content: "not json 5", Model: "test-model"},
		},
	}
	c := newTestComponent(mock)

	_, err := c.generatePlanFromMessages(context.Background(), "planning", "You are a planner.", "test prompt")
	if err == nil {
		t.Fatal("expected error after all retries exhausted")
	}
	if !strings.Contains(err.Error(), "parse plan from response") {
		t.Errorf("error = %q, should contain 'parse plan from response'", err.Error())
	}

	// Should have made maxFormatRetries LLM calls
	if len(mock.calls) != maxFormatRetries {
		t.Errorf("LLM calls = %d, want %d (maxFormatRetries)", len(mock.calls), maxFormatRetries)
	}
}

func TestGeneratePlan_HardLLMError_NoRetry(t *testing.T) {
	mock := &mockLLM{
		responses: []*llm.Response{nil},
		errs:      []error{fmt.Errorf("connection refused")},
	}
	c := newTestComponent(mock)

	_, err := c.generatePlanFromMessages(context.Background(), "planning", "You are a planner.", "test prompt")
	if err == nil {
		t.Fatal("expected error on LLM failure")
	}
	if !strings.Contains(err.Error(), "LLM completion") {
		t.Errorf("error = %q, should contain 'LLM completion'", err.Error())
	}

	// Hard errors don't retry
	if len(mock.calls) != 1 {
		t.Errorf("LLM calls = %d, want 1 (no retry on hard error)", len(mock.calls))
	}
}

func TestGeneratePlan_MessageAccumulation(t *testing.T) {
	// Fail twice, then succeed — verify the full conversation history is built correctly
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: "bad response 1", Model: "m"},
			{Content: "bad response 2", Model: "m"},
			{Content: validPlanJSON, Model: "m"},
		},
	}
	c := newTestComponent(mock)

	_, err := c.generatePlanFromMessages(context.Background(), "planning", "You are a planner.", "initial prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.calls) != 3 {
		t.Fatalf("LLM calls = %d, want 3", len(mock.calls))
	}

	// Call 1: system + user prompt
	if len(mock.calls[0].Messages) != 2 {
		t.Fatalf("call[0] messages = %d, want 2 (system + user)", len(mock.calls[0].Messages))
	}
	if mock.calls[0].Messages[0].Role != "system" {
		t.Errorf("call[0].Messages[0].Role = %q, want %q", mock.calls[0].Messages[0].Role, "system")
	}
	if mock.calls[0].Messages[1].Content != "initial prompt" {
		t.Errorf("call[0] user prompt = %q, want %q", mock.calls[0].Messages[1].Content, "initial prompt")
	}

	// Call 2: system + user + assistant(bad1) + correction
	if len(mock.calls[1].Messages) != 4 {
		t.Fatalf("call[1] messages = %d, want 4", len(mock.calls[1].Messages))
	}

	// Call 3: system + user + assistant(bad1) + correction + assistant(bad2) + correction
	if len(mock.calls[2].Messages) != 6 {
		t.Fatalf("call[2] messages = %d, want 6", len(mock.calls[2].Messages))
	}

	// Verify roles: system, user, assistant, user, assistant, user
	expectedRoles := []string{"system", "user", "assistant", "user", "assistant", "user"}
	for i, expected := range expectedRoles {
		if mock.calls[2].Messages[i].Role != expected {
			t.Errorf("call[2].Messages[%d].Role = %q, want %q",
				i, mock.calls[2].Messages[i].Role, expected)
		}
	}
}

func TestGeneratePlan_ParseErrorInCorrectionPrompt(t *testing.T) {
	// Verify the specific parse error is included in the correction prompt
	invalidJSON := `{"goal": "test", broken json here}`
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: invalidJSON, Model: "m"},
			{Content: validPlanJSON, Model: "m"},
		},
	}
	c := newTestComponent(mock)

	_, err := c.generatePlanFromMessages(context.Background(), "planning", "You are a planner.", "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The correction prompt should include the specific parse error
	// Messages: system(0), user(1), assistant(2), correction(3)
	correctionMsg := mock.calls[1].Messages[3].Content
	if !strings.Contains(correctionMsg, "parse JSON") {
		t.Errorf("correction prompt should contain the parse error type, got: %s", correctionMsg)
	}
}

func TestGeneratePlan_SystemPromptAlwaysPresent(t *testing.T) {
	// Verify that the system prompt is the first message on every LLM call,
	// even across retries. This ensures local LLMs always have JSON format context.
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: "bad", Model: "m"},
			{Content: validPlanJSON, Model: "m"},
		},
	}
	c := newTestComponent(mock)

	systemPrompt := "You are a planner. Output JSON."
	_, err := c.generatePlanFromMessages(context.Background(), "planning", systemPrompt, "make a plan")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, call := range mock.calls {
		if len(call.Messages) == 0 {
			t.Fatalf("call[%d] has no messages", i)
		}
		if call.Messages[0].Role != "system" {
			t.Errorf("call[%d].Messages[0].Role = %q, want %q", i, call.Messages[0].Role, "system")
		}
		if call.Messages[0].Content != systemPrompt {
			t.Errorf("call[%d].Messages[0].Content changed across retry", i)
		}
	}
}

func TestGeneratePlan_ValidJSON_MissingGoal_StillRetries(t *testing.T) {
	// Valid JSON but missing required "goal" field — should trigger retry
	noGoal := `{"context": "some context", "scope": {"include": []}}`
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: noGoal, Model: "m"},
			{Content: validPlanJSON, Model: "m"},
		},
	}
	c := newTestComponent(mock)

	plan, err := c.generatePlanFromMessages(context.Background(), "planning", "You are a planner.", "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Goal != "Add a goodbye endpoint" {
		t.Errorf("Goal = %q, want resolved on retry", plan.Goal)
	}

	// Should have retried
	if len(mock.calls) != 2 {
		t.Errorf("LLM calls = %d, want 2", len(mock.calls))
	}
}
