// Package recoveryagent implements ADR-037 stage 1: a JetStream processor
// that consumes RecoveryRequested events, dispatches a manager-role
// recovery agent via the agentic-loop, and emits the chosen RecoveryAction
// as a workflow.PlanDecision through the standard plan.mutation.plan_decision.add
// wire (qa-reviewer + req-executor's pattern).
package recoveryagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow/jsonutil"
	"github.com/c360studio/semspec/workflow/payloads"
)

// rawRecoveryResult is what the recovery agent's submit_work tool delivers.
// JSON-unmarshalled then validated against the closed action set + per-
// action required fields. The dispatcher consumes this struct directly
// (it doesn't survive past handleLoopCompletion); the wire output is the
// PlanDecision built in emitPlanDecision.
type rawRecoveryResult struct {
	Action        string `json:"action"`
	Diagnosis     string `json:"diagnosis"`
	RefinedPrompt string `json:"refined_prompt,omitempty"`
	// Feedback is NOT a schema field — the prompt asks for refined_prompt. But
	// mid-tier models recurringly write the fix prose under "feedback" instead
	// (observed 2026-06-13 gemini-pro mavlink-hard run 2). Captured so the
	// action-inference net below can adopt it rather than terminal-fail a
	// recoverable wedge.
	Feedback          string          `json:"feedback,omitempty"`
	ScopeChanges      json.RawMessage `json:"scope_changes,omitempty"`
	RecoverySucceeded bool            `json:"recovery_succeeded"`
}

// parsedRecoveryResult is the local typed view of a successful recovery
// agent submit_work output. Distinct from the wire payload (the wire
// output is workflow.PlanDecision); this struct carries just the fields
// the dispatcher needs to build the PlanDecision.
type parsedRecoveryResult struct {
	Action            payloads.RecoveryActionKind
	Diagnosis         string
	RefinedPrompt     string
	ScopeChanges      json.RawMessage
	RecoverySucceeded bool
}

var (
	errResultEmpty           = errors.New("recovery agent returned empty result")
	errResultMissingAction   = errors.New("recovery agent result missing action")
	errResultMissingDiag     = errors.New("recovery agent result missing diagnosis")
	errResultInvalidAction   = errors.New("recovery agent action is not in the closed set")
	errResultRefineNeedsPrmt = errors.New("recovery agent picked refine_prompt without a refined_prompt")
)

// parseRecoveryResult turns the agent's loop.Result blob into a typed
// parsedRecoveryResult.
//
// Returns an error when the result is empty, missing required fields,
// or names an action outside the closed set per ADR-037. The dispatcher
// translates the error into a PlanDecision with kind=execution_exhausted
// + escalate_human-shaped rationale (per ADR-037 stage-1 design lock #3 —
// distinct failure signal beats silent drop).
func parseRecoveryResult(raw string) (*parsedRecoveryResult, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errResultEmpty
	}

	// LLM responses commonly arrive wrapped in markdown code fences
	// (```json ... ```), prose preambles, or trailing commentary. Strip
	// these via the shared jsonutil helper before unmarshaling — same
	// pattern the rest of the codebase uses for LLM JSON output.
	// Caught 2026-05-10 take 6: gemini-pro returned a fenced result and
	// json.Unmarshal failed with `invalid character '\`'` → recovery
	// fell back to the parse-failure marker and the original diagnosis
	// was lost.
	if cleaned := jsonutil.ExtractJSON(raw); cleaned != "" {
		raw = cleaned
	}

	var rr rawRecoveryResult
	if err := json.Unmarshal([]byte(raw), &rr); err != nil {
		return nil, fmt.Errorf("unmarshal recovery result: %w", err)
	}
	// Recovery agents (esp. mid-tier models) sometimes write an action's
	// payload but omit the structured `action` label, describing the fix only
	// in prose. Rather than terminal-fail a wedge the agent believed
	// recoverable, infer the action when the payload makes it UNAMBIGUOUS.
	// Only refine_prompt qualifies: a non-empty refined_prompt has exactly one
	// matching action. scope_changes is deliberately NOT inferred — it maps to
	// both narrow_scope and split_req, so guessing would pick the wrong one.
	// Caught 2026-06-06 gemini mavlink-hard run #7: the agent emitted a
	// refine_prompt-shaped diagnosis ("needs a refined prompt with explicit
	// file paths") + recovery_succeeded but no action → execution_exhausted →
	// requirement terminal-failed → plan rejected. The prompt-side fix
	// (action-first directive) is primary; this is the parse-side safety net.
	if rr.Action == "" {
		// The fix prose belongs in refined_prompt, but gemini-pro (mavlink-hard
		// run 2, 2026-06-13) emitted it under the non-schema field `feedback` AND
		// omitted action → execution_exhausted → plan rejected, even though the
		// agent believed the wedge recoverable. Adopt `feedback` as the refined
		// prompt when refined_prompt is absent, so the inference net below still
		// fires. (project_recovery_agent_missing_action_2026_06_13)
		if strings.TrimSpace(rr.RefinedPrompt) == "" && strings.TrimSpace(rr.Feedback) != "" {
			rr.RefinedPrompt = rr.Feedback
		}
		if strings.TrimSpace(rr.RefinedPrompt) != "" {
			rr.Action = string(payloads.RecoveryActionRefinePrompt)
		} else {
			return nil, errResultMissingAction
		}
	}
	if rr.Diagnosis == "" {
		return nil, errResultMissingDiag
	}

	action := payloads.RecoveryActionKind(rr.Action)
	switch action {
	case payloads.RecoveryActionRefinePrompt,
		payloads.RecoveryActionNarrowScope,
		payloads.RecoveryActionSplitReq,
		payloads.RecoveryActionStoryReprepare,
		payloads.RecoveryActionArchitectureRevise,
		payloads.RecoveryActionEscalateHuman,
		payloads.RecoveryActionMarkUnrecoverable:
	default:
		return nil, fmt.Errorf("%w: %q", errResultInvalidAction, rr.Action)
	}
	if action == payloads.RecoveryActionRefinePrompt && strings.TrimSpace(rr.RefinedPrompt) == "" {
		return nil, errResultRefineNeedsPrmt
	}

	return &parsedRecoveryResult{
		Action:            action,
		Diagnosis:         rr.Diagnosis,
		RefinedPrompt:     rr.RefinedPrompt,
		ScopeChanges:      rr.ScopeChanges,
		RecoverySucceeded: rr.RecoverySucceeded,
	}, nil
}
