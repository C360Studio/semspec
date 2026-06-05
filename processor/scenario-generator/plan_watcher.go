package scenariogenerator

import (
	"context"
	"encoding/json"

	"github.com/c360studio/semspec/prompt"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/nats-io/nats.go/jetstream"
)

// watchPlanStates watches PLAN_STATES for plans reaching requirements_generated.
// The KV value carries plan.Requirements inline — no follow-up query needed.
func (c *Component) watchPlanStates(ctx context.Context, js jetstream.JetStream) {
	bucket, err := workflow.WaitForKVBucket(ctx, js, c.config.PlanStateBucket)
	if err != nil {
		c.logger.Warn("PLAN_STATES not available, relying on async triggers",
			"bucket", c.config.PlanStateBucket, "error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch PLAN_STATES", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Watching PLAN_STATES for stories_generated")

	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}

		var plan workflow.Plan
		if json.Unmarshal(entry.Value(), &plan) != nil {
			continue
		}
		// ADR-043 PR 4l — Bob watches only stories_generated. Sarah always
		// runs first (PR 4l deleted her Enabled hedge); the strict
		// sequential flow is architecture_generated → preparing_stories →
		// stories_generated → generating_scenarios. PR 4c had Bob also
		// watching architecture_generated as a back-compat fallback for
		// Sarah-dormant plans — that path created a claim race; PR 4l
		// removes it.
		if plan.Status != workflow.StatusStoriesGenerated {
			continue
		}
		if len(plan.Requirements) == 0 {
			continue
		}

		// Claim the plan to prevent re-trigger on partial scenario saves.
		if !workflow.ClaimPlanStatus(ctx, c.natsClient, plan.Slug, workflow.StatusGeneratingScenarios, c.logger) {
			continue
		}

		go c.generateScenariosFromKV(ctx, &plan)
	}
}

// generateScenariosFromKV dispatches scenario-generator agent loops for each
// requirement in the plan. Requirements are inline in the KV value — no
// additional query needed.
func (c *Component) generateScenariosFromKV(ctx context.Context, plan *workflow.Plan) {
	// Build architecture context once for all requirements. Bob writes
	// scenarios that reference actors, integration points, components, and the
	// architect's resolved upstream dependencies — so feed the full faithful
	// projection, not just actors+integrations.
	var archContext string
	if plan.Architecture != nil {
		archContext = prompt.FormatArchitectureContext(prompt.ProjectArchitecture(plan.Architecture))
	}

	// Load the harness catalog once per plan. ADR-041 Move 3 classifier
	// needs orchestration metadata to decide whether each selected profile
	// implies an @integration emission. Failure to load degrades gracefully
	// — the classifier sees a nil catalog and emits only @unit + @e2e per
	// capability surface; plan-reviewer rules (Move 4) will still flag the
	// resulting coverage gaps in a clearer location than a crash here.
	catalog, err := harnesscatalog.Load("")
	if err != nil {
		c.logger.Warn("Failed to load harness catalog; classifier will emit only @unit/@e2e",
			"slug", plan.Slug, "error", err)
		catalog = &harnesscatalog.Catalog{Profiles: map[string]harnesscatalog.Profile{}}
	}

	var caps []workflow.Capability
	if plan.Exploration != nil {
		caps = plan.Exploration.Capabilities
	}

	// ADR-043 PR 4j — Bob dispatches per-Story when plan.Stories carries
	// Sarah-prepared Stories for the requirement. Each Story gets its own
	// scenario-generator loop so the per-Story reviewer (PR 4h) has a
	// matching per-Story scenario set. When plan.Stories is empty (legacy
	// plans / pre-Sarah mock fixtures) the dispatcher falls back to
	// per-Requirement dispatch — preserves backwards compatibility with
	// fixtures that don't author Stories.
	for _, req := range plan.Requirements {
		emissions := Classify(req, caps, plan.Architecture, catalog)
		required := emissionsToWireTiers(emissions)

		stories := plan.StoriesForRequirement(req.ID)
		if len(stories) == 0 {
			c.dispatchPerRequirementLegacy(ctx, plan, req, required, archContext)
			continue
		}
		for _, story := range stories {
			c.dispatchPerStory(ctx, plan, req, story, required, archContext)
		}
	}
}

// dispatchPerStory issues a scenario-generator agent loop scoped to a single
// Sarah-prepared Story. The Story's title/intent/files_owned/components are
// carried in the payload so Bob's prompt can author Story-scoped scenarios.
func (c *Component) dispatchPerStory(ctx context.Context, plan *workflow.Plan, req workflow.Requirement, story workflow.Story, required []payloads.RequiredTier, archContext string) {
	genReq := buildStoryScopedRequest(plan, req, story, required, archContext)
	key := retryKey(plan.Slug, req.ID, story.ID)
	c.retry.Track(key, &scenarioRetryPayload{req: genReq, reviewFindings: plan.ReviewFormattedFindings})
	c.dispatchScenarioGenerator(ctx, genReq, "", plan.ReviewFormattedFindings)
}

// dispatchPerRequirementLegacy issues a scenario-generator agent loop scoped
// to the whole Requirement. Used for plans without Sarah-prepared Stories
// (mock fixtures or pre-ADR-043 plans). The server-side StoryID attachment
// falls back to the "first story owns the scenarios" lookup.
func (c *Component) dispatchPerRequirementLegacy(ctx context.Context, plan *workflow.Plan, req workflow.Requirement, required []payloads.RequiredTier, archContext string) {
	genReq := buildRequirementScopedRequest(plan, req, required, archContext)
	key := retryKey(plan.Slug, req.ID, "")
	c.retry.Track(key, &scenarioRetryPayload{req: genReq, reviewFindings: plan.ReviewFormattedFindings})
	c.dispatchScenarioGenerator(ctx, genReq, "", plan.ReviewFormattedFindings)
}

// buildStoryScopedRequest constructs the ScenarioGeneratorRequest payload
// for a per-Story dispatch (ADR-043 PR 4j). Extracted from dispatchPerStory
// so the wire-shape assembly is exercisable in unit tests without the
// dispatchScenarioGenerator infrastructure.
func buildStoryScopedRequest(plan *workflow.Plan, req workflow.Requirement, story workflow.Story, required []payloads.RequiredTier, archContext string) *payloads.ScenarioGeneratorRequest {
	return &payloads.ScenarioGeneratorRequest{
		Slug:                   plan.Slug,
		RequirementID:          req.ID,
		RequirementTitle:       req.Title,
		RequirementDescription: req.Description,
		PlanGoal:               plan.Goal,
		PlanContext:            plan.Context,
		ArchitectureContext:    archContext,
		RequiredTiers:          required,
		StoryID:                story.ID,
		StoryTitle:             story.Title,
		StoryIntent:            story.Intent,
		StoryFilesOwned:        append([]string(nil), story.FilesOwned...),
		StoryComponentName:     story.ComponentName,
	}
}

// buildRequirementScopedRequest constructs the ScenarioGeneratorRequest
// payload for legacy per-Requirement dispatch. Extracted for the same
// reason as buildStoryScopedRequest.
func buildRequirementScopedRequest(plan *workflow.Plan, req workflow.Requirement, required []payloads.RequiredTier, archContext string) *payloads.ScenarioGeneratorRequest {
	return &payloads.ScenarioGeneratorRequest{
		Slug:                   plan.Slug,
		RequirementID:          req.ID,
		RequirementTitle:       req.Title,
		RequirementDescription: req.Description,
		PlanGoal:               plan.Goal,
		PlanContext:            plan.Context,
		ArchitectureContext:    archContext,
		RequiredTiers:          required,
	}
}

// emissionsToWireTiers converts the classifier's TierEmission output into the
// wire-stable payloads.RequiredTier shape. Kept as a separate function so the
// classifier stays decoupled from payload types (it consumes only workflow.*
// types + the harness catalog).
func emissionsToWireTiers(emissions []TierEmission) []payloads.RequiredTier {
	if len(emissions) == 0 {
		return nil
	}
	out := make([]payloads.RequiredTier, 0, len(emissions))
	for _, e := range emissions {
		out = append(out, payloads.RequiredTier{
			Tag:               e.Tier,
			HarnessProfileIDs: e.HarnessProfileIDs,
		})
	}
	return out
}
