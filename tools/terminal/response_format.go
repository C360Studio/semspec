package terminal

import (
	"strings"

	"github.com/c360studio/semstreams/agentic"
	ssmodel "github.com/c360studio/semstreams/model"
)

// EndpointSupportsResponseFormat reports whether the resolved endpoint's
// provider+URL is known to honor the OpenAI-shape response_format on the
// wire. The discriminator follows ADR-034 in semstreams:
//
//   - anthropic: tool calling is the structured-output primitive; response_format
//     is stubbed today, so we return false to keep prompt assembly unchanged.
//   - openai (Gemini OpenAI-compat at generativelanguage.googleapis.com):
//     response_format is silently ignored; return false.
//   - openai (everything else — vLLM, sparky, OpenRouter, LocalAI, OpenAI proper):
//     honored; return true.
//   - openrouter: honored; return true.
//   - ollama: model-dependent (gemma3 ignores it; qwen3 honors it). We default
//     to true and rely on the agentic-loop's malformed-JSON fallback for outliers.
//
// A nil endpoint returns false (caller has nothing to attach to).
func EndpointSupportsResponseFormat(ep *ssmodel.EndpointConfig) bool {
	if ep == nil {
		return false
	}
	switch ep.Provider {
	case "anthropic":
		return false
	case "openai":
		if strings.Contains(ep.URL, "googleapis.com") {
			return false
		}
		return true
	case "ollama", "openrouter":
		return true
	default:
		return false
	}
}

// ResponseFormatForDeliverable builds an agentic.ResponseFormat for the
// given deliverable type using the same schema wired into submit_work via
// ToolsForDeliverable. Returns nil for unknown deliverable types.
//
// Strict is true: schemas in schemas.go satisfy the OpenAI strict-mode
// subset (TestSchemasNoAdditionalProperties + TestSchemasRequiredCompleteness
// pin this). On OpenAI proper the response is guaranteed schema-conformant;
// on vLLM/sparky/OpenRouter the same xgrammar/outlines constraint applies
// during decoding. Optional semantics are encoded as nullable types
// (`"type": ["string", "null"]`) — the model populates or sets null per the
// schema description.
func ResponseFormatForDeliverable(deliverableType string) *agentic.ResponseFormat {
	schema := schemaForDeliverable(deliverableType)
	if len(schema) == 0 {
		return nil
	}
	return &agentic.ResponseFormat{
		Type:   agentic.ResponseFormatJSONSchema,
		Name:   deliverableType + "_args",
		Schema: schema,
		Strict: true,
	}
}

// ResponseFormatForEndpoint returns the ResponseFormat to attach to a
// TaskMessage, or nil if the endpoint won't honor it. Convenience wrapper
// for dispatch sites: one call replaces the support-check + schema-build
// pair.
func ResponseFormatForEndpoint(ep *ssmodel.EndpointConfig, deliverableType string) *agentic.ResponseFormat {
	if !EndpointSupportsResponseFormat(ep) {
		return nil
	}
	return ResponseFormatForDeliverable(deliverableType)
}
