package terminal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
	ssmodel "github.com/c360studio/semstreams/model"
)

// TestStrictModeWireShape validates the end-to-end wire shape of a
// dispatch built from semspec's helpers (ResponseFormatForEndpoint +
// ToolsForEndpoint) when run through the real agentic-model client.
//
// Captures the POST body to /v1/chat/completions on a httptest.Server and
// asserts:
//
//   - response_format.type == "json_schema"
//   - response_format.json_schema.strict == true
//   - response_format.json_schema.schema.additionalProperties == false
//   - tools[].function.name == "submit_work"
//   - tools[].function.strict == true (per ADR-035, beta.51)
//   - tools[].function.parameters.additionalProperties == false
//
// This catches structural bugs (schema not strict-mode-compliant, helper
// not setting Strict, etc.) without burning real-LLM tokens. Run before
// every paid e2e to avoid wasted upstream calls.
func TestStrictModeWireShape(t *testing.T) {
	var captured map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("failed to parse captured body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "test-1",
			"object": "chat.completion",
			"created": 0,
			"model": "test-model",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
		}`))
	}))
	defer srv.Close()

	ep := &ssmodel.EndpointConfig{
		Provider:      "openai",
		URL:           srv.URL + "/v1",
		Model:         "test-model",
		SupportsTools: true,
	}

	reg := &fakeToolReg{tools: []agentic.ToolDefinition{
		{Name: "submit_work", Description: "Submit work", Parameters: map[string]any{"type": "object"}},
		{Name: "bash", Description: "Run shell command", Parameters: map[string]any{"type": "object"}},
	}}

	tools := ToolsForEndpoint(reg, "developer", ep)
	rf := ResponseFormatForEndpoint(ep, "developer")
	if rf == nil {
		t.Fatal("ResponseFormatForEndpoint returned nil — preflight bug")
	}

	client, err := agenticmodel.NewClient(ep)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	req := agentic.AgentRequest{
		RequestID:      "wire-shape-test",
		Model:          ep.Model,
		Messages:       []agentic.ChatMessage{{Role: "user", Content: "hi"}},
		Tools:          tools,
		ResponseFormat: rf,
	}

	if _, err := client.ChatCompletion(context.Background(), req); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if captured == nil {
		t.Fatal("server didn't capture a request body")
	}

	t.Run("response_format on the wire", func(t *testing.T) {
		rfWire, ok := captured["response_format"].(map[string]any)
		if !ok {
			t.Fatalf("response_format missing from request body — got %v", captured["response_format"])
		}
		if got, want := rfWire["type"], "json_schema"; got != want {
			t.Errorf("response_format.type = %v, want %v", got, want)
		}
		schemaWrap, ok := rfWire["json_schema"].(map[string]any)
		if !ok {
			t.Fatalf("response_format.json_schema missing — got %v", rfWire["json_schema"])
		}
		if got, want := schemaWrap["strict"], true; got != want {
			t.Errorf("response_format.json_schema.strict = %v, want true", got)
		}
		schema, ok := schemaWrap["schema"].(map[string]any)
		if !ok {
			t.Fatalf("response_format.json_schema.schema missing")
		}
		if got, want := schema["additionalProperties"], false; got != want {
			t.Errorf("schema.additionalProperties = %v, want false", got)
		}
		// Top-level required must include both developer fields (developer
		// schema is fully strict-mode-compliant since it always was).
		req, ok := schema["required"].([]any)
		if !ok || len(req) != 2 {
			t.Errorf("schema.required = %v, want [summary, files_modified]", schema["required"])
		}
	})

	t.Run("tools[].function.strict on the wire", func(t *testing.T) {
		toolsWire, ok := captured["tools"].([]any)
		if !ok {
			t.Fatalf("tools missing from request body — got %v", captured["tools"])
		}
		var submitWork map[string]any
		var bashTool map[string]any
		for _, raw := range toolsWire {
			tm, _ := raw.(map[string]any)
			fn, _ := tm["function"].(map[string]any)
			switch fn["name"] {
			case "submit_work":
				submitWork = fn
			case "bash":
				bashTool = fn
			}
		}
		if submitWork == nil {
			t.Fatal("submit_work tool missing from request")
		}
		if got, want := submitWork["strict"], true; got != want {
			t.Errorf("submit_work.strict = %v, want true (ADR-035 strict tool calling)", got)
		}
		params, _ := submitWork["parameters"].(map[string]any)
		if got, want := params["additionalProperties"], false; got != want {
			t.Errorf("submit_work.parameters.additionalProperties = %v, want false", got)
		}
		if bashTool != nil {
			if _, has := bashTool["strict"]; has && bashTool["strict"] == true {
				t.Errorf("bash.strict should be unset/false — only submit_work is the structured-output tool")
			}
		}
	})
}

// TestStrictModeWireShape_AnthropicNoOp confirms that anthropic-bound
// dispatches do NOT get response_format or tool.strict on the wire (per
// ADR-034 + ADR-035 provider matrix). EndpointSupportsResponseFormat
// returns false for anthropic; both helpers return zero-value, omitempty
// drops the fields.
func TestStrictModeWireShape_AnthropicNoOp(t *testing.T) {
	var captured map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","created":0,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	// Use provider:"openai" with an OpenAI-shaped server but pretend it's
	// Gemini's OpenAI-compat endpoint (URL contains googleapis.com). Our
	// EndpointSupportsResponseFormat heuristic returns false for that
	// host. We can't truly test against Anthropic without its native
	// adapter, but the no-op behavior is the same.
	ep := &ssmodel.EndpointConfig{
		Provider:      "openai",
		URL:           srv.URL + "/v1",
		Model:         "gemini-test",
		SupportsTools: true,
	}
	// Override the heuristic by directly checking: use a Gemini URL marker.
	// The httptest URL doesn't contain googleapis.com naturally; force the
	// no-op path via the helper's contract.
	if EndpointSupportsResponseFormat(&ssmodel.EndpointConfig{Provider: "anthropic"}) {
		t.Fatal("anthropic should be no-op — heuristic regression")
	}

	// To exercise the wire-side absence, build helpers against an
	// anthropic-shaped endpoint then dispatch through the OpenAI client
	// (our real production code uses the helper's nil/false results, not
	// the endpoint identity). Build helpers against anthropic.
	anthEp := &ssmodel.EndpointConfig{Provider: "anthropic"}
	tools := ToolsForEndpoint(&fakeToolReg{tools: []agentic.ToolDefinition{
		{Name: "submit_work", Description: "Submit work", Parameters: map[string]any{"type": "object"}},
	}}, "developer", anthEp)
	rf := ResponseFormatForEndpoint(anthEp, "developer")

	if rf != nil {
		t.Fatal("ResponseFormatForEndpoint should return nil for anthropic")
	}
	for _, tool := range tools {
		if tool.Strict {
			t.Errorf("tool %q.Strict = true on anthropic — should stay zero-value", tool.Name)
		}
	}

	// Now dispatch through the OpenAI client using the (correctly empty)
	// helpers, just to confirm zero-value Strict + nil ResponseFormat
	// don't appear on the wire.
	client, err := agenticmodel.NewClient(ep)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	if _, err := client.ChatCompletion(context.Background(), agentic.AgentRequest{
		RequestID: "anth-noop",
		Model:     ep.Model,
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "hi"}},
		Tools:     tools,
		ResponseFormat: rf, // nil
	}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if _, has := captured["response_format"]; has {
		t.Errorf("response_format leaked onto wire on anthropic-bound dispatch — got %v", captured["response_format"])
	}
	toolsWire, _ := captured["tools"].([]any)
	for _, raw := range toolsWire {
		tm, _ := raw.(map[string]any)
		fn, _ := tm["function"].(map[string]any)
		if strict, has := fn["strict"]; has && strict == true {
			t.Errorf("tool %v.strict leaked onto wire on anthropic-bound dispatch", fn["name"])
		}
	}
}
