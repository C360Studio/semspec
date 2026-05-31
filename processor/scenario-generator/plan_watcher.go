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

	c.logger.Info("Watching PLAN_STATES for architecture_generated")

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
		if plan.Status != workflow.StatusArchitectureGenerated {
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
	// Build architecture context once for all requirements.
	var archContext string
	if plan.Architecture != nil {
		archContext = prompt.FormatArchitectureContext(
			toActorInfos(plan.Architecture.Actors),
			toIntegrationInfos(plan.Architecture.Integrations),
		)
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

	for _, req := range plan.Requirements {
		emissions := Classify(req, caps, plan.Architecture, catalog)
		required := emissionsToWireTiers(emissions)

		genReq := &payloads.ScenarioGeneratorRequest{
			Slug:                   plan.Slug,
			RequirementID:          req.ID,
			RequirementTitle:       req.Title,
			RequirementDescription: req.Description,
			PlanGoal:               plan.Goal,
			PlanContext:            plan.Context,
			ArchitectureContext:    archContext,
			RequiredTiers:          required,
		}

		key := plan.Slug + "/" + req.ID
		c.retry.Track(key, &scenarioRetryPayload{req: genReq, reviewFindings: plan.ReviewFormattedFindings})
		c.dispatchScenarioGenerator(ctx, genReq, "", plan.ReviewFormattedFindings)
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

func toActorInfos(actors []workflow.ActorDef) []prompt.ActorInfo {
	out := make([]prompt.ActorInfo, len(actors))
	for i, a := range actors {
		out[i] = prompt.ActorInfo{Name: a.Name, Type: a.Type, Triggers: a.Triggers}
	}
	return out
}

func toIntegrationInfos(integrations []workflow.IntegrationPoint) []prompt.IntegrationInfo {
	out := make([]prompt.IntegrationInfo, len(integrations))
	for i, ip := range integrations {
		out[i] = prompt.IntegrationInfo{Name: ip.Name, Direction: ip.Direction, Protocol: ip.Protocol}
	}
	return out
}
