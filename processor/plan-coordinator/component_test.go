package plancoordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/reactive"
)

// ---------------------------------------------------------------------------
// Local mock LLM — captures calls for assertion, returns sequential responses
// ---------------------------------------------------------------------------

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

func newComponent(mock *mockLLM) *Component {
	return &Component{
		llmClient: mock,
		logger:    slog.Default(),
		config: Config{
			DefaultCapability:     "planning",
			MaxConcurrentPlanners: 3,
		},
	}
}

// ---------------------------------------------------------------------------
// Config validation tests
// ---------------------------------------------------------------------------

func TestConfig_Validate_Valid(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig() should be valid, got: %v", err)
	}
}

func TestConfig_Validate_MissingStreamName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StreamName = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty stream_name")
	}
	if !strings.Contains(err.Error(), "stream_name") {
		t.Errorf("error %q should mention stream_name", err.Error())
	}
}

func TestConfig_Validate_MissingStateBucket(t *testing.T) {
	cfg := DefaultConfig()
	cfg.StateBucket = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty state_bucket")
	}
	if !strings.Contains(err.Error(), "state_bucket") {
		t.Errorf("error %q should mention state_bucket", err.Error())
	}
}

func TestConfig_Validate_MaxConcurrentPlanners_TooLow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxConcurrentPlanners = 0
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for MaxConcurrentPlanners = 0")
	}
	if !strings.Contains(err.Error(), "max_concurrent_planners") {
		t.Errorf("error %q should mention max_concurrent_planners", err.Error())
	}
}

func TestConfig_Validate_MaxConcurrentPlanners_TooHigh(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxConcurrentPlanners = 11
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for MaxConcurrentPlanners = 11")
	}
	if !strings.Contains(err.Error(), "max_concurrent_planners") {
		t.Errorf("error %q should mention max_concurrent_planners", err.Error())
	}
}

func TestConfig_Validate_MaxConcurrentPlanners_Boundaries(t *testing.T) {
	for _, valid := range []int{1, 5, 10} {
		cfg := DefaultConfig()
		cfg.MaxConcurrentPlanners = valid
		if err := cfg.Validate(); err != nil {
			t.Errorf("MaxConcurrentPlanners=%d should be valid, got: %v", valid, err)
		}
	}
}

func TestConfig_GetContextTimeout_Default(t *testing.T) {
	cfg := Config{}
	got := cfg.GetContextTimeout()
	if got != 30*time.Second {
		t.Errorf("empty ContextTimeout = %v, want 30s", got)
	}
}

func TestConfig_GetContextTimeout_ParsesValid(t *testing.T) {
	cfg := Config{ContextTimeout: "45s"}
	got := cfg.GetContextTimeout()
	if got != 45*time.Second {
		t.Errorf("ContextTimeout=45s got %v, want 45s", got)
	}
}

func TestConfig_GetContextTimeout_FallsBackOnBadValue(t *testing.T) {
	cfg := Config{ContextTimeout: "not-a-duration"}
	got := cfg.GetContextTimeout()
	if got != 30*time.Second {
		t.Errorf("bad ContextTimeout should fall back to 30s, got %v", got)
	}
}

func TestDefaultConfig_HasExpectedDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.StreamName != "WORKFLOW" {
		t.Errorf("StreamName = %q, want WORKFLOW", cfg.StreamName)
	}
	if cfg.StateBucket != "REACTIVE_STATE" {
		t.Errorf("StateBucket = %q, want REACTIVE_STATE", cfg.StateBucket)
	}
	if cfg.FocusSubject != "workflow.async.coordination-focus" {
		t.Errorf("FocusSubject = %q unexpected", cfg.FocusSubject)
	}
	if cfg.PlannerSubject != "workflow.async.coordination-planner" {
		t.Errorf("PlannerSubject = %q unexpected", cfg.PlannerSubject)
	}
	if cfg.SynthesisSubject != "workflow.async.coordination-synthesis" {
		t.Errorf("SynthesisSubject = %q unexpected", cfg.SynthesisSubject)
	}
	if cfg.MaxConcurrentPlanners != 3 {
		t.Errorf("MaxConcurrentPlanners = %d, want 3", cfg.MaxConcurrentPlanners)
	}
	if cfg.DefaultCapability != "planning" {
		t.Errorf("DefaultCapability = %q, want planning", cfg.DefaultCapability)
	}
	if cfg.PlannerTimeout != "120s" {
		t.Errorf("PlannerTimeout = %q, want 120s", cfg.PlannerTimeout)
	}
}

// ---------------------------------------------------------------------------
// PromptsConfig helper
// ---------------------------------------------------------------------------

func TestPromptsConfig_GetCoordinatorSystem_Nil(t *testing.T) {
	var p *PromptsConfig
	if got := p.GetCoordinatorSystem(); got != "" {
		t.Errorf("nil PromptsConfig.GetCoordinatorSystem() = %q, want empty", got)
	}
}

func TestPromptsConfig_GetCoordinatorSystem_Value(t *testing.T) {
	p := &PromptsConfig{CoordinatorSystem: "/path/to/prompt.txt"}
	if got := p.GetCoordinatorSystem(); got != "/path/to/prompt.txt" {
		t.Errorf("GetCoordinatorSystem() = %q, want /path/to/prompt.txt", got)
	}
}

// ---------------------------------------------------------------------------
// Component metadata and lifecycle
// ---------------------------------------------------------------------------

func TestComponent_Meta(t *testing.T) {
	c := &Component{name: "plan-coordinator", config: DefaultConfig()}
	meta := c.Meta()

	if meta.Name != "plan-coordinator" {
		t.Errorf("Meta.Name = %q, want plan-coordinator", meta.Name)
	}
	if meta.Type != "processor" {
		t.Errorf("Meta.Type = %q, want processor", meta.Type)
	}
	if meta.Version == "" {
		t.Error("Meta.Version should not be empty")
	}
	if meta.Description == "" {
		t.Error("Meta.Description should not be empty")
	}
}

func TestComponent_WorkflowID(t *testing.T) {
	c := &Component{}
	if got := c.WorkflowID(); got != reactive.CoordinationLoopWorkflowID {
		t.Errorf("WorkflowID() = %q, want %q", got, reactive.CoordinationLoopWorkflowID)
	}
}

func TestComponent_Phase(t *testing.T) {
	c := &Component{}
	if got := c.Phase(); got == "" {
		t.Error("Phase() should not be empty")
	}
}

func TestComponent_StateManager_ReturnsNil(t *testing.T) {
	c := &Component{}
	if sm := c.StateManager(); sm != nil {
		t.Errorf("StateManager() = %v, want nil (engine manages state directly)", sm)
	}
}

func TestComponent_Initialize_Succeeds(t *testing.T) {
	c := &Component{
		name:   "plan-coordinator",
		config: DefaultConfig(),
		logger: slog.Default(),
	}
	if err := c.Initialize(); err != nil {
		t.Errorf("Initialize() error = %v, want nil", err)
	}
}

func TestComponent_Stop_WhenNotRunning(t *testing.T) {
	c := &Component{
		name:   "plan-coordinator",
		config: DefaultConfig(),
		logger: slog.Default(),
	}
	// Stop on an already-stopped component should be a no-op.
	if err := c.Stop(5 * time.Second); err != nil {
		t.Errorf("Stop() on non-running component error = %v, want nil", err)
	}
}

func TestComponent_Health_WhenStopped(t *testing.T) {
	c := &Component{
		name:   "plan-coordinator",
		config: DefaultConfig(),
		logger: slog.Default(),
	}
	h := c.Health()
	if h.Healthy {
		t.Error("Health().Healthy should be false when stopped")
	}
	if h.Status != "stopped" {
		t.Errorf("Health().Status = %q, want stopped", h.Status)
	}
}

func TestComponent_Health_ErrorCountTracksFailures(t *testing.T) {
	c := &Component{
		name:   "plan-coordinator",
		config: DefaultConfig(),
		logger: slog.Default(),
	}
	c.sessionsFailed.Add(3)
	h := c.Health()
	if h.ErrorCount != 3 {
		t.Errorf("Health().ErrorCount = %d, want 3", h.ErrorCount)
	}
}

func TestComponent_DataFlow_LastActivity(t *testing.T) {
	c := &Component{
		name:   "plan-coordinator",
		config: DefaultConfig(),
		logger: slog.Default(),
	}
	before := time.Now()
	c.updateLastActivity()
	after := time.Now()

	flow := c.DataFlow()
	if flow.LastActivity.Before(before) || flow.LastActivity.After(after) {
		t.Errorf("DataFlow().LastActivity not in expected range: %v", flow.LastActivity)
	}
}

// ---------------------------------------------------------------------------
// Port configuration
// ---------------------------------------------------------------------------

func TestComponent_InputPorts_DefaultConfig(t *testing.T) {
	c := &Component{config: DefaultConfig()}
	ports := c.InputPorts()

	if len(ports) != 3 {
		t.Fatalf("InputPorts() = %d ports, want 3", len(ports))
	}

	names := make([]string, len(ports))
	for i, p := range ports {
		names[i] = p.Name
	}

	wantNames := []string{"coordination-focus", "coordination-planners", "coordination-synthesis"}
	for _, want := range wantNames {
		found := false
		for _, got := range names {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("InputPorts() missing port %q, got %v", want, names)
		}
	}
}

func TestComponent_OutputPorts_DefaultConfig(t *testing.T) {
	c := &Component{config: DefaultConfig()}
	ports := c.OutputPorts()

	if len(ports) < 1 {
		t.Fatal("OutputPorts() should have at least 1 port")
	}
}

func TestComponent_InputPorts_NilPortConfig(t *testing.T) {
	c := &Component{config: Config{Ports: nil}}
	ports := c.InputPorts()
	if len(ports) != 0 {
		t.Errorf("InputPorts() with nil Ports = %d, want 0", len(ports))
	}
}

func TestComponent_OutputPorts_NilPortConfig(t *testing.T) {
	c := &Component{config: Config{Ports: nil}}
	ports := c.OutputPorts()
	if len(ports) != 0 {
		t.Errorf("OutputPorts() with nil Ports = %d, want 0", len(ports))
	}
}

// ---------------------------------------------------------------------------
// parseFocusAreas — extracts focus areas from LLM JSON response
// ---------------------------------------------------------------------------

func TestParseFocusAreas_Valid(t *testing.T) {
	c := newComponent(&mockLLM{})

	content := `{
  "focus_areas": [
    {"area": "api", "description": "REST API layer", "hints": ["api/", "handlers/"]},
    {"area": "data", "description": "Database models", "hints": ["models/", "store/"]}
  ]
}`
	focuses, err := c.parseFocusAreas(content)
	if err != nil {
		t.Fatalf("parseFocusAreas() error = %v", err)
	}
	if len(focuses) != 2 {
		t.Fatalf("len(focuses) = %d, want 2", len(focuses))
	}
	if focuses[0].Area != "api" {
		t.Errorf("focuses[0].Area = %q, want api", focuses[0].Area)
	}
	if focuses[1].Area != "data" {
		t.Errorf("focuses[1].Area = %q, want data", focuses[1].Area)
	}
	if len(focuses[0].Hints) != 2 {
		t.Errorf("focuses[0].Hints = %v, want 2 hints", focuses[0].Hints)
	}
}

func TestParseFocusAreas_InCodeBlock(t *testing.T) {
	c := newComponent(&mockLLM{})

	content := "Here are the focus areas:\n```json\n" + `{
  "focus_areas": [
    {"area": "auth", "description": "Authentication", "hints": ["auth/"]}
  ]
}` + "\n```\nThose are my recommendations."

	focuses, err := c.parseFocusAreas(content)
	if err != nil {
		t.Fatalf("parseFocusAreas() error = %v", err)
	}
	if len(focuses) != 1 {
		t.Fatalf("len(focuses) = %d, want 1", len(focuses))
	}
	if focuses[0].Area != "auth" {
		t.Errorf("focuses[0].Area = %q, want auth", focuses[0].Area)
	}
}

func TestParseFocusAreas_NoJSON(t *testing.T) {
	c := newComponent(&mockLLM{})
	_, err := c.parseFocusAreas("just text, no JSON here")
	if err == nil {
		t.Fatal("expected error for response with no JSON")
	}
}

func TestParseFocusAreas_EmptyFocusAreas(t *testing.T) {
	c := newComponent(&mockLLM{})
	_, err := c.parseFocusAreas(`{"focus_areas": []}`)
	if err == nil {
		t.Fatal("expected error for empty focus_areas array")
	}
}

func TestParseFocusAreas_HintsOptional(t *testing.T) {
	c := newComponent(&mockLLM{})
	content := `{"focus_areas": [{"area": "general", "description": "General analysis"}]}`
	focuses, err := c.parseFocusAreas(content)
	if err != nil {
		t.Fatalf("parseFocusAreas() error = %v", err)
	}
	if len(focuses) != 1 {
		t.Fatalf("len(focuses) = %d, want 1", len(focuses))
	}
	if focuses[0].Hints != nil && len(focuses[0].Hints) != 0 {
		t.Errorf("focuses[0].Hints should be nil/empty, got %v", focuses[0].Hints)
	}
}

// ---------------------------------------------------------------------------
// parsePlannerResult — extracts planner result from LLM JSON response
// ---------------------------------------------------------------------------

func TestParsePlannerResult_Valid(t *testing.T) {
	c := newComponent(&mockLLM{})

	content := `{
  "goal": "Implement authentication module",
  "context": "The API needs JWT-based auth",
  "scope": {
    "include": ["api/auth/", "middleware/"],
    "exclude": ["api/public/"]
  }
}`
	result, err := c.parsePlannerResult(content, "planner-123", "auth")
	if err != nil {
		t.Fatalf("parsePlannerResult() error = %v", err)
	}
	if result.PlannerID != "planner-123" {
		t.Errorf("PlannerID = %q, want planner-123", result.PlannerID)
	}
	if result.FocusArea != "auth" {
		t.Errorf("FocusArea = %q, want auth", result.FocusArea)
	}
	if result.Goal != "Implement authentication module" {
		t.Errorf("Goal = %q unexpected", result.Goal)
	}
	if result.Context != "The API needs JWT-based auth" {
		t.Errorf("Context = %q unexpected", result.Context)
	}
	if len(result.Scope.Include) != 2 {
		t.Errorf("Scope.Include len = %d, want 2", len(result.Scope.Include))
	}
	if len(result.Scope.Exclude) != 1 {
		t.Errorf("Scope.Exclude len = %d, want 1", len(result.Scope.Exclude))
	}
}

func TestParsePlannerResult_InCodeBlock(t *testing.T) {
	c := newComponent(&mockLLM{})
	content := "```json\n{\"goal\": \"Add caching\", \"context\": \"Performance\", \"scope\": {}}\n```"
	result, err := c.parsePlannerResult(content, "p-1", "caching")
	if err != nil {
		t.Fatalf("parsePlannerResult() error = %v", err)
	}
	if result.Goal != "Add caching" {
		t.Errorf("Goal = %q, want Add caching", result.Goal)
	}
}

func TestParsePlannerResult_MissingGoal(t *testing.T) {
	c := newComponent(&mockLLM{})
	content := `{"context": "Some context", "scope": {}}`
	_, err := c.parsePlannerResult(content, "p-1", "area")
	if err == nil {
		t.Fatal("expected error for missing goal")
	}
	if !strings.Contains(err.Error(), "goal") {
		t.Errorf("error %q should mention goal", err.Error())
	}
}

func TestParsePlannerResult_NoJSON(t *testing.T) {
	c := newComponent(&mockLLM{})
	_, err := c.parsePlannerResult("I couldn't create a plan for this.", "p-1", "area")
	if err == nil {
		t.Fatal("expected error for no JSON")
	}
}

func TestParsePlannerResult_DoNotTouchScope(t *testing.T) {
	c := newComponent(&mockLLM{})
	content := `{
  "goal": "Refactor safely",
  "context": "Careful approach",
  "scope": {
    "include": ["src/"],
    "do_not_touch": ["legacy/critical.go"]
  }
}`
	result, err := c.parsePlannerResult(content, "p-1", "refactor")
	if err != nil {
		t.Fatalf("parsePlannerResult() error = %v", err)
	}
	if len(result.Scope.DoNotTouch) != 1 {
		t.Errorf("DoNotTouch len = %d, want 1", len(result.Scope.DoNotTouch))
	}
	if result.Scope.DoNotTouch[0] != "legacy/critical.go" {
		t.Errorf("DoNotTouch[0] = %q, want legacy/critical.go", result.Scope.DoNotTouch[0])
	}
}

// ---------------------------------------------------------------------------
// parseSynthesizedPlan — extracts synthesized plan from LLM response
// ---------------------------------------------------------------------------

func TestParseSynthesizedPlan_Valid(t *testing.T) {
	c := newComponent(&mockLLM{})
	content := `{
  "goal": "Build a complete auth system with JWT tokens and refresh logic",
  "context": "The system needs both access and refresh tokens",
  "scope": {
    "include": ["api/auth/", "api/middleware/", "models/token.go"],
    "exclude": ["api/public/"]
  }
}`
	plan, err := c.parseSynthesizedPlan(content)
	if err != nil {
		t.Fatalf("parseSynthesizedPlan() error = %v", err)
	}
	if plan.Goal == "" {
		t.Error("Goal should not be empty")
	}
	if plan.Context == "" {
		t.Error("Context should not be empty")
	}
	if len(plan.Scope.Include) != 3 {
		t.Errorf("Scope.Include len = %d, want 3", len(plan.Scope.Include))
	}
}

func TestParseSynthesizedPlan_NoJSON(t *testing.T) {
	c := newComponent(&mockLLM{})
	_, err := c.parseSynthesizedPlan("The synthesized plan is not available in JSON format.")
	if err == nil {
		t.Fatal("expected error for no JSON")
	}
}

func TestParseSynthesizedPlan_EmptyGoalAllowed(t *testing.T) {
	// parseSynthesizedPlan itself doesn't require goal — synthesizeResults guards on empty
	c := newComponent(&mockLLM{})
	plan, err := c.parseSynthesizedPlan(`{"goal": "", "context": "ctx", "scope": {}}`)
	if err != nil {
		t.Fatalf("parseSynthesizedPlan() error = %v", err)
	}
	if plan.Goal != "" {
		t.Errorf("Goal = %q, want empty", plan.Goal)
	}
}

// ---------------------------------------------------------------------------
// simpleMerge — deterministic fallback when LLM synthesis fails
// ---------------------------------------------------------------------------

func TestSimpleMerge_SingleResult(t *testing.T) {
	c := newComponent(&mockLLM{})
	results := []workflow.PlannerResult{
		{
			PlannerID: "p1",
			FocusArea: "api",
			Goal:      "Build the API layer",
			Context:   "REST endpoints needed",
			Scope: workflow.Scope{
				Include: []string{"api/", "handlers/"},
				Exclude: []string{"vendor/"},
			},
		},
	}

	plan := c.simpleMerge(results)
	if !strings.Contains(plan.Goal, "Build the API layer") {
		t.Errorf("Goal = %q should contain original goal", plan.Goal)
	}
	if !strings.Contains(plan.Goal, "[api]") {
		t.Errorf("Goal = %q should be tagged with focus area [api]", plan.Goal)
	}
	if !strings.Contains(plan.Context, "REST endpoints needed") {
		t.Errorf("Context = %q should contain original context", plan.Context)
	}
}

func TestSimpleMerge_MultipleResults_MergesScope(t *testing.T) {
	c := newComponent(&mockLLM{})
	results := []workflow.PlannerResult{
		{
			PlannerID: "p1",
			FocusArea: "api",
			Goal:      "Build API",
			Scope: workflow.Scope{
				Include: []string{"api/", "handlers/"},
				Exclude: []string{"vendor/"},
			},
		},
		{
			PlannerID: "p2",
			FocusArea: "data",
			Goal:      "Design data layer",
			Scope: workflow.Scope{
				Include:    []string{"models/", "store/"},
				Exclude:    []string{"vendor/"},
				DoNotTouch: []string{"legacy/"},
			},
		},
	}

	plan := c.simpleMerge(results)

	// Goal should contain both focus areas
	if !strings.Contains(plan.Goal, "[api]") {
		t.Error("merged Goal should contain [api] tag")
	}
	if !strings.Contains(plan.Goal, "[data]") {
		t.Error("merged Goal should contain [data] tag")
	}

	// Include should be merged
	if !containsAll(plan.Scope.Include, "api/", "handlers/", "models/", "store/") {
		t.Errorf("Scope.Include = %v, should contain all merged paths", plan.Scope.Include)
	}

	// vendor/ should appear only once (unique)
	vendorCount := 0
	for _, s := range plan.Scope.Exclude {
		if s == "vendor/" {
			vendorCount++
		}
	}
	if vendorCount != 1 {
		t.Errorf("vendor/ appears %d times in Exclude, want exactly 1 (deduplication)", vendorCount)
	}

	// DoNotTouch
	if !containsAll(plan.Scope.DoNotTouch, "legacy/") {
		t.Errorf("Scope.DoNotTouch = %v, should contain legacy/", plan.Scope.DoNotTouch)
	}
}

func TestSimpleMerge_EmptyContext_Omitted(t *testing.T) {
	c := newComponent(&mockLLM{})
	results := []workflow.PlannerResult{
		{FocusArea: "api", Goal: "Build API", Context: ""},
		{FocusArea: "data", Goal: "Model data", Context: "Database design"},
	}

	plan := c.simpleMerge(results)

	// Empty context should not produce a tagged empty string
	if strings.Contains(plan.Context, "[api]") {
		t.Errorf("Context = %q should not include empty [api] section", plan.Context)
	}
	if !strings.Contains(plan.Context, "[data]") {
		t.Errorf("Context = %q should include [data] section", plan.Context)
	}
}

// ---------------------------------------------------------------------------
// synthesizeResults — single-result path bypasses LLM
// ---------------------------------------------------------------------------

func TestSynthesizeResults_SingleResult_NoLLMCall(t *testing.T) {
	mock := &mockLLM{}
	c := newComponent(mock)

	results := []workflow.PlannerResult{
		{
			PlannerID: "p1",
			FocusArea: "auth",
			Goal:      "Add JWT auth",
			Context:   "Stateless auth needed",
			Scope:     workflow.Scope{Include: []string{"auth/"}},
		},
	}

	plan, requestID, err := c.synthesizeResults(context.Background(), results)
	if err != nil {
		t.Fatalf("synthesizeResults() error = %v", err)
	}
	if requestID != "" {
		t.Errorf("requestID = %q, want empty (no LLM call for single result)", requestID)
	}
	if plan.Goal != "Add JWT auth" {
		t.Errorf("Goal = %q, want Add JWT auth", plan.Goal)
	}
	if plan.Context != "Stateless auth needed" {
		t.Errorf("Context = %q, want Stateless auth needed", plan.Context)
	}
	if mock.idx != 0 {
		t.Errorf("LLM should not be called for single result, got %d calls", mock.idx)
	}
}

func TestSynthesizeResults_MultipleResults_CallsLLM(t *testing.T) {
	synthesisResponse := `{
  "goal": "Build a comprehensive system",
  "context": "Merged context from all planners",
  "scope": {
    "include": ["api/", "models/"]
  }
}`
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: synthesisResponse, Model: "test-model"},
		},
	}
	c := newComponent(mock)

	results := []workflow.PlannerResult{
		{PlannerID: "p1", FocusArea: "api", Goal: "API layer", Scope: workflow.Scope{Include: []string{"api/"}}},
		{PlannerID: "p2", FocusArea: "data", Goal: "Data layer", Scope: workflow.Scope{Include: []string{"models/"}}},
	}

	plan, _, err := c.synthesizeResults(context.Background(), results)
	if err != nil {
		t.Fatalf("synthesizeResults() error = %v", err)
	}
	if plan.Goal != "Build a comprehensive system" {
		t.Errorf("Goal = %q, want synthesized goal", plan.Goal)
	}
	if mock.idx != 1 {
		t.Errorf("LLM should be called once for multiple results, got %d calls", mock.idx)
	}
}

func TestSynthesizeResults_LLMFailure_FallsBackToSimpleMerge(t *testing.T) {
	mock := &mockLLM{
		errs: []error{fmt.Errorf("LLM unavailable")},
	}
	c := newComponent(mock)

	results := []workflow.PlannerResult{
		{PlannerID: "p1", FocusArea: "api", Goal: "API goal"},
		{PlannerID: "p2", FocusArea: "data", Goal: "Data goal"},
	}

	plan, requestID, err := c.synthesizeResults(context.Background(), results)
	// Error falls back to simple merge, so err should be nil
	if err != nil {
		t.Fatalf("synthesizeResults() should not error on LLM failure (uses simpleMerge), got: %v", err)
	}
	if requestID != "" {
		t.Errorf("requestID = %q, want empty on LLM failure", requestID)
	}
	// Simple merge produces tagged goals
	if !strings.Contains(plan.Goal, "[api]") || !strings.Contains(plan.Goal, "[data]") {
		t.Errorf("fallback Goal = %q should contain tagged goals", plan.Goal)
	}
}

func TestSynthesizeResults_LLMParseFailure_FallsBackToSimpleMerge(t *testing.T) {
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: "I have synthesized the results into a unified view...", Model: "test-model"},
		},
	}
	c := newComponent(mock)

	results := []workflow.PlannerResult{
		{PlannerID: "p1", FocusArea: "api", Goal: "API goal"},
		{PlannerID: "p2", FocusArea: "data", Goal: "Data goal"},
	}

	plan, _, err := c.synthesizeResults(context.Background(), results)
	if err != nil {
		t.Fatalf("synthesizeResults() should not error on parse failure (uses simpleMerge), got: %v", err)
	}
	// Fallback simple merge
	if !strings.Contains(plan.Goal, "[api]") {
		t.Errorf("fallback Goal = %q should contain tagged goals", plan.Goal)
	}
}

func TestSynthesizeResults_EmptyGoalFromLLM_FallsBackToSimpleMerge(t *testing.T) {
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: `{"goal": "", "context": "some context", "scope": {}}`, Model: "test-model"},
		},
	}
	c := newComponent(mock)

	results := []workflow.PlannerResult{
		{PlannerID: "p1", FocusArea: "api", Goal: "API goal"},
		{PlannerID: "p2", FocusArea: "data", Goal: "Data goal"},
	}

	plan, _, err := c.synthesizeResults(context.Background(), results)
	if err != nil {
		t.Fatalf("synthesizeResults() error = %v", err)
	}
	// Empty goal from LLM triggers simple merge fallback
	if plan.Goal == "" {
		t.Error("Goal should not be empty after fallback to simpleMerge")
	}
	if !strings.Contains(plan.Goal, "[api]") {
		t.Errorf("fallback Goal = %q should contain tagged goals", plan.Goal)
	}
}

// ---------------------------------------------------------------------------
// collectPlannerResults — converts PlannerOutcome map to typed slice
// ---------------------------------------------------------------------------

func TestCollectPlannerResults_HappyPath(t *testing.T) {
	c := newComponent(&mockLLM{})

	plannerResult := workflow.PlannerResult{
		PlannerID: "p1",
		FocusArea: "auth",
		Goal:      "Implement JWT auth",
		Context:   "Stateless tokens",
		Scope:     workflow.Scope{Include: []string{"auth/"}},
	}
	resultJSON, _ := json.Marshal(plannerResult)

	state := &reactive.CoordinationState{
		PlannerResults: map[string]*reactive.PlannerOutcome{
			"p1": {
				PlannerID: "p1",
				FocusArea: "auth",
				Status:    "completed",
				Result:    resultJSON,
			},
		},
	}

	results, err := c.collectPlannerResults(state)
	if err != nil {
		t.Fatalf("collectPlannerResults() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Goal != "Implement JWT auth" {
		t.Errorf("Goal = %q unexpected", results[0].Goal)
	}
}

func TestCollectPlannerResults_SkipsFailedPlanners(t *testing.T) {
	c := newComponent(&mockLLM{})

	plannerResult := workflow.PlannerResult{
		PlannerID: "p2",
		FocusArea: "data",
		Goal:      "Data layer",
	}
	resultJSON, _ := json.Marshal(plannerResult)

	state := &reactive.CoordinationState{
		PlannerResults: map[string]*reactive.PlannerOutcome{
			"p1": {
				PlannerID: "p1",
				FocusArea: "api",
				Status:    "failed",
				Error:     "LLM timeout",
			},
			"p2": {
				PlannerID: "p2",
				FocusArea: "data",
				Status:    "completed",
				Result:    resultJSON,
			},
		},
	}

	results, err := c.collectPlannerResults(state)
	if err != nil {
		t.Fatalf("collectPlannerResults() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1 (failed planner skipped)", len(results))
	}
	if results[0].FocusArea != "data" {
		t.Errorf("results[0].FocusArea = %q, want data", results[0].FocusArea)
	}
}

func TestCollectPlannerResults_SkipsEmptyResults(t *testing.T) {
	c := newComponent(&mockLLM{})

	state := &reactive.CoordinationState{
		PlannerResults: map[string]*reactive.PlannerOutcome{
			"p1": {
				PlannerID: "p1",
				Status:    "completed",
				Result:    nil, // no result data
			},
		},
	}

	results, err := c.collectPlannerResults(state)
	if err != nil {
		t.Fatalf("collectPlannerResults() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0 (empty result skipped)", len(results))
	}
}

func TestCollectPlannerResults_InvalidJSON_ReturnsPartialWithError(t *testing.T) {
	c := newComponent(&mockLLM{})

	validResult := workflow.PlannerResult{PlannerID: "p1", FocusArea: "api", Goal: "Build API"}
	validJSON, _ := json.Marshal(validResult)

	state := &reactive.CoordinationState{
		PlannerResults: map[string]*reactive.PlannerOutcome{
			"p1": {
				PlannerID: "p1",
				Status:    "completed",
				Result:    validJSON,
			},
			"p2": {
				PlannerID: "p2",
				Status:    "completed",
				Result:    json.RawMessage(`{invalid json`),
			},
		},
	}

	results, err := c.collectPlannerResults(state)
	if err == nil {
		t.Fatal("expected error for invalid JSON result")
	}
	// Should still return the successfully-parsed results
	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1 (valid result still returned)", len(results))
	}
}

func TestCollectPlannerResults_EmptyState(t *testing.T) {
	c := newComponent(&mockLLM{})
	state := &reactive.CoordinationState{
		PlannerResults: map[string]*reactive.PlannerOutcome{},
	}

	results, err := c.collectPlannerResults(state)
	if err != nil {
		t.Fatalf("collectPlannerResults() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0", len(results))
	}
}

// ---------------------------------------------------------------------------
// determineFocusAreas — explicit focus areas bypass LLM
// ---------------------------------------------------------------------------

func TestDetermineFocusAreas_ExplicitFocuses_BypassLLM(t *testing.T) {
	mock := &mockLLM{}
	c := newComponent(mock)

	trigger := &reactive.PlanCoordinatorRequest{
		Slug:       "test-plan",
		Title:      "Test Plan",
		FocusAreas: []string{"api", "data", "auth"},
	}

	focuses, err := c.determineFocusAreas(context.Background(), trigger)
	if err != nil {
		t.Fatalf("determineFocusAreas() error = %v", err)
	}
	if len(focuses) != 3 {
		t.Fatalf("len(focuses) = %d, want 3", len(focuses))
	}
	if mock.idx != 0 {
		t.Errorf("LLM should not be called when explicit focuses provided, got %d calls", mock.idx)
	}

	// Verify each focus area is mapped correctly
	areaMap := make(map[string]*FocusArea)
	for _, f := range focuses {
		areaMap[f.Area] = f
	}
	for _, name := range []string{"api", "data", "auth"} {
		if _, ok := areaMap[name]; !ok {
			t.Errorf("missing focus area %q in result", name)
		}
	}
}

func TestDetermineFocusAreas_ExplicitFocuses_SetsDescription(t *testing.T) {
	c := newComponent(&mockLLM{})

	trigger := &reactive.PlanCoordinatorRequest{
		Slug:       "test-plan",
		FocusAreas: []string{"auth"},
	}

	focuses, err := c.determineFocusAreas(context.Background(), trigger)
	if err != nil {
		t.Fatalf("determineFocusAreas() error = %v", err)
	}
	if focuses[0].Description == "" {
		t.Error("explicit focus should have a generated Description")
	}
	if !strings.Contains(focuses[0].Description, "auth") {
		t.Errorf("Description = %q, should mention the focus area name", focuses[0].Description)
	}
}

// ---------------------------------------------------------------------------
// loadPrompt — custom prompt file loading
// ---------------------------------------------------------------------------

func TestLoadPrompt_EmptyPath_ReturnsDefault(t *testing.T) {
	c := &Component{logger: slog.Default()}
	got := c.loadPrompt("", "default system prompt")
	if got != "default system prompt" {
		t.Errorf("loadPrompt with empty path = %q, want default", got)
	}
}

func TestLoadPrompt_NonExistentFile_ReturnsDefault(t *testing.T) {
	c := &Component{logger: slog.Default()}
	got := c.loadPrompt("/nonexistent/path/prompt.txt", "fallback prompt")
	if got != "fallback prompt" {
		t.Errorf("loadPrompt with bad path = %q, want fallback", got)
	}
}

func TestLoadPrompt_ValidFile_ReturnsFileContent(t *testing.T) {
	c := &Component{logger: slog.Default()}

	// Write a temp file using os.WriteFile
	dir := t.TempDir()
	promptPath := dir + "/custom-prompt.txt"
	if err := os.WriteFile(promptPath, []byte("You are a custom coordinator."), 0o600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	got := c.loadPrompt(promptPath, "default")
	if got != "You are a custom coordinator." {
		t.Errorf("loadPrompt = %q, want file content", got)
	}
}

// ---------------------------------------------------------------------------
// Helper functions tested in isolation
// ---------------------------------------------------------------------------

func TestFocusAreas_ExtractsNames(t *testing.T) {
	focuses := []*FocusArea{
		{Area: "api", Description: "API layer"},
		{Area: "data", Description: "Data layer"},
		{Area: "auth", Description: "Auth"},
	}
	got := focusAreas(focuses)
	if len(got) != 3 {
		t.Fatalf("focusAreas() len = %d, want 3", len(got))
	}
	if got[0] != "api" || got[1] != "data" || got[2] != "auth" {
		t.Errorf("focusAreas() = %v, want [api data auth]", got)
	}
}

func TestJoinWithNewlines(t *testing.T) {
	strs := []string{"first", "second", "third"}
	got := joinWithNewlines(strs)
	if got != "first\n\nsecond\n\nthird" {
		t.Errorf("joinWithNewlines() = %q, unexpected", got)
	}
}

func TestJoinWithNewlines_Empty(t *testing.T) {
	got := joinWithNewlines(nil)
	if got != "" {
		t.Errorf("joinWithNewlines(nil) = %q, want empty", got)
	}
}

func TestUnique_DeduplicatesPreservingOrder(t *testing.T) {
	input := []string{"api/", "models/", "api/", "store/", "models/"}
	got := unique(input)
	if len(got) != 3 {
		t.Fatalf("unique() len = %d, want 3", len(got))
	}
	if got[0] != "api/" || got[1] != "models/" || got[2] != "store/" {
		t.Errorf("unique() = %v, want [api/ models/ store/]", got)
	}
}

func TestUnique_EmptyInput(t *testing.T) {
	got := unique(nil)
	if len(got) != 0 {
		t.Errorf("unique(nil) = %v, want empty", got)
	}
}

func TestUnique_AllSame(t *testing.T) {
	input := []string{"vendor/", "vendor/", "vendor/"}
	got := unique(input)
	if len(got) != 1 {
		t.Fatalf("unique(all same) len = %d, want 1", len(got))
	}
}

// ---------------------------------------------------------------------------
// CoordinatorResult payload — schema and JSON round-trip
// ---------------------------------------------------------------------------

func TestCoordinatorResult_Schema(t *testing.T) {
	r := &CoordinatorResult{}
	schema := r.Schema()
	if schema.Domain == "" {
		t.Error("Schema().Domain should not be empty")
	}
	if schema.Category == "" {
		t.Error("Schema().Category should not be empty")
	}
	if schema.Version == "" {
		t.Error("Schema().Version should not be empty")
	}
}

func TestCoordinatorResult_Validate_AlwaysPasses(t *testing.T) {
	r := &CoordinatorResult{}
	if err := r.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestCoordinatorResult_JSONRoundTrip(t *testing.T) {
	original := &CoordinatorResult{
		RequestID:     "req-123",
		TraceID:       "trace-abc",
		Slug:          "my-feature",
		PlannerCount:  3,
		Status:        "completed",
		LLMRequestIDs: []string{"llm-1", "llm-2"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded CoordinatorResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID = %q, want %q", decoded.RequestID, original.RequestID)
	}
	if decoded.Slug != original.Slug {
		t.Errorf("Slug = %q, want %q", decoded.Slug, original.Slug)
	}
	if decoded.PlannerCount != original.PlannerCount {
		t.Errorf("PlannerCount = %d, want %d", decoded.PlannerCount, original.PlannerCount)
	}
	if len(decoded.LLMRequestIDs) != len(original.LLMRequestIDs) {
		t.Errorf("LLMRequestIDs len = %d, want %d", len(decoded.LLMRequestIDs), len(original.LLMRequestIDs))
	}
}

// ---------------------------------------------------------------------------
// callLLM — uses capability from config, passes through response
// ---------------------------------------------------------------------------

func TestCallLLM_UsesDefaultCapability(t *testing.T) {
	mock := &mockLLM{
		responses: []*llm.Response{
			{Content: "response content", Model: "test-model", RequestID: "req-abc"},
		},
	}
	c := newComponent(mock)
	c.config.DefaultCapability = "planning"

	content, requestID, err := c.callLLM(context.Background(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("callLLM() error = %v", err)
	}
	if content != "response content" {
		t.Errorf("content = %q, want response content", content)
	}
	if requestID != "req-abc" {
		t.Errorf("requestID = %q, want req-abc", requestID)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("LLM call count = %d, want 1", len(mock.calls))
	}
	if mock.calls[0].Capability != "planning" {
		t.Errorf("LLM called with capability %q, want planning", mock.calls[0].Capability)
	}
}

func TestCallLLM_SetsTemperatureAndMaxTokens(t *testing.T) {
	mock := &mockLLM{
		responses: []*llm.Response{{Content: "ok", Model: "m"}},
	}
	c := newComponent(mock)

	_, _, err := c.callLLM(context.Background(), "sys", "usr")
	if err != nil {
		t.Fatalf("callLLM() error = %v", err)
	}

	req := mock.calls[0]
	if req.Temperature == nil {
		t.Fatal("Temperature should be set")
	}
	if *req.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", *req.Temperature)
	}
	if req.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", req.MaxTokens)
	}
}

func TestCallLLM_SendsSystemAndUserMessages(t *testing.T) {
	mock := &mockLLM{
		responses: []*llm.Response{{Content: "ok", Model: "m"}},
	}
	c := newComponent(mock)

	_, _, err := c.callLLM(context.Background(), "you are coordinator", "analyze this task")
	if err != nil {
		t.Fatalf("callLLM() error = %v", err)
	}

	req := mock.calls[0]
	if len(req.Messages) != 2 {
		t.Fatalf("messages count = %d, want 2 (system + user)", len(req.Messages))
	}
	if req.Messages[0].Role != "system" {
		t.Errorf("Messages[0].Role = %q, want system", req.Messages[0].Role)
	}
	if req.Messages[0].Content != "you are coordinator" {
		t.Errorf("Messages[0].Content = %q, want system prompt", req.Messages[0].Content)
	}
	if req.Messages[1].Role != "user" {
		t.Errorf("Messages[1].Role = %q, want user", req.Messages[1].Role)
	}
	if req.Messages[1].Content != "analyze this task" {
		t.Errorf("Messages[1].Content = %q, want user prompt", req.Messages[1].Content)
	}
}

func TestCallLLM_ErrorPropagates(t *testing.T) {
	mock := &mockLLM{
		errs: []error{fmt.Errorf("connection timeout")},
	}
	c := newComponent(mock)

	_, _, err := c.callLLM(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error to propagate from LLM")
	}
	if !strings.Contains(err.Error(), "LLM completion") {
		t.Errorf("error = %q, should contain 'LLM completion'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Factory registration
// ---------------------------------------------------------------------------

func TestRegister_NilRegistry(t *testing.T) {
	err := Register(nil)
	if err == nil {
		t.Fatal("Register(nil) should return error")
	}
	if !strings.Contains(err.Error(), "registry") {
		t.Errorf("error = %q, should mention registry", err.Error())
	}
}

// ---------------------------------------------------------------------------
// MaxConcurrentPlanners limiting in determineFocusAreas
// ---------------------------------------------------------------------------

func TestDetermineFocusAreas_ExplicitFocuses_NoLimitApplied(t *testing.T) {
	// Explicit focuses are returned as-is (no MaxPlanners limit applied)
	c := newComponent(&mockLLM{})
	c.config.MaxConcurrentPlanners = 2

	trigger := &reactive.PlanCoordinatorRequest{
		Slug:       "test-plan",
		FocusAreas: []string{"api", "data", "auth"}, // 3 explicit focuses
	}

	focuses, err := c.determineFocusAreas(context.Background(), trigger)
	if err != nil {
		t.Fatalf("determineFocusAreas() error = %v", err)
	}
	// Explicit focuses bypass the MaxConcurrentPlanners limit
	if len(focuses) != 3 {
		t.Errorf("len(focuses) = %d, want 3 (explicit focuses bypass limit)", len(focuses))
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// containsAll checks that all expected strings are present in the slice.
func containsAll(slice []string, expected ...string) bool {
	set := make(map[string]bool, len(slice))
	for _, s := range slice {
		set[s] = true
	}
	for _, e := range expected {
		if !set[e] {
			return false
		}
	}
	return true
}
