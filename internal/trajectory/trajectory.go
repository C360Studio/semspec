// Package trajectory provides a small client surface for fetching agentic
// loop trajectories from the agentic-loop component over NATS request/reply,
// plus prompt-safe summarisation helpers. Originally lived in
// processor/lesson-decomposer; promoted here so other components
// (execution-manager, plan-manager, reviewers, validators) can consume the
// same trajectory data on their escalation/rejection events. See ADR-037
// Stage 0 for the broader consumer set.
package trajectory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

// Subject is the NATS request/reply subject the agentic-loop processor
// handles. See semstreams agentic-loop component handleTrajectoryQuery.
const Subject = "agentic.query.trajectory"

// DefaultTimeout caps the agentic-loop response window. The handler is
// in-memory and serves either from cache or from the live trajectory
// manager — 5s is generous for both paths.
const DefaultTimeout = 5 * time.Second

// Requester is the small surface from natsclient.Client we need. Defined as
// an interface so unit tests can swap in a stub without spinning up an
// embedded NATS server, and so non-NATS callers (in-process tests, future
// gRPC adapters) can inject their own transport.
type Requester interface {
	Request(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)
}

// ErrNotFound is returned when the agentic-loop responder reports that no
// trajectory exists for the requested loop ID. Callers should treat this as
// terminal for that loop — retrying will not help.
var ErrNotFound = fmt.Errorf("trajectory not found")

// Fetch pulls a single trajectory by loop ID via NATS request/reply. The
// optional limit truncates Steps to the first N entries — useful when the
// trajectory is large and the prompt budget is tight. Pass 0 to receive
// every step.
//
// Returns ErrNotFound when the agentic-loop responds with a not-found
// error, so callers can distinguish "missing" from "transport failure" and
// avoid retrying for ever.
func Fetch(ctx context.Context, client Requester, loopID string, limit int) (*agentic.Trajectory, error) {
	if client == nil {
		return nil, fmt.Errorf("nats client required")
	}
	if loopID == "" {
		return nil, fmt.Errorf("loop id required")
	}

	req := struct {
		LoopID string `json:"loopId"`
		Limit  int    `json:"limit,omitempty"`
	}{LoopID: loopID, Limit: limit}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal trajectory request: %w", err)
	}

	resp, err := client.Request(ctx, Subject, data, DefaultTimeout)
	if err != nil {
		return nil, classifyError(err)
	}

	var traj agentic.Trajectory
	if err := json.Unmarshal(resp, &traj); err != nil {
		return nil, fmt.Errorf("unmarshal trajectory: %w", err)
	}
	if traj.LoopID == "" {
		// agentic-loop returns a populated Trajectory or an error — empty
		// LoopID without an error means we got something unexpected on the
		// wire (older responder, mock that returns {}, etc.).
		return nil, fmt.Errorf("trajectory response has empty loop_id")
	}
	return &traj, nil
}

// classifyError converts NATS request errors that originate from the
// responder's "trajectory not found" branch into ErrNotFound, so callers
// can distinguish missing data from transport failure.
func classifyError(err error) error {
	if err == nil {
		return nil
	}
	// agentic-loop's handler returns: fmt.Errorf("trajectory not found: %w", ...)
	// NATS request/reply propagates the error string back as the body of an
	// error response. The Client surfaces this as a normal error.
	if strings.Contains(err.Error(), "trajectory not found") {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return fmt.Errorf("trajectory request: %w", err)
}

// SummarizeStep produces a prompt-safe one-line summary of a trajectory
// step. Verbose tool results and full prompts are clipped to maxLen runes;
// callers do not need the full text — citing the step index back to the
// trajectory viewer is enough for a human to follow the link.
//
// Used by lesson-decomposer's prompt builder to keep input within the
// ~4-7K token budget per ADR-033, and by escalation-event log lines in
// other components per ADR-037 Stage 0.
func SummarizeStep(step agentic.TrajectoryStep, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 200
	}

	switch step.StepType {
	case "model_call":
		return clip(fmt.Sprintf("model_call(%s)", firstNonEmpty(step.Model, step.Capability)), maxLen)
	case "tool_call":
		base := fmt.Sprintf("tool_call(%s)", step.ToolName)
		if step.ToolStatus == "failed" {
			base += " FAILED:" + clip(step.ErrorMessage, 80)
		} else if step.ToolResult != "" {
			base += " → " + clip(step.ToolResult, 80)
		}
		return clip(base, maxLen)
	case "context_compaction":
		return clip(fmt.Sprintf("context_compaction(util=%.2f)", step.Utilization), maxLen)
	default:
		return clip(step.StepType, maxLen)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func clip(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}
