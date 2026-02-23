// Package main implements a mock LLM server for e2e testing.
// It serves OpenAI-compatible /v1/chat/completions responses from JSON fixture
// files, routing by the "model" field in the request. This eliminates the need
// for a real LLM during workflow wiring tests, making them fast, deterministic,
// and offline-capable.
//
// Usage:
//
//	mock-llm -fixtures /path/to/fixtures -port 11434
//
// Fixture files are JSON named by model (e.g., "mock-planner.json" maps to
// model "mock-planner"). The file content is returned as the assistant message.
//
// Sequential fixtures: If numbered files exist (e.g., "mock-reviewer.1.json",
// "mock-reviewer.2.json"), the Nth call to that model returns the Nth fixture.
// After exhausting numbered fixtures, the base "mock-reviewer.json" is used
// as a repeating fallback. This enables testing rejection→revision→approval loops.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// --- OpenAI-compatible types ---

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// --- Server ---

// capturedRequest stores the key fields of an incoming LLM request for test verification.
type capturedRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	CallIndex int           `json:"call_index"` // 1-indexed per-model call number
	Timestamp int64         `json:"timestamp"`
}

type server struct {
	fixtures map[string][]string // model name → ordered fixture contents (sequential)
	calls    atomic.Int64        // total calls served

	// Per-model call counters for sequential fixture selection.
	modelCalls   map[string]*atomic.Int64
	modelCallsMu sync.Mutex // protects lazy init of modelCalls entries

	// Per-model request capture for prompt verification in e2e tests.
	modelRequests   map[string][]capturedRequest
	modelRequestsMu sync.Mutex
}

func newServer(fixtures map[string][]string) *server {
	return &server{
		fixtures:      fixtures,
		modelCalls:    make(map[string]*atomic.Int64),
		modelRequests: make(map[string][]capturedRequest),
	}
}

// captureRequest stores a request for later retrieval via /requests endpoint.
func (s *server) captureRequest(model string, req chatRequest, callIndex int) {
	s.modelRequestsMu.Lock()
	defer s.modelRequestsMu.Unlock()
	s.modelRequests[model] = append(s.modelRequests[model], capturedRequest{
		Model:     model,
		Messages:  req.Messages,
		CallIndex: callIndex,
		Timestamp: time.Now().UnixMilli(),
	})
}

// getModelCounter returns the call counter for a model, creating it lazily.
func (s *server) getModelCounter(model string) *atomic.Int64 {
	s.modelCallsMu.Lock()
	defer s.modelCallsMu.Unlock()
	if c, ok := s.modelCalls[model]; ok {
		return c
	}
	c := &atomic.Int64{}
	s.modelCalls[model] = c
	return c
}

func main() {
	fixtureDir := flag.String("fixtures", "", "directory containing fixture response files")
	port := flag.Int("port", 11434, "port to listen on")
	flag.Parse()

	// Allow env var override
	if envDir := os.Getenv("MOCK_LLM_FIXTURES"); envDir != "" && *fixtureDir == "" {
		*fixtureDir = envDir
	}
	if *fixtureDir == "" {
		*fixtureDir = "/fixtures"
	}

	fixtures, err := loadFixtures(*fixtureDir)
	if err != nil {
		log.Fatalf("Failed to load fixtures from %s: %v", *fixtureDir, err)
	}
	log.Printf("Loaded %d model(s) from %s", len(fixtures), *fixtureDir)
	for model, seq := range fixtures {
		log.Printf("  model: %s (%d fixture(s))", model, len(seq))
	}

	s := newServer(fixtures)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/stats", s.handleStats)
	mux.HandleFunc("/requests", s.handleRequests)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Mock LLM server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	callNum := s.calls.Add(1)
	log.Printf("[call %d] model=%s messages=%d", callNum, req.Model, len(req.Messages))

	// Resolve fixture sequence: try exact model name, then strip "mock-" prefix
	seq, ok := s.fixtures[req.Model]
	if !ok {
		stripped := strings.TrimPrefix(req.Model, "mock-")
		seq, ok = s.fixtures[stripped]
	}
	if !ok {
		log.Printf("[call %d] WARNING: no fixture for model=%q, returning error", callNum, req.Model)
		http.Error(w, fmt.Sprintf("no fixture for model %q", req.Model), http.StatusNotFound)
		return
	}

	// Select fixture from sequence based on per-model call count
	counter := s.getModelCounter(req.Model)
	callIndex := int(counter.Add(1) - 1) // 0-indexed

	// Capture request for prompt verification (e2e /requests endpoint)
	s.captureRequest(req.Model, req, callIndex+1)
	var content string
	if callIndex < len(seq) {
		content = seq[callIndex]
	} else {
		content = seq[len(seq)-1] // repeat last fixture
	}

	log.Printf("[call %d] model=%s call_index=%d/%d", callNum, req.Model, callIndex+1, len(seq))

	// Wrap in OpenAI response envelope
	resp := chatResponse{
		ID:      fmt.Sprintf("mock-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []chatChoice{
			{
				Index: 0,
				Message: chatMessage{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: "stop",
			},
		},
		Usage: chatUsage{
			PromptTokens:     len(content) / 4, // rough estimate
			CompletionTokens: len(content) / 4,
			TotalTokens:      len(content) / 2,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
	log.Printf("[call %d] responded with %d bytes for model=%s", callNum, len(content), req.Model)
}

// handleModels returns the list of available mock models (Ollama-compatible).
func (s *server) handleModels(w http.ResponseWriter, _ *http.Request) {
	type modelEntry struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}
	var models []modelEntry
	for name := range s.fixtures {
		models = append(models, modelEntry{
			ID:      name,
			Object:  "model",
			OwnedBy: "mock-llm",
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   models,
	})
}

// handleStats returns call counts for test assertions.
// Returns total_calls and per-model calls_by_model breakdown.
func (s *server) handleStats(w http.ResponseWriter, _ *http.Request) {
	s.modelCallsMu.Lock()
	callsByModel := make(map[string]int64, len(s.modelCalls))
	for model, counter := range s.modelCalls {
		callsByModel[model] = counter.Load()
	}
	s.modelCallsMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"total_calls":    s.calls.Load(),
		"calls_by_model": callsByModel,
	})
}

// handleRequests returns captured request bodies for test assertions.
// Query params:
//   - model: filter by model name (optional, returns all models if omitted)
//   - call: filter by call index, 1-indexed (optional)
//
// Returns {"requests_by_model": {"mock-planner": [...], ...}}
func (s *server) handleRequests(w http.ResponseWriter, r *http.Request) {
	modelFilter := r.URL.Query().Get("model")
	callFilter := r.URL.Query().Get("call")

	s.modelRequestsMu.Lock()
	result := make(map[string][]capturedRequest)
	for model, reqs := range s.modelRequests {
		if modelFilter != "" && model != modelFilter {
			continue
		}
		if callFilter != "" {
			callIdx, err := strconv.Atoi(callFilter)
			if err == nil {
				for _, req := range reqs {
					if req.CallIndex == callIdx {
						result[model] = append(result[model], req)
					}
				}
				continue
			}
		}
		result[model] = reqs
	}
	s.modelRequestsMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"requests_by_model": result,
	})
}

// numberedFileRe matches files like "mock-reviewer.1.json", "mock-planner.2.json".
var numberedFileRe = regexp.MustCompile(`^(.+)\.(\d+)\.json$`)

// loadFixtures reads JSON files from dir and returns a map of model→content sequence.
//
// For each model, fixtures are ordered:
//  1. Numbered files (model.1.json, model.2.json, ...) in numeric order
//  2. Base file (model.json) appended as the final fallback
//
// If only model.json exists, the sequence has one entry (same behavior as before).
func loadFixtures(dir string) (map[string][]string, error) {
	// Collect raw file data: base files and numbered files separately
	baseFiles := make(map[string]string)           // model → content
	numberedFiles := make(map[string]map[int]string) // model → {index → content}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		if !json.Valid(data) {
			return fmt.Errorf("invalid JSON in %s", path)
		}

		content := string(data)

		// Check for numbered pattern: model.N.json
		if matches := numberedFileRe.FindStringSubmatch(info.Name()); matches != nil {
			model := matches[1]
			index, _ := strconv.Atoi(matches[2])
			if numberedFiles[model] == nil {
				numberedFiles[model] = make(map[int]string)
			}
			numberedFiles[model][index] = content
			return nil
		}

		// Base file: model.json
		model := strings.TrimSuffix(info.Name(), ".json")
		baseFiles[model] = content
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Build ordered sequences
	fixtures := make(map[string][]string)

	// Collect all model names
	allModels := make(map[string]bool)
	for m := range baseFiles {
		allModels[m] = true
	}
	for m := range numberedFiles {
		allModels[m] = true
	}

	for model := range allModels {
		var seq []string

		// Add numbered fixtures in order
		if numbered, ok := numberedFiles[model]; ok {
			// Get sorted indices
			indices := make([]int, 0, len(numbered))
			for idx := range numbered {
				indices = append(indices, idx)
			}
			sort.Ints(indices)

			for _, idx := range indices {
				seq = append(seq, numbered[idx])
			}
		}

		// Append base file as fallback
		if base, ok := baseFiles[model]; ok {
			seq = append(seq, base)
		}

		if len(seq) > 0 {
			fixtures[model] = seq
		}
	}

	if len(fixtures) == 0 {
		return nil, fmt.Errorf("no fixture files found in %s", dir)
	}

	return fixtures, nil
}
