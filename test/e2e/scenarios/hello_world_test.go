package scenarios

import (
	"testing"

	"github.com/c360studio/semspec/test/e2e/client"
)

// TestVerifyRequirementRetryStats proves the exec-requirement-retry assertion
// fails closed. The scenario's whole reason to exist is catching a regression
// where the in-TDD-cycle code reviewer stops rejecting and approves the dev's
// first submission outright — the false-green this fixture set used to be. A
// single mock-code-reviewer call (reject never fired) MUST be a failure, two
// calls (reject + approve) MUST pass.
func TestVerifyRequirementRetryStats(t *testing.T) {
	retry := &HelloWorldScenario{variant: HelloWorldVariant{ExpectRequirementRetry: true}}

	// 1 call = the reject/retry never fired → must error (fail closed).
	if err := retry.verifyRequirementRetryStats(
		&client.MockStats{CallsByModel: map[string]int64{"mock-code-reviewer": 1}},
		NewResult("neg-one-call"),
	); err == nil {
		t.Fatal("expected error when mock-code-reviewer called once (retry never fired), got nil")
	}

	// 0 calls (model absent) → must also error.
	if err := retry.verifyRequirementRetryStats(
		&client.MockStats{CallsByModel: map[string]int64{}},
		NewResult("neg-zero-calls"),
	); err == nil {
		t.Fatal("expected error when mock-code-reviewer never called, got nil")
	}

	// 2 calls = cycle-0 reject + cycle-1 approve → must pass.
	if err := retry.verifyRequirementRetryStats(
		&client.MockStats{CallsByModel: map[string]int64{"mock-code-reviewer": 2}},
		NewResult("pos"),
	); err != nil {
		t.Fatalf("expected pass at 2 mock-code-reviewer calls (reject+approve), got %v", err)
	}

	// Non-retry variants must be a no-op, even with zero reviewer calls, so the
	// check never spuriously fails the happy/exhaustion/rejection scenarios.
	happy := &HelloWorldScenario{}
	if err := happy.verifyRequirementRetryStats(
		&client.MockStats{CallsByModel: map[string]int64{}},
		NewResult("happy"),
	); err != nil {
		t.Fatalf("non-retry variant should be a no-op, got %v", err)
	}
}

// TestRequiredMockModelsGateOnRetryVariant proves the retry variant gates its
// /stats poll on mock-code-reviewer — without this the assertion could be
// skipped if the model simply never appeared — while the happy path does not
// over-require it.
func TestRequiredMockModelsGateOnRetryVariant(t *testing.T) {
	retry := &HelloWorldScenario{variant: HelloWorldVariant{ExpectRequirementRetry: true}}
	if !containsString(retry.requiredMockModels(), "mock-code-reviewer") {
		t.Fatal("retry variant must require mock-code-reviewer in the /stats poll")
	}
	happy := &HelloWorldScenario{}
	if containsString(happy.requiredMockModels(), "mock-code-reviewer") {
		t.Fatal("non-retry variant should not require mock-code-reviewer")
	}
}
