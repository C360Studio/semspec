package domain

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
)

// TestRenderArchitectPrompt_RevisionBaseInjected verifies the architect prompt
// carries the prior architecture as a revise-don't-rewrite base when
// PreviousArchitectureJSON is set (Phase 1 #2), and elides it otherwise.
func TestRenderArchitectPrompt_RevisionBaseInjected(t *testing.T) {
	t.Parallel()
	const priorJSON = `{"decisions":[{"title":"keep the existing driver"}]}`

	with := renderArchitectPrompt(&prompt.ArchitectPromptContext{
		Goal:                     "g",
		PreviousArchitectureJSON: priorJSON,
	})
	if !strings.Contains(with, "Previous Architecture") {
		t.Error("architect prompt missing the revision-base header when PreviousArchitectureJSON set")
	}
	if !strings.Contains(with, priorJSON) {
		t.Errorf("architect prompt missing the prior architecture JSON body\n--- prompt ---\n%s", with)
	}

	without := renderArchitectPrompt(&prompt.ArchitectPromptContext{Goal: "g"})
	if strings.Contains(without, "Previous Architecture (Revise") {
		t.Error("revision base rendered on a first-pass dispatch (PreviousArchitectureJSON empty)")
	}
}

// TestRenderRecoveryAgentPrompt_ArchitectureContext verifies recovery sees the
// architecture surface + the diagnosis steer when the wedged plan has an
// architecture, and that the prompt does not reference a non-existent action.
func TestRenderRecoveryAgentPrompt_ArchitectureContext(t *testing.T) {
	t.Parallel()
	out := renderRecoveryAgentPrompt(&prompt.RecoveryPromptContext{
		Slug:                "demo",
		EscalationReason:    "dev could not resolve the dependency",
		ArchitectureContext: "## Architecture Context\n\n### Resolved Upstream Dependencies\n\n- **MAVSDK** `io.mavsdk:mavsdk:3.16.0`\n",
	})
	if !strings.Contains(out, "io.mavsdk:mavsdk:3.16.0") {
		t.Error("recovery prompt missing the architecture context")
	}
	if !strings.Contains(out, "escalate_human") {
		t.Error("recovery prompt should steer architecture-root wedges to escalate_human (the honest interim landing)")
	}
	if strings.Contains(out, "architecture_revise") {
		t.Error("recovery prompt must NOT name architecture_revise — that action does not exist until a later phase")
	}
}
