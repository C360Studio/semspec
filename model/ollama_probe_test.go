package model

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProbeOllamaModel_OK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			t.Errorf("expected /api/show, got %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		_ = json.Unmarshal(body, &payload)
		if payload["name"] != "qwen3-coder:30b" {
			t.Errorf("unexpected model name in probe: %q", payload["name"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := probeOllamaModel(context.Background(), server.URL+"/v1", "qwen3-coder:30b"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestProbeOllamaModel_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	err := probeOllamaModel(context.Background(), server.URL, "ghost-model")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
}

func TestProbeOllamaModel_Unreachable(t *testing.T) {
	// Use a closed server URL — connection will be refused.
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	addr := server.URL
	server.Close()

	err := probeOllamaModel(context.Background(), addr, "x")
	if err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("expected unreachable error, got %v", err)
	}
}

func TestProbeOllamaEndpoints_OnlyOllamaTargets(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	registry := NewRegistry(nil, map[string]*EndpointConfig{
		"local-a":  {Provider: "ollama", URL: server.URL + "/v1", Model: "qwen3-coder:30b"},
		"local-b":  {Provider: "ollama", URL: server.URL, Model: "qwen2.5-coder:14b"},
		"cloud":    {Provider: "anthropic", Model: "claude-sonnet-4-6"},
		"openai":   {Provider: "openai", URL: "https://api.openai.com/v1", Model: "gpt-4"},
		"nilcheck": nil,
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ProbeOllamaEndpoints(context.Background(), registry, logger)

	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("expected exactly 2 ollama probes, got %d", got)
	}
}

func TestProbeOllamaEndpoints_SkipEnv(t *testing.T) {
	t.Setenv("SEMSPEC_SKIP_OLLAMA_PROBE", "1")

	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer server.Close()

	registry := NewRegistry(nil, map[string]*EndpointConfig{
		"local-a": {Provider: "ollama", URL: server.URL, Model: "x"},
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ProbeOllamaEndpoints(context.Background(), registry, logger)

	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("expected probe to skip when env set, got %d hits", got)
	}
}

func TestProbeOllamaEndpoints_LogsWarnOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	registry := NewRegistry(nil, map[string]*EndpointConfig{
		"missing": {Provider: "ollama", URL: server.URL, Model: "ghost"},
	})

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ProbeOllamaEndpoints(context.Background(), registry, logger)

	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Fatalf("expected WARN log on probe failure, got: %s", out)
	}
	if !strings.Contains(out, "missing") || !strings.Contains(out, "ghost") {
		t.Fatalf("expected endpoint name and model in WARN log, got: %s", out)
	}
}
