package recoveryagent

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/internal/trajectory"
	"github.com/c360studio/semstreams/agentic"
)

// systemPrompt is the recovery-agent's fixed persona. The agent's only
// deliverable is a single submit_work call with a RecoveryAction; no bash,
// no graph, no other tools. The persona is intentionally narrow so a small
// or mid-tier model can hold it.
const systemPrompt = `You are a recovery agent for an automated software-development pipeline.

A specialised agent (a planner, requirement generator, or developer) got wedged: it failed repeatedly within its retry budget and the pipeline escalated. Your job is to diagnose the wedge by reading the agent's trajectory plus the feedback it received, then pick exactly ONE recovery action from the closed set below.

You are NOT the wedged agent. You are NOT here to retry their work. You read the evidence and pick an action.

CLOSED ACTION SET (output one of these via submit_work):

- refine_prompt — rewrite the wedged agent's task prompt with explicit context they missed. Use this when the trajectory shows they had the answer in front of them (e.g. a graph_search hit, a reviewer hint) but didn't act on it. REQUIRES "refined_prompt" field.
- narrow_scope — reduce the wedged task's scope (e.g. limit to one file, one dependency). Use when the trajectory shows thrashing across too many concerns. Provide scope_changes as a structured JSON object describing the reduction.
- split_req — decompose the requirement into smaller requirements. Heavier than narrow_scope; only when the plan-level decomposition was clearly wrong.
- escalate_human — you analysed the wedge and the diagnosis is the deliverable; no programmatic action fits. Surfaces in the UI with your diagnosis.
- mark_unrecoverable — the goal cannot succeed from current state regardless of agent (upstream artifact missing, fixture malformed, scope contradicts another requirement).

Required fields on your submit_work output (JSON):
  {
    "action": "<one of the five above>",
    "diagnosis": "<2-6 sentences: what the trajectory shows the agent doing wrong, and what the underlying mistake is>",
    "refined_prompt": "<required only when action is refine_prompt; a complete replacement task prompt>",
    "scope_changes": <optional JSON object; for narrow_scope/split_req>,
    "recovery_succeeded": <true when refine_prompt/narrow_scope/split_req plausibly fixes the wedge; false for escalate_human/mark_unrecoverable>
  }

Rules:
1. diagnosis is REQUIRED for every action including escalate_human and mark_unrecoverable. The diagnosis IS the deliverable for those two — write it carefully.
2. Do not add new action types. The set is closed.
3. Do not call other tools. Only submit_work.
4. If the trajectory is unavailable, work from the escalation reason and last feedback alone — diagnose what you can.
5. Quote 1-3 short trajectory excerpts in your diagnosis when available; that's the evidence trail the lessons pipeline keys off.`

// buildUserPrompt assembles the per-wedge context block: escalation reason,
// last failure feedback, plan/req/task IDs, and a numbered trajectory step
// list. Designed to fit a generous-but-bounded context window — the
// trajectory step summaries are clipped to ~200 chars each per
// trajectory.SummarizeStep.
func buildUserPrompt(req recoveryPromptInput) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "RECOVERY REQUEST\n\n")
	fmt.Fprintf(&sb, "Layer: %s\n", req.Layer)
	fmt.Fprintf(&sb, "Plan slug: %s\n", req.Slug)
	if req.RequirementID != "" {
		fmt.Fprintf(&sb, "Requirement ID: %s\n", req.RequirementID)
	}
	if req.TaskID != "" {
		fmt.Fprintf(&sb, "Task ID: %s\n", req.TaskID)
	}
	if req.LoopID != "" {
		fmt.Fprintf(&sb, "Wedged agent loop ID: %s\n", req.LoopID)
	}
	if req.PriorRecoveryID != "" {
		fmt.Fprintf(&sb, "Prior recovery attempt: %s (this is a coordinator-layer retry — pick a different action shape than the prior layer)\n", req.PriorRecoveryID)
	}

	fmt.Fprintf(&sb, "\nESCALATION REASON\n%s\n", req.EscalationReason)

	if req.LastFailureFeedback != "" {
		fmt.Fprintf(&sb, "\nLAST FAILURE FEEDBACK (what the wedged agent was responding to before escalation)\n%s\n", req.LastFailureFeedback)
	}

	if len(req.TrajectorySteps) == 0 {
		fmt.Fprintf(&sb, "\nTRAJECTORY\n(no trajectory available — work from the escalation reason and last failure feedback)\n")
	} else {
		fmt.Fprintf(&sb, "\nTRAJECTORY (%d steps, may be capped)\n", len(req.TrajectorySteps))
		for i, summary := range req.TrajectorySteps {
			fmt.Fprintf(&sb, "  [%d] %s\n", i, summary)
		}
	}

	fmt.Fprintf(&sb, "\n---\nDiagnose the wedge from the evidence above and call submit_work with your chosen RecoveryAction. Do not call any other tool.")
	return sb.String()
}

// recoveryPromptInput is the pre-resolved view buildUserPrompt renders.
// Decoupled from the wire payload so buildUserPrompt stays testable
// without dragging in NATS / trajectory fetch dependencies.
type recoveryPromptInput struct {
	Layer               string
	Slug                string
	RequirementID       string
	TaskID              string
	LoopID              string
	PriorRecoveryID    string
	EscalationReason    string
	LastFailureFeedback string
	TrajectorySteps     []string
}

// summarizeTrajectory turns an agentic.Trajectory into the per-step summary
// lines buildUserPrompt expects, applying the configured cap. Returns nil
// for nil/empty trajectories so the prompt builder can branch on "no
// trajectory available."
func summarizeTrajectory(traj *agentic.Trajectory, limit int) []string {
	if traj == nil || len(traj.Steps) == 0 {
		return nil
	}
	if limit <= 0 || limit > trajectory.DefaultLogStepLimit {
		limit = trajectory.DefaultLogStepLimit
	}
	n := len(traj.Steps)
	if n > limit {
		n = limit
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, trajectory.SummarizeStep(traj.Steps[i], 200))
	}
	return out
}
