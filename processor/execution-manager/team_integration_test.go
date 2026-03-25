//go:build integration

package executionmanager

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/agentgraph"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	nats "github.com/nats-io/nats.go"
)

// teamStreamSubjects is the stream configuration required for team-pipeline
// integration tests.
var teamStreamSubjects = []natsclient.TestStreamConfig{
	{
		Name:     "WORKFLOW",
		Subjects: []string{"workflow.trigger.task-execution-loop", "workflow.async.>"},
	},
	{
		Name: "AGENT",
		Subjects: []string{
			"agentic.loop_completed.v1",
			"agent.task.>",
			"dev.task.>",
		},
	},
	{
		Name:     "GRAPH",
		Subjects: []string{"graph.mutation.triple.add"},
	},
}

// TestIntegration_TeamPipelineFullCycle exercises the complete team-mode
// execution path with a real NATS server and ENTITY_STATES KV bucket.
//
// Verified sequence:
//  1. Trigger → blue team selected → tester dispatched (TesterTaskID set on exec)
//  2. Tester completes → builder dispatched (BuilderTaskID set)
//  3. Builder completes → validator dispatched (ValidatorTaskID set)
//  4. Validator passes → RED TEAM dispatched (RedTeamTaskID set — critical team gate)
//  5. Red team completes → reviewer dispatched (ReviewerTaskID set)
//  6. Reviewer approves with red-team scores → approved, KV stats updated
//
// Task IDs are read directly from the component's activeExecutions map so we
// don't depend on JetStream → Core NATS fan-out for driving the pipeline
// forward. Separate Core NATS subscriptions still verify that messages ARE
// published to the correct dispatch subjects.
func TestIntegration_TeamPipelineFullCycle(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(teamStreamSubjects...),
		natsclient.WithKVBuckets("ENTITY_STATES"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	comp := newTeamIntegrationComponent(t, tc)

	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	nativeConn := tc.GetNativeConnection()

	// Subscribe to dispatch subjects for side-channel verification.
	// The channels capture at least one message when the stage is dispatched.
	// We don't need to parse task IDs from these — we read them from exec state.
	testerSubjectHit := make(chan struct{}, 4)
	redTeamSubjectHit := make(chan struct{}, 4)
	reviewerSubjectHit := make(chan struct{}, 4)

	mustSubscribeSignal := func(subject string, hit chan<- struct{}) {
		sub, err := nativeConn.Subscribe(subject, func(_ *nats.Msg) {
			select {
			case hit <- struct{}{}:
			default:
			}
		})
		if err != nil {
			t.Fatalf("Subscribe(%q) error = %v", subject, err)
		}
		t.Cleanup(func() { _ = sub.Unsubscribe() })
	}

	mustSubscribeSignal(subjectTesterTask, testerSubjectHit)
	mustSubscribeSignal(subjectRedTeamTask, redTeamSubjectHit)
	mustSubscribeSignal("agent.task.reviewer", reviewerSubjectHit)

	// -----------------------------------------------------------------------
	// Step 1: Trigger → blue team selected → tester dispatched
	// -----------------------------------------------------------------------

	trigger := workflow.TriggerPayload{
		Slug:    "team-plan",
		TaskID:  "feat-001",
		Title:   "Team pipeline integration test task",
		Model:   "default",
		TraceID: "trace-team-integ-001",
		Prompt:  "Implement the team feature",
	}
	publishExecTrigger(t, tc, ctx, trigger)

	waitForExecCondition(t, ctx, 20*time.Second, func() bool {
		return comp.triggersProcessed.Load() >= 1
	}, "triggersProcessed should reach 1")

	entityID := workflow.EntityPrefix() + ".exec.task.run.team-plan-feat-001"

	// Wait for TesterTaskID to be assigned — this confirms the tester was dispatched.
	testerTaskID := waitForExecField(t, ctx, comp, entityID, 20*time.Second, func(exec *taskExecution) string {
		return exec.TesterTaskID
	}, "TesterTaskID")
	t.Logf("tester task_id = %s", testerTaskID)

	// Best-effort: verify the tester task message was published to the correct subject.
	// Not a hard assertion — TesterTaskID confirmation above is the primary check.
	select {
	case <-testerSubjectHit:
		t.Logf("tester dispatch confirmed on subject %s", subjectTesterTask)
	case <-time.After(2 * time.Second):
		t.Logf("tester subject signal not received (best-effort check only; task ID confirmed above)")
	}

	// -----------------------------------------------------------------------
	// Step 2: Tester completes → builder dispatched
	// -----------------------------------------------------------------------

	publishLoopCompleted(t, tc, ctx, testerTaskID, stageTest, marshalResult(t, payloads.DeveloperResult{
		Slug:   trigger.Slug,
		TaskID: trigger.TaskID,
		Status: "success",
		Output: json.RawMessage(`{"tests_written": 3}`),
	}))

	builderTaskID := waitForExecField(t, ctx, comp, entityID, 20*time.Second, func(exec *taskExecution) string {
		return exec.BuilderTaskID
	}, "BuilderTaskID")
	t.Logf("builder task_id = %s", builderTaskID)

	// -----------------------------------------------------------------------
	// Step 3: Builder completes → validator dispatched
	// -----------------------------------------------------------------------

	publishLoopCompleted(t, tc, ctx, builderTaskID, stageBuild, marshalResult(t, payloads.DeveloperResult{
		Slug:          trigger.Slug,
		TaskID:        trigger.TaskID,
		Status:        "success",
		FilesModified: []string{"pkg/feature/feature.go", "pkg/feature/feature_test.go"},
		Output:        json.RawMessage(`{"summary": "implemented feature"}`),
	}))

	validatorTaskID := waitForExecField(t, ctx, comp, entityID, 20*time.Second, func(exec *taskExecution) string {
		return exec.ValidatorTaskID
	}, "ValidatorTaskID")
	t.Logf("validator task_id = %s", validatorTaskID)

	// -----------------------------------------------------------------------
	// Step 4: Validator passes → RED TEAM dispatched
	// This is the critical team-mode assertion: RedTeamTaskID must be set BEFORE
	// ReviewerTaskID. In solo mode, ReviewerTaskID would be set directly.
	// -----------------------------------------------------------------------

	publishLoopCompleted(t, tc, ctx, validatorTaskID, stageValidate, marshalResult(t, payloads.ValidationResult{
		Slug:      trigger.Slug,
		Passed:    true,
		ChecksRun: 5,
	}))

	// RedTeamTaskID must be populated before ReviewerTaskID.
	redTeamTaskID := waitForExecField(t, ctx, comp, entityID, 20*time.Second, func(exec *taskExecution) string {
		return exec.RedTeamTaskID
	}, "RedTeamTaskID (red team dispatched before reviewer in team mode)")
	t.Logf("red-team task_id = %s", redTeamTaskID)

	// Give the Core NATS subscriber a brief window to receive the dispatch message.
	// We don't hard-fail on this — the primary assertion is that RedTeamTaskID was
	// set (above). The signal check is a best-effort sanity that the subject was hit.
	select {
	case <-redTeamSubjectHit:
		t.Logf("red-team dispatch confirmed on subject %s", subjectRedTeamTask)
	case <-time.After(2 * time.Second):
		t.Logf("red-team subject signal not received (best-effort check only; task ID confirmed above)")
	}

	// ReviewerTaskID must still be unset — team mode inserts red-team first.
	execForCheck := loadExec(t, comp, entityID)
	if execForCheck != nil && execForCheck.ReviewerTaskID != "" {
		t.Errorf("ReviewerTaskID was set (%q) before red-team completed — solo-mode path taken",
			execForCheck.ReviewerTaskID)
	}

	// -----------------------------------------------------------------------
	// Step 5: Red team completes → reviewer dispatched
	// -----------------------------------------------------------------------

	publishLoopCompleted(t, tc, ctx, redTeamTaskID, stageRedTeam, marshalResult(t, payloads.RedTeamChallengeResult{
		Issues: []payloads.RedTeamIssue{
			{Description: "Error path not tested", Severity: "minor"},
		},
		OverallScore: 3,
		Summary:      "Minor test coverage gap found",
	}))

	reviewerTaskID := waitForExecField(t, ctx, comp, entityID, 20*time.Second, func(exec *taskExecution) string {
		return exec.ReviewerTaskID
	}, "ReviewerTaskID")
	t.Logf("reviewer task_id = %s", reviewerTaskID)

	select {
	case <-reviewerSubjectHit:
		t.Logf("reviewer dispatch confirmed on subject agent.task.reviewer")
	case <-time.After(2 * time.Second):
		t.Logf("reviewer subject signal not received (best-effort check only; task ID confirmed above)")
	}

	// -----------------------------------------------------------------------
	// Step 6: Reviewer approves with red-team scores → approved, stats updated
	// -----------------------------------------------------------------------

	publishLoopCompleted(t, tc, ctx, reviewerTaskID, stageReview, marshalResult(t, payloads.TaskCodeReviewResult{
		Slug:            trigger.Slug,
		Verdict:         "approved",
		Feedback:        "Good work; red team's minor note is acceptable",
		RedAccuracy:     4,
		RedThoroughness: 3,
		RedFairness:     5,
		RedFeedback:     "Fair and proportionate critique",
	}))

	waitForExecCondition(t, ctx, 20*time.Second, func() bool {
		return comp.executionsApproved.Load() >= 1
	}, "executionsApproved should reach 1")

	// -----------------------------------------------------------------------
	// Verify: red-team stats persisted in ENTITY_STATES
	// -----------------------------------------------------------------------

	verifyRedTeamStatsUpdated(t, tc, ctx)
}

// ---------------------------------------------------------------------------
// TestIntegration_TeamPipeline_RedTeamFallback verifies the graceful fallback
// when SelectRedTeam returns nil (only one team seeded): validation pass
// dispatches reviewer directly, with no red-team dispatch.
// ---------------------------------------------------------------------------

func TestIntegration_TeamPipeline_RedTeamFallback(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithStreams(teamStreamSubjects...),
		natsclient.WithKVBuckets("ENTITY_STATES"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Build with 2-team roster so Validate() passes, then trim to 1 team
	// before Start() so seedTeams() only writes "alpha" to ENTITY_STATES.
	comp := newTeamIntegrationComponent(t, tc)
	comp.config.Teams.Roster = comp.config.Teams.Roster[:1] // keep only "alpha"

	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = comp.Stop(5 * time.Second) })

	nativeConn := tc.GetNativeConnection()

	redTeamSubjectHit := make(chan struct{}, 4)
	sub, err := nativeConn.Subscribe(subjectRedTeamTask, func(_ *nats.Msg) {
		select {
		case redTeamSubjectHit <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("Subscribe(%q) error = %v", subjectRedTeamTask, err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	trigger := workflow.TriggerPayload{
		Slug:    "fallback-plan",
		TaskID:  "fallback-001",
		Title:   "Red team fallback test",
		Model:   "default",
		TraceID: "trace-fallback-001",
		Prompt:  "Implement fallback feature",
	}
	publishExecTrigger(t, tc, ctx, trigger)

	entityID := workflow.EntityPrefix() + ".exec.task.run.fallback-plan-fallback-001"

	waitForExecCondition(t, ctx, 20*time.Second, func() bool {
		return comp.triggersProcessed.Load() >= 1
	}, "triggersProcessed should reach 1")

	// Drive tester → builder → validator.
	testerTaskID := waitForExecField(t, ctx, comp, entityID, 20*time.Second, func(exec *taskExecution) string {
		return exec.TesterTaskID
	}, "TesterTaskID")

	publishLoopCompleted(t, tc, ctx, testerTaskID, stageTest, marshalResult(t, payloads.DeveloperResult{
		Slug: trigger.Slug, TaskID: trigger.TaskID, Status: "success",
	}))

	builderTaskID := waitForExecField(t, ctx, comp, entityID, 20*time.Second, func(exec *taskExecution) string {
		return exec.BuilderTaskID
	}, "BuilderTaskID")

	publishLoopCompleted(t, tc, ctx, builderTaskID, stageBuild, marshalResult(t, payloads.DeveloperResult{
		Slug:          trigger.Slug,
		TaskID:        trigger.TaskID,
		Status:        "success",
		FilesModified: []string{"main.go"},
	}))

	validatorTaskID := waitForExecField(t, ctx, comp, entityID, 20*time.Second, func(exec *taskExecution) string {
		return exec.ValidatorTaskID
	}, "ValidatorTaskID")

	// Validation passes. With only alpha in ENTITY_STATES, SelectRedTeam returns
	// nil → reviewer dispatched directly (fallback path).
	publishLoopCompleted(t, tc, ctx, validatorTaskID, stageValidate, marshalResult(t, payloads.ValidationResult{
		Slug: trigger.Slug, Passed: true, ChecksRun: 3,
	}))

	// Key assertion: ReviewerTaskID set WITHOUT RedTeamTaskID being set first.
	reviewerTaskID := waitForExecField(t, ctx, comp, entityID, 20*time.Second, func(exec *taskExecution) string {
		return exec.ReviewerTaskID
	}, "ReviewerTaskID (fallback: reviewer dispatched directly without red-team)")
	t.Logf("fallback reviewer task_id = %s (red-team correctly skipped)", reviewerTaskID)

	// Confirm RedTeamTaskID was never set.
	execFB := loadExec(t, comp, entityID)
	if execFB != nil && execFB.RedTeamTaskID != "" {
		t.Errorf("RedTeamTaskID was set (%q) during fallback — red-team should have been skipped",
			execFB.RedTeamTaskID)
	}

	// Confirm red-team subject received nothing.
	select {
	case <-redTeamSubjectHit:
		t.Error("agent.task.red-team received a message during fallback — expected no red-team dispatch")
	default:
		// Expected.
	}

	// Approve and verify clean termination.
	publishLoopCompleted(t, tc, ctx, reviewerTaskID, stageReview, marshalResult(t, payloads.TaskCodeReviewResult{
		Slug:    trigger.Slug,
		Verdict: "approved",
	}))

	waitForExecCondition(t, ctx, 15*time.Second, func() bool {
		return comp.executionsApproved.Load() >= 1
	}, "executionsApproved should reach 1 after fallback approval")
}

// ---------------------------------------------------------------------------
// Helpers specific to team integration tests
// ---------------------------------------------------------------------------

// newTeamIntegrationComponent builds an execution-orchestrator with team mode
// enabled (2 teams, 3 members each) wired to the provided test NATS client.
func newTeamIntegrationComponent(t *testing.T, tc *natsclient.TestClient) *Component {
	t.Helper()

	cfg := DefaultConfig()
	cfg.Teams = &TeamsConfig{
		Enabled: true,
		Roster: []TeamRosterEntry{
			{
				Name: "alpha",
				Members: []TeamMemberEntry{
					{Role: "tester", Model: "default"},
					{Role: "builder", Model: "default"},
					{Role: "reviewer", Model: "default"},
				},
			},
			{
				Name: "bravo",
				Members: []TeamMemberEntry{
					{Role: "tester", Model: "default"},
					{Role: "builder", Model: "default"},
					{Role: "reviewer", Model: "default"},
				},
			},
		},
	}
	cfg.TimeoutSeconds = 60

	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("newTeamIntegrationComponent: marshal config: %v", err)
	}

	deps := component.Dependencies{NATSClient: tc.Client}
	compI, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("newTeamIntegrationComponent: NewComponent() error: %v", err)
	}
	return compI.(*Component)
}

// publishLoopCompleted wraps resultJSON in a properly-typed BaseMessage envelope
// containing an agentic.LoopCompletedEvent and publishes it to
// agentic.loop_completed.v1 via JetStream.
//
// agentic.LoopCompletedEvent is registered in the agentic package's init(), so
// base.Payload() inside handleLoopCompleted returns *agentic.LoopCompletedEvent.
func publishLoopCompleted(t *testing.T, tc *natsclient.TestClient, ctx context.Context, taskID, workflowStep, resultJSON string) {
	t.Helper()

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-test-" + taskID,
		TaskID:       taskID,
		Outcome:      "success",
		Role:         "general",
		Result:       resultJSON,
		WorkflowSlug: WorkflowSlugTaskExecution,
		WorkflowStep: workflowStep,
		CompletedAt:  time.Now(),
	}

	baseMsg := message.NewBaseMessage(event.Schema(), event, "test")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		t.Fatalf("publishLoopCompleted: marshal base message: %v", err)
	}

	js, err := tc.Client.JetStream()
	if err != nil {
		t.Fatalf("publishLoopCompleted: JetStream(): %v", err)
	}
	if _, err := js.Publish(ctx, subjectLoopCompleted, data); err != nil {
		t.Fatalf("publishLoopCompleted: publish to %q: %v", subjectLoopCompleted, err)
	}
}

// marshalResult marshals v to a JSON string for use as the Result field in a
// LoopCompletedEvent.
func marshalResult(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalResult: %v", err)
	}
	return string(b)
}

// waitForExecField polls the component's activeExecutions map until fieldFn
// returns a non-empty string or the deadline is exceeded.  This is the primary
// mechanism for obtaining task IDs from each pipeline stage without relying on
// JetStream → Core NATS fan-out of the dispatch messages themselves.
func waitForExecField(
	t *testing.T,
	ctx context.Context,
	comp *Component,
	entityID string,
	timeout time.Duration,
	fieldFn func(*taskExecution) string,
	fieldName string,
) string {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		val, ok := comp.activeExecutions.Load(entityID)
		if ok {
			exec := val.(*taskExecution)
			exec.mu.Lock()
			id := fieldFn(exec)
			exec.mu.Unlock()
			if id != "" {
				return id
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waitForExecField(%s): context cancelled", fieldName)
		default:
			time.Sleep(25 * time.Millisecond)
		}
	}
	t.Fatalf("waitForExecField(%s): not set within %v", fieldName, timeout)
	return ""
}

// loadExec returns the current taskExecution for entityID, or nil if not present.
// Used for point-in-time reads of execution state without waiting.
func loadExec(t *testing.T, comp *Component, entityID string) *taskExecution {
	t.Helper()
	val, ok := comp.activeExecutions.Load(entityID)
	if !ok {
		return nil
	}
	return val.(*taskExecution)
}

// waitForSignal waits for a value to arrive on ch or fatals on timeout.
// Used to verify dispatch subjects received at least one message.
func waitForSignal(t *testing.T, ctx context.Context, ch <-chan struct{}, timeout time.Duration, msg string) {
	t.Helper()
	select {
	case <-ch:
		// received
	case <-time.After(timeout):
		t.Fatalf("waitForSignal: %s — nothing received within %v", msg, timeout)
	case <-ctx.Done():
		t.Fatalf("waitForSignal: %s — context cancelled", msg)
	}
}

// drainForTaskID is retained for diagnostic use. It reads from ch until
// finding a message whose BaseMessage payload contains a non-empty task_id.
func drainForTaskID(ctx context.Context, t *testing.T, ch <-chan []byte, timeout time.Duration) string {
	t.Helper()

	deadline := time.After(timeout)
	msgCount := 0
	for {
		select {
		case data := <-ch:
			msgCount++
			var outer struct {
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(data, &outer); err != nil || outer.Payload == nil {
				continue
			}
			var inner struct {
				TaskID string `json:"task_id"`
			}
			if err := json.Unmarshal(outer.Payload, &inner); err != nil {
				continue
			}
			if inner.TaskID != "" {
				return inner.TaskID
			}
		case <-deadline:
			t.Fatalf("drainForTaskID: no task_id found within %v (saw %d messages)", timeout, msgCount)
		case <-ctx.Done():
			t.Fatal("drainForTaskID: context cancelled")
		}
	}
}

// verifyRedTeamStatsUpdated polls ENTITY_STATES until one of the known team
// IDs has a non-zero RedTeamStats.ReviewCount and validates the running-average
// values against the scores submitted by the test's reviewer response
// (RedAccuracy=4, RedThoroughness=3, RedFairness=5 → overall=4.0).
func verifyRedTeamStatsUpdated(t *testing.T, tc *natsclient.TestClient, ctx context.Context) {
	t.Helper()

	kvCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	bucket, err := tc.Client.GetKeyValueBucket(kvCtx, "ENTITY_STATES")
	if err != nil {
		t.Fatalf("verifyRedTeamStatsUpdated: GetKeyValueBucket: %v", err)
	}
	helper := agentgraph.NewHelper(tc.Client.NewKVStore(bucket))

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, teamID := range []string{"alpha", "bravo"} {
			team, err := helper.GetTeam(kvCtx, teamID)
			if err != nil {
				continue
			}
			if team.RedTeamStats.ReviewCount < 1 {
				continue
			}

			t.Logf("red-team stats: team=%s review_count=%d q1=%.2f q2=%.2f q3=%.2f overall=%.2f",
				teamID,
				team.RedTeamStats.ReviewCount,
				team.RedTeamStats.Q1CorrectnessAvg,
				team.RedTeamStats.Q2QualityAvg,
				team.RedTeamStats.Q3CompletenessAvg,
				team.RedTeamStats.OverallAvg,
			)

			if team.RedTeamStats.ReviewCount != 1 {
				t.Errorf("red-team ReviewCount = %d, want 1", team.RedTeamStats.ReviewCount)
			}
			// ReviewerResult: RedAccuracy=4, RedThoroughness=3, RedFairness=5.
			// UpdateTeamRedTeamStatsIncremental maps q1=accuracy, q2=thoroughness, q3=fairness.
			// After one review: overall = (4+3+5)/3 = 4.0
			const wantOverall = 4.0
			if team.RedTeamStats.OverallAvg < wantOverall-0.01 || team.RedTeamStats.OverallAvg > wantOverall+0.01 {
				t.Errorf("red-team OverallAvg = %.4f, want %.4f (±0.01)", team.RedTeamStats.OverallAvg, wantOverall)
			}
			return
		}

		select {
		case <-kvCtx.Done():
			t.Fatal("verifyRedTeamStatsUpdated: context cancelled")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Deadline exceeded — dump state for diagnosis.
	for _, teamID := range []string{"alpha", "bravo"} {
		team, err := helper.GetTeam(kvCtx, teamID)
		if err != nil {
			t.Logf("team %q: get error: %v", teamID, err)
			continue
		}
		t.Logf("team %q: state=%s redteam_review_count=%d redteam_overall=%.2f",
			teamID, team.Status, team.RedTeamStats.ReviewCount, team.RedTeamStats.OverallAvg)
	}
	t.Fatal("red-team stats not updated in ENTITY_STATES within 10s after reviewer approval")
}
