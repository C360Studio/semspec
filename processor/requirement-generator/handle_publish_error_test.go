package requirementgenerator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/dispatchretry"
	"github.com/c360studio/semstreams/agentic"
)

// TestHandlePublishError_ValidatorRejectionRoutesToRetry pins the
// cross-package contract between plan-manager's MutationResponse.Error and
// requirement-generator's retry routing. The validators in workflow/ wrap
// ErrInvalidFileOwnership / ErrInvalidRequirementDAG with %w, plan-manager
// surfaces err.Error() into the response, and handlePublishError has to
// recognize the sentinel text to route the failure into retryOrFail (so
// the agent regenerates) instead of terminal generation-failed (which
// kills the plan).
//
// Without this test, the contract was a string-literal Sprintf in one
// package matched by strings.Contains in another with no test coupling
// them — exactly the silent failure that stalled
// project_gemini_easy_2026_04_29's run at reviewing_qa.
//
// Only the validator-rejection paths are exercised here. The terminal
// infrastructure-failure path calls sendGenerationFailed which dispatches
// a NATS request and isn't worth wiring a fake client for; the contract
// risk lives entirely in the sentinel matching, which this covers.
func TestHandlePublishError_ValidatorRejectionRoutesToRetry(t *testing.T) {
	mkComponent := func() *Component {
		return &Component{
			logger: slog.Default(),
			retry: dispatchretry.New(dispatchretry.Config{
				MaxRetries: 3,
				BackoffMs:  1,
			}),
		}
	}

	loop := &agentic.LoopEntity{ID: "test-loop"}
	slug := "test-slug"

	cases := []struct {
		name        string
		sentinel    error
		extraDetail string
	}{
		{
			name:        "file ownership rejection",
			sentinel:    workflow.ErrInvalidFileOwnership,
			extraDetail: `requirement "req.1" has empty files_owned`,
		},
		{
			name:        "DAG rejection",
			sentinel:    workflow.ErrInvalidRequirementDAG,
			extraDetail: `requirement "req.1" depends on itself`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mkComponent()
			c.retry.Track(slug, &pendingDispatch{})
			var retryCalled atomic.Bool
			retryOrFail := func(_ string) { retryCalled.Store(true) }

			// Construct an error matching the wire shape: validator wraps the
			// sentinel; plan-manager passes err.Error() through; publishResults
			// prefixes "requirements mutation failed: ".
			validatorErr := fmt.Errorf("%w: %s", tc.sentinel, tc.extraDetail)
			wireErr := fmt.Errorf("requirements mutation failed: %s", validatorErr.Error())

			c.handlePublishError(context.Background(), slug, loop, wireErr, retryOrFail)

			if !retryCalled.Load() {
				t.Errorf("expected validator-rejection (%s) to trigger retry, but retry was not called", tc.name)
			}
			if c.generationsFailed.Load() != 0 {
				t.Errorf("validator-rejection (%s) must not increment generationsFailed, got %d",
					tc.name, c.generationsFailed.Load())
			}
		})
	}
}

// TestPublishErrorContract_SentinelTextStable belt-and-suspenders: even
// without spinning up Component, prove the sentinel error strings are
// what handlePublishError matches against. Catches the case where someone
// renames the sentinel without realizing the matcher uses .Error().
func TestPublishErrorContract_SentinelTextStable(t *testing.T) {
	if got := workflow.ErrInvalidFileOwnership.Error(); !strings.Contains(got, "file ownership") {
		t.Errorf("ErrInvalidFileOwnership text drifted: %q — handlePublishError matcher must be updated", got)
	}
	if got := workflow.ErrInvalidRequirementDAG.Error(); !strings.Contains(got, "DAG") {
		t.Errorf("ErrInvalidRequirementDAG text drifted: %q — handlePublishError matcher must be updated", got)
	}
}
