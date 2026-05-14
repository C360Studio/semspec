package research

import (
	"fmt"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
)

// stringArg returns the string value at key, or "" if missing or wrong
// type. Same shape as tools/question/executor.go:stringArg — duplicated
// rather than exported because these are intentionally package-private
// adapters between the LLM's loosely-typed args map and our typed
// payloads.
func stringArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// stringSliceArg returns the []string at key, or nil if missing/wrong type.
// Coerces []any to []string element-by-element so callers tolerate the
// JSON-decoded shape ([]any of string elements) the agentic-loop hands us.
func stringSliceArg(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			str, ok := item.(string)
			if !ok {
				continue
			}
			out = append(out, str)
		}
		return out
	default:
		return nil
	}
}

// citationsArg extracts the citations array from tool args. Each citation
// is a map with {url|file, lines} — coerced to workflow.Citation values.
// Returns an error if the shape is malformed; nil + nil if the key is
// absent (caller decides whether absence is a hard reject).
func citationsArg(args map[string]any, key string) ([]workflow.Citation, error) {
	raw, ok := args[key]
	if !ok {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%q must be an array", key)
	}
	out := make([]workflow.Citation, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("citations[%d] must be an object", i)
		}
		c := workflow.Citation{
			URL:   stringFromMap(m, "url"),
			File:  stringFromMap(m, "file"),
			Lines: stringFromMap(m, "lines"),
		}
		out = append(out, c)
	}
	return out, nil
}

func stringFromMap(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// errorResult builds a tool result that surfaces a tool-side validation
// failure to the LLM. The agent sees Error in its next turn so it can
// adjust args and retry.
func errorResult(call agentic.ToolCall, msg string) agentic.ToolResult {
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: msg,
		Error:   msg,
	}
}

// truncate trims a string to maxBytes, appending "…" when truncation
// occurred. Used for log messages that may include long question text.
func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "…"
}
