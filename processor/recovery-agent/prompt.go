package recoveryagent

import (
	"github.com/c360studio/semspec/internal/trajectory"
	"github.com/c360studio/semstreams/agentic"
)

// summarizeTrajectory turns an agentic.Trajectory into the per-step summary
// lines the recovery-agent dispatch passes to prompt.RecoveryPromptContext.
// Returns nil for nil/empty trajectories so the user-prompt renderer can
// branch on "no trajectory available."
//
// Note: the legacy hand-rolled systemPrompt + buildUserPrompt + the
// recoveryPromptInput struct that previously lived here were retired
// 2026-05-11 when the recovery-agent dispatch was wired through the
// persona-fragment assembler. The trajectory summariser stayed because
// it bridges semstreams' wire-format trajectory into the
// prompt.RecoveryPromptContext.TrajectorySteps the user-prompt fragment
// reads — pure rendering of the per-wedge context, no persona content.
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
