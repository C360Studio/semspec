package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFixtures_BaseOnly(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "mock-planner.json", `{"goal":"test plan"}`)
	writeFixture(t, dir, "mock-reviewer.json", `{"verdict":"approved"}`)

	fixtures, err := loadFixtures(dir)
	if err != nil {
		t.Fatalf("loadFixtures: %v", err)
	}

	if len(fixtures) != 2 {
		t.Fatalf("expected 2 models, got %d", len(fixtures))
	}

	// Each model should have exactly 1 fixture (the base)
	for model, seq := range fixtures {
		if len(seq) != 1 {
			t.Errorf("model %q: expected 1 fixture, got %d", model, len(seq))
		}
	}
}

func TestLoadFixtures_Sequential(t *testing.T) {
	dir := t.TempDir()

	// Numbered fixtures for reviewer (rejection then approval)
	writeFixture(t, dir, "mock-reviewer.1.json", `{"verdict":"needs_changes"}`)
	writeFixture(t, dir, "mock-reviewer.2.json", `{"verdict":"approved","summary":"fixed"}`)
	// Base fallback
	writeFixture(t, dir, "mock-reviewer.json", `{"verdict":"approved","summary":"fallback"}`)

	// Non-sequential model
	writeFixture(t, dir, "mock-planner.json", `{"goal":"test"}`)

	fixtures, err := loadFixtures(dir)
	if err != nil {
		t.Fatalf("loadFixtures: %v", err)
	}

	// Reviewer should have 3 entries: .1, .2, base
	reviewerSeq := fixtures["mock-reviewer"]
	if len(reviewerSeq) != 3 {
		t.Fatalf("mock-reviewer: expected 3 fixtures, got %d", len(reviewerSeq))
	}

	// Verify order: numbered first (sorted), then base
	if !strings.Contains(reviewerSeq[0], "needs_changes") {
		t.Errorf("fixture[0] should be needs_changes, got: %s", reviewerSeq[0])
	}
	if !strings.Contains(reviewerSeq[1], "fixed") {
		t.Errorf("fixture[1] should be approved/fixed, got: %s", reviewerSeq[1])
	}
	if !strings.Contains(reviewerSeq[2], "fallback") {
		t.Errorf("fixture[2] should be approved/fallback, got: %s", reviewerSeq[2])
	}

	// Planner should have 1 entry
	plannerSeq := fixtures["mock-planner"]
	if len(plannerSeq) != 1 {
		t.Fatalf("mock-planner: expected 1 fixture, got %d", len(plannerSeq))
	}
}

func TestLoadFixtures_NumberedOnly(t *testing.T) {
	dir := t.TempDir()

	// Only numbered, no base file
	writeFixture(t, dir, "mock-reviewer.1.json", `{"verdict":"needs_changes"}`)
	writeFixture(t, dir, "mock-reviewer.2.json", `{"verdict":"approved"}`)

	fixtures, err := loadFixtures(dir)
	if err != nil {
		t.Fatalf("loadFixtures: %v", err)
	}

	seq := fixtures["mock-reviewer"]
	if len(seq) != 2 {
		t.Fatalf("expected 2 fixtures, got %d", len(seq))
	}
}

func TestLoadFixtures_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	_, err := loadFixtures(dir)
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
}

func TestSequentialFixtureSelection(t *testing.T) {
	fixtures := map[string][]string{
		"mock-reviewer": {
			`{"verdict":"needs_changes"}`,
			`{"verdict":"approved"}`,
		},
		"mock-planner": {
			`{"goal":"test plan"}`,
		},
	}

	s := newServer(fixtures)

	// First call to mock-reviewer → needs_changes
	resp1 := doCompletion(t, s, "mock-reviewer")
	if !strings.Contains(resp1, "needs_changes") {
		t.Errorf("call 1: expected needs_changes, got: %s", resp1)
	}

	// Second call to mock-reviewer → approved
	resp2 := doCompletion(t, s, "mock-reviewer")
	if !strings.Contains(resp2, "approved") {
		t.Errorf("call 2: expected approved, got: %s", resp2)
	}

	// Third call (beyond sequence) → repeats last (approved)
	resp3 := doCompletion(t, s, "mock-reviewer")
	if !strings.Contains(resp3, "approved") {
		t.Errorf("call 3: expected approved (repeat last), got: %s", resp3)
	}

	// Planner calls are independent
	planResp := doCompletion(t, s, "mock-planner")
	if !strings.Contains(planResp, "test plan") {
		t.Errorf("planner: expected test plan, got: %s", planResp)
	}
}

func TestStatsEndpoint(t *testing.T) {
	fixtures := map[string][]string{
		"mock-reviewer": {`{"verdict":"approved"}`},
		"mock-planner":  {`{"goal":"test"}`},
	}

	s := newServer(fixtures)

	// Make some calls
	doCompletion(t, s, "mock-reviewer")
	doCompletion(t, s, "mock-reviewer")
	doCompletion(t, s, "mock-planner")

	// Query stats
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()
	s.handleStats(w, req)

	var stats struct {
		TotalCalls   int64            `json:"total_calls"`
		CallsByModel map[string]int64 `json:"calls_by_model"`
	}
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}

	if stats.TotalCalls != 3 {
		t.Errorf("total_calls: expected 3, got %d", stats.TotalCalls)
	}
	if stats.CallsByModel["mock-reviewer"] != 2 {
		t.Errorf("mock-reviewer calls: expected 2, got %d", stats.CallsByModel["mock-reviewer"])
	}
	if stats.CallsByModel["mock-planner"] != 1 {
		t.Errorf("mock-planner calls: expected 1, got %d", stats.CallsByModel["mock-planner"])
	}
}

func TestStripMockPrefix(t *testing.T) {
	fixtures := map[string][]string{
		"planner": {`{"goal":"test"}`},
	}

	s := newServer(fixtures)

	// Request with "mock-" prefix should resolve to "planner"
	resp := doCompletion(t, s, "mock-planner")
	if !strings.Contains(resp, "test") {
		t.Errorf("expected mock-prefix stripping to resolve, got: %s", resp)
	}
}

func TestNumberedFileRegex(t *testing.T) {
	tests := []struct {
		filename string
		wantBase string
		wantNum  string
		match    bool
	}{
		{"mock-reviewer.1.json", "mock-reviewer", "1", true},
		{"mock-reviewer.2.json", "mock-reviewer", "2", true},
		{"mock-reviewer.10.json", "mock-reviewer", "10", true},
		{"mock-reviewer.json", "", "", false},
		{"mock-fast.json", "", "", false},
	}

	for _, tt := range tests {
		matches := numberedFileRe.FindStringSubmatch(tt.filename)
		if tt.match {
			if matches == nil {
				t.Errorf("%s: expected match, got nil", tt.filename)
				continue
			}
			if matches[1] != tt.wantBase {
				t.Errorf("%s: base=%q, want %q", tt.filename, matches[1], tt.wantBase)
			}
			if matches[2] != tt.wantNum {
				t.Errorf("%s: num=%q, want %q", tt.filename, matches[2], tt.wantNum)
			}
		} else {
			if matches != nil {
				t.Errorf("%s: expected no match, got %v", tt.filename, matches)
			}
		}
	}
}

func TestToolCallFixture(t *testing.T) {
	// Fixture with tool_calls
	toolFixture := `{
		"content": "I'll create that file for you.",
		"tool_calls": [
			{
				"id": "call_123",
				"type": "function",
				"function": {
					"name": "file_write",
					"arguments": "{\"path\":\"hello.py\",\"content\":\"print('hello')\"}"
				}
			}
		],
		"finish_reason": "tool_calls"
	}`

	fixtures := map[string][]string{
		"mock-developer": {toolFixture},
	}

	s := newServer(fixtures)

	// Make request with tools
	body := strings.NewReader(`{
		"model": "mock-developer",
		"messages": [{"role": "user", "content": "Create a hello.py file"}],
		"tools": [{"type": "function", "function": {"name": "file_write", "parameters": {}}}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body: %s", w.Code, w.Body.String())
	}

	var resp chatResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Choices) == 0 {
		t.Fatal("no choices in response")
	}

	choice := resp.Choices[0]

	// Check finish_reason
	if choice.FinishReason != "tool_calls" {
		t.Errorf("finish_reason: expected 'tool_calls', got %q", choice.FinishReason)
	}

	// Check content
	if !strings.Contains(choice.Message.Content, "create that file") {
		t.Errorf("content: expected file creation message, got %q", choice.Message.Content)
	}

	// Check tool_calls
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool_call, got %d", len(choice.Message.ToolCalls))
	}

	tc := choice.Message.ToolCalls[0]
	if tc.ID != "call_123" {
		t.Errorf("tool_call.id: expected 'call_123', got %q", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("tool_call.type: expected 'function', got %q", tc.Type)
	}
	if tc.Function.Name != "file_write" {
		t.Errorf("tool_call.function.name: expected 'file_write', got %q", tc.Function.Name)
	}
	if !strings.Contains(tc.Function.Arguments, "hello.py") {
		t.Errorf("tool_call.function.arguments: expected hello.py, got %q", tc.Function.Arguments)
	}
}

func TestToolCallMultiTurn(t *testing.T) {
	// First call returns tool_calls, second call returns final response
	fixtures := map[string][]string{
		"mock-developer": {
			// First response: tool call
			`{
				"content": "I'll create the file.",
				"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "file_write", "arguments": "{\"path\":\"test.py\"}"}}],
				"finish_reason": "tool_calls"
			}`,
			// Second response: final answer after tool result
			`{
				"content": "Done! I created test.py for you.",
				"finish_reason": "stop"
			}`,
		},
	}

	s := newServer(fixtures)

	// First call - should get tool_calls
	resp1 := doCompletionFull(t, s, "mock-developer", `[{"role":"user","content":"Create test.py"}]`)
	if resp1.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("call 1: expected finish_reason=tool_calls, got %q", resp1.Choices[0].FinishReason)
	}
	if len(resp1.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("call 1: expected 1 tool_call, got %d", len(resp1.Choices[0].Message.ToolCalls))
	}

	// Second call - includes tool result, should get final response
	resp2 := doCompletionFull(t, s, "mock-developer", `[
		{"role":"user","content":"Create test.py"},
		{"role":"assistant","content":"I'll create the file.","tool_calls":[{"id":"call_1","type":"function","function":{"name":"file_write","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"call_1","content":"File created successfully"}
	]`)
	if resp2.Choices[0].FinishReason != "stop" {
		t.Errorf("call 2: expected finish_reason=stop, got %q", resp2.Choices[0].FinishReason)
	}
	if !strings.Contains(resp2.Choices[0].Message.Content, "Done") {
		t.Errorf("call 2: expected final response, got %q", resp2.Choices[0].Message.Content)
	}
}

func TestPlainTextFixtureUnchanged(t *testing.T) {
	// Existing plain text fixtures should still work
	fixtures := map[string][]string{
		"mock-planner": {`{"goal":"Create a REST API","tasks":["task1","task2"]}`},
	}

	s := newServer(fixtures)
	resp := doCompletionFull(t, s, "mock-planner", `[{"role":"user","content":"Plan something"}]`)

	// Should be plain text response
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason=stop, got %q", resp.Choices[0].FinishReason)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 0 {
		t.Errorf("expected no tool_calls for plain text fixture")
	}
	if !strings.Contains(resp.Choices[0].Message.Content, "REST API") {
		t.Errorf("expected fixture content, got %q", resp.Choices[0].Message.Content)
	}
}

func TestCapturedRequestsIncludeTools(t *testing.T) {
	fixtures := map[string][]string{
		"mock-developer": {`{"content":"ok"}`},
	}

	s := newServer(fixtures)

	// Make request with tools
	body := strings.NewReader(`{
		"model": "mock-developer",
		"messages": [{"role": "user", "content": "Test"}],
		"tools": [
			{"type": "function", "function": {"name": "file_write", "description": "Write a file"}},
			{"type": "function", "function": {"name": "file_read", "description": "Read a file"}}
		]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}

	// Check captured request includes tools
	reqReq := httptest.NewRequest(http.MethodGet, "/requests?model=mock-developer", nil)
	reqW := httptest.NewRecorder()
	s.handleRequests(reqW, reqReq)

	var captured struct {
		RequestsByModel map[string][]capturedRequest `json:"requests_by_model"`
	}
	if err := json.NewDecoder(reqW.Body).Decode(&captured); err != nil {
		t.Fatalf("decode requests: %v", err)
	}

	reqs := captured.RequestsByModel["mock-developer"]
	if len(reqs) != 1 {
		t.Fatalf("expected 1 captured request, got %d", len(reqs))
	}

	if len(reqs[0].Tools) != 2 {
		t.Errorf("expected 2 tools in captured request, got %d", len(reqs[0].Tools))
	}
}

// --- helpers ---

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func doCompletion(t *testing.T, s *server, model string) string {
	t.Helper()
	body := strings.NewReader(`{"model":"` + model + `","messages":[{"role":"user","content":"test"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("model %s: status %d, body: %s", model, w.Code, w.Body.String())
	}

	var resp chatResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Choices) == 0 {
		t.Fatalf("no choices in response")
	}

	return resp.Choices[0].Message.Content
}

func doCompletionFull(t *testing.T, s *server, model, messagesJSON string) chatResponse {
	t.Helper()
	body := strings.NewReader(`{"model":"` + model + `","messages":` + messagesJSON + `}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("model %s: status %d, body: %s", model, w.Code, w.Body.String())
	}

	var resp chatResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Choices) == 0 {
		t.Fatalf("no choices in response")
	}

	return resp
}
