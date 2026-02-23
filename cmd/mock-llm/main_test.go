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
