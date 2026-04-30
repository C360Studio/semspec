package health

import (
	"encoding/json"
	"testing"
)

func TestJSONInText_StraightShape(t *testing.T) {
	bundle := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"{\"name\":\"graph_summary\",\"arguments\":{}}","tool_calls":[]}}}`),
		},
	}}
	got := JSONInText{}.Run(bundle)
	if len(got) != 1 {
		t.Fatalf("want 1 diagnosis, got %d: %+v", len(got), got)
	}
	if got[0].Severity != SeverityCritical {
		t.Errorf("severity = %q", got[0].Severity)
	}
	if got[0].Evidence[0].Value != "graph_summary" {
		t.Errorf("Evidence.Value = %v, want graph_summary", got[0].Evidence[0].Value)
	}
}

func TestJSONInText_LeadingPreambleStillMatches(t *testing.T) {
	// qwen2.5-coder@temp0 sometimes prefixes a sentence before the
	// JSON. Detector should still find the JSON object.
	bundle := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"Calling graph_summary now: {\"name\":\"graph_summary\"}","tool_calls":[]}}}`),
		},
	}}
	if got := (JSONInText{}).Run(bundle); len(got) != 1 {
		t.Errorf("expected 1 diagnosis with preamble, got %d: %+v", len(got), got)
	}
}

func TestJSONInText_NonStopIsSilent(t *testing.T) {
	bundle := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"tool_calls","message":{"content":"{\"name\":\"x\"}","tool_calls":[{"id":"t"}]}}}`),
		},
	}}
	if got := (JSONInText{}).Run(bundle); len(got) != 0 {
		t.Errorf("non-stop should not match, got %+v", got)
	}
}

func TestJSONInText_EmptyContentIsSilent(t *testing.T) {
	bundle := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"","tool_calls":[]}}}`),
		},
	}}
	if got := (JSONInText{}).Run(bundle); len(got) != 0 {
		t.Errorf("empty content should not match, got %+v", got)
	}
}

func TestJSONInText_PlainTextIsSilent(t *testing.T) {
	bundle := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"I have completed the analysis.","tool_calls":[]}}}`),
		},
	}}
	if got := (JSONInText{}).Run(bundle); len(got) != 0 {
		t.Errorf("plain text content should not match, got %+v", got)
	}
}

func TestJSONInText_JSONWithoutNameFieldIsSilent(t *testing.T) {
	bundle := &Bundle{Messages: []Message{
		{Sequence: 1, Subject: "agent.response.loop-A:req:r1",
			RawData: json.RawMessage(`{"payload":{"finish_reason":"stop","message":{"content":"{\"result\":\"ok\"}","tool_calls":[]}}}`),
		},
	}}
	if got := (JSONInText{}).Run(bundle); len(got) != 0 {
		t.Errorf("JSON without name field should not match, got %+v", got)
	}
}

func TestJSONObjectNameField(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantOk   bool
	}{
		{`{"name":"x"}`, "x", true},
		{`  {"name":"y"}  `, "y", true},
		{`prefix text {"name":"z"}`, "z", true},
		{`{"name":""}`, "", false},
		{`{"name":42}`, "", false},
		{`{"foo":"bar"}`, "", false},
		{``, "", false},
		{`not json`, "", false},
		{`{`, "", false},
	}
	for _, tc := range cases {
		gotName, gotOk := jsonObjectNameField(tc.in)
		if gotOk != tc.wantOk || gotName != tc.wantName {
			t.Errorf("jsonObjectNameField(%q) = (%q, %v), want (%q, %v)",
				tc.in, gotName, gotOk, tc.wantName, tc.wantOk)
		}
	}
}
