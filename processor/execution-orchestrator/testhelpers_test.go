package executionorchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/nats-io/nats.go"
)

// ---------------------------------------------------------------------------
// stubRegistry satisfies RegistryInterface for Register() tests.
// ---------------------------------------------------------------------------

type stubRegistry struct {
	called bool
	cfg    component.RegistrationConfig
}

func (r *stubRegistry) RegisterWithConfig(cfg component.RegistrationConfig) error {
	r.called = true
	r.cfg = cfg
	return nil
}

// ---------------------------------------------------------------------------
// newTestComponent builds a Component with default config and no NATS client.
// The nil NATSClient means publish/request calls are silently skipped, which
// is exactly what we want for unit tests that focus on state transitions.
// ---------------------------------------------------------------------------

func newTestComponent(t *testing.T) *Component {
	t.Helper()
	rawCfg, _ := json.Marshal(map[string]any{})
	deps := component.Dependencies{NATSClient: nil}
	disc, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("newTestComponent: NewComponent failed: %v", err)
	}
	return disc.(*Component)
}

// ---------------------------------------------------------------------------
// newTestExec creates a taskExecution for state-machine tests.
// ---------------------------------------------------------------------------

func newTestExec(slug, taskID string) *taskExecution {
	entityID := fmt.Sprintf("local.semspec.workflow.task-execution.execution.%s-%s", slug, taskID)
	return &taskExecution{
		EntityID:      entityID,
		Slug:          slug,
		TaskID:        taskID,
		Iteration:     0,
		MaxIterations: 3,
	}
}

// ---------------------------------------------------------------------------
// testCtx returns a background context that is always valid for unit tests.
// ---------------------------------------------------------------------------

func testCtx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

// ---------------------------------------------------------------------------
// makeNATSMsg wraps raw bytes in a *nats.Msg for handleTrigger / handleLoopCompleted.
// ---------------------------------------------------------------------------

func makeNATSMsg(t *testing.T, data []byte) *nats.Msg {
	t.Helper()
	return &nats.Msg{Data: data}
}

// ---------------------------------------------------------------------------
// makeTriggerMsg builds a valid BaseMessage-wrapped TriggerPayload from a
// map of payload fields, suitable for feeding into handleTrigger.
// ---------------------------------------------------------------------------

func makeTriggerMsg(t *testing.T, payloadFields map[string]any) *nats.Msg {
	t.Helper()
	payloadBytes, err := json.Marshal(payloadFields)
	if err != nil {
		t.Fatalf("makeTriggerMsg: marshal payload: %v", err)
	}

	// Wrap in the BaseMessage envelope that ParseReactivePayload expects.
	envelope := map[string]any{
		"payload": json.RawMessage(payloadBytes),
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("makeTriggerMsg: marshal envelope: %v", err)
	}
	return makeNATSMsg(t, data)
}

// ---------------------------------------------------------------------------
// makeLoopCompletedMsg builds a BaseMessage-wrapped agentic.LoopCompletedEvent.
//
// The message.BaseMessage.Payload() method returns registered types; here we
// marshal raw JSON that the component will attempt to type-assert. For tests
// that only need to exercise routing guards (wrong slug, unknown task ID), the
// payload type assertion failing is fine — it causes an early return before
// any state is mutated.
// ---------------------------------------------------------------------------

func makeLoopCompletedMsg(t *testing.T, workflowSlug, taskID, workflowStep, resultJSON string) []byte {
	t.Helper()

	// Build the inner LoopCompletedEvent payload fields.
	event := map[string]any{
		"workflow_slug":  workflowSlug,
		"task_id":        taskID,
		"workflow_step":  workflowStep,
		"result":         resultJSON,
	}
	payloadBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("makeLoopCompletedMsg: marshal event: %v", err)
	}

	// The component calls json.Unmarshal into message.BaseMessage and then
	// calls base.Payload() which uses the payload registry. For routing guard
	// tests we only need the outer envelope to be valid JSON — the type
	// assertion will fail (because the payload factory isn't registered in
	// unit tests) and the handler will return early, which is acceptable.
	envelope := map[string]any{
		"type": map[string]any{
			"domain":   "agentic",
			"category": "loop-completed",
			"version":  "v1",
		},
		"payload": json.RawMessage(payloadBytes),
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("makeLoopCompletedMsg: marshal envelope: %v", err)
	}
	return data
}
