package health

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBundle_RoundTripJSON(t *testing.T) {
	in := Bundle{
		Bundle: BundleMeta{
			Format:     BundleFormat,
			CapturedAt: time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC),
			CapturedBy: "semspec-test",
			Redactions: []string{"api_key_env", "auth_headers"},
		},
		Host: HostInfo{
			OS:                "darwin",
			Arch:              "arm64",
			SemspecVersion:    "v0.0.0-test",
			SemstreamsVersion: "beta.24",
			Ollama: &OllamaHostInfo{
				Version: "0.5.0",
			},
		},
		Config: ConfigSnapshot{
			ActiveCapabilities: map[string]string{"reviewing": "qwen3-14b"},
			RedactedEndpoints:  []string{"claude-opus"},
		},
		Plans: []KVEntry{
			{
				Key:      "abc",
				Revision: 7,
				Created:  time.Date(2026, 4, 30, 13, 50, 0, 0, time.UTC),
				Value:    json.RawMessage(`{"slug":"abc","status":"complete"}`),
			},
		},
		Loops: []KVEntry{
			{
				Key:      "loop-1",
				Revision: 3,
				Created:  time.Date(2026, 4, 30, 13, 51, 0, 0, time.UTC),
				Value:    json.RawMessage(`{"id":"loop-1","state":"complete"}`),
			},
		},
		Messages: []Message{
			{
				Sequence:    42,
				Timestamp:   time.Date(2026, 4, 30, 13, 59, 0, 0, time.UTC),
				Subject:     "agent.response.foo",
				MessageType: "agentic.response.v1",
				TraceID:     "trace-123",
				RawData:     json.RawMessage(`{"payload":{"finish_reason":"stop"}}`),
			},
		},
		Metrics: MetricsSnapshot{
			LoopActiveLoops:        7,
			LoopContextUtilization: 0.42,
			ModelRequestsTotal:     31,
			CapturedAt:             time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC),
		},
		Diagnoses: []Diagnosis{
			{
				Shape:    "TestShape",
				Severity: SeverityWarning,
				Evidence: []EvidenceRef{
					{Kind: EvidenceAgentResponse, ID: "42", Field: "finish_reason", Value: "stop"},
				},
				Remediation: "Do X",
				MemoryRef:   "feedback_test.md",
			},
		},
		TrajectoryRefs: []TrajectoryRef{
			{LoopID: "loop-1", Filename: "trajectories/loop-1.json", Steps: 16, Outcome: "success"},
		},
	}

	data, err := json.Marshal(&in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out Bundle
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Spot-check load-bearing fields rather than DeepEqual — json.RawMessage
	// equality requires byte-level match which formatting can disturb.
	if out.Bundle.Format != BundleFormat {
		t.Errorf("Bundle.Format = %q, want %q", out.Bundle.Format, BundleFormat)
	}
	if !out.Bundle.CapturedAt.Equal(in.Bundle.CapturedAt) {
		t.Errorf("CapturedAt round-trip mismatch")
	}
	if out.Host.OS != "darwin" || out.Host.Arch != "arm64" {
		t.Errorf("Host basic fields lost: %+v", out.Host)
	}
	if out.Host.Ollama == nil || out.Host.Ollama.Version != "0.5.0" {
		t.Errorf("Ollama optional struct lost")
	}
	if out.Config.ActiveCapabilities["reviewing"] != "qwen3-14b" {
		t.Errorf("ConfigSnapshot map lost")
	}
	if len(out.Plans) != 1 || out.Plans[0].Key != "abc" || out.Plans[0].Revision != 7 {
		t.Errorf("Plans envelope lost: %+v", out.Plans)
	}
	if len(out.Loops) != 1 || out.Loops[0].Key != "loop-1" || out.Loops[0].Revision != 3 {
		t.Errorf("Loops envelope lost: %+v", out.Loops)
	}
	if len(out.Messages) != 1 || out.Messages[0].Sequence != 42 {
		t.Errorf("Messages lost")
	}
	if out.Metrics.LoopActiveLoops != 7 || out.Metrics.LoopContextUtilization != 0.42 {
		t.Errorf("Metrics fields lost")
	}
	if len(out.Diagnoses) != 1 || out.Diagnoses[0].Shape != "TestShape" {
		t.Errorf("Diagnoses lost")
	}
	if len(out.TrajectoryRefs) != 1 || out.TrajectoryRefs[0].LoopID != "loop-1" {
		t.Errorf("TrajectoryRefs lost")
	}
}

func TestBundle_OmitsOptionalFields(t *testing.T) {
	// A minimal bundle with no Ollama, no trajectory refs should not
	// emit those keys — adopters running cloud LLMs shouldn't see
	// confusing empty Ollama sections in their bundle.
	in := Bundle{
		Bundle:    BundleMeta{Format: BundleFormat, CapturedAt: time.Now().UTC(), CapturedBy: "test"},
		Diagnoses: []Diagnosis{},
		Plans:     []KVEntry{},
		Loops:     []KVEntry{},
		Messages:  []Message{},
	}
	data, err := json.Marshal(&in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	if strings.Contains(s, `"ollama":`) {
		t.Errorf("ollama field should be omitted when nil, got: %s", s)
	}
	if strings.Contains(s, `"trajectory_refs":`) {
		t.Errorf("trajectory_refs should be omitted when empty/nil, got: %s", s)
	}
}

func TestBundleFormat_IsV1(t *testing.T) {
	// The format constant is the load-bearing bundle contract. Any
	// future change to it ships as v2 with a parallel writer; this
	// test pins the current value so a typo can't slip the contract.
	if BundleFormat != "v1" {
		t.Errorf("BundleFormat = %q, want %q", BundleFormat, "v1")
	}
}
