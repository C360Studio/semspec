package health

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestThinkingSpiral_AboveThresholdMatches(t *testing.T) {
	// completion_tokens=915 is the v107-rerun fixture's per-attempt
	// shape; well above the 500-token threshold. Pin that value
	// since it's the canonical shape this detector was named for.
	bundle := &Bundle{Messages: []Message{
		spiralResp("loop-A:req:r1", 1, 915),
	}}
	got := ThinkingSpiral{}.Run(bundle)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d: %+v", len(got), got)
	}
	if got[0].Severity != SeverityWarning {
		t.Errorf("severity = %q", got[0].Severity)
	}
	if v, ok := got[0].Evidence[0].Value.(int); !ok || v != 915 {
		t.Errorf("Evidence.Value = %v, want 915", got[0].Evidence[0].Value)
	}
}

func TestThinkingSpiral_BelowThresholdIsSilent(t *testing.T) {
	bundle := &Bundle{Messages: []Message{
		spiralResp("loop-A:req:r1", 1, 100),
	}}
	if got := (ThinkingSpiral{}).Run(bundle); len(got) != 0 {
		t.Errorf("below threshold should not match, got %+v", got)
	}
}

func TestThinkingSpiral_AtThresholdIsSilent(t *testing.T) {
	// The check is strictly > 500, not >=, so the boundary itself
	// doesn't fire. Pin that shape.
	bundle := &Bundle{Messages: []Message{
		spiralResp("loop-A:req:r1", 1, 500),
	}}
	if got := (ThinkingSpiral{}).Run(bundle); len(got) != 0 {
		t.Errorf("threshold (=500) should not match (>500 only), got %+v", got)
	}
}

func TestThinkingSpiral_NotStopIsSilent(t *testing.T) {
	bundle := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"length","message":{"content":"","tool_calls":[]},"usage":{"completion_tokens":900}}}`),
		},
	}}
	if got := (ThinkingSpiral{}).Run(bundle); len(got) != 0 {
		t.Errorf("non-stop finish should not match, got %+v", got)
	}
}

func TestThinkingSpiral_HasToolCallsIsSilent(t *testing.T) {
	// A model that did call a tool is not in a thinking spiral, even
	// if it also burned tokens. Different shape.
	bundle := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"","tool_calls":[{"id":"t"}]},"usage":{"completion_tokens":900}}}`),
		},
	}}
	if got := (ThinkingSpiral{}).Run(bundle); len(got) != 0 {
		t.Errorf("with tool_calls should not match, got %+v", got)
	}
}

func TestThinkingSpiral_NumericSeqOrder(t *testing.T) {
	// Same lex-vs-numeric sort regression class as
	// EmptyStopAfterToolCalls. Pin numeric ordering.
	bundle := &Bundle{Messages: []Message{
		spiralResp("loop-A:req:r1", 99, 800),
		spiralResp("loop-A:req:r2", 1000, 800),
		spiralResp("loop-A:req:r3", 9, 800),
	}}
	got := ThinkingSpiral{}.Run(bundle)
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	wantSeqs := []string{"9", "99", "1000"}
	for i, want := range wantSeqs {
		if got[i].Evidence[0].ID != want {
			t.Errorf("got[%d].ID = %q, want %q", i, got[i].Evidence[0].ID, want)
		}
	}
}

func TestThinkingSpiral_RemediationIncludesObservedAndThreshold(t *testing.T) {
	// The remediation string shows the operator both numbers so
	// they can decide whether the threshold is right for their model.
	bundle := &Bundle{Messages: []Message{
		spiralResp("loop-A:req:r1", 1, 715),
	}}
	got := ThinkingSpiral{}.Run(bundle)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if !strings.Contains(got[0].Remediation, "715") || !strings.Contains(got[0].Remediation, "500") {
		t.Errorf("remediation should cite observed (715) and threshold (500): %q", got[0].Remediation)
	}
}

// spiralResp builds an agent.response with finish_reason=stop, no
// tool_calls, and the given completion_tokens. Test helper.
func spiralResp(subjectTail string, seq int64, completionTokens int) Message {
	body := fmt.Sprintf(
		`{"payload":{"finish_reason":"stop","message":{"content":"","tool_calls":[]},"usage":{"completion_tokens":%d}}}`,
		completionTokens,
	)
	return Message{
		Sequence: seq,
		Subject:  "agent.response." + subjectTail,
		RawData:  json.RawMessage(body),
	}
}
