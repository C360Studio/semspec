package workflow

import "testing"

func TestQALevelIntegrationIsExecutable(t *testing.T) {
	if !QALevelIntegration.IsValid() {
		t.Fatal("integration should be a valid QA level")
	}
	if !QALevelIntegration.UsesSandboxTests() {
		t.Fatal("integration should run the configured QA command in the sandbox")
	}
	if QALevel("full").IsValid() {
		t.Fatal("full remains outside MVP sandbox execution and should not be a valid snapshotted level")
	}
}

func TestPlanEffectiveQALevelPreservesIntegration(t *testing.T) {
	plan := &Plan{QALevel: QALevelIntegration}
	if got := plan.EffectiveQALevel(); got != QALevelIntegration {
		t.Fatalf("EffectiveQALevel() = %q, want integration", got)
	}
}
