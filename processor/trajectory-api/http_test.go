package trajectoryapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semspec/llm"
)

func TestExtractIDFromPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		prefix   string
		expected string
	}{
		{
			name:     "basic loop ID extraction",
			path:     "/trajectory-api/loops/loop-123",
			prefix:   "/loops/",
			expected: "loop-123",
		},
		{
			name:     "trace ID extraction",
			path:     "/trajectory-api/traces/trace-456",
			prefix:   "/traces/",
			expected: "trace-456",
		},
		{
			name:     "ID with trailing slash",
			path:     "/trajectory-api/loops/loop-123/",
			prefix:   "/loops/",
			expected: "loop-123",
		},
		{
			name:     "ID with additional segments",
			path:     "/trajectory-api/loops/loop-123/extra/path",
			prefix:   "/loops/",
			expected: "loop-123",
		},
		{
			name:     "empty path",
			path:     "/trajectory-api/loops/",
			prefix:   "/loops/",
			expected: "",
		},
		{
			name:     "prefix not found",
			path:     "/other-api/traces/trace-123",
			prefix:   "/loops/",
			expected: "",
		},
		{
			name:     "UUID format ID",
			path:     "/trajectory-api/loops/550e8400-e29b-41d4-a716-446655440000",
			prefix:   "/loops/",
			expected: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:     "ID with spaces gets trimmed",
			path:     "/trajectory-api/loops/ loop-123 ",
			prefix:   "/loops/",
			expected: "loop-123",
		},
		{
			name:     "trace ID containing dots",
			path:     "/trajectory-api/traces/abc.def.ghi",
			prefix:   "/traces/",
			expected: "abc.def.ghi",
		},
		{
			name:     "loop ID with version-like format",
			path:     "/trajectory-api/loops/loop-v1.2.3",
			prefix:   "/loops/",
			expected: "loop-v1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractIDFromPath(tt.path, tt.prefix)
			if result != tt.expected {
				t.Errorf("extractIDFromPath(%q, %q) = %q, want %q",
					tt.path, tt.prefix, result, tt.expected)
			}
		})
	}
}

func TestBuildTrajectory(t *testing.T) {
	c := &Component{}
	now := time.Now()
	endTime := now.Add(5 * time.Second)

	loopState := &LoopState{
		ID:        "loop-123",
		TraceID:   "trace-456",
		Status:    "completed",
		Iteration: 3,
		StartedAt: &now,
		EndedAt:   &endTime,
	}

	calls := []*llm.CallRecord{
		{
			RequestID:    "req-1",
			TraceID:      "trace-456",
			LoopID:       "loop-123",
			Model:        "gpt-4",
			Provider:     "openai",
			Capability:   "planning",
			TokensIn:     100,
			TokensOut:    50,
			DurationMs:   1000,
			StartedAt:    now,
			CompletedAt:  now.Add(time.Second),
			FinishReason: "stop",
			Messages:     []llm.Message{{Role: "user", Content: "hello"}},
			Response:     "Hello! How can I help you?",
		},
		{
			RequestID:    "req-2",
			TraceID:      "trace-456",
			LoopID:       "loop-123",
			Model:        "gpt-4",
			Provider:     "openai",
			Capability:   "coding",
			TokensIn:     200,
			TokensOut:    100,
			DurationMs:   2000,
			StartedAt:    now.Add(2 * time.Second),
			CompletedAt:  now.Add(4 * time.Second),
			FinishReason: "stop",
			Messages:     []llm.Message{{Role: "user", Content: "write code"}},
			Response:     "Here is the code...",
		},
	}

	trajectory := c.buildTrajectory(loopState, calls, []*llm.ToolCallRecord{}, false)

	if trajectory.LoopID != "loop-123" {
		t.Errorf("LoopID = %q, want %q", trajectory.LoopID, "loop-123")
	}
	if trajectory.TraceID != "trace-456" {
		t.Errorf("TraceID = %q, want %q", trajectory.TraceID, "trace-456")
	}
	if trajectory.Status != "completed" {
		t.Errorf("Status = %q, want %q", trajectory.Status, "completed")
	}
	if trajectory.Steps != 3 {
		t.Errorf("Steps = %d, want %d", trajectory.Steps, 3)
	}
	if trajectory.ModelCalls != 2 {
		t.Errorf("ModelCalls = %d, want %d", trajectory.ModelCalls, 2)
	}
	if trajectory.TokensIn != 300 {
		t.Errorf("TokensIn = %d, want %d", trajectory.TokensIn, 300)
	}
	if trajectory.TokensOut != 150 {
		t.Errorf("TokensOut = %d, want %d", trajectory.TokensOut, 150)
	}
	// Duration should be calculated from loop state
	expectedDuration := endTime.Sub(now).Milliseconds()
	if trajectory.DurationMs != expectedDuration {
		t.Errorf("DurationMs = %d, want %d", trajectory.DurationMs, expectedDuration)
	}
	// Entries should not be included when includeEntries is false
	if len(trajectory.Entries) != 0 {
		t.Errorf("Entries count = %d, want 0 (includeEntries=false)", len(trajectory.Entries))
	}
}

func TestBuildTrajectory_WithEntries(t *testing.T) {
	c := &Component{}
	now := time.Now()

	loopState := &LoopState{
		ID:      "loop-123",
		TraceID: "trace-456",
		Status:  "running",
	}

	calls := []*llm.CallRecord{
		{
			RequestID:    "req-1",
			TraceID:      "trace-456",
			LoopID:       "loop-123",
			Model:        "claude-3",
			Provider:     "anthropic",
			Capability:   "planning",
			TokensIn:     100,
			TokensOut:    50,
			DurationMs:   1000,
			StartedAt:    now,
			FinishReason: "stop",
			Retries:      1,
			Messages:     []llm.Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}},
			Response:     "This is a response",
		},
	}

	trajectory := c.buildTrajectory(loopState, calls, []*llm.ToolCallRecord{}, true)

	if len(trajectory.Entries) != 1 {
		t.Fatalf("Entries count = %d, want 1", len(trajectory.Entries))
	}

	entry := trajectory.Entries[0]
	if entry.Type != "model_call" {
		t.Errorf("Entry.Type = %q, want %q", entry.Type, "model_call")
	}
	if entry.Model != "claude-3" {
		t.Errorf("Entry.Model = %q, want %q", entry.Model, "claude-3")
	}
	if entry.Provider != "anthropic" {
		t.Errorf("Entry.Provider = %q, want %q", entry.Provider, "anthropic")
	}
	if entry.Capability != "planning" {
		t.Errorf("Entry.Capability = %q, want %q", entry.Capability, "planning")
	}
	if entry.TokensIn != 100 {
		t.Errorf("Entry.TokensIn = %d, want %d", entry.TokensIn, 100)
	}
	if entry.TokensOut != 50 {
		t.Errorf("Entry.TokensOut = %d, want %d", entry.TokensOut, 50)
	}
	if entry.DurationMs != 1000 {
		t.Errorf("Entry.DurationMs = %d, want %d", entry.DurationMs, 1000)
	}
	if entry.FinishReason != "stop" {
		t.Errorf("Entry.FinishReason = %q, want %q", entry.FinishReason, "stop")
	}
	if entry.Retries != 1 {
		t.Errorf("Entry.Retries = %d, want %d", entry.Retries, 1)
	}
	if entry.MessagesCount != 2 {
		t.Errorf("Entry.MessagesCount = %d, want %d", entry.MessagesCount, 2)
	}
	if entry.ResponsePreview != "This is a response" {
		t.Errorf("Entry.ResponsePreview = %q, want %q", entry.ResponsePreview, "This is a response")
	}
}

func TestBuildTrajectory_EmptyCalls(t *testing.T) {
	c := &Component{}
	now := time.Now()

	loopState := &LoopState{
		ID:        "loop-123",
		TraceID:   "trace-456",
		Status:    "completed",
		Iteration: 0,
		StartedAt: &now,
	}

	calls := []*llm.CallRecord{}

	trajectory := c.buildTrajectory(loopState, calls, []*llm.ToolCallRecord{}, true)

	if trajectory.LoopID != "loop-123" {
		t.Errorf("LoopID = %q, want %q", trajectory.LoopID, "loop-123")
	}
	if trajectory.ModelCalls != 0 {
		t.Errorf("ModelCalls = %d, want %d", trajectory.ModelCalls, 0)
	}
	if trajectory.TokensIn != 0 {
		t.Errorf("TokensIn = %d, want %d", trajectory.TokensIn, 0)
	}
	if trajectory.TokensOut != 0 {
		t.Errorf("TokensOut = %d, want %d", trajectory.TokensOut, 0)
	}
	if len(trajectory.Entries) != 0 {
		t.Errorf("Entries count = %d, want 0", len(trajectory.Entries))
	}
}

func TestBuildTrajectory_ResponseTruncation(t *testing.T) {
	c := &Component{}
	now := time.Now()

	loopState := &LoopState{
		ID:      "loop-123",
		TraceID: "trace-456",
		Status:  "completed",
	}

	// Create a response longer than 200 characters
	longResponse := ""
	for i := 0; i < 250; i++ {
		longResponse += "x"
	}

	calls := []*llm.CallRecord{
		{
			RequestID: "req-1",
			TraceID:   "trace-456",
			LoopID:    "loop-123",
			Model:     "gpt-4",
			Provider:  "openai",
			StartedAt: now,
			Response:  longResponse,
		},
	}

	trajectory := c.buildTrajectory(loopState, calls, []*llm.ToolCallRecord{}, true)

	if len(trajectory.Entries) != 1 {
		t.Fatalf("Entries count = %d, want 1", len(trajectory.Entries))
	}

	entry := trajectory.Entries[0]
	// Should be truncated to 200 chars + "..."
	expectedLen := 203
	if len(entry.ResponsePreview) != expectedLen {
		t.Errorf("ResponsePreview length = %d, want %d", len(entry.ResponsePreview), expectedLen)
	}
	if entry.ResponsePreview[200:] != "..." {
		t.Errorf("ResponsePreview should end with '...', got %q", entry.ResponsePreview[200:])
	}
}

func TestBuildTrajectory_DurationFromCalls(t *testing.T) {
	c := &Component{}
	now := time.Now()

	// Loop state without endedAt - duration should be sum of call durations
	loopState := &LoopState{
		ID:        "loop-123",
		TraceID:   "trace-456",
		Status:    "running",
		StartedAt: &now,
		EndedAt:   nil,
	}

	calls := []*llm.CallRecord{
		{
			RequestID:  "req-1",
			TraceID:    "trace-456",
			LoopID:     "loop-123",
			StartedAt:  now,
			DurationMs: 500,
		},
		{
			RequestID:  "req-2",
			TraceID:    "trace-456",
			LoopID:     "loop-123",
			StartedAt:  now.Add(time.Second),
			DurationMs: 700,
		},
	}

	trajectory := c.buildTrajectory(loopState, calls, []*llm.ToolCallRecord{}, false)

	// Without loop endedAt, duration is sum of call durations
	expectedDuration := int64(1200)
	if trajectory.DurationMs != expectedDuration {
		t.Errorf("DurationMs = %d, want %d (sum of call durations)", trajectory.DurationMs, expectedDuration)
	}
}

func TestHandleGetLoopTrajectory_MethodNotAllowed(t *testing.T) {
	c := &Component{}

	req := httptest.NewRequest(http.MethodPost, "/trajectory-api/loops/loop-123", nil)
	w := httptest.NewRecorder()

	c.handleGetLoopTrajectory(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleGetLoopTrajectory_MissingID(t *testing.T) {
	c := &Component{}

	req := httptest.NewRequest(http.MethodGet, "/trajectory-api/loops/", nil)
	w := httptest.NewRecorder()

	c.handleGetLoopTrajectory(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGetTraceTrajectory_MethodNotAllowed(t *testing.T) {
	c := &Component{}

	req := httptest.NewRequest(http.MethodPost, "/trajectory-api/traces/trace-123", nil)
	w := httptest.NewRecorder()

	c.handleGetTraceTrajectory(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleGetTraceTrajectory_MissingID(t *testing.T) {
	c := &Component{}

	req := httptest.NewRequest(http.MethodGet, "/trajectory-api/traces/", nil)
	w := httptest.NewRecorder()

	c.handleGetTraceTrajectory(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestTrajectoryResponseFormat verifies the JSON structure of trajectory responses.
func TestTrajectoryResponseFormat(t *testing.T) {
	c := &Component{}
	now := time.Now()
	endTime := now.Add(2 * time.Second)

	loopState := &LoopState{
		ID:        "loop-test",
		TraceID:   "trace-test",
		Status:    "completed",
		Iteration: 2,
		StartedAt: &now,
		EndedAt:   &endTime,
	}

	calls := []*llm.CallRecord{
		{
			RequestID:   "req-1",
			Model:       "gpt-4",
			Provider:    "openai",
			Capability:  "planning",
			TokensIn:    50,
			TokensOut:   25,
			DurationMs:  500,
			StartedAt:   now,
			CompletedAt: now.Add(500 * time.Millisecond),
		},
	}

	trajectory := c.buildTrajectory(loopState, calls, []*llm.ToolCallRecord{}, true)

	// Verify JSON marshaling works correctly
	data, err := json.Marshal(trajectory)
	if err != nil {
		t.Fatalf("Failed to marshal trajectory: %v", err)
	}

	var unmarshaled Trajectory
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal trajectory: %v", err)
	}

	if unmarshaled.LoopID != trajectory.LoopID {
		t.Errorf("Unmarshaled LoopID = %q, want %q", unmarshaled.LoopID, trajectory.LoopID)
	}
	if unmarshaled.TraceID != trajectory.TraceID {
		t.Errorf("Unmarshaled TraceID = %q, want %q", unmarshaled.TraceID, trajectory.TraceID)
	}
	if unmarshaled.Status != trajectory.Status {
		t.Errorf("Unmarshaled Status = %q, want %q", unmarshaled.Status, trajectory.Status)
	}
	if unmarshaled.Steps != trajectory.Steps {
		t.Errorf("Unmarshaled Steps = %d, want %d", unmarshaled.Steps, trajectory.Steps)
	}
	if unmarshaled.ModelCalls != trajectory.ModelCalls {
		t.Errorf("Unmarshaled ModelCalls = %d, want %d", unmarshaled.ModelCalls, trajectory.ModelCalls)
	}
	if unmarshaled.TokensIn != trajectory.TokensIn {
		t.Errorf("Unmarshaled TokensIn = %d, want %d", unmarshaled.TokensIn, trajectory.TokensIn)
	}
	if unmarshaled.TokensOut != trajectory.TokensOut {
		t.Errorf("Unmarshaled TokensOut = %d, want %d", unmarshaled.TokensOut, trajectory.TokensOut)
	}
	if len(unmarshaled.Entries) != len(trajectory.Entries) {
		t.Errorf("Unmarshaled Entries count = %d, want %d", len(unmarshaled.Entries), len(trajectory.Entries))
	}
}

// TestTrajectoryEntryError verifies error field propagation in entries.
func TestTrajectoryEntryError(t *testing.T) {
	c := &Component{}
	now := time.Now()

	loopState := &LoopState{
		ID:     "loop-error",
		Status: "failed",
	}

	calls := []*llm.CallRecord{
		{
			RequestID:  "req-1",
			Model:      "gpt-4",
			Provider:   "openai",
			StartedAt:  now,
			DurationMs: 100,
			Error:      "connection timeout",
		},
	}

	trajectory := c.buildTrajectory(loopState, calls, []*llm.ToolCallRecord{}, true)

	if len(trajectory.Entries) != 1 {
		t.Fatalf("Entries count = %d, want 1", len(trajectory.Entries))
	}

	entry := trajectory.Entries[0]
	if entry.Error != "connection timeout" {
		t.Errorf("Entry.Error = %q, want %q", entry.Error, "connection timeout")
	}
}

// TestRegisterHTTPHandlers verifies handler registration by checking the mux pattern.
func TestRegisterHTTPHandlers(t *testing.T) {
	c := &Component{}
	mux := http.NewServeMux()

	// This should not panic
	c.RegisterHTTPHandlers("/trajectory-api/", mux)

	// Verify handlers are registered - we test this by checking handler paths
	// that would return 400 Bad Request (missing ID) instead of 404 (no handler)
	tests := []struct {
		name         string
		path         string
		method       string
		expectedCode int
	}{
		{
			name:         "loops handler registered - missing ID",
			path:         "/trajectory-api/loops/",
			method:       http.MethodGet,
			expectedCode: http.StatusBadRequest, // Missing ID returns 400, not 404
		},
		{
			name:         "traces handler registered - missing ID",
			path:         "/trajectory-api/traces/",
			method:       http.MethodGet,
			expectedCode: http.StatusBadRequest, // Missing ID returns 400, not 404
		},
		{
			name:         "loops handler wrong method",
			path:         "/trajectory-api/loops/test-id",
			method:       http.MethodPost,
			expectedCode: http.StatusMethodNotAllowed,
		},
		{
			name:         "traces handler wrong method",
			path:         "/trajectory-api/traces/test-id",
			method:       http.MethodPost,
			expectedCode: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("Status = %d, want %d (body: %s)", w.Code, tt.expectedCode, w.Body.String())
			}
		})
	}
}

// TestLoopStateJSONSerialization verifies LoopState JSON marshaling.
func TestLoopStateJSONSerialization(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	endTime := now.Add(5 * time.Second)

	state := &LoopState{
		ID:        "loop-123",
		TraceID:   "trace-456",
		Status:    "completed",
		Role:      "planner",
		Model:     "gpt-4",
		Iteration: 5,
		StartedAt: &now,
		EndedAt:   &endTime,
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Failed to marshal LoopState: %v", err)
	}

	var unmarshaled LoopState
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal LoopState: %v", err)
	}

	if unmarshaled.ID != state.ID {
		t.Errorf("ID = %q, want %q", unmarshaled.ID, state.ID)
	}
	if unmarshaled.TraceID != state.TraceID {
		t.Errorf("TraceID = %q, want %q", unmarshaled.TraceID, state.TraceID)
	}
	if unmarshaled.Status != state.Status {
		t.Errorf("Status = %q, want %q", unmarshaled.Status, state.Status)
	}
	if unmarshaled.Role != state.Role {
		t.Errorf("Role = %q, want %q", unmarshaled.Role, state.Role)
	}
	if unmarshaled.Model != state.Model {
		t.Errorf("Model = %q, want %q", unmarshaled.Model, state.Model)
	}
	if unmarshaled.Iteration != state.Iteration {
		t.Errorf("Iteration = %d, want %d", unmarshaled.Iteration, state.Iteration)
	}
}

// TestTrajectoryEntryJSONSerialization verifies TrajectoryEntry JSON marshaling.
func TestTrajectoryEntryJSONSerialization(t *testing.T) {
	now := time.Now().Truncate(time.Second)

	entry := TrajectoryEntry{
		Type:            "model_call",
		Timestamp:       now,
		DurationMs:      1500,
		Model:           "claude-3-opus",
		Provider:        "anthropic",
		Capability:      "coding",
		TokensIn:        500,
		TokensOut:       250,
		FinishReason:    "stop",
		Retries:         2,
		MessagesCount:   3,
		ResponsePreview: "Generated code...",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Failed to marshal TrajectoryEntry: %v", err)
	}

	var unmarshaled TrajectoryEntry
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal TrajectoryEntry: %v", err)
	}

	if unmarshaled.Type != entry.Type {
		t.Errorf("Type = %q, want %q", unmarshaled.Type, entry.Type)
	}
	if unmarshaled.Model != entry.Model {
		t.Errorf("Model = %q, want %q", unmarshaled.Model, entry.Model)
	}
	if unmarshaled.Provider != entry.Provider {
		t.Errorf("Provider = %q, want %q", unmarshaled.Provider, entry.Provider)
	}
	if unmarshaled.Capability != entry.Capability {
		t.Errorf("Capability = %q, want %q", unmarshaled.Capability, entry.Capability)
	}
	if unmarshaled.DurationMs != entry.DurationMs {
		t.Errorf("DurationMs = %d, want %d", unmarshaled.DurationMs, entry.DurationMs)
	}
	if unmarshaled.TokensIn != entry.TokensIn {
		t.Errorf("TokensIn = %d, want %d", unmarshaled.TokensIn, entry.TokensIn)
	}
	if unmarshaled.TokensOut != entry.TokensOut {
		t.Errorf("TokensOut = %d, want %d", unmarshaled.TokensOut, entry.TokensOut)
	}
	if unmarshaled.FinishReason != entry.FinishReason {
		t.Errorf("FinishReason = %q, want %q", unmarshaled.FinishReason, entry.FinishReason)
	}
	if unmarshaled.Retries != entry.Retries {
		t.Errorf("Retries = %d, want %d", unmarshaled.Retries, entry.Retries)
	}
	if unmarshaled.MessagesCount != entry.MessagesCount {
		t.Errorf("MessagesCount = %d, want %d", unmarshaled.MessagesCount, entry.MessagesCount)
	}
	if unmarshaled.ResponsePreview != entry.ResponsePreview {
		t.Errorf("ResponsePreview = %q, want %q", unmarshaled.ResponsePreview, entry.ResponsePreview)
	}
}
