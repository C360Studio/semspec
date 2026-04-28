package scenariogenerator

import (
	"context"
	"encoding/json"

	"github.com/c360studio/semspec/prompt"
	"github.com/c360studio/semspec/workflow"
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

	for _, req := range plan.Requirements {
		genReq := &payloads.ScenarioGeneratorRequest{
			Slug:                   plan.Slug,
			RequirementID:          req.ID,
			RequirementTitle:       req.Title,
			RequirementDescription: req.Description,
			PlanGoal:               plan.Goal,
			PlanContext:            plan.Context,
			ArchitectureContext:    archContext,
		}

		key := plan.Slug + "/" + req.ID
		c.retry.Track(key, &scenarioRetryPayload{req: genReq, reviewFindings: plan.ReviewFormattedFindings})
		c.dispatchScenarioGenerator(ctx, genReq, "", plan.ReviewFormattedFindings)
	}
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
