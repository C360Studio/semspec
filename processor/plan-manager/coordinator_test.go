package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// ---------------------------------------------------------------------------
// Local mock LLM — captures calls for assertion, returns sequential responses
// ---------------------------------------------------------------------------

type mockCoordLLM struct {
	responses []*llm.Response
	errs      []error
	calls     []llm.Request
	idx       int
}

func (m *mockCoordLLM) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	m.calls = append(m.calls, req)
	i := m.idx
	m.idx++

	if i < len(m.errs) && m.errs[i] != nil {
		return nil, m.errs[i]
	}
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return nil, fmt.Errorf("mockCoordLLM: no response configured for call %d", i)
}

func newTestCoord(mock *mockCoordLLM) *coordinator {
	co := &coordinator{
		config: CoordinatorConfig{
			DefaultCapability:     "planning",
			MaxConcurrentPlanners: 3,
			TimeoutSeconds:        1800,
		},
		logger:    slog.Default(),
		llmClient: mock,
	}
	return co
}

// ---------------------------------------------------------------------------
// CoordinatorConfig validation and helpers
// ---------------------------------------------------------------------------

func TestCoordinatorConfig_IsAutoApprove_Nil(t *testing.T) {
	cfg := CoordinatorConfig{}
	if !cfg.IsAutoApprove() {
		t.Error("nil AutoApprove should default to true")
	}
}

func TestCoordinatorConfig_IsAutoApprove_False(t *testing.T) {
	f := false
	cfg := CoordinatorConfig{AutoApprove: &f}
	if cfg.IsAutoApprove() {
		t.Error("explicit false should return false")
	}
}

func TestCoordinatorConfig_GetTimeout_Default(t *testing.T) {
	cfg := CoordinatorConfig{}
	if got := cfg.GetTimeout(); got != 30*time.Minute {
		t.Errorf("GetTimeout() default = %v, want 30m", got)
	}
}

func TestCoordinatorConfig_GetTimeout_Set(t *testing.T) {
	cfg := CoordinatorConfig{TimeoutSeconds: 60}
	if got := cfg.GetTimeout(); got != 60*time.Second {
		t.Errorf("GetTimeout() = %v, want 60s", got)
	}
}

func TestCoordinatorConfig_GetSemsourceReadinessBudget_Default(t *testing.T) {
	cfg := CoordinatorConfig{}
	if got := cfg.GetSemsourceReadinessBudget(); got != 2*time.Second {
		t.Errorf("GetSemsourceReadinessBudget() default = %v, want 2s", got)
	}
}

func TestCoordinatorConfig_GetSemsourceReadinessBudget_Set(t *testing.T) {
	cfg := CoordinatorConfig{SemsourceReadinessBudget: "5s"}
	if got := cfg.GetSemsourceReadinessBudget(); got != 5*time.Second {
		t.Errorf("GetSemsourceReadinessBudget() = %v, want 5s", got)
	}
}

// ---------------------------------------------------------------------------
// parseFocusAreas — extracts focus areas from LLM JSON response
// ---------------------------------------------------------------------------

func TestCoord_ParseFocusAreas_Valid(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})

	content := `{
  "focus_areas": [
    {"area": "api", "description": "REST API layer", "hints": ["api/", "handlers/"]},
    {"area": "data", "description": "Database models", "hints": ["models/", "store/"]}
  ]
}`
	focuses, err := co.parseFocusAreas(content)
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

func TestCoord_ParseFocusAreas_InCodeBlock(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})

	content := "Here are the focus areas:\n```json\n" + `{
  "focus_areas": [
    {"area": "auth", "description": "Authentication", "hints": ["auth/"]}
  ]
}` + "\n```\nThose are my recommendations."

	focuses, err := co.parseFocusAreas(content)
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

func TestCoord_ParseFocusAreas_NoJSON(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})
	_, err := co.parseFocusAreas("just text, no JSON here")
	if err == nil {
		t.Fatal("expected error for response with no JSON")
	}
}

func TestCoord_ParseFocusAreas_EmptyFocusAreas(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})
	_, err := co.parseFocusAreas(`{"focus_areas": []}`)
	if err == nil {
		t.Fatal("expected error for empty focus_areas array")
	}
}

func TestCoord_ParseFocusAreas_HintsOptional(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})
	content := `{"focus_areas": [{"area": "general", "description": "General analysis"}]}`
	focuses, err := co.parseFocusAreas(content)
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
// parsePlannerResult
// ---------------------------------------------------------------------------

func TestCoord_ParsePlannerResult_Valid(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})

	content := `{
  "goal": "Implement authentication module",
  "context": "The API needs JWT-based auth",
  "scope": {
    "include": ["api/auth/", "middleware/"],
    "exclude": ["api/public/"]
  }
}`
	result, llmIDs := co.parsePlannerResult(content, "planner-123")
	if result == nil {
		t.Fatal("parsePlannerResult() returned nil")
	}
	if result.PlannerID != "planner-123" {
		t.Errorf("PlannerID = %q, want planner-123", result.PlannerID)
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
	if llmIDs != nil {
		t.Errorf("llmIDs = %v, want nil", llmIDs)
	}
}

func TestCoord_ParsePlannerResult_InvalidJSON_FallsBackToRaw(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})
	result, _ := co.parsePlannerResult("I couldn't create a plan for this.", "p-1")
	if result == nil {
		t.Fatal("parsePlannerResult() should return fallback result")
	}
	if result.Goal != "I couldn't create a plan for this." {
		t.Errorf("fallback Goal = %q, want raw result", result.Goal)
	}
}

func TestCoord_ParsePlannerResult_DoNotTouchScope(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})
	content := `{
  "goal": "Refactor safely",
  "context": "Careful approach",
  "scope": {
    "include": ["src/"],
    "do_not_touch": ["legacy/critical.go"]
  }
}`
	result, _ := co.parsePlannerResult(content, "p-1")
	if result == nil {
		t.Fatal("parsePlannerResult() returned nil")
	}
	if len(result.Scope.DoNotTouch) != 1 {
		t.Errorf("DoNotTouch len = %d, want 1", len(result.Scope.DoNotTouch))
	}
	if result.Scope.DoNotTouch[0] != "legacy/critical.go" {
		t.Errorf("DoNotTouch[0] = %q, want legacy/critical.go", result.Scope.DoNotTouch[0])
	}
}

// ---------------------------------------------------------------------------
// parseSynthesizedPlan
// ---------------------------------------------------------------------------

func TestCoord_ParseSynthesizedPlan_Valid(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})
	content := `{
  "goal": "Build a complete auth system with JWT tokens and refresh logic",
  "context": "The system needs both access and refresh tokens",
  "scope": {
    "include": ["api/auth/", "api/middleware/", "models/token.go"],
    "exclude": ["api/public/"]
  }
}`
	plan, err := co.parseSynthesizedPlan(content)
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

func TestCoord_ParseSynthesizedPlan_NoJSON(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})
	_, err := co.parseSynthesizedPlan("The synthesized plan is not available in JSON format.")
	if err == nil {
		t.Fatal("expected error for no JSON")
	}
}

func TestCoord_ParseSynthesizedPlan_EmptyGoalAllowed(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})
	plan, err := co.parseSynthesizedPlan(`{"goal": "", "context": "ctx", "scope": {}}`)
	if err != nil {
		t.Fatalf("parseSynthesizedPlan() error = %v", err)
	}
	if plan.Goal != "" {
		t.Errorf("Goal = %q, want empty", plan.Goal)
	}
}

// ---------------------------------------------------------------------------
// simpleMerge
// ---------------------------------------------------------------------------

func TestCoord_SimpleMerge_SingleResult(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})
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

	plan := co.simpleMerge(results)
	if !strings.Contains(plan.Goal, "Build the API layer") {
		t.Errorf("Goal = %q should contain original goal", plan.Goal)
	}
	if !strings.Contains(plan.Goal, "[api]") {
		t.Errorf("Goal = %q should be tagged with focus area [api]", plan.Goal)
	}
}

func TestCoord_SimpleMerge_MultipleResults_MergesScope(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})
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

	plan := co.simpleMerge(results)

	if !strings.Contains(plan.Goal, "[api]") {
		t.Error("merged Goal should contain [api] tag")
	}
	if !strings.Contains(plan.Goal, "[data]") {
		t.Error("merged Goal should contain [data] tag")
	}
	if !coordContainsAll(plan.Scope.Include, "api/", "handlers/", "models/", "store/") {
		t.Errorf("Scope.Include = %v, should contain all merged paths", plan.Scope.Include)
	}

	vendorCount := 0
	for _, s := range plan.Scope.Exclude {
		if s == "vendor/" {
			vendorCount++
		}
	}
	if vendorCount != 1 {
		t.Errorf("vendor/ appears %d times in Exclude, want exactly 1 (deduplication)", vendorCount)
	}
	if !coordContainsAll(plan.Scope.DoNotTouch, "legacy/") {
		t.Errorf("Scope.DoNotTouch = %v, should contain legacy/", plan.Scope.DoNotTouch)
	}
}

func TestCoord_SimpleMerge_EmptyContext_Omitted(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})
	results := []workflow.PlannerResult{
		{FocusArea: "api", Goal: "Build API", Context: ""},
		{FocusArea: "data", Goal: "Model data", Context: "Database design"},
	}

	plan := co.simpleMerge(results)

	if strings.Contains(plan.Context, "[api]") {
		t.Errorf("Context = %q should not include empty [api] section", plan.Context)
	}
	if !strings.Contains(plan.Context, "[data]") {
		t.Errorf("Context = %q should include [data] section", plan.Context)
	}
}

// ---------------------------------------------------------------------------
// synthesizeResults
// ---------------------------------------------------------------------------

func TestCoord_SynthesizeResults_SingleResult_NoLLMCall(t *testing.T) {
	mock := &mockCoordLLM{}
	co := newTestCoord(mock)

	results := []workflow.PlannerResult{
		{
			PlannerID: "p1",
			FocusArea: "auth",
			Goal:      "Add JWT auth",
			Context:   "Stateless auth needed",
			Scope:     workflow.Scope{Include: []string{"auth/"}},
		},
	}

	plan, requestID, err := co.synthesizeResults(context.Background(), results)
	if err != nil {
		t.Fatalf("synthesizeResults() error = %v", err)
	}
	if requestID != "" {
		t.Errorf("requestID = %q, want empty (no LLM call for single result)", requestID)
	}
	if plan.Goal != "Add JWT auth" {
		t.Errorf("Goal = %q, want Add JWT auth", plan.Goal)
	}
	if mock.idx != 0 {
		t.Errorf("LLM should not be called for single result, got %d calls", mock.idx)
	}
}

func TestCoord_SynthesizeResults_MultipleResults_CallsLLM(t *testing.T) {
	synthesisResponse := `{
  "goal": "Build a comprehensive system",
  "context": "Merged context from all planners",
  "scope": {
    "include": ["api/", "models/"]
  }
}`
	mock := &mockCoordLLM{
		responses: []*llm.Response{
			{Content: synthesisResponse, Model: "test-model"},
		},
	}
	co := newTestCoord(mock)

	results := []workflow.PlannerResult{
		{PlannerID: "p1", FocusArea: "api", Goal: "API layer", Scope: workflow.Scope{Include: []string{"api/"}}},
		{PlannerID: "p2", FocusArea: "data", Goal: "Data layer", Scope: workflow.Scope{Include: []string{"models/"}}},
	}

	plan, _, err := co.synthesizeResults(context.Background(), results)
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

func TestCoord_SynthesizeResults_LLMFailure_FallsBackToSimpleMerge(t *testing.T) {
	mock := &mockCoordLLM{
		errs: []error{fmt.Errorf("LLM unavailable")},
	}
	co := newTestCoord(mock)

	results := []workflow.PlannerResult{
		{PlannerID: "p1", FocusArea: "api", Goal: "API goal"},
		{PlannerID: "p2", FocusArea: "data", Goal: "Data goal"},
	}

	plan, requestID, err := co.synthesizeResults(context.Background(), results)
	if err != nil {
		t.Fatalf("synthesizeResults() should not error on LLM failure (uses simpleMerge), got: %v", err)
	}
	if requestID != "" {
		t.Errorf("requestID = %q, want empty on LLM failure", requestID)
	}
	if !strings.Contains(plan.Goal, "[api]") || !strings.Contains(plan.Goal, "[data]") {
		t.Errorf("fallback Goal = %q should contain tagged goals", plan.Goal)
	}
}

func TestCoord_SynthesizeResults_EmptyGoalFromLLM_FallsBackToSimpleMerge(t *testing.T) {
	mock := &mockCoordLLM{
		responses: []*llm.Response{
			{Content: `{"goal": "", "context": "some context", "scope": {}}`, Model: "test-model"},
		},
	}
	co := newTestCoord(mock)

	results := []workflow.PlannerResult{
		{PlannerID: "p1", FocusArea: "api", Goal: "API goal"},
		{PlannerID: "p2", FocusArea: "data", Goal: "Data goal"},
	}

	plan, _, err := co.synthesizeResults(context.Background(), results)
	if err != nil {
		t.Fatalf("synthesizeResults() error = %v", err)
	}
	if plan.Goal == "" {
		t.Error("Goal should not be empty after fallback to simpleMerge")
	}
}

// ---------------------------------------------------------------------------
// determineFocusAreas
// ---------------------------------------------------------------------------

func TestCoord_DetermineFocusAreas_ExplicitFocuses_BypassLLM(t *testing.T) {
	mock := &mockCoordLLM{}
	co := newTestCoord(mock)

	trigger := &payloads.PlanCoordinatorRequest{
		Slug:       "test-plan",
		Title:      "Test Plan",
		FocusAreas: []string{"api", "data", "auth"},
	}

	focuses, err := co.determineFocusAreas(context.Background(), trigger)
	if err != nil {
		t.Fatalf("determineFocusAreas() error = %v", err)
	}
	if len(focuses) != 3 {
		t.Fatalf("len(focuses) = %d, want 3", len(focuses))
	}
	if mock.idx != 0 {
		t.Errorf("LLM should not be called when explicit focuses provided, got %d calls", mock.idx)
	}

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

func TestCoord_DetermineFocusAreas_ExplicitFocuses_SetsDescription(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})

	trigger := &payloads.PlanCoordinatorRequest{
		Slug:       "test-plan",
		FocusAreas: []string{"auth"},
	}

	focuses, err := co.determineFocusAreas(context.Background(), trigger)
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
// coordinationExecution state helpers
// ---------------------------------------------------------------------------

func TestCoord_CoordinationExecution_AllPlannersComplete(t *testing.T) {
	exec := &coordinationExecution{
		ExpectedPlanners: 2,
		CompletedResults: map[string]*workflow.PlannerResult{
			"t1": {Goal: "g1"},
		},
	}
	if exec.allPlannersComplete() {
		t.Error("1 of 2 should not be complete")
	}
	exec.CompletedResults["t2"] = &workflow.PlannerResult{Goal: "g2"}
	if !exec.allPlannersComplete() {
		t.Error("2 of 2 should be complete")
	}
}

func TestCoord_CoordinationExecution_CollectResults(t *testing.T) {
	exec := &coordinationExecution{
		CompletedResults: map[string]*workflow.PlannerResult{
			"t1": {Goal: "g1"},
			"t2": {Goal: "g2"},
		},
	}
	results := exec.collectResults()
	if len(results) != 2 {
		t.Fatalf("collectResults() len = %d, want 2", len(results))
	}
}

func TestCoord_FocusAreasJSON(t *testing.T) {
	areas := []*FocusArea{{Area: "api"}, {Area: "data"}}
	got := focusAreasJSON(areas)
	var names []string
	if err := json.Unmarshal([]byte(got), &names); err != nil {
		t.Fatalf("focusAreasJSON produced invalid JSON: %v", err)
	}
	if len(names) != 2 || names[0] != "api" || names[1] != "data" {
		t.Errorf("focusAreasJSON() = %v, want [api data]", names)
	}
}

// ---------------------------------------------------------------------------
// CoordinatorResult payload
// ---------------------------------------------------------------------------

func TestCoord_CoordinatorResult_Schema(t *testing.T) {
	r := &CoordinatorResult{}
	schema := r.Schema()
	if schema.Domain == "" || schema.Category == "" || schema.Version == "" {
		t.Error("Schema() fields should not be empty")
	}
}

func TestCoord_CoordinatorResult_JSONRoundTrip(t *testing.T) {
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
	if decoded.PlannerCount != original.PlannerCount {
		t.Errorf("PlannerCount = %d, want %d", decoded.PlannerCount, original.PlannerCount)
	}
}

// ---------------------------------------------------------------------------
// callLLM
// ---------------------------------------------------------------------------

func TestCoord_CallLLM_UsesDefaultCapability(t *testing.T) {
	mock := &mockCoordLLM{
		responses: []*llm.Response{
			{Content: "response content", Model: "test-model", RequestID: "req-abc"},
		},
	}
	co := newTestCoord(mock)
	co.config.DefaultCapability = "planning"

	content, requestID, err := co.callLLM(context.Background(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("callLLM() error = %v", err)
	}
	if content != "response content" {
		t.Errorf("content = %q, want response content", content)
	}
	if requestID != "req-abc" {
		t.Errorf("requestID = %q, want req-abc", requestID)
	}
	if mock.calls[0].Capability != "planning" {
		t.Errorf("LLM called with capability %q, want planning", mock.calls[0].Capability)
	}
}

func TestCoord_CallLLM_ErrorPropagates(t *testing.T) {
	mock := &mockCoordLLM{
		errs: []error{fmt.Errorf("connection timeout")},
	}
	co := newTestCoord(mock)

	_, _, err := co.callLLM(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error to propagate from LLM")
	}
	if !strings.Contains(err.Error(), "LLM completion") {
		t.Errorf("error = %q, should contain 'LLM completion'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// coordUnique helper
// ---------------------------------------------------------------------------

func TestCoordUnique_DeduplicatesPreservingOrder(t *testing.T) {
	input := []string{"api/", "models/", "api/", "store/", "models/"}
	got := coordUnique(input)
	if len(got) != 3 {
		t.Fatalf("coordUnique() len = %d, want 3", len(got))
	}
	if got[0] != "api/" || got[1] != "models/" || got[2] != "store/" {
		t.Errorf("coordUnique() = %v, want [api/ models/ store/]", got)
	}
}

func TestCoordUnique_EmptyInput(t *testing.T) {
	got := coordUnique(nil)
	if len(got) != 0 {
		t.Errorf("coordUnique(nil) = %v, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// parseCoordTaskID
// ---------------------------------------------------------------------------

func TestParseCoordTaskID_Valid(t *testing.T) {
	expectedEntityID := workflow.EntityPrefix() + ".exec.plan.run.my-slug"
	role, entityID := parseCoordTaskID("planner.0::" + expectedEntityID)
	if role != "planner.0" {
		t.Errorf("role = %q, want planner.0", role)
	}
	if entityID != expectedEntityID {
		t.Errorf("entityID = %q unexpected", entityID)
	}
}

func TestParseCoordTaskID_NoSeparator(t *testing.T) {
	role, entityID := parseCoordTaskID("no-separator-here")
	if role != "" || entityID != "" {
		t.Errorf("parseCoordTaskID without separator should return empty strings, got %q, %q", role, entityID)
	}
}

// ---------------------------------------------------------------------------
// parseReviewerVerdict
// ---------------------------------------------------------------------------

func TestCoord_ParseReviewerVerdict_Approved(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})
	verdict, summary := co.parseReviewerVerdict(`{"verdict":"approved","summary":"Looks good"}`)
	if verdict != "approved" {
		t.Errorf("verdict = %q, want approved", verdict)
	}
	if summary != "Looks good" {
		t.Errorf("summary = %q, want 'Looks good'", summary)
	}
}

func TestCoord_ParseReviewerVerdict_InvalidJSON(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})
	verdict, _ := co.parseReviewerVerdict("not json")
	if verdict != "escalated" {
		t.Errorf("verdict = %q, want escalated for invalid JSON", verdict)
	}
}

func TestCoord_ParseReviewerVerdict_EmptyVerdict(t *testing.T) {
	co := newTestCoord(&mockCoordLLM{})
	verdict, _ := co.parseReviewerVerdict(`{"verdict":"","summary":""}`)
	if verdict != "escalated" {
		t.Errorf("verdict = %q, want escalated for empty verdict", verdict)
	}
}

// ---------------------------------------------------------------------------
// Stop when not running
// ---------------------------------------------------------------------------

func TestCoordinator_Stop_WhenNotRunning(_ *testing.T) {
	co := newCoordinator(CoordinatorConfig{}, nil, slog.Default())
	// Should not panic or block
	co.Stop()
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func coordContainsAll(slice []string, expected ...string) bool {
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
