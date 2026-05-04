package lessondecomposer

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestNewComponent_AppliesDefaults(t *testing.T) {
	deps := component.Dependencies{Logger: discardLogger()}
	got, err := NewComponent(json.RawMessage(`{}`), deps)
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	c, ok := got.(*Component)
	if !ok {
		t.Fatalf("expected *Component, got %T", got)
	}
	if c.config.StreamName != "WORKFLOW" {
		t.Errorf("StreamName default = %q, want WORKFLOW", c.config.StreamName)
	}
	if c.config.ConsumerName != "lesson-decomposer" {
		t.Errorf("ConsumerName default = %q, want lesson-decomposer", c.config.ConsumerName)
	}
	if c.config.FilterSubject != "workflow.events.lesson.decompose.requested.>" {
		t.Errorf("FilterSubject default = %q", c.config.FilterSubject)
	}
}

func TestNewComponent_PreservesExplicitConfig(t *testing.T) {
	raw := json.RawMessage(`{
		"enabled": false,
		"stream_name": "ALT",
		"consumer_name": "custom",
		"filter_subject": "alt.subject"
	}`)
	got, err := NewComponent(raw, component.Dependencies{Logger: discardLogger()})
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	c := got.(*Component)
	if c.config.Enabled {
		t.Error("Enabled should be false from config")
	}
	if c.config.StreamName != "ALT" {
		t.Errorf("StreamName = %q, want ALT", c.config.StreamName)
	}
	if c.config.ConsumerName != "custom" {
		t.Errorf("ConsumerName = %q", c.config.ConsumerName)
	}
	if c.config.FilterSubject != "alt.subject" {
		t.Errorf("FilterSubject = %q", c.config.FilterSubject)
	}
}

func TestComponent_MetaDescribesPhase(t *testing.T) {
	c := &Component{name: "lesson-decomposer"}
	meta := c.Meta()
	if meta.Name != "lesson-decomposer" {
		t.Errorf("Meta.Name = %q", meta.Name)
	}
	if meta.Type != "processor" {
		t.Errorf("Meta.Type = %q", meta.Type)
	}
}

func TestComponent_HealthReportsStoppedBeforeStart(t *testing.T) {
	c := &Component{}
	h := c.Health()
	if h.Healthy {
		t.Error("Health should not be healthy before Start")
	}
	if h.Status != "stopped" {
		t.Errorf("Health.Status = %q, want stopped", h.Status)
	}
}

func TestComponent_InputPortsExposesFilterSubject(t *testing.T) {
	c := &Component{
		config: Config{FilterSubject: "test.subject"},
	}
	ports := c.InputPorts()
	if len(ports) != 1 {
		t.Fatalf("expected 1 input port, got %d", len(ports))
	}
	if ports[0].Direction != component.DirectionInput {
		t.Errorf("Direction = %v", ports[0].Direction)
	}
	natsPort, ok := ports[0].Config.(component.NATSPort)
	if !ok {
		t.Fatalf("Config is %T, want NATSPort", ports[0].Config)
	}
	if natsPort.Subject != "test.subject" {
		t.Errorf("Subject = %q, want test.subject", natsPort.Subject)
	}
}

func TestConfigValidate_RejectsEmptyFields(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing stream", Config{ConsumerName: "x", FilterSubject: "y"}},
		{"missing consumer", Config{StreamName: "x", FilterSubject: "y"}},
		{"missing filter", Config{StreamName: "x", ConsumerName: "y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestRegister_NilRegistryReturnsError(t *testing.T) {
	if err := Register(nil); err == nil {
		t.Error("Register(nil) should return error")
	}
}

// ADR-035 audit B.3 — pin the per-reason counter discrimination so the
// named-quirks list (audit sites A.1/A.3/D.6) can read accurate
// per-failure-class rates from operators' counters and structured logs.
// Each test seeds an in-flight dispatch, calls handleLoopCompletion with
// a Result that triggers exactly one rejection class, and asserts only
// that class's counter incremented.

// newTestComponent returns a Component that's safe to invoke
// handleLoopCompletion on without standing up NATS or the prompt
// assembler. Only the fields the rejection path reads need to be live:
// logger, inFlight, and the rejection CounterVec + handles. The
// CounterVec is constructed (not registered with any
// MetricsRegistry) so .Inc() works and testutil.ToFloat64 reads
// return real values without panicking. lessonWriter is left nil
// so successful parses short-circuit on the wiring check —
// these tests only exercise the rejection branches.
//
// Each call returns a Component with a fresh CounterVec so tests
// don't share counter state across the table. The ADR-035
// migration moved this state from per-Component atomic.Int64 to
// prometheus.Counter handles; the prior atomic.Int64 fields were
// reset by struct construction, so a fresh-CounterVec-per-test
// preserves the same isolation.
func newTestComponent() *Component {
	rejectionsCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "semspec_lesson_decomposer_rejections_total_test",
			Help: "Test-only Component-local CounterVec; not registered.",
		},
		[]string{"reason"},
	)
	return &Component{
		logger:                    discardLogger(),
		inFlight:                  make(map[string]*inFlightDispatch),
		rejectionsCounter:         rejectionsCounter,
		parseErrorRejections:      rejectionsCounter.WithLabelValues(string(rejectionParseError)),
		missingFieldsRejections:   rejectionsCounter.WithLabelValues(string(rejectionMissingFields)),
		missingEvidenceRejections: rejectionsCounter.WithLabelValues(string(rejectionMissingEvidence)),
		emptyEvidenceRejections:   rejectionsCounter.WithLabelValues(string(rejectionEmptyEvidence)),
	}
}

func TestHandleLoopCompletion_ParseFailure_IncrementsParseErrorCounter(t *testing.T) {
	c := newTestComponent()
	c.trackInFlight("task-pe", &payloads.LessonDecomposeRequested{Slug: "p"}, "test-model")

	loop := &agentic.LoopEntity{
		ID:      "loop-pe",
		TaskID:  "task-pe",
		Outcome: agentic.OutcomeSuccess,
		Result:  "this is not json at all",
	}
	c.handleLoopCompletion(context.Background(), loop)

	if got := int64(testutil.ToFloat64(c.parseErrorRejections)); got != 1 {
		t.Errorf("parseErrorRejections = %d, want 1", got)
	}
	assertOnlyCounter(t, c, "parseErrorRejections")
}

func TestHandleLoopCompletion_BuildLessonMissingFields_IncrementsMissingFieldsCounter(t *testing.T) {
	c := newTestComponent()
	c.trackInFlight("task-mf", &payloads.LessonDecomposeRequested{Slug: "p"}, "test-model")

	// Valid JSON but summary/detail/injection_form all empty — buildLesson
	// rejects with errLessonMissingFields.
	loop := &agentic.LoopEntity{
		ID:      "loop-mf",
		TaskID:  "task-mf",
		Outcome: agentic.OutcomeSuccess,
		Result:  `{"category_ids":["x"],"root_cause_role":"developer"}`,
	}
	c.handleLoopCompletion(context.Background(), loop)

	if got := int64(testutil.ToFloat64(c.missingFieldsRejections)); got != 1 {
		t.Errorf("missingFieldsRejections = %d, want 1", got)
	}
	assertOnlyCounter(t, c, "missingFieldsRejections")
}

func TestHandleLoopCompletion_BuildLessonMissingEvidence_IncrementsMissingEvidenceCounter(t *testing.T) {
	c := newTestComponent()
	c.trackInFlight("task-me", &payloads.LessonDecomposeRequested{Slug: "p"}, "test-model")

	// Required fields present but evidence_steps and evidence_files both
	// absent — buildLesson rejects with errLessonNoEvidence.
	loop := &agentic.LoopEntity{
		ID:      "loop-me",
		TaskID:  "task-me",
		Outcome: agentic.OutcomeSuccess,
		Result:  `{"summary":"x","detail":"y","injection_form":"z","root_cause_role":"developer"}`,
	}
	c.handleLoopCompletion(context.Background(), loop)

	if got := int64(testutil.ToFloat64(c.missingEvidenceRejections)); got != 1 {
		t.Errorf("missingEvidenceRejections = %d, want 1", got)
	}
	assertOnlyCounter(t, c, "missingEvidenceRejections")
}

func TestHandleLoopCompletion_BuildLessonEmptyEvidence_IncrementsEmptyEvidenceCounter(t *testing.T) {
	c := newTestComponent()
	c.trackInFlight("task-ee", &payloads.LessonDecomposeRequested{Slug: "p"}, "test-model")

	// Evidence arrays present but every entry is blank — sanitisation
	// drops them all and buildLesson rejects with errLessonEmptyEvidence.
	loop := &agentic.LoopEntity{
		ID:      "loop-ee",
		TaskID:  "task-ee",
		Outcome: agentic.OutcomeSuccess,
		Result:  `{"summary":"x","detail":"y","injection_form":"z","root_cause_role":"developer","evidence_steps":[{"loop_id":"","step_index":0}],"evidence_files":[{"path":" "}]}`,
	}
	c.handleLoopCompletion(context.Background(), loop)

	if got := int64(testutil.ToFloat64(c.emptyEvidenceRejections)); got != 1 {
		t.Errorf("emptyEvidenceRejections = %d, want 1", got)
	}
	assertOnlyCounter(t, c, "emptyEvidenceRejections")
}

// assertOnlyCounter pins that a specific rejection counter is the ONLY
// one incremented — protects against a future refactor accidentally
// double-counting or miscategorizing a failure class.
func assertOnlyCounter(t *testing.T, c *Component, expected string) {
	t.Helper()
	counters := map[string]int64{
		"parseErrorRejections":      int64(testutil.ToFloat64(c.parseErrorRejections)),
		"missingFieldsRejections":   int64(testutil.ToFloat64(c.missingFieldsRejections)),
		"missingEvidenceRejections": int64(testutil.ToFloat64(c.missingEvidenceRejections)),
		"emptyEvidenceRejections":   int64(testutil.ToFloat64(c.emptyEvidenceRejections)),
	}
	for name, val := range counters {
		if name == expected {
			continue
		}
		if val != 0 {
			t.Errorf("counter %s = %d, want 0 (only %s should fire)", name, val, expected)
		}
	}
}

func TestClassifyBuildLessonError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want rejectionReason
	}{
		{"unrecognized error falls back to missing_fields", errors.New("not a sentinel error"), rejectionMissingFields},
		{"explicit missing fields", errLessonMissingFields, rejectionMissingFields},
		{"nil result", errLessonNilResult, rejectionMissingFields},
		{"no evidence", errLessonNoEvidence, rejectionMissingEvidence},
		{"empty evidence", errLessonEmptyEvidence, rejectionEmptyEvidence},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyBuildLessonError(tt.err)
			if got != tt.want {
				t.Errorf("classifyBuildLessonError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestRejectionRawHead_TruncatesAtBoundary(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
	}{
		{"short", "abc", 3},
		{"exactly cap", strings.Repeat("x", rejectionRawHeadBytes), rejectionRawHeadBytes},
		{"over cap", strings.Repeat("x", rejectionRawHeadBytes*2), rejectionRawHeadBytes},
		{"empty", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectionRawHead(tt.input)
			if len(got) != tt.wantLen {
				t.Errorf("rejectionRawHead len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}
