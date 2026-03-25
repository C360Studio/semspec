package review_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/c360studio/semstreams/agentic"

	"github.com/c360studio/semspec/agentgraph"
	"github.com/c360studio/semspec/tools/review"
	"github.com/c360studio/semspec/workflow"
)

// -- mock graph --

// mockGraph implements review.GraphHelper for testing. It stores the most
// recent call to each method and can be pre-configured to return errors.
type mockGraph struct {
	agent          *workflow.Agent
	getAgentErr    error
	recordErr      error
	incrementErr   error
	updateStatsErr error

	recordedReview *agentgraph.Review
	incrementedIDs []string
	updatedStats   *workflow.ReviewStats
}

func (m *mockGraph) GetAgent(_ context.Context, _ string) (*workflow.Agent, error) {
	if m.getAgentErr != nil {
		return nil, m.getAgentErr
	}
	if m.agent != nil {
		return m.agent, nil
	}
	return &workflow.Agent{ID: "agent-1", ReviewStats: workflow.ReviewStats{}}, nil
}

func (m *mockGraph) RecordReview(_ context.Context, r agentgraph.Review) error {
	if m.recordErr != nil {
		return m.recordErr
	}
	m.recordedReview = &r
	return nil
}

func (m *mockGraph) IncrementAgentErrorCounts(_ context.Context, _ string, categoryIDs []string) error {
	if m.incrementErr != nil {
		return m.incrementErr
	}
	m.incrementedIDs = categoryIDs
	return nil
}

func (m *mockGraph) UpdateAgentStats(_ context.Context, _ string, stats workflow.ReviewStats) error {
	if m.updateStatsErr != nil {
		return m.updateStatsErr
	}
	m.updatedStats = &stats
	return nil
}

// -- helpers --

// testRegistry returns an ErrorCategoryRegistry with two categories suitable
// for testing: "missing_tests" and "wrong_pattern".
func testRegistry(t *testing.T) *workflow.ErrorCategoryRegistry {
	t.Helper()
	data := []byte(`{
		"categories": [
			{
				"id":          "missing_tests",
				"label":       "Missing Tests",
				"description": "Implementation lacks adequate test coverage",
				"signals":     ["no test file", "untested branch"],
				"guidance":    "Add unit and integration tests for all non-trivial code paths"
			},
			{
				"id":          "wrong_pattern",
				"label":       "Wrong Pattern",
				"description": "Implementation deviates from established project patterns",
				"signals":     ["direct db access", "missing context propagation"],
				"guidance":    "Follow the patterns described in the project SOPs"
			}
		]
	}`)
	reg, err := workflow.LoadErrorCategoriesFromBytes(data)
	if err != nil {
		t.Fatalf("testRegistry: %v", err)
	}
	return reg
}

// makeReviewCall builds an agentic.ToolCall for review_scenario.
func makeReviewCall(id string, args map[string]any) agentic.ToolCall {
	return agentic.ToolCall{
		ID:        id,
		Name:      "review_scenario",
		Arguments: args,
		LoopID:    "loop-1",
		TraceID:   "trace-1",
	}
}

// acceptedArgs returns a minimal set of valid accepted-review arguments.
func acceptedArgs() map[string]any {
	return map[string]any{
		"scenario_id":     "scenario-abc",
		"agent_id":        "agent-1",
		"verdict":         "accepted",
		"q1_correctness":  float64(4),
		"q2_quality":      float64(3),
		"q3_completeness": float64(4),
	}
}

// rejectedArgs returns a minimal set of valid rejected-review arguments with one error.
func rejectedArgs() map[string]any {
	return map[string]any{
		"scenario_id":     "scenario-abc",
		"agent_id":        "agent-1",
		"verdict":         "rejected",
		"q1_correctness":  float64(2),
		"q2_quality":      float64(2),
		"q3_completeness": float64(2),
		"explanation":     "Tests are missing and the pattern is wrong.",
		"errors": []any{
			map[string]any{"category_id": "missing_tests"},
			map[string]any{"category_id": "wrong_pattern"},
		},
	}
}

// mustUnmarshalResponse unmarshals the ToolResult.Content into a map.
func mustUnmarshalResponse(t *testing.T, content string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		t.Fatalf("unmarshal response: %v (content=%q)", err, content)
	}
	return m
}

// -- tests --

func TestExecute_AcceptedReview(t *testing.T) {
	t.Parallel()

	g := &mockGraph{}
	exec := review.NewExecutor(g, testRegistry(t))
	call := makeReviewCall("call-1", acceptedArgs())

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute() result.Error = %q, want empty", result.Error)
	}

	// RecordReview must be called.
	if g.recordedReview == nil {
		t.Fatal("RecordReview was not called")
	}
	if g.recordedReview.Verdict != agentgraph.VerdictAccepted {
		t.Errorf("recorded verdict = %q, want %q", g.recordedReview.Verdict, agentgraph.VerdictAccepted)
	}

	// UpdateAgentStats must be called.
	if g.updatedStats == nil {
		t.Fatal("UpdateAgentStats was not called")
	}

	// IncrementAgentErrorCounts must NOT be called for accepted reviews.
	if g.incrementedIDs != nil {
		t.Errorf("IncrementAgentErrorCounts called with %v, want not called", g.incrementedIDs)
	}

	// Verify response payload.
	resp := mustUnmarshalResponse(t, result.Content)
	if resp["verdict"] != "accepted" {
		t.Errorf("response verdict = %v, want accepted", resp["verdict"])
	}
	if resp["scenario_id"] != "scenario-abc" {
		t.Errorf("response scenario_id = %v, want scenario-abc", resp["scenario_id"])
	}
	if resp["agent_id"] != "agent-1" {
		t.Errorf("response agent_id = %v, want agent-1", resp["agent_id"])
	}
	if resp["errors"] != nil {
		t.Errorf("response errors = %v, want nil for accepted review", resp["errors"])
	}
}

func TestExecute_RejectedWithErrors(t *testing.T) {
	t.Parallel()

	g := &mockGraph{}
	exec := review.NewExecutor(g, testRegistry(t))
	call := makeReviewCall("call-2", rejectedArgs())

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute() result.Error = %q, want empty", result.Error)
	}

	// All three graph methods must be called for a rejected review.
	if g.recordedReview == nil {
		t.Fatal("RecordReview was not called")
	}
	if g.updatedStats == nil {
		t.Fatal("UpdateAgentStats was not called")
	}
	if len(g.incrementedIDs) == 0 {
		t.Fatal("IncrementAgentErrorCounts was not called")
	}

	// Verify the correct category IDs were incremented.
	wantIDs := map[string]bool{"missing_tests": true, "wrong_pattern": true}
	for _, id := range g.incrementedIDs {
		if !wantIDs[id] {
			t.Errorf("unexpected category ID incremented: %q", id)
		}
		delete(wantIDs, id)
	}
	for id := range wantIDs {
		t.Errorf("expected category ID %q not incremented", id)
	}

	resp := mustUnmarshalResponse(t, result.Content)
	if resp["verdict"] != "rejected" {
		t.Errorf("response verdict = %v, want rejected", resp["verdict"])
	}
}

func TestExecute_RejectedWithRelatedEntities(t *testing.T) {
	t.Parallel()

	g := &mockGraph{}
	exec := review.NewExecutor(g, testRegistry(t))

	args := map[string]any{
		"scenario_id":     "scenario-xyz",
		"agent_id":        "agent-1",
		"verdict":         "rejected",
		"q1_correctness":  float64(3),
		"q2_quality":      float64(3),
		"q3_completeness": float64(3),
		"explanation":     "Some issues found.",
		"errors": []any{
			map[string]any{
				"category_id":        "missing_tests",
				"related_entity_ids": []any{"sop-testing-101", "file-auth_test.go"},
			},
		},
	}

	call := makeReviewCall("call-3", args)
	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute() result.Error = %q, want empty", result.Error)
	}
	if g.recordedReview == nil {
		t.Fatal("RecordReview was not called")
	}
	if len(g.recordedReview.Errors) != 1 {
		t.Fatalf("recorded Errors len = %d, want 1", len(g.recordedReview.Errors))
	}

	ref := g.recordedReview.Errors[0]
	if ref.CategoryID != "missing_tests" {
		t.Errorf("CategoryID = %q, want missing_tests", ref.CategoryID)
	}
	if len(ref.RelatedEntityIDs) != 2 {
		t.Fatalf("RelatedEntityIDs len = %d, want 2", len(ref.RelatedEntityIDs))
	}
	if ref.RelatedEntityIDs[0] != "sop-testing-101" {
		t.Errorf("RelatedEntityIDs[0] = %q, want sop-testing-101", ref.RelatedEntityIDs[0])
	}
	if ref.RelatedEntityIDs[1] != "file-auth_test.go" {
		t.Errorf("RelatedEntityIDs[1] = %q, want file-auth_test.go", ref.RelatedEntityIDs[1])
	}
}

func TestExecute_RejectedWithoutErrors(t *testing.T) {
	t.Parallel()

	g := &mockGraph{}
	exec := review.NewExecutor(g, testRegistry(t))

	args := map[string]any{
		"scenario_id":     "scenario-abc",
		"agent_id":        "agent-1",
		"verdict":         "rejected",
		"q1_correctness":  float64(3),
		"q2_quality":      float64(3),
		"q3_completeness": float64(3),
		// no "errors" key — Validate() must reject this
	}

	call := makeReviewCall("call-4", args)
	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want validation error for rejected without errors")
	}
	// RecordReview must NOT be called.
	if g.recordedReview != nil {
		t.Error("RecordReview was called, want no call on validation failure")
	}
}

func TestExecute_InvalidCategoryID(t *testing.T) {
	t.Parallel()

	g := &mockGraph{}
	exec := review.NewExecutor(g, testRegistry(t))

	args := map[string]any{
		"scenario_id":     "scenario-abc",
		"agent_id":        "agent-1",
		"verdict":         "rejected",
		"q1_correctness":  float64(2),
		"q2_quality":      float64(2),
		"q3_completeness": float64(2),
		"explanation":     "Some issues.",
		"errors": []any{
			map[string]any{"category_id": "nonexistent_category"},
		},
	}

	call := makeReviewCall("call-5", args)
	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want validation error for unknown category ID")
	}
	if g.recordedReview != nil {
		t.Error("RecordReview was called, want no call on validation failure")
	}
}

func TestExecute_AllFivesAcceptedWithoutExplanation(t *testing.T) {
	t.Parallel()

	g := &mockGraph{}
	exec := review.NewExecutor(g, testRegistry(t))

	args := map[string]any{
		"scenario_id":     "scenario-abc",
		"agent_id":        "agent-1",
		"verdict":         "accepted",
		"q1_correctness":  float64(5),
		"q2_quality":      float64(5),
		"q3_completeness": float64(5),
		// no explanation — anti-inflation guard must fire
	}

	call := makeReviewCall("call-6", args)
	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want anti-inflation validation error")
	}
	if g.recordedReview != nil {
		t.Error("RecordReview was called, want no call on validation failure")
	}
}

func TestExecute_UnknownAgentID(t *testing.T) {
	t.Parallel()

	g := &mockGraph{
		getAgentErr: fmt.Errorf("agent not found"),
	}
	exec := review.NewExecutor(g, testRegistry(t))
	call := makeReviewCall("call-7", acceptedArgs())

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error from GetAgent")
	}
	if g.recordedReview != nil {
		t.Error("RecordReview was called, want no call when GetAgent fails")
	}
}

func TestExecute_GraphRecordError(t *testing.T) {
	t.Parallel()

	g := &mockGraph{
		recordErr: fmt.Errorf("graph write timeout"),
	}
	exec := review.NewExecutor(g, testRegistry(t))
	call := makeReviewCall("call-8", acceptedArgs())

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error from RecordReview")
	}
}

func TestExecute_GraphIncrementError(t *testing.T) {
	t.Parallel()

	// IncrementAgentErrorCounts fails but the review should still succeed.
	g := &mockGraph{
		incrementErr: fmt.Errorf("transient error"),
	}
	exec := review.NewExecutor(g, testRegistry(t))
	call := makeReviewCall("call-9", rejectedArgs())

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Errorf("Execute() result.Error = %q, want empty (increment is best-effort)", result.Error)
	}
	// RecordReview must still have been called.
	if g.recordedReview == nil {
		t.Error("RecordReview was not called")
	}
}

func TestExecute_StatsUpdated(t *testing.T) {
	t.Parallel()

	// Start with an agent that already has two reviews incorporated.
	existingStats := workflow.ReviewStats{}
	existingStats.UpdateStats(3, 3, 3) // first review
	existingStats.UpdateStats(4, 4, 4) // second review

	g := &mockGraph{
		agent: &workflow.Agent{
			ID:          "agent-1",
			ReviewStats: existingStats,
		},
	}
	exec := review.NewExecutor(g, testRegistry(t))

	// Third review: q1=5, q2=5, q3=5 with explanation (anti-inflation compliant).
	args := map[string]any{
		"scenario_id":     "scenario-abc",
		"agent_id":        "agent-1",
		"verdict":         "accepted",
		"q1_correctness":  float64(5),
		"q2_quality":      float64(5),
		"q3_completeness": float64(5),
		"explanation":     "Outstanding work: all edge cases covered and documentation is thorough.",
	}

	call := makeReviewCall("call-10", args)
	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute() result.Error = %q, want empty", result.Error)
	}
	if g.updatedStats == nil {
		t.Fatal("UpdateAgentStats was not called")
	}

	// Running average of [3,4,5] = 4.0 for each dimension.
	const wantAvg = 4.0
	const epsilon = 1e-9

	if diff := g.updatedStats.Q1CorrectnessAvg - wantAvg; diff > epsilon || diff < -epsilon {
		t.Errorf("Q1CorrectnessAvg = %.6f, want %.6f", g.updatedStats.Q1CorrectnessAvg, wantAvg)
	}
	if diff := g.updatedStats.Q2QualityAvg - wantAvg; diff > epsilon || diff < -epsilon {
		t.Errorf("Q2QualityAvg = %.6f, want %.6f", g.updatedStats.Q2QualityAvg, wantAvg)
	}
	if diff := g.updatedStats.Q3CompletenessAvg - wantAvg; diff > epsilon || diff < -epsilon {
		t.Errorf("Q3CompletenessAvg = %.6f, want %.6f", g.updatedStats.Q3CompletenessAvg, wantAvg)
	}
	if g.updatedStats.ReviewCount != 3 {
		t.Errorf("ReviewCount = %d, want 3", g.updatedStats.ReviewCount)
	}
}

func TestListTools(t *testing.T) {
	t.Parallel()

	exec := review.NewExecutor(&mockGraph{}, testRegistry(t))
	tools := exec.ListTools()

	if len(tools) != 1 {
		t.Fatalf("ListTools() returned %d definitions, want 1", len(tools))
	}

	def := tools[0]
	if def.Name != "review_scenario" {
		t.Errorf("tool Name = %q, want %q", def.Name, "review_scenario")
	}
	if def.Description == "" {
		t.Error("tool Description is empty")
	}
	if def.Parameters == nil {
		t.Fatal("tool Parameters is nil")
	}

	required, ok := def.Parameters["required"].([]string)
	if !ok {
		t.Fatalf("Parameters[required] type = %T, want []string", def.Parameters["required"])
	}
	wantRequired := map[string]bool{
		"scenario_id":     true,
		"agent_id":        true,
		"verdict":         true,
		"q1_correctness":  true,
		"q2_quality":      true,
		"q3_completeness": true,
	}
	if len(required) != len(wantRequired) {
		t.Errorf("required len = %d, want %d (fields: %v)", len(required), len(wantRequired), required)
	}
	for _, r := range required {
		if !wantRequired[r] {
			t.Errorf("unexpected required field %q", r)
		}
	}

	props, ok := def.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("Parameters.properties is not a map")
	}

	// Verify verdict has enum constraint.
	verdictProp, ok := props["verdict"].(map[string]any)
	if !ok {
		t.Fatal("verdict property is not a map")
	}
	enumVals, ok := verdictProp["enum"].([]string)
	if !ok {
		t.Fatalf("verdict enum type = %T, want []string", verdictProp["enum"])
	}
	wantEnum := map[string]bool{"accepted": true, "rejected": true}
	if len(enumVals) != len(wantEnum) {
		t.Errorf("verdict enum = %v, want [accepted rejected]", enumVals)
	}
	for _, v := range enumVals {
		if !wantEnum[v] {
			t.Errorf("unexpected verdict enum value %q", v)
		}
	}

	// Verify rating properties have min/max constraints.
	for _, ratingKey := range []string{"q1_correctness", "q2_quality", "q3_completeness"} {
		prop, ok := props[ratingKey].(map[string]any)
		if !ok {
			t.Errorf("%s property is not a map", ratingKey)
			continue
		}
		if prop["minimum"] != 1 {
			t.Errorf("%s minimum = %v, want 1", ratingKey, prop["minimum"])
		}
		if prop["maximum"] != 5 {
			t.Errorf("%s maximum = %v, want 5", ratingKey, prop["maximum"])
		}
	}

	// Verify errors property is an array with items schema.
	errorsProp, ok := props["errors"].(map[string]any)
	if !ok {
		t.Fatal("errors property is not a map")
	}
	if errorsProp["type"] != "array" {
		t.Errorf("errors type = %v, want array", errorsProp["type"])
	}
	errItems, ok := errorsProp["items"].(map[string]any)
	if !ok {
		t.Fatal("errors.items is not a map")
	}
	itemRequired, ok := errItems["required"].([]string)
	if !ok {
		t.Fatalf("errors.items.required type = %T, want []string", errItems["required"])
	}
	if len(itemRequired) != 1 || itemRequired[0] != "category_id" {
		t.Errorf("errors.items.required = %v, want [category_id]", itemRequired)
	}
}
