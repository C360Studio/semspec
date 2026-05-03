package health

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// GraphToolFailure detects graph_search / graph_query / graph_summary tool
// invocations that returned a payload error. The Semantic Knowledge Graph
// (SKG) is table stakes for semspec — every plan-phase agent uses graph
// tools to ground its understanding of the workspace, and a SKG that
// silently returns errors makes the agent operate on partial information.
// The 2026-05-03 v3 regression caught this: every graph_search call
// returned EOF on http://localhost:8080/graph-gateway/graphql for the
// entire run while the watch sidecar reported errors=0, because no
// detector was looking at tool.result payloads.
//
// Match conditions: a message-logger entry with
//
//	subject starts with "tool.result"
//	raw_data.payload.error is a non-empty string
//	the call_id resolves to a graph tool (graph_search / graph_query /
//	  graph_summary) — we get this either from the result payload's name
//	  field, or by looking up the matching tool.execute.* call dispatch.
//
// Severity is critical: SKG failure is a hard infrastructure problem the
// operator must fix before another paid run; emitting warning would let
// it slide into the noise.
type GraphToolFailure struct{}

// GraphToolFailureShape is the Diagnosis.Shape value for this detector.
const GraphToolFailureShape = "GraphToolFailure"

// Name implements Detector.
func (GraphToolFailure) Name() string { return GraphToolFailureShape }

// graphToolNames is the set of tool names whose result errors the
// detector treats as SKG breakage. Other tools have their own failure
// classes (bash exit codes, http_request transport errors); those are
// expected to surface via other detectors or remediation paths.
var graphToolNames = map[string]bool{
	"graph_search":  true,
	"graph_query":   true,
	"graph_summary": true,
}

// Run implements Detector. Pure; no I/O.
func (GraphToolFailure) Run(b *Bundle) []Diagnosis {
	if b == nil || len(b.Messages) == 0 {
		return nil
	}

	// Pre-pass: tool.execute.<tool_name> dispatches carry the tool name
	// and its call_id, so we can map call_id → tool name for the matching
	// tool.result.<call_id> entry. Tool results don't always carry the
	// name in the payload directly (depends on the producer).
	callIDToName := make(map[string]string)
	for _, m := range b.Messages {
		if !strings.HasPrefix(m.Subject, "tool.execute.") {
			continue
		}
		var env struct {
			Payload struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(m.RawData, &env); err != nil {
			continue
		}
		if env.Payload.ID != "" && env.Payload.Name != "" {
			callIDToName[env.Payload.ID] = env.Payload.Name
		}
	}

	type seqDiag struct {
		seq  int64
		diag Diagnosis
	}
	var matches []seqDiag
	malformed := 0

	for _, m := range b.Messages {
		if !strings.HasPrefix(m.Subject, "tool.result") {
			continue
		}
		var env struct {
			Payload struct {
				CallID string `json:"call_id"`
				Name   string `json:"name"`
				Error  string `json:"error"`
				LoopID string `json:"loop_id"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(m.RawData, &env); err != nil {
			malformed++
			continue
		}
		if env.Payload.Error == "" {
			continue
		}
		toolName := env.Payload.Name
		if toolName == "" {
			toolName = callIDToName[env.Payload.CallID]
		}
		if !graphToolNames[toolName] {
			continue
		}

		matches = append(matches, seqDiag{
			seq: m.Sequence,
			diag: Diagnosis{
				Shape:    GraphToolFailureShape,
				Severity: SeverityCritical,
				Evidence: []EvidenceRef{
					{
						Kind:  EvidenceLogLine,
						ID:    strconv.FormatInt(m.Sequence, 10),
						Field: "payload.error",
						Value: env.Payload.Error,
					},
					{
						Kind:  EvidenceLoopEntry,
						ID:    env.Payload.LoopID,
						Field: "tool",
						Value: toolName,
					},
				},
				Remediation: "Graph tool " + toolName + " returned an error — the Semantic Knowledge Graph is broken or unreachable. Check semspec→graph-gateway connectivity (URL/port mismatches between local registration and the actual listener), seminstruct-summary container health for answer_synthesis timeouts, and semsource indexing status. Every plan-phase agent depends on graph tools; running another paid LLM job against a broken SKG is wasted spend.",
				MemoryRef:   "feedback_active_monitoring_means_polling.md",
			},
		})
	}

	sort.Slice(matches, func(i, j int) bool { return matches[i].seq < matches[j].seq })

	out := make([]Diagnosis, 0, len(matches)+1)
	for _, m := range matches {
		out = append(out, m.diag)
	}
	if malformed > 0 {
		out = append(out, undeterminedFromMalformed(GraphToolFailureShape, malformed))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
