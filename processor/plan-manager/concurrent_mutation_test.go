package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
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
