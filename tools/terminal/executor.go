// Package terminal provides terminal tools that signal loop completion.
// Terminal tools return ToolResult with StopLoop=true, which causes the
// semstreams agentic loop to exit immediately. The Content becomes
// the LoopCompletedEvent.Result.
//
// submit_work arguments ARE the structured output — the fields described
// in the agent's output format instructions are passed directly as arguments.
package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/c360studio/semstreams/agentic"
)

// Executor handles the submit_work terminal tool.
//
// workDir + tripleEmitter are optional. When both are set, the
// planner scope.include vs scope.create structural check fires at
// submit_work time: any path under args.scope.include that doesn't
// exist on disk turns the submission into a directive RETRY HINT
// telling the model to move it to scope.create. See scope_validator.go.
type Executor struct {
	workDir       string
	tripleEmitter scopeValidatorTripleEmitter
}

// CallContext mirrors tools/bash.CallContext: per-call attribution
// for SKG triples. Sourced from agentic.ToolCall.LoopID + Metadata
// (role, model) at submit_work time.
type CallContext struct {
	CallID string // loop ID
	Role   string // planner | reviewer | developer | ...
	Model  string // endpoint name from the registry
}

// NewExecutor creates a terminal tool executor.
func NewExecutor() *Executor {
	return &Executor{}
}

// WithWorkDir sets the workspace root used by the planner scope
// validator. When empty (default), the scope check is skipped.
// Returns the receiver for chaining.
func (e *Executor) WithWorkDir(dir string) *Executor {
	e.workDir = dir
	return e
}

// WithTripleEmitter installs a triple writer so per-fire SKG triples
// get emitted alongside the WARN log + Prom counter increment when
// the planner scope check fires. Returns the receiver for chaining.
// Nil-safe.
func (e *Executor) WithTripleEmitter(tw scopeValidatorTripleEmitter) *Executor {
	e.tripleEmitter = tw
	return e
}

// ListTools returns the terminal tool definitions.
// The global schema uses additionalProperties as a fallback. Prefer passing
// per-role schemas via ToolsForDeliverable() in TaskMessage.Tools.
func (e *Executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "submit_work",
			Description: "Submit your completed work. Pass your output fields as named parameters.",
			Parameters: map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
		},
		// ask_question is handled by tools/question/executor.go (non-terminal tool).
	}
}

// Execute handles terminal tool calls.
func (e *Executor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "submit_work":
		return e.submitWork(ctx, call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown terminal tool: %s", call.Name),
		}, nil
	}
}

// submitWork signals task completion. The call arguments ARE the structured output —
// agents pass their deliverable fields as top-level named parameters.
// The JSON content becomes the LoopCompletedEvent.Result for downstream parsers.
//
// When deliverable_type metadata is present, arguments are validated against the
// role-specific schema. Validation errors return StopLoop=false so the LLM can
// fix and retry within the same loop iteration.
//
// For deliverable_type="plan" with workDir configured, an additional
// structural check runs after the schema validator: every path under
// scope.include must exist on disk. Misses return a directive
// RETRY HINT (see scope_validator.go) — the agent reads this on its
// next turn and either moves the path to scope.create or removes it.
func (e *Executor) submitWork(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	deliverableType, _ := call.Metadata["deliverable_type"].(string)

	// Arguments ARE the deliverable — no result wrapper.
	args := call.Arguments
	if len(args) == 0 {
		hint := ExpectedFieldsHint(deliverableType)
		slog.Warn("submit_work called with empty arguments",
			"call_id", call.ID,
			"deliverable_type", deliverableType)
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("submit_work requires named parameters. %s", hint),
		}, nil
	}

	if validator := GetDeliverableValidator(deliverableType); validator != nil {
		if err := validator(args); err != nil {
			slog.Warn("submit_work validation failed",
				"deliverable_type", deliverableType,
				"error", err.Error(),
				"call_id", call.ID,
				"keys", deliverableKeys(args))
			return agentic.ToolResult{
				CallID: call.ID,
				Error:  fmt.Sprintf("validation failed: %s", err.Error()),
			}, nil
		}
	}

	// Planner scope.include vs scope.create structural check —
	// recurring planner bug captured in
	// project_planner_dashed_paths_cascade_2026_05_03.md (Bug #5).
	// Reproduced 2026-05-05 on gemini-pro: planner submitted
	// scope.include with create-paths, plan-reviewer rejected 3x
	// with the same complaint, escalated. This check rejects at the
	// planner boundary so the model gets a directive hint *before*
	// reviewer cycles burn tokens. Skipped when workDir is unset.
	if deliverableType == "plan" {
		cc := CallContext{
			CallID: call.LoopID,
			Role:   stringFromMetadata(call.Metadata, "role"),
			Model:  stringFromMetadata(call.Metadata, "model"),
		}
		if hint := validatePlanScope(ctx, e.workDir, e.tripleEmitter, cc, args); hint != "" {
			return agentic.ToolResult{
				CallID: call.ID,
				Error:  hint,
			}, nil
		}
	}

	data, _ := json.Marshal(args)
	slog.Info("submit_work accepted — returning StopLoop=true",
		"call_id", call.ID,
		"deliverable_type", deliverableType,
		"keys", deliverableKeys(args))
	return agentic.ToolResult{
		CallID:   call.ID,
		Content:  string(data),
		StopLoop: true,
	}, nil
}

// stringFromMetadata fetches a string from the tool-call metadata,
// returning "" on absence or wrong type.
func stringFromMetadata(md map[string]any, key string) string {
	if md == nil {
		return ""
	}
	v, _ := md[key].(string)
	return v
}

// deliverableKeys returns a diagnostic string showing each key and its value type.
// Example: "goal:[]interface{}(2), context:string, scope:map[string]interface{}"
func deliverableKeys(d map[string]any) string {
	parts := make([]string, 0, len(d))
	for k, v := range d {
		switch val := v.(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%s:string(%d)", k, len(val)))
		case []any:
			parts = append(parts, fmt.Sprintf("%s:[]any(%d)", k, len(val)))
		case map[string]any:
			parts = append(parts, fmt.Sprintf("%s:map(%d)", k, len(val)))
		case float64:
			parts = append(parts, fmt.Sprintf("%s:number", k))
		case bool:
			parts = append(parts, fmt.Sprintf("%s:bool", k))
		case nil:
			parts = append(parts, fmt.Sprintf("%s:null", k))
		default:
			parts = append(parts, fmt.Sprintf("%s:%T", k, v))
		}
	}
	return strings.Join(parts, ", ")
}
