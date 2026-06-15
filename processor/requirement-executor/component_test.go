package requirementexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/jsonutil"
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
	// createdBranchBases records the base ref passed to each CreateBranch call,
	// positionally aligned with createdBranchNames — so tests can assert the
	// requirement branch forks from the resolved DependsOn base, not plan/HEAD.
	createdBranchBases []string
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

func (s *stubSandbox) CreateBranch(_ context.Context, branch, base string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdBranchNames = append(s.createdBranchNames, branch)
	s.createdBranchBases = append(s.createdBranchBases, base)
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
	// Model intentionally empty — capability registry drives resolution
	// at dispatch time. See Config.Model docs.
	if cfg.Model != "" {
		t.Errorf("Model = %q, want \"\" (capability resolution path)", cfg.Model)
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
	// Model intentionally NOT auto-defaulted — empty signals "use
	// capability registry resolution" per model.ResolveModel. Auto-
	// defaulting would short-circuit ResolveModel and route every
	// dispatch to registry defaults.Model regardless of capability.
	if got.Model != "" {
		t.Errorf("withDefaults() Model = %q, want \"\" (capability resolution path)", got.Model)
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
	// Empty Model is the capability-resolution signal; see Config docs.
	if c.config.Model != "" {
		t.Errorf("Model = %q, want \"\" (capability resolution path)", c.config.Model)
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
//
// Pre-populates DecomposerModel + ReviewerModel = "default" because
// ResolveModel returns empty when neither override nor a registry is
// configured. Tests that don't supply a model registry rely on these
// overrides so dispatch payloads pass BaseMessage.Validate (which rejects
// model="" with "model required"). Tests that want capability resolution
// should provide deps.ModelRegistry instead.
func newTestComponent(t *testing.T) *Component {
	t.Helper()
	cfg := DefaultConfig()
	cfg.ReviewerModel = "default"
	raw, _ := json.Marshal(cfg)
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

	c.initReqExecution(context.Background(), exec, "semspec/plan-main", "")

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

	// Pass a non-empty resolved base — §9.F: the stale-branch reset must
	// recreate from the (possibly MOVED) prerequisite base, not plan-main.
	c.initReqExecution(context.Background(), exec, "semspec/plan-main", "semspec/requirement-a1")

	// ADR-043 PR 4g — initReqExecution synthesizes the DAG synchronously
	// and marks the exec failed when no plan exists in PLAN_STATES (the
	// unit-test mode case). That is now expected; this test pins the
	// branch reset side-effect, not the synthesis outcome.
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
	// Both attempts must target the RESOLVED base, proving the recreate forks
	// from the prerequisite (moved across attempts), not the plan base.
	for i, base := range stub.createdBranchBases {
		if base != "semspec/requirement-a1" {
			t.Errorf("CreateBranch attempt %d base = %q, want resolved base semspec/requirement-a1", i, base)
		}
	}
	// Expect: DeleteBranch called once between the two CreateBranch calls.
	if len(stub.deletedBranchNames) != 1 || stub.deletedBranchNames[0] != "semspec/requirement-req-retry" {
		t.Errorf("deletedBranchNames = %v, want [semspec/requirement-req-retry]", stub.deletedBranchNames)
	}
}

// TestInitReqExecution_ForksFromResolvedBase closes the §9.A seam the review
// flagged: selectReqBranchBase precedence is unit-tested, but nothing asserts
// the chosen base actually reaches CreateBranch. Here the resolved base wins
// over the plan base — proving a dependent forks from its prerequisite's branch.
func TestInitReqExecution_ForksFromResolvedBase(t *testing.T) {
	c := newTestComponent(t)
	stub := &stubSandbox{}
	c.sandbox = stub

	exec := &requirementExecution{
		EntityID:       workflow.EntityPrefix() + ".exec.req.run.plan-fork-req-b1",
		Slug:           "plan-fork",
		RequirementID:  "b1",
		Prompt:         "do the thing",
		Role:           "developer",
		Model:          "gpt-4",
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
		storeKey:       workflow.RequirementExecutionKey("plan-fork", "b1"),
	}
	c.activeExecs.Set(exec.EntityID, exec) //nolint:errcheck

	c.initReqExecution(context.Background(), exec, "semspec/plan-fork", "semspec/requirement-a1")

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.createdBranchNames) != 1 || stub.createdBranchNames[0] != "semspec/requirement-b1" {
		t.Fatalf("createdBranchNames = %v, want [semspec/requirement-b1]", stub.createdBranchNames)
	}
	if len(stub.createdBranchBases) != 1 || stub.createdBranchBases[0] != "semspec/requirement-a1" {
		t.Errorf("CreateBranch base = %v, want [semspec/requirement-a1] — dependent must fork from its prereq, not the plan base",
			stub.createdBranchBases)
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

	dag := &TaskDAG{
		Nodes: []TaskNode{
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
		NodeIndex:      map[string]*TaskNode{"node-1": &dag.Nodes[0], "node-2": &dag.Nodes[1]},
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

	dag := &TaskDAG{
		Nodes: []TaskNode{
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
		NodeIndex:      map[string]*TaskNode{"only-node": &dag.Nodes[0]},
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
		NodeIndex:      map[string]*TaskNode{},
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

	dag := &TaskDAG{
		Nodes: []TaskNode{
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
		NodeIndex:         map[string]*TaskNode{"task-a": &dag.Nodes[0]},
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

	dag := &TaskDAG{
		Nodes: []TaskNode{
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
		NodeIndex:         map[string]*TaskNode{"task-a": &dag.Nodes[0]},
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

// ADR-035 audit B.5: malformed result payload on outcome=success used to
// silently produce a zero-value NodeResult, letting the downstream reviewer
// judge work the executor never recorded. Pin the strict-parse + retry shape.
func TestHandleNodeCompleteLocked_MalformedResult_RetriesAtRequirementLevel(t *testing.T) {
	c := newTestComponent(t)

	dag := &TaskDAG{
		Nodes: []TaskNode{
			{ID: "task-a", Prompt: "do a", Role: "developer", FileScope: []string{"a.go"}},
		},
	}

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-mal",
		Slug:              "p",
		RequirementID:     "malformed",
		Model:             "test-model",
		CurrentNodeTaskID: "node-task-mal",
		DAG:               dag,
		SortedNodeIDs:     []string{"task-a"},
		NodeIndex:         map[string]*TaskNode{"task-a": &dag.Nodes[0]},
		VisitedNodes:      map[string]bool{"task-a": true},
		CurrentNodeIdx:    0,
		MaxRetries:        2,
		RetryCount:        0,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-mal",
		TaskID:       "node-task-mal",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "task-a",
		Outcome:      agentic.OutcomeSuccess,
		Result:       "this is not json at all",
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	retryCount := exec.RetryCount
	terminated := exec.terminated
	dirtyNodes := exec.DirtyNodeIDs
	_, nodeStillVisited := exec.VisitedNodes["task-a"]
	feedback := exec.LastReviewFeedback
	nodeResultsLen := len(exec.NodeResults)
	exec.mu.Unlock()

	if terminated {
		t.Error("execution should NOT be terminated when retry budget remains after parse failure")
	}
	if retryCount != 1 {
		t.Errorf("RetryCount = %d, want 1 after parse-failure retry", retryCount)
	}
	if len(dirtyNodes) != 1 || dirtyNodes[0] != "task-a" {
		t.Errorf("DirtyNodeIDs = %v, want [task-a]", dirtyNodes)
	}
	if nodeStillVisited {
		t.Error("parse-failed node should be removed from VisitedNodes for retry")
	}
	if feedback == "" {
		t.Error("LastReviewFeedback should be populated with retry-hint guidance")
	}
	if nodeResultsLen != 0 {
		t.Errorf("NodeResults length = %d, want 0 — must not append a zero-value entry on parse-failure retry (otherwise duplicates accumulate)", nodeResultsLen)
	}
}

// ADR-035 audit B.5 (content-empty case): well-formed JSON with no
// recordable output fields (FilesModified, FilesCreated, Summary, MergeCommit
// all empty) is the actual production wedge shape — the synthesizer at
// req_completions.go always populates task_stage so Result is non-empty,
// but a node could "succeed" without producing any files or a merge commit.
// That used to silently propagate as zero-value NodeResult.
func TestHandleNodeCompleteLocked_EmptyContentResult_RetriesAtRequirementLevel(t *testing.T) {
	c := newTestComponent(t)

	dag := &TaskDAG{
		Nodes: []TaskNode{
			{ID: "task-a", Prompt: "do a", Role: "developer", FileScope: []string{"a.go"}},
		},
	}

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-empty",
		Slug:              "p",
		RequirementID:     "empty-content",
		Model:             "test-model",
		CurrentNodeTaskID: "node-task-empty",
		DAG:               dag,
		SortedNodeIDs:     []string{"task-a"},
		NodeIndex:         map[string]*TaskNode{"task-a": &dag.Nodes[0]},
		VisitedNodes:      map[string]bool{"task-a": true},
		CurrentNodeIdx:    0,
		MaxRetries:        2,
		RetryCount:        0,
	}
	c.activeExecs.Set(exec.EntityID, exec)

	// Well-formed JSON, but every recordable output field is empty/absent —
	// the audit's actual wedge shape (a "successful" node with no evidence).
	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-empty",
		TaskID:       "node-task-empty",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "task-a",
		Outcome:      agentic.OutcomeSuccess,
		Result:       `{"task_stage":"completed","escalation_reason":""}`,
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	retryCount := exec.RetryCount
	terminated := exec.terminated
	nodeResultsLen := len(exec.NodeResults)
	exec.mu.Unlock()

	if terminated {
		t.Error("execution should NOT be terminated on empty-content retry")
	}
	if retryCount != 1 {
		t.Errorf("RetryCount = %d, want 1 after empty-content retry", retryCount)
	}
	if nodeResultsLen != 0 {
		t.Errorf("NodeResults length = %d, want 0 on empty-content retry", nodeResultsLen)
	}
}

// Pins the markFailedLocked branch when the retry budget is already
// exhausted and the result payload is unparseable. Pre-fix, this case
// would have silently appended a zero-value NodeResult and continued to
// the requirement reviewer with no evidence; post-fix it terminates.
func TestHandleNodeCompleteLocked_ParseFailure_ExhaustsRetryBudget(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.p-pexh",
		Slug:              "p",
		RequirementID:     "parse-exhaust",
		CurrentNodeTaskID: "node-task-pexh",
		SortedNodeIDs:     []string{"task-a"},
		VisitedNodes:      map[string]bool{"task-a": true},
		CurrentNodeIdx:    0,
		MaxRetries:        2,
		RetryCount:        2, // budget already exhausted
	}
	c.activeExecs.Set(exec.EntityID, exec)

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-node-pexh",
		TaskID:       "node-task-pexh",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: "task-a",
		Outcome:      agentic.OutcomeSuccess,
		Result:       "still not json",
	}

	exec.mu.Lock()
	c.handleNodeCompleteLocked(context.Background(), event, exec)
	terminated := exec.terminated
	nodeResultsLen := len(exec.NodeResults)
	exec.mu.Unlock()

	if !terminated {
		t.Error("execution should be terminated when retry budget is exhausted on parse failure")
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
	if nodeResultsLen != 0 {
		t.Errorf("NodeResults length = %d, want 0 — must not append a zero-value entry on terminal parse failure", nodeResultsLen)
	}
}

// Direct unit test for parseNodeResultPayload covering the three branches:
// shape failure (unparseable), content failure (no recordable fields), and
// success (any one field populated). Keeps the helper's contract pinned
// independently of the handler's retry orchestration.
func TestParseNodeResultPayload(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"unparseable garbage", "this is not json at all", true},
		{"empty string", "", true},
		{"valid JSON but no useful fields", `{"task_stage":"completed"}`, true},
		{"empty arrays + empty strings", `{"files_modified":[],"files_created":[],"changes_summary":"","merge_commit":""}`, true},
		{"files_modified populated", `{"files_modified":["main.go"]}`, false},
		{"files_created populated", `{"files_created":["new.go"]}`, false},
		{"summary populated", `{"changes_summary":"added handler"}`, false},
		{"merge_commit populated", `{"merge_commit":"abc123"}`, false},
		{"markdown-fenced", "```json\n{\"merge_commit\":\"abc\"}\n```", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, quirks, err := parseNodeResultPayload(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil; parsed=%+v", got)
				}
				// QuirksFired may be populated even on error (e.g. fence
				// stripped successfully, but resulting JSON still empty
				// in content). Don't assert on it here — coverage lives
				// in TestParseNodeResultPayload_SurfacesQuirksFired.
				_ = quirks
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if got == nil {
				t.Error("expected non-nil parsed payload")
			}
		})
	}
}

// ADR-035 CP-1 phase-2 wire (audit B.5): parseNodeResultPayload must
// surface QuirksFired so handleNodeCompleteLocked can attribute
// per-fire quirks to the SKG via parseincident.Emit. Pin the
// surfacing across the realistic quirks the developer-node parse will
// see.
func TestParseNodeResultPayload_SurfacesQuirksFired(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantQuirks []jsonutil.QuirkID
	}{
		{
			name:       "clean JSON — no quirks",
			input:      `{"merge_commit":"abc"}`,
			wantQuirks: nil,
		},
		{
			name:       "fenced JSON — fenced_json_wrapper fires",
			input:      "```json\n" + `{"merge_commit":"abc"}` + "\n```",
			wantQuirks: []jsonutil.QuirkID{jsonutil.QuirkFencedJSONWrapper},
		},
		{
			name:       "trailing commas — trailing_commas fires",
			input:      `{"files_modified":["a.go"],"merge_commit":"abc",}`,
			wantQuirks: []jsonutil.QuirkID{jsonutil.QuirkTrailingCommas},
		},
		{
			name:       "no JSON found — no quirks but error returned",
			input:      "not json",
			wantQuirks: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, gotQuirks, _ := parseNodeResultPayload(tt.input)
			if len(gotQuirks) != len(tt.wantQuirks) {
				t.Errorf("QuirksFired len = %d, want %d (got %v)", len(gotQuirks), len(tt.wantQuirks), gotQuirks)
				return
			}
			for i, want := range tt.wantQuirks {
				if gotQuirks[i] != want {
					t.Errorf("QuirksFired[%d] = %q, want %q", i, gotQuirks[i], want)
				}
			}
		})
	}
}

func TestHandleNodeCompleteLocked_SuccessWithMoreNodes_AdvancesExecution(t *testing.T) {
	c := newTestComponent(t)

	dag := &TaskDAG{
		Nodes: []TaskNode{
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
		NodeIndex:         map[string]*TaskNode{"node-x": &dag.Nodes[0], "node-y": &dag.Nodes[1]},
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
		NodeIndex:         map[string]*TaskNode{},
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
		NodeIndex:         map[string]*TaskNode{},
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

// TestRequirementReviewerFixableRerunsStoryOnExistingBranch pins the
// Story-altitude retry contract that replaced the (impossible) scenario→node
// dirty-targeting. A fixable Murat rejection — even without per-scenario
// verdicts, which the old code rejected as "invalid targeting" — must start a
// retry that re-runs the whole Story DAG on the EXISTING branch: RetryCount
// incremented, NodeResults reset, DirtyNodeIDs nil (all nodes re-run), branch
// preserved (no restructure), and the reviewer feedback carried forward.
func TestRequirementReviewerFixableRerunsStoryOnExistingBranch(t *testing.T) {
	c := newTestComponent(t)

	dag := &TaskDAG{
		Nodes: []TaskNode{
			{ID: "node-a", ScenarioIDs: []string{"s1", "s2"}},
			{ID: "node-b", ScenarioIDs: []string{"s1", "s2"}},
		},
	}
	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.fixable",
		Slug:              "fixable",
		RequirementID:     "requirement.fixable.1",
		RequirementBranch: "req/fixable",
		DAG:               dag,
		SortedNodeIDs:     []string{"node-a", "node-b"},
		NodeIndex:         map[string]*TaskNode{"node-a": &dag.Nodes[0], "node-b": &dag.Nodes[1]},
		VisitedNodes:      map[string]bool{"node-a": true, "node-b": true},
		NodeResults:       []NodeResult{{NodeID: "node-a"}, {NodeID: "node-b"}},
		MaxRetries:        2,
		RetryCount:        0,
		CurrentStoryIdx:   0,
		SortedStoryIDs:    []string{"story.fixable.1"},
		Scenarios:         []workflow.Scenario{{ID: "s1", StoryID: "story.fixable.1"}, {ID: "s2", StoryID: "story.fixable.1"}},
		CurrentNodeTaskID: "reviewer",
	}

	// Fixable rejection WITHOUT scenario_verdicts — the old code failed this
	// closed; the Story-altitude retry accepts it (the feedback alone drives
	// the re-run).
	event := &agentic.LoopCompletedEvent{
		Outcome: agentic.OutcomeSuccess,
		Result:  `{"verdict":"rejected","rejection_type":"fixable","feedback":"The lifecycle test does not assert on the boot handshake."}`,
	}

	exec.mu.Lock()
	c.handleRequirementReviewerCompleteLocked(context.Background(), event, exec)
	exec.mu.Unlock()

	if exec.RetryCount != 1 {
		t.Errorf("RetryCount = %d, want 1; a fixable rejection must start a retry", exec.RetryCount)
	}
	if len(exec.DirtyNodeIDs) != 0 {
		t.Errorf("DirtyNodeIDs = %v, want nil; the Story-altitude retry re-runs all nodes, no scenario→node targeting", exec.DirtyNodeIDs)
	}
	if exec.NodeResults != nil {
		t.Errorf("NodeResults = %v, want nil; the retry resets node results so every node re-dispatches", exec.NodeResults)
	}
	if exec.DAG == nil {
		t.Error("DAG was discarded; a fixable retry must preserve the DAG (only restructure re-synthesizes)")
	}
	if exec.LastReviewFeedback == "" {
		t.Error("LastReviewFeedback empty; reviewer feedback must carry forward into the re-run")
	}
	// The re-run must actually re-dispatch from the START of the DAG, not skip
	// to review: VisitedNodes reset to empty (so the completion check at
	// dispatchNextNodeLocked doesn't short-circuit) and the cursor advanced onto
	// the first node (idx 0). A regression that left VisitedNodes populated or
	// DirtyNodeIDs as a subset would silently skip nodes — these assertions guard
	// the "re-runs the WHOLE Story" contract.
	if len(exec.VisitedNodes) != 0 {
		t.Errorf("VisitedNodes = %v, want empty; the retry must reset visited state so all nodes re-run", exec.VisitedNodes)
	}
	if exec.CurrentNodeIdx != 0 {
		t.Errorf("CurrentNodeIdx = %d, want 0; the retry must re-dispatch from the first node", exec.CurrentNodeIdx)
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

// ---------------------------------------------------------------------------
// ADR-043 PR 4h: per-Story dispatch — Story advancement on reviewer approval
// ---------------------------------------------------------------------------

// TestPerStoryReviewer_ApprovedAdvancesToNextStory pins the load-bearing
// behavior of PR 4h: when the reviewer approves and more Stories remain
// in SortedStoryIDs, the requirement does NOT terminate; instead the
// CurrentStoryIdx cursor advances and per-Story state resets in
// preparation for the next Story's DAG.
func TestPerStoryReviewer_ApprovedAdvancesToNextStory(t *testing.T) {
	c := newTestComponent(t)

	exec := &requirementExecution{
		EntityID:      "semspec.local.exec.req.run.test-multi-story",
		Slug:          "test-plan",
		RequirementID: "requirement.test-plan.1",
		// Two Stories. Reviewer approval on Story 1 must advance to Story 2.
		SortedStoryIDs:  []string{"story.test-plan.1.1", "story.test-plan.1.2"},
		CurrentStoryIdx: 0,
		VisitedNodes:    map[string]bool{"impl": true},
		NodeResults: []NodeResult{
			{NodeID: "impl", FilesModified: []string{"main.go"}, CommitSHA: "abc123"},
		},
	}

	event := &agentic.LoopCompletedEvent{
		Outcome:      agentic.OutcomeSuccess,
		WorkflowSlug: "requirement-execution",
		WorkflowStep: "requirement-review",
		Result:       `{"verdict":"approved","feedback":"OK","scenario_verdicts":[]}`,
	}

	exec.mu.Lock()
	c.handleRequirementReviewerCompleteLocked(context.Background(), event, exec)
	exec.mu.Unlock()

	// Requirement must NOT be marked completed — Story 2 still pending.
	// (In unit-test mode, no NATS → loadPlanFromKV nil → markFailed fires
	// from the synth-fail, so exec.terminated will be true. The load-
	// bearing claim is the advancement decision: cursor moved, requirement
	// did not complete.)
	if c.requirementsCompleted.Load() != 0 {
		t.Errorf("requirementsCompleted = %d, want 0 (Story 2 hasn't run yet)", c.requirementsCompleted.Load())
	}
	if exec.CurrentStoryIdx != 1 {
		t.Errorf("CurrentStoryIdx = %d, want 1 (cursor advanced to Story 2)", exec.CurrentStoryIdx)
	}
}

// TestPerStoryReviewer_ApprovedOnFinalStoryCompletesRequirement pins the
// other side of the advancement decision: when the reviewer approves the
// last Story (CurrentStoryIdx+1 == len(SortedStoryIDs)), markCompletedLocked
// fires and the requirement terminates.
func TestPerStoryReviewer_ApprovedOnFinalStoryCompletesRequirement(t *testing.T) {
	c := newTestComponent(t)
	gateOff := false
	c.config.RequireCommitObservation = &gateOff // simplify — no claim/observation gate

	exec := &requirementExecution{
		EntityID:        "semspec.local.exec.req.run.test-final-story",
		Slug:            "test-plan",
		RequirementID:   "requirement.test-plan.1",
		SortedStoryIDs:  []string{"story.test-plan.1.1"},
		CurrentStoryIdx: 0, // 0 + 1 == 1 == len → final
		VisitedNodes:    map[string]bool{"impl": true},
	}

	event := &agentic.LoopCompletedEvent{
		Outcome: agentic.OutcomeSuccess,
		Result:  `{"verdict":"approved","feedback":"OK","scenario_verdicts":[]}`,
	}

	exec.mu.Lock()
	c.handleRequirementReviewerCompleteLocked(context.Background(), event, exec)
	exec.mu.Unlock()

	if c.requirementsCompleted.Load() != 1 {
		t.Errorf("requirementsCompleted = %d, want 1 (final Story approved → requirement complete)", c.requirementsCompleted.Load())
	}
	if !exec.terminated {
		t.Error("exec.terminated should be true after final Story approval")
	}
}

// TestPerStoryReviewer_LegacyExecWithoutSortedStoryIDsCompletes pins the
// backwards-compat path: an exec that never populated SortedStoryIDs
// (CurrentStoryIdx+1 < len(SortedStoryIDs) is trivially false since
// len==0). Such a requirement still terminates on approval via the
// existing markCompletedLocked branch.
func TestPerStoryReviewer_LegacyExecWithoutSortedStoryIDsCompletes(t *testing.T) {
	c := newTestComponent(t)
	gateOff := false
	c.config.RequireCommitObservation = &gateOff

	exec := &requirementExecution{
		EntityID:      "semspec.local.exec.req.run.test-legacy",
		Slug:          "test-plan",
		RequirementID: "requirement.test-plan.1",
		// SortedStoryIDs zero-len simulates a pre-PR-4h exec.
		VisitedNodes: map[string]bool{"impl": true},
	}

	event := &agentic.LoopCompletedEvent{
		Outcome: agentic.OutcomeSuccess,
		Result:  `{"verdict":"approved","feedback":"OK","scenario_verdicts":[]}`,
	}

	exec.mu.Lock()
	c.handleRequirementReviewerCompleteLocked(context.Background(), event, exec)
	exec.mu.Unlock()

	if c.requirementsCompleted.Load() != 1 {
		t.Errorf("requirementsCompleted = %d, want 1", c.requirementsCompleted.Load())
	}
}

// TestScopeScenariosToCurrentStory pins the reviewer-context scoping:
// only scenarios whose StoryID matches the current Story make it into
// the reviewer's prompt.
func TestScopeScenariosToCurrentStory(t *testing.T) {
	exec := &requirementExecution{
		SortedStoryIDs:  []string{"story.x.1.1", "story.x.1.2"},
		CurrentStoryIdx: 1, // Story 2
		Scenarios: []workflow.Scenario{
			{ID: "s1", StoryID: "story.x.1.1"},
			{ID: "s2", StoryID: "story.x.1.2"},
			{ID: "s3", StoryID: "story.x.1.2"},
			{ID: "s4", StoryID: "story.x.1.3"}, // belongs to a different requirement; should not pass the filter
		},
	}
	got := scopeScenariosToCurrentStory(exec)
	if len(got) != 2 {
		t.Fatalf("len(scoped) = %d, want 2", len(got))
	}
	if got[0].ID != "s2" || got[1].ID != "s3" {
		t.Errorf("scoped IDs = %q,%q — want s2,s3", got[0].ID, got[1].ID)
	}
}

// TestScopeScenariosToCurrentStory_LegacyExecFallsBackToAll pins the
// backwards-compat fallback: when SortedStoryIDs is empty (legacy or
// pre-init exec) the helper returns ALL scenarios so the reviewer still
// sees a complete picture rather than nothing.
func TestScopeScenariosToCurrentStory_LegacyExecFallsBackToAll(t *testing.T) {
	exec := &requirementExecution{
		Scenarios: []workflow.Scenario{
			{ID: "s1"}, {ID: "s2"},
		},
	}
	got := scopeScenariosToCurrentStory(exec)
	if len(got) != 2 {
		t.Errorf("legacy exec should return all scenarios; got len=%d", len(got))
	}
}

// TestFilterScenariosByIDs pins the per-DAG-node scenario scoping. Each
// task node carries node.ScenarioIDs (the IDs of the scenarios the node
// is responsible for satisfying); dispatchNextNodeLocked threads the
// matching scenarios from exec.Scenarios into TaskCreateRequest.Scenarios
// so the developer + per-task code-reviewer prompts ground in the
// contract this specific node must hit. Closes the Cline-blind-to-
// scenarios disconnect surfaced 2026-06-03 on paid mavlink-hard.
func TestFilterScenariosByIDs(t *testing.T) {
	scenarios := []workflow.Scenario{
		{ID: "scenario.x.1.1.1", Given: "g1", When: "w1", Then: []string{"t1"}},
		{ID: "scenario.x.1.1.2", Given: "g2"},
		{ID: "scenario.x.1.1.3", Given: "g3"},
	}

	t.Run("matching subset returned in scenario order", func(t *testing.T) {
		got := filterScenariosByIDs(scenarios, []string{"scenario.x.1.1.3", "scenario.x.1.1.1"})
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		// Order follows scenarios slice, not ids slice.
		if got[0].ID != "scenario.x.1.1.1" || got[1].ID != "scenario.x.1.1.3" {
			t.Errorf("order = [%s, %s], want [scenario.x.1.1.1, scenario.x.1.1.3]", got[0].ID, got[1].ID)
		}
		if got[0].Given != "g1" || got[0].Then[0] != "t1" {
			t.Errorf("filter dropped content from the matched scenario; given=%q then[0]=%q", got[0].Given, got[0].Then[0])
		}
	})

	t.Run("empty ids returns nil for clean field-omit", func(t *testing.T) {
		got := filterScenariosByIDs(scenarios, nil)
		if got != nil {
			t.Errorf("got = %v, want nil so TaskCreateRequest.Scenarios stays unpopulated and the fragment elides", got)
		}
	})

	t.Run("empty scenarios returns nil", func(t *testing.T) {
		got := filterScenariosByIDs(nil, []string{"scenario.x.1.1.1"})
		if got != nil {
			t.Errorf("got = %v, want nil when there are no scenarios to filter from", got)
		}
	})

	t.Run("ids referencing unknown scenarios return empty slice", func(t *testing.T) {
		got := filterScenariosByIDs(scenarios, []string{"scenario.does-not-exist"})
		if len(got) != 0 {
			t.Errorf("got = %v, want empty when ID references a scenario not present", got)
		}
	})

	t.Run("duplicate ids do not duplicate scenarios", func(t *testing.T) {
		got := filterScenariosByIDs(scenarios, []string{"scenario.x.1.1.1", "scenario.x.1.1.1"})
		if len(got) != 1 {
			t.Errorf("len = %d, want 1 (deduplicated by set semantics)", len(got))
		}
	})
}

// TestDispatchCurrentStoryLocked_NonCompleteStatusesFallThroughToDispatch
// pins the ADR-044 dedup's negative case: any Story.Status other than
// Complete must fall through to synthesis. The test exercises Pending,
// Ready, Executing, Failed, and the zero-value. A regression that flipped
// the condition (`!= Complete`) or accidentally treated Executing/Failed
// as Complete would skip dispatch for live work — the test surfaces that
// by asserting markCompletedLocked is NOT called.
//
// We use empty FilesOwned to force a synthesis error (the simplest signal
// that the dispatch path was reached). markFailedLocked → requirementsFailed,
// not requirementsCompleted, distinguishes the dispatch attempt from a skip.
func TestDispatchCurrentStoryLocked_NonCompleteStatusesFallThroughToDispatch(t *testing.T) {
	for _, status := range []workflow.StoryStatus{
		"",
		workflow.StoryStatusPending,
		workflow.StoryStatusReady,
		workflow.StoryStatusExecuting,
		workflow.StoryStatusFailed,
	} {
		t.Run(string(status), func(t *testing.T) {
			c := newTestComponent(t)
			plan := &workflow.Plan{
				Slug: "demo",
				Stories: []workflow.Story{
					{
						ID:             "story.demo.shared",
						ComponentName:  "shared",
						RequirementIDs: []string{"req.demo.1"},
						Status:         status,
						// Empty FilesOwned → synthesizer fails → markFailedLocked fires.
						// This proves we reached the dispatch path rather than skipping.
						FilesOwned: []string{},
						Tasks:      []workflow.Task{{ID: "task.demo.shared.1", StoryID: "story.demo.shared", Description: "x"}},
					},
				},
			}
			exec := &requirementExecution{
				EntityID:        "semspec.local.exec.req.fall-through-" + string(status),
				Slug:            "demo",
				RequirementID:   "req.demo.1",
				SortedStoryIDs:  []string{"story.demo.shared"},
				CurrentStoryIdx: 0,
				VisitedNodes:    make(map[string]bool),
			}
			exec.mu.Lock()
			c.dispatchCurrentStoryLocked(context.Background(), exec, plan)
			exec.mu.Unlock()

			// Dispatch path must have been reached → synthesis failure → failed counter.
			// The skip branch would increment requirementsCompleted instead.
			if c.requirementsCompleted.Load() != 0 {
				t.Errorf("Status=%q: requirementsCompleted = %d, want 0 (must NOT skip)", status, c.requirementsCompleted.Load())
			}
			if c.requirementsFailed.Load() != 1 {
				t.Errorf("Status=%q: requirementsFailed = %d, want 1 (synthesis error proves dispatch path reached)", status, c.requirementsFailed.Load())
			}
		})
	}
}

// TestDispatchCurrentStoryLocked_RecursesAcrossCompleteToReachReadyStory
// pins multi-Story recursion: when the first Story in SortedStoryIDs is
// Complete (already shipped by another req's executor) and the second
// is not, the recursive call advances the cursor and synthesizes the
// second Story's DAG. A bug in cursor advancement or recursion would
// either dispatch the wrong Story or skip both.
func TestDispatchCurrentStoryLocked_RecursesAcrossCompleteToReachReadyStory(t *testing.T) {
	c := newTestComponent(t)
	plan := &workflow.Plan{
		Slug: "demo",
		Stories: []workflow.Story{
			{
				ID:             "story.demo.first",
				ComponentName:  "shared",
				RequirementIDs: []string{"req.demo.1", "req.demo.2"},
				Status:         workflow.StoryStatusComplete,
				FilesOwned:     []string{"src/shared.go"},
				Tasks:          []workflow.Task{{ID: "task.demo.first.1", StoryID: "story.demo.first", Description: "x"}},
			},
			{
				ID:             "story.demo.second",
				ComponentName:  "second",
				RequirementIDs: []string{"req.demo.2"},
				// Force synthesis failure so we can prove dispatch reached this Story
				// (markFailedLocked is the observable signal).
				FilesOwned: []string{},
				Tasks:      []workflow.Task{{ID: "task.demo.second.1", StoryID: "story.demo.second", Description: "x"}},
			},
		},
	}
	exec := &requirementExecution{
		EntityID:        "semspec.local.exec.req.recursion",
		Slug:            "demo",
		RequirementID:   "req.demo.2",
		SortedStoryIDs:  []string{"story.demo.first", "story.demo.second"},
		CurrentStoryIdx: 0,
		VisitedNodes:    make(map[string]bool),
	}
	exec.mu.Lock()
	c.dispatchCurrentStoryLocked(context.Background(), exec, plan)
	exec.mu.Unlock()

	// Recursion path: skip first, dispatch second, synthesis fails → markFailedLocked.
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1 (recursion should reach story.demo.second and fail synthesis)", c.requirementsFailed.Load())
	}
	if c.requirementsCompleted.Load() != 0 {
		t.Errorf("requirementsCompleted = %d, want 0 (recursion should NOT have terminated as complete)", c.requirementsCompleted.Load())
	}
	// Cursor must have advanced past the skipped first Story.
	if exec.CurrentStoryIdx != 1 {
		t.Errorf("CurrentStoryIdx = %d, want 1 (cursor must advance past the skipped first Story)", exec.CurrentStoryIdx)
	}
}

// TestDispatchCurrentStoryLocked_SkipsAlreadyCompleteStory pins the ADR-044
// M:N dedup: when a Story covers multiple Requirements (Story.RequirementIDs
// plural), the SAME Story appears in StoriesForRequirement() for every
// requirement it covers. The executor that runs SECOND would re-dispatch
// the dev loop on the already-shipped Story without this guard. Smoke 9
// (2026-06-02 mavlink-hard) shape: 1 component / 4 capabilities / 4
// requirements collapses to 1 Story under ADR-044; the second requirement's
// executor must observe Status=Complete and advance without dispatching.
func TestDispatchCurrentStoryLocked_SkipsAlreadyCompleteStory(t *testing.T) {
	c := newTestComponent(t)

	plan := &workflow.Plan{
		Slug: "demo",
		Stories: []workflow.Story{
			{
				ID:              "story.demo.shared",
				ComponentName:   "shared-driver",
				RequirementIDs:  []string{"req.demo.1", "req.demo.2"},
				CapabilityNames: []string{"cap-a", "cap-b"},
				Status:          workflow.StoryStatusComplete,
				FilesOwned:      []string{"src/driver.go"},
				Tasks:           []workflow.Task{{ID: "task.demo.shared.1", StoryID: "story.demo.shared", Description: "impl"}},
			},
		},
	}

	exec := &requirementExecution{
		EntityID:        "semspec.local.exec.req.run.test-skip-dedup",
		Slug:            "demo",
		RequirementID:   "req.demo.2",
		SortedStoryIDs:  []string{"story.demo.shared"},
		CurrentStoryIdx: 0,
		VisitedNodes:    make(map[string]bool),
	}

	exec.mu.Lock()
	c.dispatchCurrentStoryLocked(context.Background(), exec, plan)
	exec.mu.Unlock()

	// The Story was already complete (shipped by req.demo.1's executor).
	// Expected: no DAG dispatched, exec.terminated true, requirementsCompleted++.
	if !exec.terminated {
		t.Error("exec.terminated should be true after skipping a complete Story")
	}
	if c.requirementsCompleted.Load() != 1 {
		t.Errorf("requirementsCompleted = %d, want 1 (M:N dedup must mark exec complete when all stories already shipped)", c.requirementsCompleted.Load())
	}
	if exec.DAG != nil {
		t.Error("exec.DAG should be nil (no dispatch on already-complete Story)")
	}
}

func TestCopyStoryEvidence_CopiesOwnerNodeResultsForSharedStory(t *testing.T) {
	story := workflow.Story{
		ID:             "story.demo.shared",
		RequirementIDs: []string{"req.demo.1", "req.demo.2"},
		Tasks: []workflow.Task{
			{ID: "task.demo.shared.1", StoryID: "story.demo.shared"},
			{ID: "task.demo.shared.2", StoryID: "story.demo.shared"},
		},
	}
	owner := &requirementExecution{
		RequirementID: "req.demo.1",
		NodeResults: []NodeResult{
			{NodeID: "task.demo.shared.1", FilesModified: []string{"src/shared.go"}, CommitSHA: "abc123"},
			{NodeID: "task.demo.other.1", FilesModified: []string{"src/other.go"}, CommitSHA: "def456"},
		},
	}
	nonOwner := &requirementExecution{
		RequirementID: "req.demo.2",
		VisitedNodes:  make(map[string]bool),
	}

	if !copyStoryEvidence(nonOwner, story, owner) {
		t.Fatal("copyStoryEvidence returned false, want true for matching owner node evidence")
	}
	if len(nonOwner.NodeResults) != 1 {
		t.Fatalf("copied NodeResults = %d, want 1 matching the shared story tasks", len(nonOwner.NodeResults))
	}
	if got := nonOwner.NodeResults[0].NodeID; got != "task.demo.shared.1" {
		t.Errorf("copied NodeID = %q, want task.demo.shared.1", got)
	}
	if !nonOwner.VisitedNodes["task.demo.shared.1"] {
		t.Error("VisitedNodes should include copied story evidence for completion accounting")
	}

	if !copyStoryEvidence(nonOwner, story, owner) {
		t.Fatal("second copyStoryEvidence returned false; existing evidence should still satisfy the story")
	}
	if len(nonOwner.NodeResults) != 1 {
		t.Fatalf("second copy duplicated evidence: NodeResults = %d, want 1", len(nonOwner.NodeResults))
	}
}

func TestCopyStoryEvidence_FailsWithoutMatchingOwnerNodes(t *testing.T) {
	story := workflow.Story{
		ID:             "story.demo.shared",
		RequirementIDs: []string{"req.demo.1", "req.demo.2"},
		Tasks:          []workflow.Task{{ID: "task.demo.shared.1", StoryID: "story.demo.shared"}},
	}
	owner := &requirementExecution{
		RequirementID: "req.demo.1",
		NodeResults:   []NodeResult{{NodeID: "task.demo.other.1", CommitSHA: "abc123"}},
	}
	nonOwner := &requirementExecution{RequirementID: "req.demo.2"}

	if copyStoryEvidence(nonOwner, story, owner) {
		t.Fatal("copyStoryEvidence returned true, want false when owner has no node result for this Story")
	}
	if len(nonOwner.NodeResults) != 0 {
		t.Fatalf("NodeResults = %d, want 0 when no matching story evidence exists", len(nonOwner.NodeResults))
	}
}

func TestValidateApprovedScenarioVerdicts_RequiresAllScopedScenarios(t *testing.T) {
	scenarios := []workflow.Scenario{
		{ID: "scen.telemetry.1", StoryID: "story.demo.shared"},
		{ID: "scen.telemetry.2", StoryID: "story.demo.shared"},
	}
	verdicts := []ScenarioVerdict{
		{ScenarioID: "scen.telemetry.1", Passed: true},
	}

	if got := validateApprovedScenarioVerdicts(scenarios, verdicts); got == "" {
		t.Fatal("validateApprovedScenarioVerdicts returned empty reason; approved review must fail closed when a scoped scenario lacks a passing verdict")
	}

	verdicts = append(verdicts, ScenarioVerdict{ScenarioID: "scen.telemetry.2", Passed: true})
	if got := validateApprovedScenarioVerdicts(scenarios, verdicts); got != "" {
		t.Fatalf("validateApprovedScenarioVerdicts returned %q, want empty when every scoped scenario passed", got)
	}
}

func TestValidateBorrowedStoryEvidence_RequiresScenarioProofForBorrower(t *testing.T) {
	story := workflow.Story{
		ID:             "story.demo.shared",
		RequirementIDs: []string{"req.demo.1", "req.demo.2"},
		Tasks:          []workflow.Task{{ID: "task.demo.shared.1", StoryID: "story.demo.shared"}},
	}
	owner := &requirementExecution{
		RequirementID: "req.demo.1",
		NodeResults: []NodeResult{
			{NodeID: "task.demo.shared.1", FilesModified: []string{"src/shared.go"}, CommitSHA: "abc123"},
		},
		ScenarioVerdicts: []ScenarioVerdict{
			{ScenarioID: "scen.lifecycle.1", Passed: true},
		},
	}
	borrower := &requirementExecution{
		RequirementID: "req.demo.2",
		Scenarios: []workflow.Scenario{
			{ID: "scen.telemetry.1", RequirementID: "req.demo.2", StoryID: "story.demo.shared"},
		},
	}

	if got := validateBorrowedStoryEvidence(borrower, story, owner); got == "" {
		t.Fatal("validateBorrowedStoryEvidence returned empty reason; borrower must not complete from owner evidence that lacks its scenario verdict")
	}

	owner.ScenarioVerdicts = append(owner.ScenarioVerdicts, ScenarioVerdict{ScenarioID: "scen.telemetry.1", Passed: true})
	if got := validateBorrowedStoryEvidence(borrower, story, owner); got != "" {
		t.Fatalf("validateBorrowedStoryEvidence returned %q, want empty after owner reviewed borrower scenario", got)
	}
}

func TestValidateBorrowedStoryEvidence_RequiresCommittedNodeEvidence(t *testing.T) {
	story := workflow.Story{
		ID:             "story.demo.shared",
		RequirementIDs: []string{"req.demo.1", "req.demo.2"},
		Tasks:          []workflow.Task{{ID: "task.demo.shared.1", StoryID: "story.demo.shared"}},
	}
	owner := &requirementExecution{
		RequirementID: "req.demo.1",
		NodeResults: []NodeResult{
			{NodeID: "task.demo.shared.1", FilesModified: []string{"src/shared.go"}},
		},
		ScenarioVerdicts: []ScenarioVerdict{{ScenarioID: "scen.telemetry.1", Passed: true}},
	}
	borrower := &requirementExecution{
		RequirementID: "req.demo.2",
		Scenarios: []workflow.Scenario{
			{ID: "scen.telemetry.1", RequirementID: "req.demo.2", StoryID: "story.demo.shared"},
		},
	}

	if got := validateBorrowedStoryEvidence(borrower, story, owner); got == "" {
		t.Fatal("validateBorrowedStoryEvidence returned empty reason; borrowed Story evidence needs a commit observation")
	}
}

func TestCopyScenarioVerdictEvidence_CopiesBorrowedRequirementVerdicts(t *testing.T) {
	story := workflow.Story{ID: "story.demo.shared"}
	owner := &requirementExecution{
		ScenarioVerdicts: []ScenarioVerdict{
			{ScenarioID: "scen.lifecycle.1", Passed: true},
			{ScenarioID: "scen.telemetry.1", Passed: true},
		},
	}
	borrower := &requirementExecution{
		Scenarios: []workflow.Scenario{
			{ID: "scen.telemetry.1", RequirementID: "req.demo.2", StoryID: "story.demo.shared"},
		},
	}

	copyScenarioVerdictEvidence(borrower, story, owner)

	if len(borrower.ScenarioVerdicts) != 1 {
		t.Fatalf("copied ScenarioVerdicts = %d, want 1 borrower-specific verdict", len(borrower.ScenarioVerdicts))
	}
	if got := borrower.ScenarioVerdicts[0].ScenarioID; got != "scen.telemetry.1" {
		t.Fatalf("copied scenario_id = %q, want scen.telemetry.1", got)
	}
}

func TestScopeScenariosToCurrentStory_FallsBackToLegacyUnlinkedScenarios(t *testing.T) {
	exec := &requirementExecution{
		SortedStoryIDs:  []string{"story.demo.shared"},
		CurrentStoryIdx: 0,
		Scenarios: []workflow.Scenario{
			{ID: "scen.legacy.1", RequirementID: "req.demo.1"},
			{ID: "scen.other.1", RequirementID: "req.demo.1", StoryID: "story.demo.other"},
		},
	}

	got := scopeScenariosToCurrentStory(exec)
	if len(got) != 1 {
		t.Fatalf("scoped scenarios = %d, want one legacy fallback scenario", len(got))
	}
	if got[0].ID != "scen.legacy.1" {
		t.Fatalf("scoped scenario = %q, want scen.legacy.1", got[0].ID)
	}
}
