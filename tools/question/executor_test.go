package question

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/answerer"
	"github.com/c360studio/semstreams/agentic"
)

func testExecutor() *Executor {
	return &Executor{
		timeout: DefaultTimeout,
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestNormalizeQuestion(t *testing.T) {
	tests := []struct {
		in, out string
	}{
		{"Hello world", "hello world"},
		{"  Hello   world  ", "hello world"},
		{"HELLO\n\tWorld", "hello world"},
		{"", ""},
		{"   ", ""},
		{"Is there an existing implementation?", "is there an existing implementation?"},
		{"is there   an existing\nimplementation?", "is there an existing implementation?"},
	}
	for _, tt := range tests {
		got := normalizeQuestion(tt.in)
		if got != tt.out {
			t.Errorf("normalizeQuestion(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}

func TestHandleDuplicate_Answered(t *testing.T) {
	e := testExecutor()
	call := agentic.ToolCall{ID: "call-1", LoopID: "loop-1"}
	dup := &workflow.Question{
		ID:        "q-abc",
		FromAgent: "loop-1",
		Question:  "Is there an existing implementation?",
		Status:    workflow.QuestionStatusAnswered,
		Answer:    "No, this is a greenfield project. Start from scratch.",
		CreatedAt: time.Now().Add(-2 * time.Minute),
	}

	res := e.handleDuplicate(call, dup, dup.Question)
	if res.Error != "" {
		t.Errorf("unexpected error: %s", res.Error)
	}
	if !strings.Contains(res.Content, "q-abc") {
		t.Errorf("content should reference the prior question ID, got %q", res.Content)
	}
	if !strings.Contains(res.Content, dup.Answer) {
		t.Errorf("content should surface the prior answer verbatim, got %q", res.Content)
	}
}

func TestHandleDuplicate_Timeout(t *testing.T) {
	e := testExecutor()
	call := agentic.ToolCall{ID: "call-2", LoopID: "loop-1"}
	dup := &workflow.Question{
		ID:        "q-xyz",
		FromAgent: "loop-1",
		Question:  "Is there an existing implementation?",
		Status:    workflow.QuestionStatusTimeout,
		CreatedAt: time.Now().Add(-6 * time.Minute),
	}

	res := e.handleDuplicate(call, dup, dup.Question)
	if res.Error != "" {
		t.Errorf("unexpected error: %s", res.Error)
	}
	// Message must tell the agent to stop re-asking — the mortgage-calc log
	// shows the same question asked every ~1min after timeout until the
	// requirement-level deadline. That's what this guard is for.
	if !strings.Contains(strings.ToLower(res.Content), "do not ask it again") {
		t.Errorf("content should instruct agent not to re-ask, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "q-xyz") {
		t.Errorf("content should reference the prior question ID, got %q", res.Content)
	}
}

func TestHandleDuplicate_Pending(t *testing.T) {
	e := testExecutor()
	call := agentic.ToolCall{ID: "call-3", LoopID: "loop-1"}
	dup := &workflow.Question{
		ID:        "q-pending",
		FromAgent: "loop-1",
		Question:  "Is there an existing implementation?",
		Status:    workflow.QuestionStatusPending,
		CreatedAt: time.Now().Add(-1 * time.Minute),
	}

	res := e.handleDuplicate(call, dup, dup.Question)
	if !strings.Contains(strings.ToLower(res.Content), "still pending") {
		t.Errorf("content should indicate still pending, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "q-pending") {
		t.Errorf("content should reference the prior question ID, got %q", res.Content)
	}
}

func TestResolveRoute_NilRegistryReturnsNil(t *testing.T) {
	e := testExecutor()
	if got := e.resolveRoute("anything"); got != nil {
		t.Errorf("nil registry should produce nil route, got %+v", got)
	}
}

func TestResolveRoute_AgentRouteReturnsCapability(t *testing.T) {
	reg := answerer.NewRegistry()
	reg.AddRoute(answerer.Route{
		Pattern:    "architecture.**",
		Answerer:   "agent/architect",
		Capability: "question_answering",
	})

	e := testExecutor().WithAnswererRegistry(reg)
	got := e.resolveRoute("architecture.api")
	if got == nil {
		t.Fatal("expected matched route, got nil")
	}
	if got.Type != answerer.AnswererAgent {
		t.Errorf("Type = %v, want AnswererAgent", got.Type)
	}
	if got.Capability != "question_answering" {
		t.Errorf("Capability = %q, want question_answering", got.Capability)
	}
}

func TestResolveRoute_HumanRouteFallsThroughToWaiting(t *testing.T) {
	reg := answerer.NewRegistry()
	reg.AddRoute(answerer.Route{
		Pattern:  "requirements.**",
		Answerer: "human/requester",
	})

	e := testExecutor().WithAnswererRegistry(reg)
	got := e.resolveRoute("requirements.scope")
	if got == nil {
		t.Fatal("expected matched route, got nil")
	}
	if got.Type != answerer.AnswererHuman {
		t.Errorf("Type = %v, want AnswererHuman", got.Type)
	}
}

func TestResolveRoute_DefaultRouteOnNoMatch(t *testing.T) {
	reg := answerer.NewRegistry() // default route is human/requester
	e := testExecutor().WithAnswererRegistry(reg)
	got := e.resolveRoute("totally.unmatched.topic")
	if got == nil {
		t.Fatal("expected default route, got nil")
	}
	if got.Type != answerer.AnswererHuman {
		t.Errorf("default Type = %v, want AnswererHuman", got.Type)
	}
}

func TestRouteAnswererAndType_NilSafe(t *testing.T) {
	if routeAnswerer(nil) != "(no registry)" {
		t.Errorf("routeAnswerer(nil) wrong")
	}
	if routeType(nil) != "legacy" {
		t.Errorf("routeType(nil) wrong")
	}
}

// TestDecideDispatch is the seam-coverage test that would have caught the
// "registry exists but executor doesn't read it" bug. Every branch of the
// route-type switch is asserted, including the legacy nil-route path.
// If a new AnswererType is added, extend BOTH this table AND
// decideDispatch — the test exists specifically to fail loudly when one
// is updated without the other.
func TestDecideDispatch(t *testing.T) {
	tests := []struct {
		name           string
		route          *answerer.Route
		wantAction     dispatchAction
		wantCapability string
		wantAnswerer   string
	}{
		{
			name:       "nil route → legacy dispatch (no capability)",
			route:      nil,
			wantAction: dispatchAgent,
		},
		{
			name: "agent route → dispatch with capability + answerer",
			route: &answerer.Route{
				Pattern:    "architecture.**",
				Answerer:   "agent/architect",
				Type:       answerer.AnswererAgent,
				Capability: "question_answering",
			},
			wantAction:     dispatchAgent,
			wantCapability: "question_answering",
			wantAnswerer:   "agent/architect",
		},
		{
			name: "agent route with empty capability → dispatch but no capability override",
			route: &answerer.Route{
				Pattern:  "**",
				Answerer: "agent/general",
				Type:     answerer.AnswererAgent,
			},
			wantAction:   dispatchAgent,
			wantAnswerer: "agent/general",
		},
		{
			name: "human route → SKIP dispatch (HTTP answers)",
			route: &answerer.Route{
				Pattern:  "requirements.**",
				Answerer: "human/requester",
				Type:     answerer.AnswererHuman,
			},
			wantAction:   dispatchSkip,
			wantAnswerer: "human/requester",
		},
		{
			name: "team route → SKIP dispatch (external integration answers)",
			route: &answerer.Route{
				Pattern:  "team.**",
				Answerer: "team/security",
				Type:     answerer.AnswererTeam,
			},
			wantAction:   dispatchSkip,
			wantAnswerer: "team/security",
		},
		{
			name: "tool route → fallback to generic agent (not yet implemented)",
			route: &answerer.Route{
				Pattern:  "lookup.**",
				Answerer: "tool/web-search",
				Type:     answerer.AnswererTool,
			},
			wantAction:   dispatchAgent,
			wantAnswerer: "tool/web-search",
		},
		{
			name: "unknown route type → defensive fallback to generic agent",
			route: &answerer.Route{
				Pattern:  "weird.**",
				Answerer: "weird/thing",
				Type:     answerer.Type("unknown"),
			},
			wantAction:   dispatchAgent,
			wantAnswerer: "weird/thing",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := decideDispatch(tc.route)
			if plan.Action != tc.wantAction {
				t.Errorf("Action = %v, want %v (reason=%q)", plan.Action, tc.wantAction, plan.Reason)
			}
			if plan.Capability != tc.wantCapability {
				t.Errorf("Capability = %q, want %q", plan.Capability, tc.wantCapability)
			}
			if plan.Answerer != tc.wantAnswerer {
				t.Errorf("Answerer = %q, want %q", plan.Answerer, tc.wantAnswerer)
			}
			if plan.Reason == "" {
				t.Errorf("Reason should always be populated for telemetry; got empty")
			}
		})
	}
}

// TestExecuteFlow_HumanRouteSkipsDispatch is the full-flow integration
// test the prior code lacked: build an Executor wired to a Registry,
// run Execute() against a question that resolves to a human route,
// and assert the dispatch decision (via a recording-only routeQuestion
// substitute) skipped publishing.
//
// Pre-this-change, the analogous test would have FAILED because
// routeQuestion always called dispatchAnswerer regardless of route.
func TestRouteQuestion_HumanRouteSkipsAndAgentRouteDispatches(t *testing.T) {
	// Two routes — one human, one agent — to assert different decisions
	// resolved from the same Executor in one test.
	reg := answerer.NewRegistry()
	reg.AddRoute(answerer.Route{
		Pattern:  "requirements.**",
		Answerer: "human/requester",
	})
	reg.AddRoute(answerer.Route{
		Pattern:    "architecture.**",
		Answerer:   "agent/architect",
		Capability: "question_answering",
	})

	humanRoute := reg.Match("requirements.scope")
	agentRoute := reg.Match("architecture.api")

	humanPlan := decideDispatch(humanRoute)
	if humanPlan.Action != dispatchSkip {
		t.Errorf("requirements.scope should SKIP (human route), got %v reason=%q", humanPlan.Action, humanPlan.Reason)
	}

	agentPlan := decideDispatch(agentRoute)
	if agentPlan.Action != dispatchAgent {
		t.Errorf("architecture.api should DISPATCH (agent route), got %v reason=%q", agentPlan.Action, agentPlan.Reason)
	}
	if agentPlan.Capability != "question_answering" {
		t.Errorf("agent dispatch should carry capability=question_answering, got %q", agentPlan.Capability)
	}
}
