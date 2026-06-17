package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestHandleScenariosMutation_ConcurrentDifferentRequirementsPreservesAll
// pins the single-writer invariant for plan-manager: when N concurrent
// handleScenariosMutation calls land on the same plan with distinct
// RequirementIDs, ALL N scenario batches must persist.
//
// On the current (pre-fix) code this test FAILS because the
// `c.mu.RLock(); ps := c.plans; c.mu.RUnlock(); plan, ok := ps.get(slug);
// ...; ps.save(ctx, plan)` sequence in every mutation handler is not
// serialized per slug. NATS dispatches each subscription's handler in its
// own goroutine — two concurrent handlers read the same plan snapshot,
// each appends its batch to a local copy, and whichever ps.save lands
// second silently overwrites the first's append. The first batch is lost.
//
// Reference: go-reviewer Pass 4 finding P4-C1 and the cross-pass synthesis
// memory note (project_go_reviewer_cross_pass_synthesis_adr043.md).
//
// After the per-slug-mutex fix this test PASSES because each handler
// acquires the slug's mutex at entry and releases at exit, serializing
// the get/mutate/save sequence.
func TestHandleScenariosMutation_ConcurrentDifferentRequirementsPreservesAll(t *testing.T) {
	const slug = "concurrent-scen"
	const numReqs = 64

	ctx := context.Background()
	c := setupTestComponent(t)

	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusGeneratingScenarios
	plan.Requirements = make([]workflow.Requirement, numReqs)
	for i := 0; i < numReqs; i++ {
		plan.Requirements[i] = workflow.Requirement{
			ID:    fmt.Sprintf("req.%s.%d", slug, i),
			Title: fmt.Sprintf("R%d", i),
		}
	}
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save initial plan: %v", err)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < numReqs; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			reqID := fmt.Sprintf("req.%s.%d", slug, idx)
			scenarioID := fmt.Sprintf("scen.%s.%d", slug, idx)
			req := ScenariosMutationRequest{
				Slug:          slug,
				RequirementID: reqID,
				Scenarios: []workflow.Scenario{
					{ID: scenarioID, RequirementID: reqID},
				},
			}
			data, err := json.Marshal(req)
			if err != nil {
				t.Errorf("marshal: %v", err)
				return
			}
			<-start // wait for the green light
			resp := c.handleScenariosMutation(ctx, data)
			if !resp.Success {
				t.Errorf("handler returned !success for req %d: %s", idx, resp.Error)
			}
		}(i)
	}

	close(start)
	wg.Wait()

	got, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan disappeared after concurrent mutations")
	}

	// Build a presence set so duplicate scenario IDs don't mask losses.
	present := make(map[string]bool, len(got.Scenarios))
	for _, s := range got.Scenarios {
		present[s.ID] = true
	}

	var missing []string
	for i := 0; i < numReqs; i++ {
		want := fmt.Sprintf("scen.%s.%d", slug, i)
		if !present[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Errorf("LOST UPDATE — %d of %d concurrent scenario batches missing from plan.Scenarios after handlers returned; this proves the read-modify-write race in handleScenariosMutation. First few missing: %v",
			len(missing), numReqs, missing[:min(5, len(missing))])
	}
}

// TestHandleDeleteRequirement_ConcurrentDeletesAndReads regresses the
// plan-workflow "deleted requirement still accessible: status=200" failure.
// Two defects combined there:
//
//  1. The mutating HTTP requirement/scenario handlers did the same
//     get → mutate → save sequence as the NATS mutation handlers but withOUT
//     the per-slug lock, so a concurrent write (e.g. the requirements.generated
//     wholesale replace fired by approval) could clobber an HTTP delete and the
//     requirement reappeared on the next GET.
//  2. The delete/deprecate handlers filtered plan.Requirements/Scenarios in
//     place (slice[:0]) on a shallow copy whose backing array the unlocked
//     GET/List handlers read concurrently — a data race.
//
// Concurrent deletes on distinct requirements must ALL stick (no lost update,
// proving the HTTP handlers now hold the slug lock), and concurrent List
// readers must not race the deleters (proving the fresh-slice fix). Run under
// -race (Taskfile `test` does): the old in-place filter trips the detector.
func TestHandleDeleteRequirement_ConcurrentDeletesAndReads(t *testing.T) {
	const slug = "concurrent-delete"
	const numReqs = 32

	c := setupTestComponent(t)
	reqs := make([]workflow.Requirement, numReqs)
	scenarios := make([]workflow.Scenario, numReqs)
	for i := 0; i < numReqs; i++ {
		id := fmt.Sprintf("requirement.%s.%d", slug, i)
		reqs[i] = workflow.Requirement{
			ID:     id,
			PlanID: workflow.PlanEntityID(slug),
			Title:  fmt.Sprintf("R%d", i),
			Status: workflow.RequirementStatusActive,
		}
		scenarios[i] = workflow.Scenario{ID: fmt.Sprintf("scenario.%s.%d", slug, i), RequirementID: id}
	}
	setupTestPlanWith(t, c, slug, reqs, scenarios)

	var wg sync.WaitGroup
	start := make(chan struct{})

	// Concurrent readers — deliberately NOT holding the slug lock (GET/List are
	// read paths). They must never observe a torn backing array.
	for r := 0; r < 6; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				rec := httptest.NewRecorder()
				c.handleListRequirements(rec, httptest.NewRequest(http.MethodGet, "/x", nil), slug)
				rec2 := httptest.NewRecorder()
				c.handleListScenarios(rec2, httptest.NewRequest(http.MethodGet, "/x", nil), slug)
			}
		}()
	}

	// Concurrent deleters, one requirement each.
	for i := 0; i < numReqs; i++ {
		reqID := reqs[i].ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			rec := httptest.NewRecorder()
			c.handleDeleteRequirement(rec, httptest.NewRequest(http.MethodDelete, "/x", nil), slug, reqID)
			if rec.Code != http.StatusNoContent {
				t.Errorf("delete %s: status = %d, want %d", reqID, rec.Code, http.StatusNoContent)
			}
		}()
	}

	close(start)
	wg.Wait()

	got, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan disappeared after concurrent deletes")
	}
	// Every delete must have stuck — a surviving requirement is a lost update
	// (the clobber the per-slug lock prevents). Cascade also removes scenarios.
	if len(got.Requirements) != 0 {
		t.Errorf("LOST DELETE — %d of %d requirements survived concurrent deletes; the per-slug lock did not serialize the HTTP delete path. Survivors: %d",
			len(got.Requirements), numReqs, len(got.Requirements))
	}
	if len(got.Scenarios) != 0 {
		t.Errorf("cascade left %d scenarios after all requirements deleted", len(got.Scenarios))
	}
}

// TestHandleStoriesMutation_ConcurrentSameSlugSerializeMonotonically pins
// that two back-to-back handleStoriesMutation calls on the same plan
// converge to a consistent terminal state. Today both would race to set
// plan.Status=stories_generated and Stories=req.Stories — and either order
// is acceptable, but if locking is broken the cache/KV/graph layers can
// diverge (cache shows one set, KV shows another).
//
// This test mainly serves as a smoke test for "no panic, no deadlock,
// consistent final state" once the per-slug lock is in place. Pre-fix it
// can also expose the cache-vs-KV divergence under tighter timing windows.
func TestHandleStoriesMutation_ConcurrentSameSlugSerializeMonotonically(t *testing.T) {
	const slug = "concurrent-stories"

	ctx := context.Background()
	c := setupTestComponent(t)
	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusPreparingStories
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save initial plan: %v", err)
	}

	storyA := validStory(fmt.Sprintf("story.%s.A", slug), "req.x.1", "Story A")
	storyB := validStory(fmt.Sprintf("story.%s.B", slug), "req.x.1", "Story B")

	reqs := []storiesMutationRequest{
		{Slug: slug, Stories: []workflow.Story{storyA}, StoryCount: 1},
		{Slug: slug, Stories: []workflow.Story{storyB}, StoryCount: 1},
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, r := range reqs {
		wg.Add(1)
		go func(r storiesMutationRequest) {
			defer wg.Done()
			data, _ := json.Marshal(r)
			<-start
			// One will succeed (preparing_stories → stories_generated), the
			// other will hit invalid transition (stories_generated →
			// stories_generated). Both are acceptable outcomes; the test
			// asserts no panic + a consistent terminal state.
			_ = c.handleStoriesMutation(ctx, data)
		}(r)
	}
	close(start)
	wg.Wait()

	got, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan disappeared")
	}
	if got.EffectiveStatus() != workflow.StatusStoriesGenerated {
		t.Errorf("final status = %s, want stories_generated", got.EffectiveStatus())
	}
	if len(got.Stories) != 1 {
		t.Errorf("Stories count = %d, want exactly 1 (the winning mutation's batch)", len(got.Stories))
	}
}
