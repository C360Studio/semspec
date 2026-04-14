// Package changeproposalhandler provides unit tests for the change-proposal-handler component.
//
// Test coverage:
//   - Config defaults and validation
//   - Config.GetTimeout fallback behaviour
//   - handleMessage: malformed envelope, malformed payload, missing required fields
//   - handleCascadeRequest: nil NATS client guard
//   - Component metadata, health, and port definitions
package changeproposalhandler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/message"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// ---------------------------------------------------------------------------
// mockMsg — minimal jetstream.Msg implementation for unit tests
// ---------------------------------------------------------------------------

// mockMsg satisfies jetstream.Msg so handleMessage can be exercised without NATS.
type mockMsg struct {
	mu     sync.Mutex
	data   []byte
	acked  bool
	naked  bool
	termed bool
}

func newMockMsg(data []byte) *mockMsg { return &mockMsg{data: data} }

func (m *mockMsg) Data() []byte { return m.data }
func (m *mockMsg) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked = true
	return nil
}
func (m *mockMsg) Nak() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.naked = true
	return nil
}
func (m *mockMsg) Term() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.termed = true
	return nil
}

// The remaining jetstream.Msg methods are no-ops to satisfy the interface.
func (m *mockMsg) NakWithDelay(_ time.Duration) error        { return nil }
func (m *mockMsg) InProgress() error                         { return nil }
func (m *mockMsg) DoubleAck(_ context.Context) error         { return nil }
func (m *mockMsg) TermWithReason(_ string) error             { return nil }
func (m *mockMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *mockMsg) Headers() nats.Header                      { return nil }
func (m *mockMsg) Subject() string {
	return "workflow.trigger.change-proposal-cascade"
}
func (m *mockMsg) Reply() string { return "" }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildValidCascadeMsg wraps a valid ChangeProposalCascadeRequest in a BaseMessage
// and returns the marshalled bytes.
func buildValidCascadeMsg(t *testing.T, req *payloads.ChangeProposalCascadeRequest) []byte {
	t.Helper()
	baseMsg := message.NewBaseMessage(req.Schema(), req, "test")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		t.Fatalf("buildValidCascadeMsg: %v", err)
	}
	return data
}

// buildRawEnvelope builds a JSON envelope with the given raw payload bytes.
// Use this for constructing intentionally malformed payloads.
func buildRawEnvelope(payload string) []byte {
	return []byte(fmt.Sprintf(`{"payload":%s}`, payload))
}

// newTestComponent returns a Component with defaults and the supplied repoRoot — no NATS.
func newTestComponent(repoRoot string) *Component {
	cfg := DefaultConfig()
	return &Component{
		name:     "change-proposal-handler",
		config:   cfg,
		logger:   slog.Default(),
		repoRoot: repoRoot,
		// natsClient intentionally nil — not needed for parse/validate unit tests
	}
}

// ---------------------------------------------------------------------------
// Config tests
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.StreamName == "" {
		t.Error("DefaultConfig().StreamName must not be empty")
	}
	if cfg.ConsumerName == "" {
		t.Error("DefaultConfig().ConsumerName must not be empty")
	}
	if cfg.TriggerSubject == "" {
		t.Error("DefaultConfig().TriggerSubject must not be empty")
	}
	if cfg.AcceptedSubject == "" {
		t.Error("DefaultConfig().AcceptedSubject must not be empty")
	}
	if cfg.TimeoutSeconds <= 0 {
		t.Errorf("DefaultConfig().TimeoutSeconds = %d, want > 0", cfg.TimeoutSeconds)
	}
	if cfg.Ports == nil {
		t.Error("DefaultConfig().Ports must not be nil")
	}
	if len(cfg.Ports.Inputs) == 0 {
		t.Error("DefaultConfig().Ports.Inputs must have at least one entry")
	}
	if len(cfg.Ports.Outputs) == 0 {
		t.Error("DefaultConfig().Ports.Outputs must have at least one entry")
	}
}

func TestConfig_Validate(t *testing.T) {
	base := DefaultConfig()

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid default config",
			mutate:  func(_ *Config) {},
			wantErr: false,
		},
		{
			name:    "missing stream_name",
			mutate:  func(c *Config) { c.StreamName = "" },
			wantErr: true,
		},
		{
			name:    "missing consumer_name",
			mutate:  func(c *Config) { c.ConsumerName = "" },
			wantErr: true,
		},
		{
			name:    "missing trigger_subject",
			mutate:  func(c *Config) { c.TriggerSubject = "" },
			wantErr: true,
		},
		{
			name:    "missing accepted_subject",
			mutate:  func(c *Config) { c.AcceptedSubject = "" },
			wantErr: true,
		},
		{
			name:    "timeout_seconds too low",
			mutate:  func(c *Config) { c.TimeoutSeconds = 9 },
			wantErr: true,
		},
		{
			name:    "timeout_seconds at minimum",
			mutate:  func(c *Config) { c.TimeoutSeconds = 10 },
			wantErr: false,
		},
		{
			name:    "timeout_seconds too high",
			mutate:  func(c *Config) { c.TimeoutSeconds = 601 },
			wantErr: true,
		},
		{
			name:    "timeout_seconds at maximum",
			mutate:  func(c *Config) { c.TimeoutSeconds = 600 },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base // copy
			tt.mutate(&cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_GetTimeout(t *testing.T) {
	t.Run("uses TimeoutSeconds when positive", func(t *testing.T) {
		cfg := Config{TimeoutSeconds: 30}
		got := cfg.GetTimeout()
		if got != 30*time.Second {
			t.Errorf("GetTimeout() = %v, want 30s", got)
		}
	})

	t.Run("falls back to 120s when TimeoutSeconds is zero", func(t *testing.T) {
		cfg := Config{TimeoutSeconds: 0}
		got := cfg.GetTimeout()
		if got != 120*time.Second {
			t.Errorf("GetTimeout() = %v, want 120s fallback", got)
		}
	})

	t.Run("falls back to 120s when TimeoutSeconds is negative", func(t *testing.T) {
		cfg := Config{TimeoutSeconds: -1}
		got := cfg.GetTimeout()
		if got != 120*time.Second {
			t.Errorf("GetTimeout() = %v, want 120s fallback", got)
		}
	})
}

// ---------------------------------------------------------------------------
// handleMessage — parse / validate path
// ---------------------------------------------------------------------------

func TestHandleMessage_MalformedEnvelope(t *testing.T) {
	c := newTestComponent(t.TempDir())
	msg := newMockMsg([]byte(`{not valid json`))

	c.handleMessage(context.Background(), msg)

	if !msg.termed {
		t.Errorf("expected Term() for malformed JSON envelope, acked=%v naked=%v termed=%v",
			msg.acked, msg.naked, msg.termed)
	}
}

func TestHandleMessage_MalformedPayload(t *testing.T) {
	c := newTestComponent(t.TempDir())

	// Valid envelope structure but the payload field is a JSON string, not an object.
	// json.Unmarshal into ChangeProposalCascadeRequest will fail.
	data := buildRawEnvelope(`"not-an-object"`)
	msg := newMockMsg(data)

	c.handleMessage(context.Background(), msg)

	if !msg.termed {
		t.Errorf("expected Term() for malformed payload, acked=%v naked=%v termed=%v",
			msg.acked, msg.naked, msg.termed)
	}
}

func TestHandleMessage_MissingProposalID(t *testing.T) {
	c := newTestComponent(t.TempDir())

	// Build a payload that would fail Validate() — proposal_id is empty.
	// We construct the raw JSON directly to bypass buildValidCascadeMsg validation.
	payload := `{"proposal_id":"","slug":"some-plan"}`
	data := buildRawEnvelope(payload)
	msg := newMockMsg(data)

	c.handleMessage(context.Background(), msg)

	if !msg.termed {
		t.Errorf("expected Term() for missing proposal_id, acked=%v naked=%v termed=%v",
			msg.acked, msg.naked, msg.termed)
	}
}

func TestHandleMessage_MissingSlug(t *testing.T) {
	c := newTestComponent(t.TempDir())

	// Build a payload with proposal_id set but no slug.
	payload := `{"proposal_id":"cp-123","slug":""}`
	data := buildRawEnvelope(payload)
	msg := newMockMsg(data)

	c.handleMessage(context.Background(), msg)

	if !msg.termed {
		t.Errorf("expected Term() for missing slug, acked=%v naked=%v termed=%v",
			msg.acked, msg.naked, msg.termed)
	}
}

func TestHandleMessage_ProposalNotFound_Naks(t *testing.T) {
	c := newTestComponent(t.TempDir())
	// natsClient is nil — handleCascadeRequest returns an error immediately.

	req := &payloads.ChangeProposalCascadeRequest{
		ProposalID: "cp-nonexistent",
		Slug:       "no-such-plan",
	}
	data := buildValidCascadeMsg(t, req)
	msg := newMockMsg(data)

	c.handleMessage(context.Background(), msg)

	// NATS client unavailable → cascade error → Nak (retriable, not a permanent failure).
	if !msg.naked {
		t.Errorf("expected Nak() for cascade error, acked=%v naked=%v termed=%v",
			msg.acked, msg.naked, msg.termed)
	}
}

// ---------------------------------------------------------------------------
// handleCascadeRequest — business logic path (filesystem only)
// ---------------------------------------------------------------------------

// TestHandleCascadeRequest_NilNATSClient verifies that handleCascadeRequest
// returns an error immediately when the NATS client is nil.
func TestHandleCascadeRequest_NilNATSClient(t *testing.T) {
	c := newTestComponent(t.TempDir())
	// natsClient is nil — set by newTestComponent intentionally.

	req := &payloads.ChangeProposalCascadeRequest{
		ProposalID: "cp-missing",
		Slug:       "no-such-plan",
	}
	_, err := c.handleCascadeRequest(context.Background(), req)
	if err == nil {
		t.Fatal("handleCascadeRequest() expected error when NATS client is nil, got nil")
	}
}

// TestHandleCascadeRequest_EmptyAffectedReqIDs verifies that when the proposal
// has no affected requirement IDs the cascade completes without error and no
// tasks are dirtied.
//
// Note: the caller of handleCascadeRequest calls publishAcceptedEvent which requires a live
// natsClient. Because cascade.ChangeProposal returns early when
// AffectedReqIDs is empty the cascade succeeds before reaching the publish
// step, so the nil-natsClient panic is triggered. We use defer/recover here
// to assert the filesystem state was not modified before the publish attempt.
// ---------------------------------------------------------------------------
// Metadata and health
// ---------------------------------------------------------------------------

func TestMeta(t *testing.T) {
	c := &Component{name: "change-proposal-handler"}
	meta := c.Meta()

	if meta.Name != "change-proposal-handler" {
		t.Errorf("Meta.Name = %q, want %q", meta.Name, "change-proposal-handler")
	}
	if meta.Type != "processor" {
		t.Errorf("Meta.Type = %q, want %q", meta.Type, "processor")
	}
	if meta.Description == "" {
		t.Error("Meta.Description should not be empty")
	}
	if meta.Version == "" {
		t.Error("Meta.Version should not be empty")
	}
}

func TestHealth_NotRunning(t *testing.T) {
	c := &Component{name: "change-proposal-handler", logger: slog.Default()}

	health := c.Health()

	if health.Healthy {
		t.Error("Health.Healthy should be false when component is stopped")
	}
	if health.Status != "stopped" {
		t.Errorf("Health.Status = %q, want %q", health.Status, "stopped")
	}
}

func TestHealth_Running(t *testing.T) {
	c := &Component{name: "change-proposal-handler", logger: slog.Default()}

	c.mu.Lock()
	c.running = true
	c.startTime = time.Now()
	c.mu.Unlock()

	health := c.Health()

	if !health.Healthy {
		t.Error("Health.Healthy should be true when running")
	}
	if health.Status != "running" {
		t.Errorf("Health.Status = %q, want %q", health.Status, "running")
	}
}

func TestInputPorts(t *testing.T) {
	c := &Component{config: DefaultConfig()}
	ports := c.InputPorts()

	if len(ports) == 0 {
		t.Error("InputPorts should return at least one port")
	}
	for _, p := range ports {
		if p.Name == "" {
			t.Error("input port Name must not be empty")
		}
		if p.Direction != "input" {
			t.Errorf("input port %q direction = %q, want %q", p.Name, p.Direction, "input")
		}
	}
}

func TestOutputPorts(t *testing.T) {
	c := &Component{config: DefaultConfig()}
	ports := c.OutputPorts()

	if len(ports) == 0 {
		t.Error("OutputPorts should return at least one port")
	}
	for _, p := range ports {
		if p.Name == "" {
			t.Error("output port Name must not be empty")
		}
		if p.Direction != "output" {
			t.Errorf("output port %q direction = %q, want %q", p.Name, p.Direction, "output")
		}
	}
}

func TestInputPorts_NilPortConfig(t *testing.T) {
	c := &Component{config: Config{Ports: nil}}
	ports := c.InputPorts()
	if len(ports) != 0 {
		t.Errorf("InputPorts with nil Ports should return empty slice, got %d", len(ports))
	}
}

func TestOutputPorts_NilPortConfig(t *testing.T) {
	c := &Component{config: Config{Ports: nil}}
	ports := c.OutputPorts()
	if len(ports) != 0 {
		t.Errorf("OutputPorts with nil Ports should return empty slice, got %d", len(ports))
	}
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

func TestStart_NilNATSClient(t *testing.T) {
	c := newTestComponent(t.TempDir())
	// natsClient is nil

	err := c.Start(context.Background())
	if err == nil {
		t.Fatal("Start() should return error when NATS client is nil")
	}
	if c.IsRunning() {
		t.Error("component should not be running after failed Start()")
	}
}

func TestStop_WhenNotRunning(t *testing.T) {
	c := newTestComponent(t.TempDir())

	if err := c.Stop(time.Second); err != nil {
		t.Errorf("Stop() when not running should be a no-op, got error: %v", err)
	}
}

func TestInitialize(t *testing.T) {
	c := newTestComponent(t.TempDir())

	if err := c.Initialize(); err != nil {
		t.Errorf("Initialize() error = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// Metrics atomics
// ---------------------------------------------------------------------------

func TestRequestsProcessed_Increments(t *testing.T) {
	c := newTestComponent(t.TempDir())

	// Inject a malformed message — handleMessage increments requestsProcessed before
	// the early-return path for parse errors.
	msg := newMockMsg([]byte(`not-json`))
	c.handleMessage(context.Background(), msg)

	if c.requestsProcessed.Load() != 1 {
		t.Errorf("requestsProcessed = %d, want 1", c.requestsProcessed.Load())
	}
}

func TestDataFlow(t *testing.T) {
	c := newTestComponent(t.TempDir())
	flow := c.DataFlow()

	// DataFlow metrics are not actively tracked yet — verify the call is safe.
	_ = flow.LastActivity
}
