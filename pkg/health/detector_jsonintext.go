package health

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// JSONInText detects an agent.response that finished with stop and a
// non-empty content payload that parses as a JSON object containing
// a "name" field — the canonical shape of a model emitting a tool
// call as text instead of using the function-calling channel.
//
// Match conditions (all must hold):
//
//	finish_reason == "stop"
//	message.content != ""
//	message.content parses as a JSON object
//	the parsed object has a "name" field
//
// The detector intentionally does not require message.tool_calls==[].
// A model that emits both text and a real tool call is a different
// (rarer) shape, but the JSON-in-text emission alone still indicates
// the function-calling channel was bypassed and any tool the model
// "described" in text will not execute.
//
// Severity is critical: the loop will not advance because no tool is
// actually being invoked, just printed.
type JSONInText struct{}

// JSONInTextShape is the Diagnosis.Shape value for this detector.
const JSONInTextShape = "JSONInText"

// Name implements Detector.
func (JSONInText) Name() string { return JSONInTextShape }

// Run implements Detector. Pure; no I/O.
func (JSONInText) Run(b *Bundle) []Diagnosis {
	if b == nil || len(b.Messages) == 0 {
		return nil
	}
	byLoop, malformed := walkAgentResponses(b.Messages)

	type seqDiag struct {
		seq  int64
		diag Diagnosis
	}
	var matches []seqDiag
	for _, entries := range byLoop {
		for _, r := range entries {
			if !r.IsStop() || r.Content == "" {
				continue
			}
			name, ok := jsonObjectNameField(r.Content)
			if !ok {
				continue
			}
			matches = append(matches, seqDiag{
				seq: r.Sequence,
				diag: Diagnosis{
					Shape:    JSONInTextShape,
					Severity: SeverityCritical,
					Evidence: []EvidenceRef{
						{
							Kind:  EvidenceAgentResponse,
							ID:    strconv.FormatInt(r.Sequence, 10),
							Field: "message.content",
							Value: name,
						},
					},
					Remediation: "Model emitted a tool call as JSON text in message.content (looks like {\"name\": \"" + name + "\", ...}) instead of using the function-calling channel. The tool will NOT execute. On retry, reinforce the function-calling channel in the system prompt or downgrade to a model with stronger native tool support.",
				},
			})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].seq < matches[j].seq })

	out := make([]Diagnosis, 0, len(matches)+1)
	for _, m := range matches {
		out = append(out, m.diag)
	}
	if malformed > 0 {
		out = append(out, undeterminedFromMalformed(JSONInTextShape, malformed))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// jsonObjectNameField parses content as a JSON object and returns the
// "name" field if present and non-empty. Returns ("", false) when the
// content isn't a JSON object, the object lacks a "name" field, or
// the field isn't a string.
//
// Tolerant of leading/trailing whitespace and tolerates leading text
// up to the first opening brace — qwen2.5-coder@temp0 sometimes prefixes
// a single sentence ("Calling graph_summary now:") before emitting the
// JSON. We match the canonical shape; lone text without a JSON suffix
// won't match.
func jsonObjectNameField(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "{") {
		// Try to find the first { inside the content. Conservative:
		// only walk to a max of 256 chars of prefix to avoid scanning
		// pathologically long preambles.
		const maxPrefixScan = 256
		idx := strings.IndexByte(trimmed, '{')
		if idx < 0 || idx > maxPrefixScan {
			return "", false
		}
		trimmed = trimmed[idx:]
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return "", false
	}
	raw, ok := obj["name"]
	if !ok {
		return "", false
	}
	name, ok := raw.(string)
	if !ok || name == "" {
		return "", false
	}
	return name, true
}
