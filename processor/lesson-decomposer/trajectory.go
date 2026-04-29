// Package lessondecomposer — trajectory.go provides the NATS request/reply
// surface for fetching agentic trajectories from the agentic-loop component.
// Phase 2b uses this to pull the developer's loop trajectory at rejection time
// so the decomposer can cite specific steps as evidence.
package lessondecomposer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

// trajectorySubject is the NATS request/reply subject the agentic-loop
// processor handles. See semstreams agentic-loop component handleTrajectoryQuery.
const trajectorySubject = "agentic.query.trajectory"

// trajectoryRequestTimeout caps the agentic-loop response window. The handler
// is in-memory and serves either from cache or from the live trajectory
// manager — 5s is generous for both paths.
const trajectoryRequestTimeout = 5 * time.Second

// trajectoryRequester is the small surface from natsclient.Client we need.
// Defined as an interface so unit tests can swap in a stub without spinning
// up an embedded NATS server.
type trajectoryRequester interface {
	Request(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)
}

// fetchTrajectory pulls a single trajectory by loop ID via NATS request/reply.
// The optional limit truncates Steps to the first N entries — useful when the
// trajectory is large and the prompt budget is tight. Pass 0 to receive every
// step.
//
// Returns ErrTrajectoryNotFound when the agentic-loop responds with a
// not-found error, so callers can distinguish "missing" from "transport
// failure" and avoid retrying for ever.
func fetchTrajectory(ctx context.Context, client trajectoryRequester, loopID string, limit int) (*agentic.Trajectory, error) {
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

	resp, err := client.Request(ctx, trajectorySubject, data, trajectoryRequestTimeout)
	if err != nil {
		return nil, classifyTrajectoryError(err)
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

// ErrTrajectoryNotFound is returned when the agentic-loop responder reports
// that no trajectory exists for the requested loop ID. Callers should treat
// this as terminal for that loop — retrying will not help.
var ErrTrajectoryNotFound = fmt.Errorf("trajectory not found")

// classifyTrajectoryError converts NATS request errors that originate from
// the responder's "trajectory not found" branch into ErrTrajectoryNotFound,
// so the decomposer can distinguish missing data from transport failure.
func classifyTrajectoryError(err error) error {
	if err == nil {
		return nil
	}
	// agentic-loop's handler returns: fmt.Errorf("trajectory not found: %w", ...)
	// NATS request/reply propagates the error string back as the body of an
	// error response. The Client surfaces this as a normal error.
	if strContains(err.Error(), "trajectory not found") {
		return fmt.Errorf("%w: %v", ErrTrajectoryNotFound, err)
	}
	return fmt.Errorf("trajectory request: %w", err)
}

// strContains is a tiny helper to avoid importing strings in the hot path.
func strContains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// summarizeStep produces a prompt-safe one-line summary of a trajectory step.
// Verbose tool results and full prompts are clipped to maxLen runes; the
// decomposer does not need the full text — citing the step index back to the
// trajectory viewer is enough for a human to follow the link.
//
// Used by the prompt builder (Phase 2b commit 2) to keep input within the
// decomposer's ~4-7K token budget per ADR-033.
func summarizeStep(step agentic.TrajectoryStep, maxLen int) string {
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
		return clip(fmt.Sprintf("%s", step.StepType), maxLen)
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
