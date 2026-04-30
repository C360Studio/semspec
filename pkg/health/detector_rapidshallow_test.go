package health

import (
	"encoding/json"
	"fmt"
	"testing"
)

// respWithToolNames builds an agent.response message with the given
// tool-call names. Test helper.
func respWithToolNames(subjectTail string, seq int64, toolNames ...string) Message {
	type tc struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	calls := make([]tc, len(toolNames))
	for i, n := range toolNames {
		calls[i].Function.Name = n
	}
	body, _ := json.Marshal(struct {
		Payload struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				ToolCalls []tc   `json:"tool_calls"`
			} `json:"message"`
		} `json:"payload"`
	}{
		Payload: struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				ToolCalls []tc   `json:"tool_calls"`
			} `json:"message"`
		}{
			FinishReason: "tool_calls",
			Message: struct {
				Content   string `json:"content"`
				ToolCalls []tc   `json:"tool_calls"`
			}{
				ToolCalls: calls,
			},
		},
	})
	return Message{
		Sequence: seq,
		Subject:  "agent.response." + subjectTail,
		RawData:  body,
	}
}

func TestRapidShallowToolCalls_FiresAfterThreshold(t *testing.T) {
	// 6 bash calls in a row, no submit_work — should fire exactly at
	// the threshold.
	msgs := []Message{}
	for i := int64(1); i <= 6; i++ {
		msgs = append(msgs, respWithToolNames("loop-A:req:r"+fmt.Sprint(i), i, "bash"))
	}
	bundle := &Bundle{Messages: msgs}
	got := RapidShallowToolCalls{}.Run(bundle)
	if len(got) != 1 {
		t.Fatalf("want 1 diagnosis at threshold (6 tool calls), got %d: %+v", len(got), got)
	}
	if got[0].Severity != SeverityWarning {
		t.Errorf("severity = %q, want warning", got[0].Severity)
	}
	if got[0].Evidence[0].Kind != EvidenceLoopEntry {
		t.Errorf("evidence kind = %q, want %q", got[0].Evidence[0].Kind, EvidenceLoopEntry)
	}
	if got[0].Evidence[0].ID != "loop-A" {
		t.Errorf("evidence ID = %q, want loop-A", got[0].Evidence[0].ID)
	}
	if v, ok := got[0].Evidence[0].Value.(int); !ok || v != 6 {
		t.Errorf("evidence value = %v, want 6", got[0].Evidence[0].Value)
	}
}

func TestRapidShallowToolCalls_BelowThresholdSilent(t *testing.T) {
	// 5 tool calls — one below the threshold, no submit_work yet.
	// The loop is making progress; don't alert.
	msgs := []Message{}
	for i := int64(1); i <= 5; i++ {
		msgs = append(msgs, respWithToolNames("loop-A:req:r"+fmt.Sprint(i), i, "bash"))
	}
	bundle := &Bundle{Messages: msgs}
	if got := (RapidShallowToolCalls{}).Run(bundle); len(got) != 0 {
		t.Errorf("below threshold should not fire, got %+v", got)
	}
}

func TestRapidShallowToolCalls_SubmitWorkSuppresses(t *testing.T) {
	// 5 tool calls, then submit_work on the 6th — loop converged.
	// No alert even though we crossed the threshold count.
	msgs := []Message{
		respWithToolNames("loop-A:req:r1", 1, "bash"),
		respWithToolNames("loop-A:req:r2", 2, "bash"),
		respWithToolNames("loop-A:req:r3", 3, "graph_search"),
		respWithToolNames("loop-A:req:r4", 4, "bash"),
		respWithToolNames("loop-A:req:r5", 5, "graph_query"),
		respWithToolNames("loop-A:req:r6", 6, "submit_work"),
	}
	bundle := &Bundle{Messages: msgs}
	if got := (RapidShallowToolCalls{}).Run(bundle); len(got) != 0 {
		t.Errorf("submit_work suppresses; got %+v", got)
	}
}

func TestRapidShallowToolCalls_SubmitWorkOnFirstCallIsFine(t *testing.T) {
	// Trivial loop: model immediately submits. Detector must not fire.
	bundle := &Bundle{Messages: []Message{
		respWithToolNames("loop-A:req:r1", 1, "submit_work"),
	}}
	if got := (RapidShallowToolCalls{}).Run(bundle); len(got) != 0 {
		t.Errorf("immediate submit_work is fine; got %+v", got)
	}
}

func TestRapidShallowToolCalls_SubmitWorkAlongsideOthersInOneCall(t *testing.T) {
	// Some providers return multiple tool_calls in a single response;
	// if submit_work is one of them, the loop is converging — no alert.
	bundle := &Bundle{Messages: []Message{
		respWithToolNames("loop-A:req:r1", 1, "graph_search", "bash"),
		respWithToolNames("loop-A:req:r2", 2, "bash"),
		respWithToolNames("loop-A:req:r3", 3, "bash"),
		respWithToolNames("loop-A:req:r4", 4, "bash"),
		respWithToolNames("loop-A:req:r5", 5, "bash"),
		respWithToolNames("loop-A:req:r6", 6, "bash"),
		respWithToolNames("loop-A:req:r7", 7, "submit_work", "bash"),
	}}
	if got := (RapidShallowToolCalls{}).Run(bundle); len(got) != 0 {
		t.Errorf("multi-tool response containing submit_work suppresses; got %+v", got)
	}
}

func TestRapidShallowToolCalls_LoopIsolation(t *testing.T) {
	// Loop A is wedged (8 bash, no submit), Loop B is fine (2 bash +
	// submit). Detector should fire on A only.
	msgs := []Message{
		respWithToolNames("loop-A:req:r1", 1, "bash"),
		respWithToolNames("loop-A:req:r2", 2, "bash"),
		respWithToolNames("loop-A:req:r3", 3, "bash"),
		respWithToolNames("loop-A:req:r4", 4, "bash"),
		respWithToolNames("loop-A:req:r5", 5, "bash"),
		respWithToolNames("loop-A:req:r6", 6, "bash"),
		respWithToolNames("loop-A:req:r7", 7, "bash"),
		respWithToolNames("loop-A:req:r8", 8, "bash"),
		respWithToolNames("loop-B:req:r1", 100, "bash"),
		respWithToolNames("loop-B:req:r2", 101, "submit_work"),
	}
	bundle := &Bundle{Messages: msgs}
	got := RapidShallowToolCalls{}.Run(bundle)
	if len(got) != 1 {
		t.Fatalf("want 1 diagnosis (loop-A only), got %d", len(got))
	}
	if got[0].Evidence[0].ID != "loop-A" {
		t.Errorf("expected loop-A, got %q", got[0].Evidence[0].ID)
	}
}

func TestRapidShallowToolCalls_NonToolResponsesIgnored(t *testing.T) {
	// agent.responses with no tool_calls (e.g. empty stops) don't count
	// toward the threshold. EmptyStopAfterToolCalls owns that shape.
	msgs := []Message{
		// 4 tool-call responses
		respWithToolNames("loop-A:req:r1", 1, "bash"),
		respWithToolNames("loop-A:req:r2", 2, "bash"),
		respWithToolNames("loop-A:req:r3", 3, "bash"),
		respWithToolNames("loop-A:req:r4", 4, "bash"),
		// 3 non-tool-call responses (empty stops, content-only, etc.)
		{Sequence: 5, Subject: "agent.response.loop-A:req:r5",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"thinking","tool_calls":[]}}}`),
		},
		{Sequence: 6, Subject: "agent.response.loop-A:req:r6",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"more thinking","tool_calls":[]}}}`),
		},
		{Sequence: 7, Subject: "agent.response.loop-A:req:r7",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"...","tool_calls":[]}}}`),
		},
	}
	bundle := &Bundle{Messages: msgs}
	if got := (RapidShallowToolCalls{}).Run(bundle); len(got) != 0 {
		t.Errorf("non-tool responses don't count toward threshold; got %+v", got)
	}
}

func TestRapidShallowToolCalls_MalformedSurfacesUndetermined(t *testing.T) {
	// Malformed agent.response RawData should bump malformed-count and
	// surface a SeverityUndetermined diagnosis distinct from the
	// "found nothing" path.
	msgs := []Message{
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{not json`),
		},
	}
	bundle := &Bundle{Messages: msgs}
	got := RapidShallowToolCalls{}.Run(bundle)
	var sawUndetermined bool
	for _, d := range got {
		if d.Severity == SeverityUndetermined {
			sawUndetermined = true
		}
	}
	if !sawUndetermined {
		t.Errorf("malformed entry should surface SeverityUndetermined; got %+v", got)
	}
}

func TestRapidShallowToolCalls_RunsViaRunAll(t *testing.T) {
	// Belt-and-braces: confirm the detector slots into the standard
	// RunAll machinery without surprises.
	msgs := []Message{}
	for i := int64(1); i <= 7; i++ {
		msgs = append(msgs, respWithToolNames("loop-A:req:r"+fmt.Sprint(i), i, "bash"))
	}
	bundle := &Bundle{Messages: msgs}
	RunAll(bundle, []Detector{RapidShallowToolCalls{}})
	if len(bundle.Diagnoses) != 1 {
		t.Errorf("RunAll: expected 1 diagnosis, got %d", len(bundle.Diagnoses))
	}
}
