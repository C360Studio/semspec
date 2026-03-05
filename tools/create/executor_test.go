package create_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"

	"github.com/c360studio/semspec/tools/create"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// makeCall builds an agentic.ToolCall for create_tool with the given arguments.
func makeCall(id, loopID, traceID string, args map[string]any) agentic.ToolCall {
	return agentic.ToolCall{
		ID:        id,
		Name:      "create_tool",
		Arguments: args,
		LoopID:    loopID,
		TraceID:   traceID,
	}
}

// mustUnmarshalSpec unmarshals the ToolResult.Content into a FlowSpec.
func mustUnmarshalSpec(t *testing.T, content string) create.FlowSpec {
	t.Helper()
	var spec create.FlowSpec
	if err := json.Unmarshal([]byte(content), &spec); err != nil {
		t.Fatalf("unmarshal FlowSpec from %q: %v", content, err)
	}
	return spec
}

// processor is a convenience builder for a processor map argument.
func processor(id, component string) map[string]any {
	return map[string]any{
		"id":        id,
		"component": component,
	}
}

// processors wraps a variadic list of processor maps into a []any.
func processors(ps ...map[string]any) []any {
	out := make([]any, len(ps))
	for i, p := range ps {
		out[i] = p
	}
	return out
}

// wiringRule is a convenience builder for a wiring rule map argument.
func wiringRule(from, to string) map[string]any {
	return map[string]any{"from": from, "to": to}
}

// wiring wraps a variadic list of wiring rule maps into a []any.
func wiring(ws ...map[string]any) []any {
	out := make([]any, len(ws))
	for i, w := range ws {
		out[i] = w
	}
	return out
}

// minimalSpec returns arguments for a minimal valid FlowSpec.
func minimalSpec() map[string]any {
	return map[string]any{
		"name":        "my_tool",
		"description": "Does something useful",
		"processors":  processors(processor("step1", "llm-agent")),
	}
}

// ---------------------------------------------------------------------------
// FlowSpec.Validate tests
// ---------------------------------------------------------------------------

func TestValidate_MinimalValidSpec(t *testing.T) {
	t.Parallel()

	spec := create.FlowSpec{
		Name:        "my_tool",
		Description: "A useful tool",
		Processors:  []create.ProcessorRef{{ID: "step1", Component: "llm-agent"}},
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for minimal valid spec", err)
	}
}

func TestValidate_EmptyName_ReturnsError(t *testing.T) {
	t.Parallel()

	spec := create.FlowSpec{
		Name:        "",
		Description: "A useful tool",
		Processors:  []create.ProcessorRef{{ID: "step1", Component: "llm-agent"}},
	}
	err := spec.Validate()
	if err == nil {
		t.Error("Validate() = nil, want error for empty name")
	}
}

func TestValidate_InvalidNameChars_ReturnsError(t *testing.T) {
	t.Parallel()

	invalidNames := []string{
		"my-tool", // hyphen not allowed
		"my tool", // space not allowed
		"my.tool", // dot not allowed
		"my/tool", // slash not allowed
		"123!abc", // exclamation not allowed
	}

	for _, name := range invalidNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := create.FlowSpec{
				Name:        name,
				Description: "A tool",
				Processors:  []create.ProcessorRef{{ID: "step1", Component: "llm-agent"}},
			}
			if err := spec.Validate(); err == nil {
				t.Errorf("Validate() = nil, want error for invalid name %q", name)
			}
		})
	}
}

func TestValidate_NameTooLong_ReturnsError(t *testing.T) {
	t.Parallel()

	// 65 characters — one over the limit.
	longName := strings.Repeat("a", 65)
	spec := create.FlowSpec{
		Name:        longName,
		Description: "A tool",
		Processors:  []create.ProcessorRef{{ID: "step1", Component: "llm-agent"}},
	}
	err := spec.Validate()
	if err == nil {
		t.Error("Validate() = nil, want error for name exceeding 64 characters")
	}
}

func TestValidate_MaxLengthName_Valid(t *testing.T) {
	t.Parallel()

	// Exactly 64 characters — at the limit.
	maxName := strings.Repeat("a", 64)
	spec := create.FlowSpec{
		Name:        maxName,
		Description: "A tool",
		Processors:  []create.ProcessorRef{{ID: "step1", Component: "llm-agent"}},
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for 64-char name", err)
	}
}

func TestValidate_EmptyDescription_ReturnsError(t *testing.T) {
	t.Parallel()

	spec := create.FlowSpec{
		Name:        "my_tool",
		Description: "",
		Processors:  []create.ProcessorRef{{ID: "step1", Component: "llm-agent"}},
	}
	if err := spec.Validate(); err == nil {
		t.Error("Validate() = nil, want error for empty description")
	}
}

func TestValidate_NoProcessors_ReturnsError(t *testing.T) {
	t.Parallel()

	spec := create.FlowSpec{
		Name:        "my_tool",
		Description: "A useful tool",
		Processors:  nil,
	}
	if err := spec.Validate(); err == nil {
		t.Error("Validate() = nil, want error for empty processors")
	}
}

func TestValidate_TooManyProcessors_ReturnsError(t *testing.T) {
	t.Parallel()

	procs := make([]create.ProcessorRef, 21)
	for i := range procs {
		procs[i] = create.ProcessorRef{ID: fmt.Sprintf("step%d", i+1), Component: "llm-agent"}
	}
	spec := create.FlowSpec{
		Name:        "my_tool",
		Description: "A useful tool",
		Processors:  procs,
	}
	err := spec.Validate()
	if err == nil {
		t.Error("Validate() = nil, want error for more than 20 processors")
	}
}

func TestValidate_MaxProcessors_Valid(t *testing.T) {
	t.Parallel()

	procs := make([]create.ProcessorRef, 20)
	for i := range procs {
		procs[i] = create.ProcessorRef{ID: fmt.Sprintf("step%d", i+1), Component: "llm-agent"}
	}
	spec := create.FlowSpec{
		Name:        "my_tool",
		Description: "A useful tool",
		Processors:  procs,
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for exactly 20 processors", err)
	}
}

func TestValidate_DuplicateProcessorIDs_ReturnsError(t *testing.T) {
	t.Parallel()

	spec := create.FlowSpec{
		Name:        "my_tool",
		Description: "A useful tool",
		Processors: []create.ProcessorRef{
			{ID: "dup", Component: "llm-agent"},
			{ID: "dup", Component: "validator"},
		},
	}
	err := spec.Validate()
	if err == nil {
		t.Error("Validate() = nil, want error for duplicate processor IDs")
	}
	if err != nil && !strings.Contains(err.Error(), "dup") {
		t.Errorf("error = %q, want mention of duplicate ID", err.Error())
	}
}

func TestValidate_WiringFromUnknownProcessor_ReturnsError(t *testing.T) {
	t.Parallel()

	spec := create.FlowSpec{
		Name:        "my_tool",
		Description: "A useful tool",
		Processors:  []create.ProcessorRef{{ID: "step1", Component: "llm-agent"}},
		Wiring: []create.WiringRule{
			{From: "ghost.output.result", To: "step1.input.data"},
		},
	}
	err := spec.Validate()
	if err == nil {
		t.Error("Validate() = nil, want error for wiring from unknown processor")
	}
	if err != nil && !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error = %q, want mention of unknown processor ID %q", err.Error(), "ghost")
	}
}

func TestValidate_WiringToUnknownProcessor_ReturnsError(t *testing.T) {
	t.Parallel()

	spec := create.FlowSpec{
		Name:        "my_tool",
		Description: "A useful tool",
		Processors:  []create.ProcessorRef{{ID: "step1", Component: "llm-agent"}},
		Wiring: []create.WiringRule{
			{From: "input.data", To: "ghost.input.data"},
		},
	}
	err := spec.Validate()
	if err == nil {
		t.Error("Validate() = nil, want error for wiring to unknown processor")
	}
	if err != nil && !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error = %q, want mention of unknown processor ID %q", err.Error(), "ghost")
	}
}

func TestValidate_ValidWiringInputToProcessor(t *testing.T) {
	t.Parallel()

	spec := create.FlowSpec{
		Name:        "my_tool",
		Description: "A useful tool",
		Processors:  []create.ProcessorRef{{ID: "step1", Component: "llm-agent"}},
		Wiring: []create.WiringRule{
			{From: "input.prompt", To: "step1.input.prompt"},
		},
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for valid input→processor wiring", err)
	}
}

func TestValidate_ValidWiringProcessorToOutput(t *testing.T) {
	t.Parallel()

	spec := create.FlowSpec{
		Name:        "my_tool",
		Description: "A useful tool",
		Processors:  []create.ProcessorRef{{ID: "step1", Component: "llm-agent"}},
		Wiring: []create.WiringRule{
			{From: "step1.output.result", To: "output.result"},
		},
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for valid processor→output wiring", err)
	}
}

func TestValidate_ValidWiringProcessorToProcessor(t *testing.T) {
	t.Parallel()

	spec := create.FlowSpec{
		Name:        "my_tool",
		Description: "A useful tool",
		Processors: []create.ProcessorRef{
			{ID: "step1", Component: "llm-agent"},
			{ID: "step2", Component: "validator"},
		},
		Wiring: []create.WiringRule{
			{From: "input.data", To: "step1.input.data"},
			{From: "step1.output.result", To: "step2.input.data"},
			{From: "step2.output.verdict", To: "output.verdict"},
		},
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for valid multi-step pipeline", err)
	}
}

func TestValidate_CycleTwoProcessors_ReturnsError(t *testing.T) {
	t.Parallel()

	// step1 → step2 → step1 forms a cycle.
	spec := create.FlowSpec{
		Name:        "my_tool",
		Description: "A cycling tool",
		Processors: []create.ProcessorRef{
			{ID: "step1", Component: "llm-agent"},
			{ID: "step2", Component: "validator"},
		},
		Wiring: []create.WiringRule{
			{From: "step1.output.a", To: "step2.input.a"},
			{From: "step2.output.b", To: "step1.input.b"},
		},
	}
	err := spec.Validate()
	if err == nil {
		t.Error("Validate() = nil, want cycle detection error")
	}
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "cycle") {
		t.Errorf("error = %q, want mention of cycle", err.Error())
	}
}

func TestValidate_SelfCycleProcessor_ReturnsError(t *testing.T) {
	t.Parallel()

	spec := create.FlowSpec{
		Name:        "my_tool",
		Description: "A self-wiring tool",
		Processors:  []create.ProcessorRef{{ID: "loop", Component: "llm-agent"}},
		Wiring: []create.WiringRule{
			{From: "loop.output.x", To: "loop.input.x"},
		},
	}
	err := spec.Validate()
	if err == nil {
		t.Error("Validate() = nil, want error for processor wired to itself")
	}
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "cycle") {
		t.Errorf("error = %q, want mention of cycle", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Executor.Execute tests
// ---------------------------------------------------------------------------

func TestExecute_ValidSpec_ReturnsValidatedJSON(t *testing.T) {
	t.Parallel()

	exec := create.NewExecutor()
	call := makeCall("call-1", "loop-1", "trace-1", minimalSpec())

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute() result.Error = %q, want empty", result.Error)
	}
	if result.CallID != "call-1" {
		t.Errorf("CallID = %q, want %q", result.CallID, "call-1")
	}
	if result.LoopID != "loop-1" {
		t.Errorf("LoopID = %q, want %q", result.LoopID, "loop-1")
	}
	if result.TraceID != "trace-1" {
		t.Errorf("TraceID = %q, want %q", result.TraceID, "trace-1")
	}

	spec := mustUnmarshalSpec(t, result.Content)
	if spec.Name != "my_tool" {
		t.Errorf("spec.Name = %q, want %q", spec.Name, "my_tool")
	}
	if len(spec.Processors) != 1 {
		t.Fatalf("spec.Processors len = %d, want 1", len(spec.Processors))
	}
	if spec.Processors[0].ID != "step1" {
		t.Errorf("Processors[0].ID = %q, want %q", spec.Processors[0].ID, "step1")
	}
}

func TestExecute_MultiProcessorWithWiring_ReturnsValidatedJSON(t *testing.T) {
	t.Parallel()

	exec := create.NewExecutor()
	call := makeCall("call-2", "loop-1", "", map[string]any{
		"name":        "pipeline_tool",
		"description": "A two-step pipeline",
		"processors": processors(
			processor("llm", "llm-agent"),
			processor("validate", "validator"),
		),
		"wiring": wiring(
			wiringRule("input.prompt", "llm.input.prompt"),
			wiringRule("llm.output.text", "validate.input.text"),
			wiringRule("validate.output.verdict", "output.verdict"),
		),
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("Execute() result.Error = %q, want empty", result.Error)
	}

	spec := mustUnmarshalSpec(t, result.Content)
	if len(spec.Processors) != 2 {
		t.Errorf("spec.Processors len = %d, want 2", len(spec.Processors))
	}
	if len(spec.Wiring) != 3 {
		t.Errorf("spec.Wiring len = %d, want 3", len(spec.Wiring))
	}
}

func TestExecute_InvalidName_ReturnsErrorResult(t *testing.T) {
	t.Parallel()

	exec := create.NewExecutor()
	call := makeCall("call-3", "loop-1", "", map[string]any{
		"name":        "my-tool", // hyphen not allowed
		"description": "Bad name",
		"processors":  processors(processor("step1", "llm-agent")),
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error for invalid name")
	}
	if result.Content != "" {
		t.Errorf("Execute() result.Content = %q, want empty on error", result.Content)
	}
}

func TestExecute_NoProcessors_ReturnsErrorResult(t *testing.T) {
	t.Parallel()

	exec := create.NewExecutor()
	call := makeCall("call-4", "loop-1", "", map[string]any{
		"name":        "my_tool",
		"description": "No processors",
		"processors":  []any{},
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error for empty processors")
	}
}

func TestExecute_DuplicateProcessorIDs_ReturnsErrorResult(t *testing.T) {
	t.Parallel()

	exec := create.NewExecutor()
	call := makeCall("call-5", "loop-1", "", map[string]any{
		"name":        "my_tool",
		"description": "Duplicate IDs",
		"processors": processors(
			processor("dup", "llm-agent"),
			processor("dup", "validator"),
		),
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error for duplicate IDs")
	}
}

func TestExecute_InvalidWiringRef_ReturnsErrorResult(t *testing.T) {
	t.Parallel()

	exec := create.NewExecutor()
	call := makeCall("call-6", "loop-1", "", map[string]any{
		"name":        "my_tool",
		"description": "Bad wiring",
		"processors":  processors(processor("step1", "llm-agent")),
		"wiring": wiring(
			wiringRule("ghost.output.x", "step1.input.x"),
		),
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want error for invalid wiring reference")
	}
}

func TestExecute_CycleInWiring_ReturnsErrorResult(t *testing.T) {
	t.Parallel()

	exec := create.NewExecutor()
	call := makeCall("call-7", "loop-1", "", map[string]any{
		"name":        "my_tool",
		"description": "Cyclic wiring",
		"processors": processors(
			processor("a", "llm-agent"),
			processor("b", "validator"),
		),
		"wiring": wiring(
			wiringRule("a.output.x", "b.input.x"),
			wiringRule("b.output.y", "a.input.y"),
		),
	})

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("Execute() unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("Execute() result.Error is empty, want cycle detection error")
	}
	if !strings.Contains(strings.ToLower(result.Error), "cycle") {
		t.Errorf("result.Error = %q, want mention of cycle", result.Error)
	}
}

func TestExecute_ListTools_ReturnsOneDefinition(t *testing.T) {
	t.Parallel()

	exec := create.NewExecutor()
	tools := exec.ListTools()

	if len(tools) != 1 {
		t.Fatalf("ListTools() returned %d definitions, want 1", len(tools))
	}

	def := tools[0]
	if def.Name != "create_tool" {
		t.Errorf("tool Name = %q, want %q", def.Name, "create_tool")
	}
	if def.Description == "" {
		t.Error("tool Description is empty")
	}
	if def.Parameters == nil {
		t.Fatal("tool Parameters is nil")
	}

	required, ok := def.Parameters["required"].([]string)
	if !ok {
		t.Fatalf("Parameters[required] type = %T, want []string", def.Parameters["required"])
	}
	wantRequired := map[string]bool{"name": true, "description": true, "processors": true}
	for _, r := range required {
		if !wantRequired[r] {
			t.Errorf("unexpected required field %q", r)
		}
	}
	if len(required) != len(wantRequired) {
		t.Errorf("required len = %d, want %d", len(required), len(wantRequired))
	}
}
