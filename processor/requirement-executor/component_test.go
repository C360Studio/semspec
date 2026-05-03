package requirementexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semspec/tools/decompose"
	_ "github.com/c360studio/semspec/tools/decompose" // ensure decompose package is imported
	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	sscache "github.com/c360studio/semstreams/pkg/cache"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// ---------------------------------------------------------------------------
// stubSandbox records sandboxClient calls for tests that need to verify
// worktree/branch lifecycle (e.g. failure paths must not delete node worktrees).
// ---------------------------------------------------------------------------

type stubSandbox struct {
	mu                 sync.Mutex
	deletedWorktreeIDs []string
	createdWorktreeIDs []string
	deletedBranchNames []string
	createdBranchNames []string
	deleteWorktreeErr  error
	createBranchErr    error
	// createBranchErrOnce, when set, is returned on the FIRST CreateBranch
	// call and then cleared — subsequent calls return nil. Used to simulate
	// a stale-branch-on-retry scenario where the first create is refused
	// and the second (after delete) succeeds.
	createBranchErrOnce error
}

func (s *stubSandbox) CreateWorktree(_ context.Context, taskID string, _ ...sandbox.WorktreeOption) (*sandbox.WorktreeInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdWorktreeIDs = append(s.createdWorktreeIDs, taskID)
	return &sandbox.WorktreeInfo{Status: "ready", Branch: "agent/" + taskID, Path: "/tmp/" + taskID}, nil
}

func (s *stubSandbox) DeleteWorktree(_ context.Context, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletedWorktreeIDs = append(s.deletedWorktreeIDs, taskID)
	return s.deleteWorktreeErr
}

func (s *stubSandbox) CreateBranch(_ context.Context, branch, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdBranchNames = append(s.createdBranchNames, branch)
	if s.createBranchErrOnce != nil {
		err := s.createBranchErrOnce
		s.createBranchErrOnce = nil
		return err
	}
	return s.createBranchErr
}

func (s *stubSandbox) DeleteBranch(_ context.Context, branch string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletedBranchNames = append(s.deletedBranchNames, branch)
	return nil
}

func (s *stubSandbox) deletedSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.deletedWorktreeIDs))
	copy(out, s.deletedWorktreeIDs)
	return out
}

// ---------------------------------------------------------------------------
// mockMsg implements jetstream.Msg for unit tests.
// ---------------------------------------------------------------------------

type mockMsg struct {
	data    []byte
	subject string
	acked   bool
	naked   bool
}

func (m *mockMsg) Data() []byte                              { return m.data }
func (m *mockMsg) Subject() string                           { return m.subject }
func (m *mockMsg) Reply() string                             { return "" }
func (m *mockMsg) Headers() nats.Header                      { return nil }
func (m *mockMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *mockMsg) Ack() error                                { m.acked = true; return nil }
func (m *mockMsg) DoubleAck(_ context.Context) error         { m.acked = true; return nil }
func (m *mockMsg) Nak() error                                { m.naked = true; return nil }
func (m *mockMsg) NakWithDelay(_ time.Duration) error        { m.naked = true; return nil }
func (m *mockMsg) InProgress() error                         { return nil }
func (m *mockMsg) Term() error                               { return nil }
func (m *mockMsg) TermWithReason(_ string) error             { return nil }

// ---------------------------------------------------------------------------
// Wire-format helpers
// ---------------------------------------------------------------------------

// buildTriggerMsg builds a *mockMsg carrying a RequirementExecutionRequest
// wrapped in the minimal BaseMessage envelope that ParseReactivePayload expects.
func buildTriggerMsg(req payloads.RequirementExecutionRequest) *mockMsg {
	payload, err := json.Marshal(req)
	if err != nil {
		panic("buildTriggerMsg: marshal request: " + err.Error())
	}
	envelope := map[string]json.RawMessage{"payload": payload}
	data, err := json.Marshal(envelope)
	if err != nil {
		panic("buildTriggerMsg: marshal envelope: " + err.Error())
	}
	return &mockMsg{data: data, subject: "workflow.trigger.requirement-execution-loop"}
}

// ---------------------------------------------------------------------------
// Config tests
// ---------------------------------------------------------------------------

func TestDefaultConfig_HasExpectedDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.TimeoutSeconds != 3600 {
		t.Errorf("TimeoutSeconds = %d, want 3600", cfg.TimeoutSeconds)
	}
	if cfg.Model != "default" {
		t.Errorf("Model = %q, want default", cfg.Model)
	}
	if cfg.Ports == nil {
		t.Fatal("Ports should not be nil")
	}
	if len(cfg.Ports.Inputs) == 0 {
		t.Error("Ports.Inputs should not be empty")
	}
	if len(cfg.Ports.Outputs) == 0 {
		t.Error("Ports.Outputs should not be empty")
	}
}

func TestConfig_Validate_Valid(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig().Validate() error = %v, want nil", err)
	}
}

func TestConfig_Validate_ZeroTimeout(t *testing.T) {
	cfg := Config{TimeoutSeconds: 0}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for TimeoutSeconds = 0")
	}
	if !strings.Contains(err.Error(), "timeout_seconds") {
		t.Errorf("error %q should mention timeout_seconds", err.Error())
	}
}

func TestConfig_Validate_NegativeTimeout(t *testing.T) {
	cfg := Config{TimeoutSeconds: -1}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative TimeoutSeconds")
	}
}

func TestConfig_GetTimeout_PositiveValue(t *testing.T) {
	cfg := Config{TimeoutSeconds: 120}
	got := cfg.GetTimeout()
	if got != 120*time.Second {
		t.Errorf("GetTimeout() = %v, want 120s", got)
	}
}

func TestConfig_GetTimeout_ZeroFallsBackToDefault(t *testing.T) {
	cfg := Config{TimeoutSeconds: 0}
	got := cfg.GetTimeout()
	if got != 60*time.Minute {
		t.Errorf("GetTimeout() with 0 = %v, want 60m", got)
	}
}

func TestConfig_GetTimeout_NegativeFallsBackToDefault(t *testing.T) {
	cfg := Config{TimeoutSeconds: -5}
	got := cfg.GetTimeout()
	if got != 60*time.Minute {
		t.Errorf("GetTimeout() with negative = %v, want 60m", got)
	}
}

func TestConfig_WithDefaults_PreservesSetFields(t *testing.T) {
	cfg := Config{TimeoutSeconds: 900, Model: "gpt-4"}
	got := cfg.withDefaults()
	if got.TimeoutSeconds != 900 {
		t.Errorf("withDefaults() TimeoutSeconds = %d, want 900", got.TimeoutSeconds)
	}
	if got.Model != "gpt-4" {
		t.Errorf("withDefaults() Model = %q, want gpt-4", got.Model)
	}
}

func TestConfig_WithDefaults_FillsZeroFields(t *testing.T) {
	cfg := Config{}
	got := cfg.withDefaults()
	if got.TimeoutSeconds != 3600 {
		t.Errorf("withDefaults() TimeoutSeconds = %d, want 3600", got.TimeoutSeconds)
	}
	if got.Model != "default" {
		t.Errorf("withDefaults() Model = %q, want default", got.Model)
	}
	if got.Ports == nil {
		t.Error("withDefaults() Ports should not be nil")
	}
}

func TestConfig_WithDefaults_NilPortsFilledByDefault(t *testing.T) {
	cfg := Config{TimeoutSeconds: 1800}
	got := cfg.withDefaults()
	if got.Ports == nil {
		t.Fatal("withDefaults() should fill nil Ports")
	}

	// Default inputs should include the KV watcher for EXECUTION_STATES.
	const wantSubject = "req.>"
	subjectFound := false
	for _, p := range got.Ports.Inputs {
		if p.Subject == wantSubject {
			subjectFound = true
			break
		}
	}
	if !subjectFound {
		t.Errorf("default Ports.Inputs should contain subject %q", wantSubject)
	}
}

// TestConfig_Validate_ValidTimeout confirms positive timeout passes Validate
func TestConfig_Validate_ValidTimeout(t *testing.T) {
	for _, secs := range []int{1, 60, 3600, 86400} {
		cfg := Config{TimeoutSeconds: secs}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with TimeoutSeconds=%d error = %v, want nil", secs, err)
		}
	}
}

// ---------------------------------------------------------------------------
// NewComponent construction tests
// ---------------------------------------------------------------------------

func TestNewComponent_ValidConfig(t *testing.T) {
	cfg := DefaultConfig()
	raw, _ := json.Marshal(cfg)

	comp, err := NewComponent(raw, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent() error = %v, want nil", err)
	}
	if comp == nil {
		t.Fatal("NewComponent() returned nil")
	}
}

func TestNewComponent_AppliesDefaults(t *testing.T) {
	raw := []byte(`{}`)
	comp, err := NewComponent(raw, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent() with empty config error = %v, want nil", err)
	}

	c := comp.(*Component)
	if c.config.TimeoutSeconds != 3600 {
		t.Errorf("TimeoutSeconds = %d, want 3600", c.config.TimeoutSeconds)
	}
	if c.config.Model != "default" {
		t.Errorf("Model = %q, want default", c.config.Model)
	}
}

func TestNewComponent_InvalidJSON(t *testing.T) {
	_, err := NewComponent([]byte(`{invalid`), component.Dependencies{})
	if err == nil {
		t.Fatal("NewComponent() with invalid JSON should return error")
	}
}

func TestNewComponent_ExplicitlyInvalidConfig(t *testing.T) {
	cfg := Config{TimeoutSeconds: 0} // withDefaults fills to 3600
	got := cfg.withDefaults()
	if err := got.Validate(); err != nil {
		t.Errorf("after withDefaults, Validate() should pass, got: %v", err)
	}

	// Validate called directly on a bad config DOES fail.
	bad := Config{TimeoutSeconds: -1}
	if err := bad.Validate(); err == nil {
		t.Fatal("Config.Validate() with TimeoutSeconds=-1 should fail")
	}
}

func TestNewComponent_UsesDefaultLogger(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, err := NewComponent(raw, component.Dependencies{Logger: nil})
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}
	c := comp.(*Component)
	if c.logger == nil {
		t.Error("logger should not be nil even when deps.Logger is nil")
	}
}

func TestNewComponent_BuildsInputAndOutputPorts(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, err := NewComponent(raw, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent() error = %v", err)
	}
	c := comp.(*Component)
	if len(c.inputPorts) == 0 {
		t.Error("inputPorts should not be empty after NewComponent")
	}
	if len(c.outputPorts) == 0 {
		t.Error("outputPorts should not be empty after NewComponent")
	}
}

func TestNewComponent_ImplementsDiscoverable(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	if _, ok := comp.(component.Discoverable); !ok {
		t.Error("NewComponent() result should implement component.Discoverable")
	}
}

// ---------------------------------------------------------------------------
// Meta / Health / Ports tests
// ---------------------------------------------------------------------------

func TestMeta_ReturnsExpectedValues(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	meta := c.Meta()
	if meta.Name != componentName {
		t.Errorf("Meta().Name = %q, want %q", meta.Name, componentName)
	}
	if meta.Type != "processor" {
		t.Errorf("Meta().Type = %q, want processor", meta.Type)
	}
	if meta.Version != componentVersion {
		t.Errorf("Meta().Version = %q, want %q", meta.Version, componentVersion)
	}
	if meta.Description == "" {
		t.Error("Meta().Description should not be empty")
	}
}

func TestHealth_NotRunning(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	h := c.Health()
	if h.Healthy {
		t.Error("Health().Healthy should be false when component has not started")
	}
	if h.Status != "stopped" {
		t.Errorf("Health().Status = %q, want stopped", h.Status)
	}
}

func TestHealth_Running_IsHealthy(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	h := c.Health()
	if !h.Healthy {
		t.Error("Health().Healthy should be true when running")
	}
	if h.Status != "healthy" {
		t.Errorf("Health().Status = %q, want healthy", h.Status)
	}
}

func TestHealth_ErrorCountReflectsActualErrors(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()
	c.errors.Add(5)

	h := c.Health()
	if h.ErrorCount != 5 {
		t.Errorf("Health().ErrorCount = %d, want 5", h.ErrorCount)
	}
}

func TestInputPorts_MatchDefaultConfig(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	ports := c.InputPorts()
	if len(ports) == 0 {
		t.Fatal("InputPorts() should not be empty")
	}
}

func TestOutputPorts_MatchDefaultConfig(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	ports := c.OutputPorts()
	if len(ports) == 0 {
		t.Fatal("OutputPorts() should not be empty")
	}
}

func TestConfigSchema_HasProperties(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	schema := c.ConfigSchema()
	if schema.Properties == nil {
		t.Error("ConfigSchema().Properties should not be nil")
	}
}

func TestInitialize_IsNoOp(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)
	if err := c.Initialize(); err != nil {
		t.Errorf("Initialize() error = %v, want nil", err)
	}
}

func TestStop_WhenNotRunning_IsNoOp(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)
	if err := c.Stop(time.Second); err != nil {
		t.Errorf("Stop() on non-running component error = %v, want nil", err)
	}
}

func TestDataFlow_LastActivityUpdates(t *testing.T) {
	raw, _ := json.Marshal(DefaultConfig())
	comp, _ := NewComponent(raw, component.Dependencies{})
	c := comp.(*Component)

	before := time.Now()
	c.updateLastActivity()
	after := time.Now()

	flow := c.DataFlow()
	if flow.LastActivity.Before(before) || flow.LastActivity.After(after) {
		t.Errorf("DataFlow().LastActivity = %v, want in range [%v, %v]",
			flow.LastActivity, before, after)
	}
}

// newTestComponent creates a Component with no NATS client suitable for
// unit-testing handler logic without I/O.
func newTestComponent(t *testing.T) *Component {
	t.Helper()
	raw, _ := json.Marshal(DefaultConfig())
	comp, err := NewComponent(raw, component.Dependencies{})
	if err != nil {
		t.Fatalf("newTestComponent: %v", err)
	}
	c := comp.(*Component)

	// Initialize typed cache that is normally created in Start().
	ae, err := sscache.NewTTL[*requirementExecution](context.Background(), 4*time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatalf("newTestComponent: create active execs cache: %v", err)
	}
	c.activeExecs = ae
	return c
}

// ---------------------------------------------------------------------------
// markCompletedLocked / markFailedLocked / markErrorLocked — guard tests
// ---------------------------------------------------------------------------

func TestMarkCompletedLocked_SetsTerminatedAndIncrements(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:      "entity-1",
		Slug:          "plan-1",
		RequirementID: "req-1",
		VisitedNodes:  make(map[string]bool),
	}

	exec.mu.Lock()
	c.markCompletedLocked(context.Background(), exec)
	exec.mu.Unlock()

	if !exec.terminated {
		t.Error("exec.terminated should be true after markCompletedLocked")
	}
	if c.requirementsCompleted.Load() != 1 {
		t.Errorf("requirementsCompleted = %d, want 1", c.requirementsCompleted.Load())
	}
}

func TestMarkCompletedLocked_AlreadyTerminated_NoDoubleIncrement(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:      "entity-2",
		Slug:          "plan-2",
		RequirementID: "req-2",
		terminated:    true,
		VisitedNodes:  make(map[string]bool),
	}

	exec.mu.Lock()
	c.markCompletedLocked(context.Background(), exec)
	exec.mu.Unlock()

	if c.requirementsCompleted.Load() != 0 {
		t.Errorf("requirementsCompleted = %d, want 0 (already terminated)", c.requirementsCompleted.Load())
	}
}

func TestMarkFailedLocked_SetsTerminatedAndIncrements(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:      "entity-3",
		Slug:          "plan-3",
		RequirementID: "req-3",
	}

	exec.mu.Lock()
	c.markFailedLocked(context.Background(), exec, "decomposer returned error")
	exec.mu.Unlock()

	if !exec.terminated {
		t.Error("exec.terminated should be true after markFailedLocked")
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

func TestMarkFailedLocked_AlreadyTerminated_NoDoubleIncrement(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:      "entity-4",
		Slug:          "plan-4",
		RequirementID: "req-4",
		terminated:    true,
	}

	exec.mu.Lock()
	c.markFailedLocked(context.Background(), exec, "late failure")
	exec.mu.Unlock()

	if c.requirementsFailed.Load() != 0 {
		t.Errorf("requirementsFailed = %d, want 0 (already terminated)", c.requirementsFailed.Load())
	}
}

func TestMarkErrorLocked_SetsTerminatedAndIncrements(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:      "entity-5",
		Slug:          "plan-5",
		RequirementID: "req-5",
	}

	exec.mu.Lock()
	c.markErrorLocked(context.Background(), exec, "infrastructure failure")
	exec.mu.Unlock()

	if !exec.terminated {
		t.Error("exec.terminated should be true after markErrorLocked")
	}
	if c.errors.Load() != 1 {
		t.Errorf("errors = %d, want 1", c.errors.Load())
	}
}

func TestMarkErrorLocked_AlreadyTerminated_NoDoubleIncrement(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:      "entity-6",
		Slug:          "plan-6",
		RequirementID: "req-6",
		terminated:    true,
	}

	exec.mu.Lock()
	c.markErrorLocked(context.Background(), exec, "late error")
	exec.mu.Unlock()

	if c.errors.Load() != 0 {
		t.Errorf("errors = %d, want 0 (already terminated)", c.errors.Load())
	}
}

func TestMarkAll_OnlyFirstTerminationWins(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:      "entity-race",
		Slug:          "p",
		RequirementID: "race",
		VisitedNodes:  make(map[string]bool),
	}

	exec.mu.Lock()
	c.markCompletedLocked(context.Background(), exec)
	c.markFailedLocked(context.Background(), exec, "should be ignored")
	c.markErrorLocked(context.Background(), exec, "also ignored")
	exec.mu.Unlock()

	if c.requirementsCompleted.Load() != 1 {
		t.Errorf("requirementsCompleted = %d, want 1", c.requirementsCompleted.Load())
	}
	if c.requirementsFailed.Load() != 0 {
		t.Errorf("requirementsFailed = %d, want 0", c.requirementsFailed.Load())
	}
	if c.errors.Load() != 0 {
		t.Errorf("errors = %d, want 0", c.errors.Load())
	}
}

// ---------------------------------------------------------------------------
// cleanupExecutionLocked — removes from maps
// ---------------------------------------------------------------------------

func TestCleanupExecutionLocked_RemovesFromActiveExecutions(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.plan-c-req-c",
		DecomposerTaskID:  "decomp-c",
		CurrentNodeTaskID: "node-c",
		VisitedNodes:      make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.cleanupExecutionLocked(exec, true)
	exec.mu.Unlock()

	if _, ok := c.activeExecs.Get(exec.EntityID); ok {
		t.Error("activeExecs should not contain entity after cleanup")
	}
}

func TestCleanupExecutionLocked_StopsTimeoutTimer(t *testing.T) {
	c := newTestComponent(t)

	timerStopped := false
	exec := &requirementExecution{
		EntityID:     workflow.EntityPrefix() + ".exec.req.run.plan-d-req-d",
		VisitedNodes: make(map[string]bool),
		timeoutTimer: &timeoutHandle{
			stop: func() { timerStopped = true },
		},
	}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.cleanupExecutionLocked(exec, true)
	exec.mu.Unlock()

	if !timerStopped {
		t.Error("timeoutHandle.stop should be called during cleanup")
	}
}

func TestCleanupExecutionLocked_NilTimer_NoPanic(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:     workflow.EntityPrefix() + ".exec.req.run.plan-e-req-e",
		VisitedNodes: make(map[string]bool),
		timeoutTimer: nil,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.cleanupExecutionLocked(exec, true)
	exec.mu.Unlock()
}

// TestCleanupExecutionLocked_FailurePath_PreservesNodeWorktrees guards the
// cross-component race that broke the mortgage-calc early-adopter run: when
// the parent requirement terminates with in-flight node task executions,
// requirement-executor must NOT delete node worktrees out from under
// execution-manager. Deleting them produced silent merge failures that then
// still marked the task approved. The reviewer worktree (owned by us) must
// still be cleaned.
func TestCleanupExecutionLocked_FailurePath_PreservesNodeWorktrees(t *testing.T) {
	c := newTestComponent(t)
	stub := &stubSandbox{}
	c.sandbox = stub

	exec := &requirementExecution{
		EntityID:       workflow.EntityPrefix() + ".exec.req.run.plan-fail-req-fail",
		VisitedNodes:   make(map[string]bool),
		NodeTaskIDs:    []string{"node-1", "node-2", "node-3"},
		ReviewerTaskID: "requirement-rev-1",
	}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.cleanupExecutionLocked(exec, false)
	exec.mu.Unlock()

	deleted := stub.deletedSnapshot()
	for _, nodeID := range exec.NodeTaskIDs {
		for _, got := range deleted {
			if got == nodeID {
				t.Errorf("node worktree %q was deleted on failure path; "+
					"deleting in-flight node worktrees races execution-manager", nodeID)
			}
		}
	}
	// Reviewer worktree is owned by requirement-executor — safe to clean in all paths.
	foundReviewer := false
	for _, got := range deleted {
		if got == exec.ReviewerTaskID {
			foundReviewer = true
			break
		}
	}
	if !foundReviewer {
		t.Errorf("reviewer worktree %q should be deleted even on failure path; got deletes=%v",
			exec.ReviewerTaskID, deleted)
	}
}

// TestCleanupExecutionLocked_SuccessPath_DeletesNodeWorktrees is the inverse:
// on the happy path (all nodes merged), node worktrees SHOULD be cleaned up.
func TestCleanupExecutionLocked_SuccessPath_DeletesNodeWorktrees(t *testing.T) {
	c := newTestComponent(t)
	stub := &stubSandbox{}
	c.sandbox = stub

	exec := &requirementExecution{
		EntityID:       workflow.EntityPrefix() + ".exec.req.run.plan-ok-req-ok",
		VisitedNodes:   make(map[string]bool),
		NodeTaskIDs:    []string{"node-1", "node-2"},
		ReviewerTaskID: "requirement-rev-2",
	}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.cleanupExecutionLocked(exec, true)
	exec.mu.Unlock()

	deleted := stub.deletedSnapshot()
	want := []string{"node-1", "node-2", "requirement-rev-2"}
	if len(deleted) != len(want) {
		t.Fatalf("deleted worktrees: want %v, got %v", want, deleted)
	}
	seen := make(map[string]bool, len(deleted))
	for _, d := range deleted {
		seen[d] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("expected %q to be deleted on success path; got %v", w, deleted)
		}
	}
}

// TestInitReqExecution_CreateBranchFailure_MarksError pins invariant B4 from
// docs/audit/task-11-worktree-invariants.md: if sandbox.CreateBranch fails
// during requirement init, the execution MUST transition to error and stop.
// Previously (pre-Phase-2) this was downgraded to a WARN with RequirementBranch
// left empty, which meant downstream task dispatch would pass
// scenario_branch="" to the sandbox and silently merge tasks into whatever
// HEAD pointed at — total loss of per-requirement isolation.
func TestInitReqExecution_CreateBranchFailure_MarksError(t *testing.T) {
	c := newTestComponent(t)
	stub := &stubSandbox{createBranchErr: fmt.Errorf("sandbox unavailable")}
	c.sandbox = stub

	exec := &requirementExecution{
		EntityID:       workflow.EntityPrefix() + ".exec.req.run.plan-b4-req-b4",
		Slug:           "plan-b4",
		RequirementID:  "req-b4",
		Prompt:         "do the thing",
		Role:           "developer",
		Model:          "gpt-4",
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
		storeKey:       workflow.RequirementExecutionKey("plan-b4", "req-b4"),
	}
	c.activeExecs.Set(exec.EntityID, exec) //nolint:errcheck

	c.initReqExecution(context.Background(), exec, "semspec/plan-main")

	if !exec.terminated {
		t.Error("exec.terminated should be true after CreateBranch failure")
	}
	if exec.RequirementBranch != "" {
		t.Errorf("exec.RequirementBranch = %q, want empty on failure", exec.RequirementBranch)
	}
	if got := c.errors.Load(); got != 1 {
		t.Errorf("errors counter = %d, want 1 (markErrorLocked should have incremented)", got)
	}
	// Verify the attempt was made with the correct branch/base and that
	// NOTHING was leaked — no worktree, no lingering branch. B4's failure
	// mode was "warn and proceed with partial state," so the regression
	// test must positively assert the absence of partial state.
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.createdBranchNames) != 1 || stub.createdBranchNames[0] != "semspec/requirement-req-b4" {
		t.Errorf("createdBranchNames = %v, want [semspec/requirement-req-b4]", stub.createdBranchNames)
	}
	if len(stub.createdWorktreeIDs) != 0 {
		t.Errorf("createdWorktreeIDs = %v, want empty on CreateBranch failure", stub.createdWorktreeIDs)
	}
	if len(stub.deletedBranchNames) != 0 {
		t.Errorf("deletedBranchNames = %v, want empty — no branch was created successfully", stub.deletedBranchNames)
	}
	// exec must have been removed from activeExecs (cleanupExecutionLocked runs in markErrorLocked).
	if _, stillActive := c.activeExecs.Get(exec.EntityID); stillActive {
		t.Error("exec should have been removed from activeExecs after markErrorLocked → cleanupExecutionLocked")
	}
}

// TestInitReqExecution_StaleBranchFromPriorAttempt_DeletesAndRecreates
// closes the Phase 5 review-driven gap: A3 strict CreateBranch started
// returning ErrBranchExistsAtDifferentBase when a prior failed attempt
// left a stale per-requirement branch behind. The plan-level /retry path
// deletes the req KV entry (handleReqResetMutation), triggering a fresh
// initReqExecution. Without this hardening, that second call would
// errors-out with 409 and mark the requirement errored — a retry would
// never make forward progress.
//
// Expected behavior: on 409 from CreateBranch, delete the stale branch
// and retry CreateBranch at the requested base. The execution should
// proceed normally (no markErrorLocked), with the branch recorded on
// exec.RequirementBranch.
func TestInitReqExecution_StaleBranchFromPriorAttempt_DeletesAndRecreates(t *testing.T) {
	c := newTestComponent(t)
	stub := &stubSandbox{
		createBranchErrOnce: fmt.Errorf("wrap: %w", sandbox.ErrBranchExistsAtDifferentBase),
	}
	c.sandbox = stub

	exec := &requirementExecution{
		EntityID:       workflow.EntityPrefix() + ".exec.req.run.plan-retry-req-retry",
		Slug:           "plan-retry",
		RequirementID:  "req-retry",
		Prompt:         "do the thing",
		Role:           "developer",
		Model:          "gpt-4",
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
		storeKey:       workflow.RequirementExecutionKey("plan-retry", "req-retry"),
	}
	c.activeExecs.Set(exec.EntityID, exec) //nolint:errcheck

	c.initReqExecution(context.Background(), exec, "semspec/plan-main")

	if exec.terminated {
		t.Error("exec.terminated = true; recovery via delete-and-recreate should have succeeded")
	}
	if c.errors.Load() != 0 {
		t.Errorf("errors counter = %d, want 0 (recovery not a real error)", c.errors.Load())
	}
	if exec.RequirementBranch != "semspec/requirement-req-retry" {
		t.Errorf("exec.RequirementBranch = %q, want %q — recreate must set it",
			exec.RequirementBranch, "semspec/requirement-req-retry")
	}

	stub.mu.Lock()
	defer stub.mu.Unlock()
	// Expect: CreateBranch called twice (fail, then succeed after delete).
	if len(stub.createdBranchNames) != 2 {
		t.Errorf("createdBranchNames = %v, want 2 calls (fail + retry)", stub.createdBranchNames)
	}
	// Expect: DeleteBranch called once between the two CreateBranch calls.
	if len(stub.deletedBranchNames) != 1 || stub.deletedBranchNames[0] != "semspec/requirement-req-retry" {
		t.Errorf("deletedBranchNames = %v, want [semspec/requirement-req-retry]", stub.deletedBranchNames)
	}
}

// ---------------------------------------------------------------------------
// requirementExecution struct initialization tests
// ---------------------------------------------------------------------------

func TestRequirementExecution_InitializesCorrectly(t *testing.T) {
	exec := &requirementExecution{
		EntityID:       workflow.EntityPrefix() + ".exec.req.run.p-r",
		Slug:           "p",
		RequirementID:  "r",
		Prompt:         "do the thing",
		Role:           "developer",
		Model:          "gpt-4",
		ProjectID:      "proj-1",
		TraceID:        "trace-1",
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
	}

	if exec.terminated {
		t.Error("new execution should not be terminated")
	}
	if exec.CurrentNodeIdx != -1 {
		t.Errorf("CurrentNodeIdx should be -1 before execution, got %d", exec.CurrentNodeIdx)
	}
	if len(exec.VisitedNodes) != 0 {
		t.Error("VisitedNodes should be empty initially")
	}
	if exec.DAG != nil {
		t.Error("DAG should be nil before decomposition completes")
	}
}

func TestRequirementExecution_VisitedNodesTracking(t *testing.T) {
	exec := &requirementExecution{
		VisitedNodes: make(map[string]bool),
	}

	exec.VisitedNodes["node-a"] = true
	exec.VisitedNodes["node-b"] = true

	if len(exec.VisitedNodes) != 2 {
		t.Errorf("VisitedNodes len = %d, want 2", len(exec.VisitedNodes))
	}
	if !exec.VisitedNodes["node-a"] {
		t.Error("node-a should be in VisitedNodes")
	}
}

// ---------------------------------------------------------------------------
// dispatchNextNodeLocked — index advancement tests (no NATS I/O)
// ---------------------------------------------------------------------------

func TestDispatchNextNodeLocked_AdvancesCurrentNodeIdx(t *testing.T) {
	c := newTestComponent(t)

	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "node-1", Prompt: "First task", Role: "developer", FileScope: []string{"a.go"}},
			{ID: "node-2", Prompt: "Second task", Role: "developer", FileScope: []string{"b.go"}},
		},
	}

	exec := &requirementExecution{
		EntityID:       workflow.EntityPrefix() + ".exec.req.run.p-r",
		Slug:           "p",
		RequirementID:  "r",
		DAG:            dag,
		SortedNodeIDs:  []string{"node-1", "node-2"},
		NodeIndex:      map[string]*decompose.TaskNode{"node-1": &dag.Nodes[0], "node-2": &dag.Nodes[1]},
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.dispatchNextNodeLocked(context.Background(), exec)
	idx := exec.CurrentNodeIdx
	exec.mu.Unlock()

	if idx != 0 {
		t.Errorf("CurrentNodeIdx = %d, want 0 after first dispatch", idx)
	}
}

func TestDispatchNextNodeLocked_AllNodesExhausted_DispatchesReviewer(t *testing.T) {
	c := newTestComponent(t)

	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "only-node", Prompt: "The only task", Role: "developer", FileScope: []string{"x.go"}},
		},
	}

	exec := &requirementExecution{
		EntityID:       workflow.EntityPrefix() + ".exec.req.run.p-r2",
		Slug:           "p",
		RequirementID:  "r2",
		Model:          "default",
		Prompt:         "implement requirement",
		DAG:            dag,
		SortedNodeIDs:  []string{"only-node"},
		NodeIndex:      map[string]*decompose.TaskNode{"only-node": &dag.Nodes[0]},
		CurrentNodeIdx: 0,
		VisitedNodes:   make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.dispatchNextNodeLocked(context.Background(), exec)
	reviewerTaskID := exec.ReviewerTaskID
	terminated := exec.terminated
	exec.mu.Unlock()

	if terminated {
		t.Error("execution should not be terminated yet: reviewer is pending")
	}
	if reviewerTaskID == "" {
		t.Error("ReviewerTaskID should be set after all nodes complete (review dispatched)")
	}
}

func TestDispatchNextNodeLocked_MissingNodeInIndex_MarksError(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:       workflow.EntityPrefix() + ".exec.req.run.p-r3",
		Slug:           "p",
		RequirementID:  "r3",
		SortedNodeIDs:  []string{"ghost-node"},
		NodeIndex:      map[string]*decompose.TaskNode{},
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.dispatchNextNodeLocked(context.Background(), exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("execution should be terminated (error) when node is missing from index")
	}
	if c.errors.Load() != 1 {
		t.Errorf("errors = %d, want 1", c.errors.Load())
	}
}

// ---------------------------------------------------------------------------
// handleDecomposerCompleteLocked — DAG parse tests
// ---------------------------------------------------------------------------

func TestHandleDecomposerCompleteLocked_FailedOutcome_MarksExecFailed(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:         workflow.EntityPrefix() + ".exec.req.run.p-rd",
		Slug:             "p",
		RequirementID:    "rd",
		DecomposerTaskID: "decomp-d",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
		// Exhausted retry budget — next failure terminates.
		DecomposerAttempt: c.config.MaxDecomposerRetries + 1,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-d",
		TaskID:       "decomp-d",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: stageDecompose,
		Outcome:      agentic.OutcomeFailed,
	}

	exec.mu.Lock()
	c.handleDecomposerCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("failed decomposer outcome should terminate the execution")
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

func TestHandleDecomposerCompleteLocked_MalformedResult_MarksExecFailed(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:         workflow.EntityPrefix() + ".exec.req.run.p-rm",
		Slug:             "p",
		RequirementID:    "rm",
		DecomposerTaskID: "decomp-m",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
		// Exhausted retry budget — next failure terminates.
		DecomposerAttempt: c.config.MaxDecomposerRetries + 1,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-m",
		TaskID:       "decomp-m",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: stageDecompose,
		Outcome:      agentic.OutcomeSuccess,
		Result:       `not valid json`,
	}

	exec.mu.Lock()
	c.handleDecomposerCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("malformed decomposer result should terminate the execution")
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

func TestHandleDecomposerCompleteLocked_InvalidDAG_Cycle_MarksExecFailed(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:         workflow.EntityPrefix() + ".exec.req.run.p-ri",
		Slug:             "p",
		RequirementID:    "ri",
		DecomposerTaskID: "decomp-i",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
		// Exhausted retry budget — next failure terminates.
		DecomposerAttempt: c.config.MaxDecomposerRetries + 1,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	// DAG with a cycle — Validate() will reject it.
	cycleResult := `{
		"goal": "build something",
		"dag": {
			"nodes": [
				{"id": "a", "prompt": "p", "role": "dev", "depends_on": ["b"], "file_scope": ["a.go"]},
				{"id": "b", "prompt": "p", "role": "dev", "depends_on": ["a"], "file_scope": ["b.go"]}
			]
		}
	}`

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-i",
		TaskID:       "decomp-i",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: stageDecompose,
		Outcome:      agentic.OutcomeSuccess,
		Result:       cycleResult,
	}

	exec.mu.Lock()
	c.handleDecomposerCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("cyclic DAG should cause the execution to terminate as failed")
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

// TestHandleDecomposerCompleteLocked_EmptyDAG_RetriesWithFeedback verifies that
// an invalid DAG (e.g., empty nodes array from an under-powered model) does NOT
// terminate the execution while the retry budget is not exhausted. Instead the
// previous error is stored for the next dispatch prompt.
func TestHandleDecomposerCompleteLocked_EmptyDAG_RetriesWithFeedback(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:         workflow.EntityPrefix() + ".exec.req.run.p-retry",
		Slug:             "p",
		RequirementID:    "retry",
		Title:            "retry me",
		Model:            "test-model", // required for the re-dispatch marshal path
		DecomposerTaskID: "decomp-retry",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
		// First attempt — well under the retry budget.
		DecomposerAttempt: 1,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	// Empty nodes array — Validate() rejects with "dag must contain at least one node".
	emptyResult := `{"goal": "x", "dag": {"nodes": []}}`

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-retry",
		TaskID:       "decomp-retry",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: stageDecompose,
		Outcome:      agentic.OutcomeSuccess,
		Result:       emptyResult,
	}

	exec.mu.Lock()
	c.handleDecomposerCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	lastError := exec.DecomposerLastError
	exec.mu.Unlock()

	if terminated {
		t.Error("retry path should NOT terminate the execution while budget remains")
	}
	if c.requirementsFailed.Load() != 0 {
		t.Errorf("requirementsFailed = %d during retry, want 0", c.requirementsFailed.Load())
	}
	if lastError == "" {
		t.Error("DecomposerLastError should be populated for the retry prompt")
	}
	if !strings.Contains(lastError, "at least one node") {
		t.Errorf("DecomposerLastError = %q, want it to mention the validation failure", lastError)
	}
}

func TestHandleDecomposerCompleteLocked_ValidDAG_PopulatesExecution(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:         workflow.EntityPrefix() + ".exec.req.run.p-rv",
		Slug:             "p",
		RequirementID:    "rv",
		DecomposerTaskID: "decomp-v",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	validResult := `{
		"goal": "implement auth",
		"dag": {
			"nodes": [
				{"id": "setup", "prompt": "setup env", "role": "developer", "file_scope": ["setup.go"]},
				{"id": "impl",  "prompt": "write code", "role": "developer", "depends_on": ["setup"], "file_scope": ["impl.go"]}
			]
		}
	}`

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-v",
		TaskID:       "decomp-v",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: stageDecompose,
		Outcome:      agentic.OutcomeSuccess,
		Result:       validResult,
	}

	exec.mu.Lock()
	c.handleDecomposerCompleteLocked(context.Background(), event, exec)
	dagSet := exec.DAG != nil
	nodeCount := len(exec.SortedNodeIDs)
	indexLen := len(exec.NodeIndex)
	exec.mu.Unlock()

	if !dagSet {
		t.Error("exec.DAG should be set after successful decomposition")
	}
	if nodeCount != 2 {
		t.Errorf("SortedNodeIDs len = %d, want 2", nodeCount)
	}
	if indexLen != 2 {
		t.Errorf("NodeIndex len = %d, want 2", indexLen)
	}
}

func TestHandleDecomposerCompleteLocked_ValidDAG_TopologicalOrder(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:         workflow.EntityPrefix() + ".exec.req.run.p-rv2",
		Slug:             "p",
		RequirementID:    "rv2",
		DecomposerTaskID: "decomp-v2",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	// Linear chain: setup → impl → test
	chainResult := `{
		"goal": "build and test",
		"dag": {
			"nodes": [
				{"id": "setup", "prompt": "prepare", "role": "developer", "file_scope": ["setup.go"]},
				{"id": "impl",  "prompt": "implement", "role": "developer", "depends_on": ["setup"], "file_scope": ["impl.go"]},
				{"id": "test",  "prompt": "test it",   "role": "developer", "depends_on": ["impl"],  "file_scope": ["impl_test.go"]}
			]
		}
	}`

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-v2",
		TaskID:       "decomp-v2",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: stageDecompose,
		Outcome:      agentic.OutcomeSuccess,
		Result:       chainResult,
	}

	exec.mu.Lock()
	c.handleDecomposerCompleteLocked(context.Background(), event, exec)
	sorted := make([]string, len(exec.SortedNodeIDs))
	copy(sorted, exec.SortedNodeIDs)
	exec.mu.Unlock()

	if len(sorted) != 3 {
		t.Fatalf("SortedNodeIDs len = %d, want 3", len(sorted))
	}
	if sorted[0] != "setup" || sorted[1] != "impl" || sorted[2] != "test" {
		t.Errorf("SortedNodeIDs = %v, want [setup impl test]", sorted)
	}
}

// ---------------------------------------------------------------------------
// handleNodeCompleteLocked — serial execution advancement tests
// ---------------------------------------------------------------------------

func TestHandleNodeCompleteLocked_FailedOutcome_MarksExecFailed(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-rnf",
		Slug:              "p",
		RequirementID:     "rnf",
		CurrentNodeTaskID: "node-task-fail",
		SortedNodeIDs:     []string{"task-a"},
		VisitedNodes:      make(map[string]bool),
		CurrentNodeIdx:    0,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-fail",
		TaskID:       "node-task-fail",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "task-a",
		Outcome:      agentic.OutcomeFailed,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("failed node should terminate the requirement execution")
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

func TestHandleNodeCompleteLocked_FailedOutcome_RetriesWhenBudgetRemains(t *testing.T) {
	c := newTestComponent(t)

	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "task-a", Prompt: "do a", Role: "developer", FileScope: []string{"a.go"}},
		},
	}

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-retry",
		Slug:              "p",
		RequirementID:     "retry-test",
		Model:             "test-model",
		CurrentNodeTaskID: "node-task-retry",
		DAG:               dag,
		SortedNodeIDs:     []string{"task-a"},
		NodeIndex:         map[string]*decompose.TaskNode{"task-a": &dag.Nodes[0]},
		VisitedNodes:      make(map[string]bool),
		CurrentNodeIdx:    0,
		MaxRetries:        2,
		RetryCount:        0,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-retry",
		TaskID:       "node-task-retry",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "task-a",
		Outcome:      agentic.OutcomeFailed,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	retryCount := exec.RetryCount
	terminated := exec.terminated
	dirtyNodes := exec.DirtyNodeIDs
	_, nodeStillVisited := exec.VisitedNodes["task-a"]
	exec.mu.Unlock()

	if terminated {
		t.Error("execution should NOT be terminated when retry budget remains")
	}
	if retryCount != 1 {
		t.Errorf("RetryCount = %d, want 1", retryCount)
	}
	if len(dirtyNodes) != 1 || dirtyNodes[0] != "task-a" {
		t.Errorf("DirtyNodeIDs = %v, want [task-a]", dirtyNodes)
	}
	if nodeStillVisited {
		t.Error("failed node should be removed from VisitedNodes for retry")
	}
}

// Bug-#6 pin: when execution-manager escalates due to TDD-budget exhaustion
// (task_stage="escalated"), requirement-executor MUST NOT layer-2 retry even
// if the requirement-level retry budget remains. Re-dispatching just spawns
// a NEW task with cycle=0 against the same upstream defect and burns another
// full TDD budget. Caught 2026-05-03 on openrouter @easy /health.
func TestHandleNodeCompleteLocked_FailedOutcome_SkipsRetryOnTDDExhaustion(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-tddx",
		Slug:              "p",
		RequirementID:     "tdd-exhaust",
		CurrentNodeTaskID: "node-task-tddx",
		SortedNodeIDs:     []string{"task-a"},
		VisitedNodes:      make(map[string]bool),
		CurrentNodeIdx:    0,
		MaxRetries:        2,
		RetryCount:        0, // budget remaining — would normally retry
	}
	c.activeExecs.Set(exec.EntityID, exec)

	resultPayload := `{"task_stage":"escalated","escalation_reason":"fixable rejections exceeded TDD cycle budget"}`
	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-tddx",
		TaskID:       "node-task-tddx",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "task-a",
		Outcome:      agentic.OutcomeFailed,
		Result:       resultPayload,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	retryCount := exec.RetryCount
	exec.mu.Unlock()

	if !terminated {
		t.Error("TDD-exhausted node should terminate the requirement execution without layer-2 retry")
	}
	if retryCount != 0 {
		t.Errorf("RetryCount = %d, want 0 (no layer-2 retry on TDD exhaustion)", retryCount)
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

// Sibling pin for the non-escalated path: if task_stage is "error" (transient
// agent flake — claim/observation mismatch, merge race), the layer-2 retry
// MUST still fire when budget remains. Without this, the bug-#6 fix would
// over-correct and break the validated 2026-04-29 Gemini @easy retry path.
func TestHandleNodeCompleteLocked_FailedOutcome_RetriesOnTransientError(t *testing.T) {
	c := newTestComponent(t)

	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "task-a", Prompt: "do a", Role: "developer", FileScope: []string{"a.go"}},
		},
	}

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-terr",
		Slug:              "p",
		RequirementID:     "transient-err",
		Model:             "test-model",
		CurrentNodeTaskID: "node-task-terr",
		DAG:               dag,
		SortedNodeIDs:     []string{"task-a"},
		NodeIndex:         map[string]*decompose.TaskNode{"task-a": &dag.Nodes[0]},
		VisitedNodes:      make(map[string]bool),
		CurrentNodeIdx:    0,
		MaxRetries:        2,
		RetryCount:        0,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-terr",
		TaskID:       "node-task-terr",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "task-a",
		Outcome:      agentic.OutcomeFailed,
		Result:       `{"task_stage":"error","escalation_reason":"merge_failed: conflict"}`,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	retryCount := exec.RetryCount
	exec.mu.Unlock()

	if terminated {
		t.Error("transient error with retry budget remaining should NOT terminate the execution")
	}
	if retryCount != 1 {
		t.Errorf("RetryCount = %d, want 1 (layer-2 retry should fire on transient error)", retryCount)
	}
}

func TestHandleNodeCompleteLocked_FailedOutcome_ExhaustsRetryBudget(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-exhaust",
		Slug:              "p",
		RequirementID:     "exhaust-test",
		CurrentNodeTaskID: "node-task-exhaust",
		SortedNodeIDs:     []string{"task-a"},
		VisitedNodes:      make(map[string]bool),
		CurrentNodeIdx:    0,
		MaxRetries:        2,
		RetryCount:        2, // budget already exhausted
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-exhaust",
		TaskID:       "node-task-exhaust",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "task-a",
		Outcome:      agentic.OutcomeFailed,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("execution should be terminated when retry budget is exhausted")
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

func TestHandleNodeCompleteLocked_SuccessWithMoreNodes_AdvancesExecution(t *testing.T) {
	c := newTestComponent(t)

	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "node-x", Prompt: "do x", Role: "developer", FileScope: []string{"x.go"}},
			{ID: "node-y", Prompt: "do y", Role: "developer", FileScope: []string{"y.go"}},
		},
	}

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-rnm",
		Slug:              "p",
		RequirementID:     "rnm",
		Model:             "test-model",
		CurrentNodeTaskID: "node-task-x",
		DAG:               dag,
		SortedNodeIDs:     []string{"node-x", "node-y"},
		NodeIndex:         map[string]*decompose.TaskNode{"node-x": &dag.Nodes[0], "node-y": &dag.Nodes[1]},
		CurrentNodeIdx:    0,
		VisitedNodes:      make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-x",
		TaskID:       "node-task-x",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "node-x",
		Outcome:      agentic.OutcomeSuccess,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	visited := len(exec.VisitedNodes)
	nodeIdx := exec.CurrentNodeIdx
	terminated := exec.terminated
	exec.mu.Unlock()

	if visited != 1 {
		t.Errorf("VisitedNodes len = %d, want 1 after node-x complete", visited)
	}
	if terminated {
		t.Error("execution should not be terminated — node-y is still pending")
	}
	if nodeIdx != 1 {
		t.Errorf("CurrentNodeIdx = %d, want 1 after advancing to node-y", nodeIdx)
	}
	if c.errors.Load() != 0 {
		t.Errorf("errors = %d, want 0 — dispatch of node-y should succeed with model set", c.errors.Load())
	}
}

func TestHandleNodeCompleteLocked_LastNodeSuccess_DispatchesReviewer(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-rnl",
		Slug:              "p",
		RequirementID:     "rnl",
		Model:             "default",
		Prompt:            "implement requirement",
		CurrentNodeTaskID: "node-task-last",
		SortedNodeIDs:     []string{"only"},
		NodeIndex:         map[string]*decompose.TaskNode{},
		CurrentNodeIdx:    0,
		VisitedNodes:      make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-last",
		TaskID:       "node-task-last",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "only",
		Outcome:      agentic.OutcomeSuccess,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	reviewerTaskID := exec.ReviewerTaskID
	terminated := exec.terminated
	exec.mu.Unlock()

	if terminated {
		t.Error("execution should not be terminated yet: reviewer is pending")
	}
	if reviewerTaskID == "" {
		t.Error("ReviewerTaskID should be set after all nodes complete (review dispatched)")
	}
	if c.requirementsCompleted.Load() != 0 {
		t.Errorf("requirementsCompleted = %d, want 0 (reviewer verdict not received yet)", c.requirementsCompleted.Load())
	}
}

func TestHandleNodeCompleteLocked_NodeIDRemovedFromTaskIndex(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-rnr",
		Slug:              "p",
		RequirementID:     "rnr",
		Model:             "default",
		Prompt:            "implement requirement",
		CurrentNodeTaskID: "node-task-rm",
		SortedNodeIDs:     []string{"rm-node"},
		NodeIndex:         map[string]*decompose.TaskNode{},
		CurrentNodeIdx:    0,
		VisitedNodes:      make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-rm",
		TaskID:       "node-task-rm",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "rm-node",
		Outcome:      agentic.OutcomeSuccess,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	exec.mu.Unlock()

}

// ---------------------------------------------------------------------------
// Execution timeout tests
// ---------------------------------------------------------------------------

func TestStartExecutionTimeoutLocked_FiresAfterDuration(t *testing.T) {
	c := newTestComponent(t)
	c.config.TimeoutSeconds = 1 // fire after 1 second

	exec := &requirementExecution{
		EntityID:      workflow.EntityPrefix() + ".exec.req.run.p-timeout",
		Slug:          "p",
		RequirementID: "timeout",
		VisitedNodes:  make(map[string]bool),
	}
	c.activeExecs.Set(exec.EntityID, exec)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		exec.mu.Lock()
		c.startExecutionTimeoutLocked(exec)
		exec.mu.Unlock()
	}()
	wg.Wait()

	// Wait for the timer to fire (up to 3s).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		exec.mu.Lock()
		terminated := exec.terminated
		exec.mu.Unlock()
		if terminated {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	exec.mu.Lock()
	terminated := exec.terminated
	exec.mu.Unlock()

	if !terminated {
		t.Error("execution should be terminated by timeout after 1 second")
	}
	if c.errors.Load() != 1 {
		t.Errorf("errors = %d, want 1 after timeout", c.errors.Load())
	}
}

func TestStartExecutionTimeoutLocked_StopPreventsTimerFiring(t *testing.T) {
	c := newTestComponent(t)
	c.config.TimeoutSeconds = 60 // 60s — will not fire in test

	exec := &requirementExecution{
		EntityID:      workflow.EntityPrefix() + ".exec.req.run.p-notimeout",
		Slug:          "p",
		RequirementID: "notimeout",
		VisitedNodes:  make(map[string]bool),
	}

	exec.mu.Lock()
	c.startExecutionTimeoutLocked(exec)
	// Stop the timer immediately.
	if exec.timeoutTimer != nil {
		exec.timeoutTimer.stop()
	}
	exec.mu.Unlock()

	// Give any racing goroutine a brief moment.
	time.Sleep(10 * time.Millisecond)

	exec.mu.Lock()
	terminated := exec.terminated
	exec.mu.Unlock()

	if terminated {
		t.Error("execution should not be terminated when timer was stopped before firing")
	}
}

// ---------------------------------------------------------------------------
// Factory / Register tests
// ---------------------------------------------------------------------------

type mockRegistry struct {
	registered bool
	lastConfig component.RegistrationConfig
	returnErr  error
}

func (m *mockRegistry) RegisterWithConfig(cfg component.RegistrationConfig) error {
	m.registered = true
	m.lastConfig = cfg
	return m.returnErr
}

func TestRegister_Succeeds(t *testing.T) {
	reg := &mockRegistry{}
	err := Register(reg)
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}
	if !reg.registered {
		t.Error("Register() should call RegisterWithConfig")
	}
	if reg.lastConfig.Name != componentName {
		t.Errorf("Name = %q, want %q", reg.lastConfig.Name, componentName)
	}
	if reg.lastConfig.Factory == nil {
		t.Error("Factory should not be nil")
	}
	if reg.lastConfig.Version != componentVersion {
		t.Errorf("Version = %q, want %q", reg.lastConfig.Version, componentVersion)
	}
	if reg.lastConfig.Type != "processor" {
		t.Errorf("Type = %q, want processor", reg.lastConfig.Type)
	}
}

func TestRegister_NilRegistry_ReturnsError(t *testing.T) {
	err := Register(nil)
	if err == nil {
		t.Fatal("Register(nil) should return error")
	}
}

// ---------------------------------------------------------------------------
// Metrics consistency tests
// ---------------------------------------------------------------------------

func TestMetrics_SeparateCountersForCompletedAndFailed(t *testing.T) {
	c := newTestComponent(t)

	execCompleted := &requirementExecution{
		EntityID:      "entity-completed",
		Slug:          "p",
		RequirementID: "req-completed",
		VisitedNodes:  make(map[string]bool),
	}
	execFailed := &requirementExecution{
		EntityID:      "entity-failed",
		Slug:          "p",
		RequirementID: "req-failed",
	}

	execCompleted.mu.Lock()
	c.markCompletedLocked(context.Background(), execCompleted)
	execCompleted.mu.Unlock()

	execFailed.mu.Lock()
	c.markFailedLocked(context.Background(), execFailed, "test failure")
	execFailed.mu.Unlock()

	if c.requirementsCompleted.Load() != 1 {
		t.Errorf("requirementsCompleted = %d, want 1", c.requirementsCompleted.Load())
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
	if c.errors.Load() != 0 {
		t.Errorf("errors = %d, want 0", c.errors.Load())
	}
}

// ---------------------------------------------------------------------------
// ParseReactivePayload round-trip tests (via workflow/payloads)
// ---------------------------------------------------------------------------

func TestParseReactivePayload_RequirementExecutionRequest_RoundTrip(t *testing.T) {
	original := payloads.RequirementExecutionRequest{
		RequirementID: "req-rt",
		Slug:          "rt-plan",
		Title:         "Round-trip requirement",
		Description:   "Test round trip",
		Prompt:        "round-trip test",
		Role:          "developer",
		Model:         "gpt-4",
		ProjectID:     "proj-rt",
		TraceID:       "trace-rt",
	}

	msg := buildTriggerMsg(original)
	parsed, err := payloads.ParseReactivePayload[payloads.RequirementExecutionRequest](msg.Data())
	if err != nil {
		t.Fatalf("ParseReactivePayload() error = %v", err)
	}

	if parsed.RequirementID != original.RequirementID {
		t.Errorf("RequirementID = %q, want %q", parsed.RequirementID, original.RequirementID)
	}
	if parsed.Slug != original.Slug {
		t.Errorf("Slug = %q, want %q", parsed.Slug, original.Slug)
	}
	if parsed.TraceID != original.TraceID {
		t.Errorf("TraceID = %q, want %q", parsed.TraceID, original.TraceID)
	}
}

func TestParseReactivePayload_MalformedEnvelope_ReturnsError(t *testing.T) {
	_, err := payloads.ParseReactivePayload[payloads.RequirementExecutionRequest]([]byte(`not json`))
	if err == nil {
		t.Fatal("ParseReactivePayload with malformed envelope should return error")
	}
}

func TestParseReactivePayload_MissingPayloadKey_ReturnsError(t *testing.T) {
	data := []byte(`{"type": "something"}`)
	_, err := payloads.ParseReactivePayload[payloads.RequirementExecutionRequest](data)
	if err == nil {
		t.Fatal("ParseReactivePayload with missing payload key should return error")
	}
}

func TestParseReactivePayload_EmptyPayload_ReturnsError(t *testing.T) {
	data := []byte(`{"type":"something"}`)
	_, err := payloads.ParseReactivePayload[payloads.RequirementExecutionRequest](data)
	if err == nil {
		t.Fatal("ParseReactivePayload with absent payload key should return error")
	}
	if !strings.Contains(err.Error(), "empty payload") {
		t.Errorf("error %q should contain %q", err.Error(), "empty payload")
	}
}

// ---------------------------------------------------------------------------
// buildDecomposerPrompt tests
// ---------------------------------------------------------------------------

func TestBuildDecomposerPrompt_UsesExplicitPromptWhenSet(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		RequirementID: "req-1",
		Title:         "Add auth",
		Description:   "Add authentication",
		Prompt:        "explicit custom prompt",
	}

	got := c.buildDecomposerPrompt(exec, "")
	if got != "explicit custom prompt" {
		t.Errorf("buildDecomposerPrompt() = %q, want explicit prompt", got)
	}
}

func TestBuildDecomposerPrompt_BuildsFromContext(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		RequirementID: "req-1",
		Title:         "Add user authentication",
		Description:   "Implement JWT-based auth",
	}

	got := c.buildDecomposerPrompt(exec, "")
	if !strings.Contains(got, "Add user authentication") {
		t.Errorf("prompt should contain title, got: %s", got)
	}
	if !strings.Contains(got, "JWT-based auth") {
		t.Errorf("prompt should contain description, got: %s", got)
	}
}

func TestBuildDecomposerPrompt_IncludesPrerequisites(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		RequirementID: "req-2",
		Title:         "Add OAuth",
		DependsOn: []payloads.PrereqContext{
			{
				RequirementID: "req-1",
				Title:         "Add JWT auth",
				Description:   "Basic JWT auth",
				FilesModified: []string{"auth/jwt.go"},
				Summary:       "Implemented JWT tokens",
			},
		},
	}

	got := c.buildDecomposerPrompt(exec, "")
	if !strings.Contains(got, "Prerequisite Requirements") {
		t.Errorf("prompt should mention prerequisites, got: %s", got)
	}
	if !strings.Contains(got, "Add JWT auth") {
		t.Errorf("prompt should include prereq title, got: %s", got)
	}
	if !strings.Contains(got, "auth/jwt.go") {
		t.Errorf("prompt should include prereq files modified, got: %s", got)
	}
}

// ---------------------------------------------------------------------------
// Decomposer coverage gate — scenarios on the requirement must appear in at
// least one node's scenario_ids. Without the gate a DAG that left scenarios
// uncovered would only surface later when the executor's fixable-retry path
// tried to route a failed scenario to a node. Catching it here shortens the
// feedback loop and gives the decomposer a specific list to fix.
// ---------------------------------------------------------------------------

func TestUncoveredInputScenarios(t *testing.T) {
	cases := []struct {
		name      string
		scenarios []workflow.Scenario
		nodes     []decompose.TaskNode
		want      []string
	}{
		{
			name:      "no scenarios returns nil",
			scenarios: nil,
			nodes:     []decompose.TaskNode{{ID: "n1"}},
			want:      nil,
		},
		{
			name: "every scenario covered returns nil",
			scenarios: []workflow.Scenario{
				{ID: "s1"}, {ID: "s2"},
			},
			nodes: []decompose.TaskNode{
				{ID: "n1", ScenarioIDs: []string{"s1"}},
				{ID: "n2", ScenarioIDs: []string{"s2"}},
			},
			want: nil,
		},
		{
			name: "no nodes carrying any ID returns every scenario",
			scenarios: []workflow.Scenario{
				{ID: "s-b"}, {ID: "s-a"},
			},
			nodes: []decompose.TaskNode{
				{ID: "n1"}, {ID: "n2"},
			},
			want: []string{"s-a", "s-b"},
		},
		{
			name: "partial coverage returns the uncovered ids sorted",
			scenarios: []workflow.Scenario{
				{ID: "s1"}, {ID: "s2"}, {ID: "s3"},
			},
			nodes: []decompose.TaskNode{
				{ID: "n1", ScenarioIDs: []string{"s2"}},
			},
			want: []string{"s1", "s3"},
		},
		{
			name: "scenarios with empty ID are skipped (malformed input)",
			scenarios: []workflow.Scenario{
				{ID: ""}, {ID: "s1"},
			},
			nodes: []decompose.TaskNode{
				{ID: "n1", ScenarioIDs: []string{"s1"}},
			},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := uncoveredInputScenarios(tc.scenarios, tc.nodes)
			if len(got) != len(tc.want) {
				t.Fatalf("len=%d want %d; got=%v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d]=%q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// Coverage gap on a DAG that parses fine but doesn't cite every scenario must
// NOT terminate the requirement — it should route through retryOrFail so the
// decomposer re-runs with actionable feedback.
func TestHandleDecomposerCompleteLocked_CoverageGap_RetriesWithFeedback(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-cov",
		Slug:              "p",
		RequirementID:     "cov",
		Title:             "cover all scenarios",
		Model:             "test-model",
		DecomposerTaskID:  "decomp-cov",
		VisitedNodes:      make(map[string]bool),
		CurrentNodeIdx:    -1,
		DecomposerAttempt: 1,
		Scenarios: []workflow.Scenario{
			{ID: "s-happy", Given: "x", When: "y", Then: []string{"z"}},
			{ID: "s-edge", Given: "a", When: "b", Then: []string{"c"}},
		},
	}
	c.activeExecs.Set(exec.EntityID, exec)

	// DAG covers s-happy but leaves s-edge uncovered.
	result := `{
		"goal": "cover",
		"dag": {
			"nodes": [
				{"id": "n1", "prompt": "do x", "role": "developer", "file_scope": ["a.go"], "scenario_ids": ["s-happy"]}
			]
		}
	}`

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-cov",
		TaskID:       "decomp-cov",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: stageDecompose,
		Outcome:      agentic.OutcomeSuccess,
		Result:       result,
	}

	exec.mu.Lock()
	c.handleDecomposerCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	dagSet := exec.DAG != nil
	lastError := exec.DecomposerLastError
	exec.mu.Unlock()

	if terminated {
		t.Error("coverage gap should route through retry, not terminate")
	}
	if dagSet {
		t.Error("exec.DAG should NOT be populated when coverage gate fails")
	}
	if !strings.Contains(lastError, "s-edge") {
		t.Errorf("DecomposerLastError should call out the uncovered scenario id; got %q", lastError)
	}
	if !strings.Contains(lastError, "scenario_ids") {
		t.Errorf("DecomposerLastError should explain the required field; got %q", lastError)
	}
}

// When EnforceScenarioCoverage is explicitly false (mock-LLM runs), a coverage
// gap must NOT block — it logs a WARN and proceeds. This is the escape hatch
// for fixtures that can't cite runtime-generated scenario IDs yet.
func TestHandleDecomposerCompleteLocked_CoverageGap_GateDisabled_Proceeds(t *testing.T) {
	c := newTestComponent(t)
	disabled := false
	c.config.EnforceScenarioCoverage = &disabled

	exec := &requirementExecution{
		EntityID:         workflow.EntityPrefix() + ".exec.req.run.p-covoff",
		Slug:             "p",
		RequirementID:    "covoff",
		DecomposerTaskID: "decomp-covoff",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
		Scenarios:        []workflow.Scenario{{ID: "s1"}, {ID: "s2"}},
	}
	c.activeExecs.Set(exec.EntityID, exec)

	// Covers s1 only, leaves s2 uncovered.
	result := `{
		"goal": "cover",
		"dag": {
			"nodes": [
				{"id": "n1", "prompt": "x", "role": "developer", "file_scope": ["a.go"], "scenario_ids": ["s1"]}
			]
		}
	}`
	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-covoff",
		TaskID:       "decomp-covoff",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: stageDecompose,
		Outcome:      agentic.OutcomeSuccess,
		Result:       result,
	}

	exec.mu.Lock()
	c.handleDecomposerCompleteLocked(context.Background(), event, exec)
	dagSet := exec.DAG != nil
	lastError := exec.DecomposerLastError
	exec.mu.Unlock()

	if !dagSet {
		t.Error("with gate disabled, DAG should be populated even on coverage gap")
	}
	if lastError != "" {
		t.Errorf("DecomposerLastError should remain empty when gate is disabled; got %q", lastError)
	}
}

// A DAG that cites every input scenario id should proceed normally through
// decomposer-complete. Regression guard — the coverage gate must be a no-op
// on well-formed output.
func TestHandleDecomposerCompleteLocked_FullCoverage_PopulatesDAG(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:         workflow.EntityPrefix() + ".exec.req.run.p-covok",
		Slug:             "p",
		RequirementID:    "covok",
		DecomposerTaskID: "decomp-covok",
		VisitedNodes:     make(map[string]bool),
		CurrentNodeIdx:   -1,
		Scenarios: []workflow.Scenario{
			{ID: "s1"}, {ID: "s2"},
		},
	}
	c.activeExecs.Set(exec.EntityID, exec)

	result := `{
		"goal": "cover",
		"dag": {
			"nodes": [
				{"id": "n1", "prompt": "do x", "role": "developer", "file_scope": ["a.go"], "scenario_ids": ["s1", "s2"]}
			]
		}
	}`
	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-decomp-covok",
		TaskID:       "decomp-covok",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: stageDecompose,
		Outcome:      agentic.OutcomeSuccess,
		Result:       result,
	}

	exec.mu.Lock()
	c.handleDecomposerCompleteLocked(context.Background(), event, exec)
	dagSet := exec.DAG != nil
	nodeCount := len(exec.SortedNodeIDs)
	lastError := exec.DecomposerLastError
	exec.mu.Unlock()

	if !dagSet {
		t.Error("exec.DAG should be set for a fully-covered DAG")
	}
	if nodeCount != 1 {
		t.Errorf("SortedNodeIDs len = %d, want 1", nodeCount)
	}
	if lastError != "" {
		t.Errorf("DecomposerLastError should be cleared on success; got %q", lastError)
	}
}

// Prompt must surface scenario IDs explicitly — without them the LLM has no
// way to populate node.scenario_ids correctly. This test locks in the "id="
// marker and the coverage contract sentence.
func TestBuildDecomposerPrompt_EmitsScenarioIDs(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		RequirementID: "req-scenarios",
		Title:         "Add auth",
		Scenarios: []workflow.Scenario{
			{ID: "sc-login", Given: "a user", When: "they log in", Then: []string{"ok"}},
			{ID: "sc-logout", Given: "a user", When: "they log out", Then: []string{"ok"}},
		},
	}
	got := c.buildDecomposerPrompt(exec, "")

	for _, id := range []string{"sc-login", "sc-logout"} {
		want := fmt.Sprintf("[id=%s]", id)
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing scenario id marker %q; got: %s", want, got)
		}
	}
	if !strings.Contains(got, "scenario_ids") {
		t.Errorf("prompt should explain the scenario_ids contract; got: %s", got)
	}
}

// ---------------------------------------------------------------------------
// Coverage gap tripwire — uncoveredFailedScenarios + restructure escalation.
// ---------------------------------------------------------------------------

func execWithNodes(nodes []*decompose.TaskNode) *requirementExecution {
	exec := &requirementExecution{
		NodeIndex: make(map[string]*decompose.TaskNode, len(nodes)),
	}
	for _, n := range nodes {
		exec.SortedNodeIDs = append(exec.SortedNodeIDs, n.ID)
		exec.NodeIndex[n.ID] = n
	}
	return exec
}

func TestUncoveredFailedScenarios(t *testing.T) {
	c := newTestComponent(t)

	cases := []struct {
		name     string
		nodes    []*decompose.TaskNode
		verdicts []ScenarioVerdict
		want     []string
	}{
		{
			name:     "no failures returns nil",
			nodes:    []*decompose.TaskNode{{ID: "n1", ScenarioIDs: []string{"s1"}}},
			verdicts: []ScenarioVerdict{{ScenarioID: "s1", Passed: true}},
			want:     nil,
		},
		{
			name: "all failed scenarios covered returns nil",
			nodes: []*decompose.TaskNode{
				{ID: "n1", ScenarioIDs: []string{"s1"}},
				{ID: "n2", ScenarioIDs: []string{"s2"}},
			},
			verdicts: []ScenarioVerdict{
				{ScenarioID: "s1", Passed: false},
				{ScenarioID: "s2", Passed: false},
			},
			want: nil,
		},
		{
			name: "zero nodes with scenario_ids returns every failed id",
			nodes: []*decompose.TaskNode{
				{ID: "n1"},
				{ID: "n2"},
			},
			verdicts: []ScenarioVerdict{
				{ScenarioID: "s-b", Passed: false},
				{ScenarioID: "s-a", Passed: false},
			},
			want: []string{"s-a", "s-b"},
		},
		{
			name: "partial coverage returns only uncovered ids sorted",
			nodes: []*decompose.TaskNode{
				{ID: "n1", ScenarioIDs: []string{"s2"}},
			},
			verdicts: []ScenarioVerdict{
				{ScenarioID: "s2", Passed: false},
				{ScenarioID: "s1", Passed: false},
				{ScenarioID: "s3", Passed: false},
			},
			want: []string{"s1", "s3"},
		},
		{
			name: "passed scenarios never show up even if uncovered",
			nodes: []*decompose.TaskNode{
				{ID: "n1", ScenarioIDs: []string{"s1"}},
			},
			verdicts: []ScenarioVerdict{
				{ScenarioID: "s1", Passed: false},
				{ScenarioID: "s2", Passed: true}, // passed + uncovered, irrelevant
			},
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := c.uncoveredFailedScenarios(execWithNodes(tc.nodes), tc.verdicts)
			if len(got) != len(tc.want) {
				t.Fatalf("len(uncovered)=%d want %d; got=%v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("uncovered[%d]=%q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestRequirementCompletion_RejectsApprovalWithoutCommitObservation pins the
// claim/observation contract for requirement-level completion. Sibling of
// bug #9: even when individual node merges silently no-op, the reviewer can
// approve based on the worktree state (which has the work, just unmerged),
// and the requirement gets marked completed despite zero impact on main.
//
// Contract (config.RequireCommitObservation, default true): when any
// NodeResult claims FilesModified but its CommitSHA is empty,
// markCompletedLocked must NOT be called — the requirement fails with a
// claim/observation mismatch reason. The test enables the gate explicitly
// to be robust against future config-default flips.
func TestRequirementCompletion_RejectsApprovalWithoutCommitObservation(t *testing.T) {
	c := newTestComponent(t)
	gateOn := true
	c.config.RequireCommitObservation = &gateOn

	exec := &requirementExecution{
		EntityID:      "semspec.local.exec.req.run.test-claim-only",
		Slug:          "test-plan",
		RequirementID: "requirement.test-plan.1",
		VisitedNodes:  map[string]bool{"impl-health": true},
		// Developer claimed work — these are the FilesModified the reviewer
		// based its approval on. In the smoking-gun bug pattern these files
		// exist in the worktree but never reached main due to merge no-op.
		// CommitSHA is empty because the merge was a silent no-op.
		NodeResults: []NodeResult{
			{
				NodeID:        "impl-health",
				FilesModified: []string{"main.go", "main_test.go"},
				Summary:       "Implemented /health endpoint with tests",
				CommitSHA:     "", // ← the smoking gun
			},
		},
	}

	// Reviewer approves based on worktree contents. There is no commit
	// observation in the event payload — the reviewer doesn't know whether
	// the per-node merges actually landed in main.
	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-1",
		TaskID:       "requirement-rev-test-claim-only",
		Outcome:      agentic.OutcomeSuccess,
		WorkflowSlug: "requirement-execution",
		WorkflowStep: "requirement-review",
		Result:       `{"verdict":"approved","feedback":"All scenarios pass.","scenario_verdicts":[{"scenario_id":"s1","verdict":"approved"}]}`,
	}

	exec.mu.Lock()
	c.handleRequirementReviewerCompleteLocked(context.Background(), event, exec)
	exec.mu.Unlock()

	// Contract: with RequireCommitObservation enabled, the requirement must
	// NOT be marked completed when a node claimed work but produced no
	// commit observation. Instead it must transition to phaseFailed with
	// a claim/observation mismatch reason.
	if c.requirementsCompleted.Load() != 0 {
		t.Errorf("Requirement was marked completed despite NodeResult.CommitSHA empty for FilesModified=%v. The RequireCommitObservation gate did not fire — contract violation.",
			exec.NodeResults[0].FilesModified)
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1 (claim/observation mismatch should fail the requirement)", c.requirementsFailed.Load())
	}
}
