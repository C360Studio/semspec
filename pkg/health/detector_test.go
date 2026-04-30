package health

import (
	"testing"
)

// stubDetector is a test-only Detector. It records whether Run was
// called and emits a configurable diagnosis list.
type stubDetector struct {
	name      string
	diagnoses []Diagnosis
	called    bool
}

func (s *stubDetector) Name() string { return s.name }

func (s *stubDetector) Run(_ *Bundle) []Diagnosis {
	s.called = true
	return s.diagnoses
}

func TestRunAll_AppendsDiagnosesPreservingOrder(t *testing.T) {
	bundle := &Bundle{}
	d1 := &stubDetector{
		name: "first",
		diagnoses: []Diagnosis{
			{Shape: "first-A", Severity: SeverityWarning},
			{Shape: "first-B", Severity: SeverityCritical},
		},
	}
	d2 := &stubDetector{
		name:      "second",
		diagnoses: []Diagnosis{{Shape: "second-A", Severity: SeverityInfo}},
	}

	RunAll(bundle, []Detector{d1, d2})

	if !d1.called || !d2.called {
		t.Fatalf("both detectors should have been called: d1=%v d2=%v", d1.called, d2.called)
	}
	if len(bundle.Diagnoses) != 3 {
		t.Fatalf("len(diagnoses) = %d, want 3", len(bundle.Diagnoses))
	}
	want := []string{"first-A", "first-B", "second-A"}
	for i, got := range bundle.Diagnoses {
		if got.Shape != want[i] {
			t.Errorf("diagnoses[%d].Shape = %q, want %q", i, got.Shape, want[i])
		}
	}
}

func TestRunAll_NilBundleIsNoop(t *testing.T) {
	d := &stubDetector{name: "x"}
	RunAll(nil, []Detector{d})
	if d.called {
		t.Error("detector should not have been called against nil bundle")
	}
}

func TestRunAll_NilDetectorSkipped(t *testing.T) {
	// Defensive: callers shouldn't pass nil detectors, but if they do,
	// the loop should skip rather than panic.
	bundle := &Bundle{}
	d := &stubDetector{
		name:      "real",
		diagnoses: []Diagnosis{{Shape: "X", Severity: SeverityInfo}},
	}
	RunAll(bundle, []Detector{nil, d, nil})
	if !d.called {
		t.Error("real detector skipped past nil neighbours")
	}
	if len(bundle.Diagnoses) != 1 {
		t.Errorf("len(diagnoses) = %d, want 1", len(bundle.Diagnoses))
	}
}

func TestRunAll_DetectorReturningNilIsValid(t *testing.T) {
	// Per Detector interface doc: nil and empty slice are equivalent
	// "found nothing" sentinels.
	bundle := &Bundle{}
	clean := &stubDetector{name: "clean", diagnoses: nil}
	dirty := &stubDetector{name: "dirty", diagnoses: []Diagnosis{{Shape: "Y"}}}

	RunAll(bundle, []Detector{clean, dirty})
	if !clean.called {
		t.Error("clean detector should still have been called")
	}
	if len(bundle.Diagnoses) != 1 || bundle.Diagnoses[0].Shape != "Y" {
		t.Errorf("expected only the dirty detector's diagnosis, got %+v", bundle.Diagnoses)
	}
}

func TestSeverityValues(t *testing.T) {
	// Pin the severity strings — they're part of the bundle contract.
	cases := map[Severity]string{
		SeverityInfo:     "info",
		SeverityWarning:  "warning",
		SeverityCritical: "critical",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("severity %v = %q, want %q", got, string(got), want)
		}
	}
}

func TestEvidenceKindValues(t *testing.T) {
	// Same pin for evidence-kind strings — bundle readers may
	// switch on these.
	cases := map[EvidenceKind]string{
		EvidenceAgentResponse: "agent_response",
		EvidenceAgentRequest:  "agent_request",
		EvidenceMetricSample:  "metric_sample",
		EvidenceLoopEntry:     "loop_entry",
		EvidenceLogLine:       "log_line",
		EvidencePlanState:     "plan_state",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("evidence kind %v = %q, want %q", got, string(got), want)
		}
	}
}
