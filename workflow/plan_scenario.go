package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// ScenariosJSONFile is the filename for machine-readable scenario storage (JSON format).
const ScenariosJSONFile = "scenarios.json"

// SaveScenarios saves scenarios to ENTITY_STATES as triples.
// Each scenario is stored as a separate entity keyed by ScenarioEntityID.
// Multi-valued fields (Then) are written as individual triples.
func SaveScenarios(ctx context.Context, tw *graphutil.TripleWriter, scenarios []Scenario, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	for i := range scenarios {
		if err := writeScenarioTriples(ctx, tw, &scenarios[i]); err != nil {
			return fmt.Errorf("save scenario %s: %w", scenarios[i].ID, err)
		}
	}

	return nil
}

// writeScenarioTriples writes all Scenario fields as individual triples.
func writeScenarioTriples(ctx context.Context, tw *graphutil.TripleWriter, s *Scenario) error {
	if tw == nil {
		return nil
	}
	entityID := ScenarioEntityID(s.ID)

	// Upsert scalars (UpdateTriple = remove+add) and replace edge lists
	// (ReplaceTripleList) so re-persisting the scenario on every plan mutation —
	// which happens on every execution status update via writeChildTriples — does
	// not accumulate duplicate triples. graph-ingest AddTriple is append-only, so
	// plain WriteTriple here grew scenario entities unboundedly during execution
	// (the #132 plan-entity bloat, same class as requirements/capabilities/decisions).
	// Scenarios are the most numerous child (N per requirement, each with several
	// Then clauses), so this was the largest contributor to the ENTITY_STATES leak.
	_ = tw.UpdateTriple(ctx, entityID, semspec.ScenarioGiven, s.Given)
	_ = tw.UpdateTriple(ctx, entityID, semspec.ScenarioWhen, s.When)
	if err := tw.UpdateTriple(ctx, entityID, semspec.ScenarioStatus, string(s.Status)); err != nil {
		return fmt.Errorf("write scenario status: %w", err)
	}
	_ = tw.UpdateTriple(ctx, entityID, semspec.ScenarioRequirement, RequirementEntityID(s.RequirementID))
	_ = tw.UpdateTriple(ctx, entityID, semspec.ScenarioCreatedAt, s.CreatedAt.Format(time.RFC3339))

	title := s.When
	if len(title) > 100 {
		title = title[:97] + "..."
	}
	_ = tw.UpdateTriple(ctx, entityID, semspec.DCTitle, title)

	// Multi-valued Then clauses — replace the full edge list each persist.
	_ = tw.ReplaceTripleList(ctx, entityID, semspec.ScenarioThen, s.Then)

	// Tier + facet tags (ADR-041 Move 1). Multi-valued; replace the full list.
	_ = tw.ReplaceTripleList(ctx, entityID, semspec.ScenarioTag, s.Tags)

	// Harness profile bindings (ADR-041 Move 1). Multi-valued; replace the full
	// list. Values are catalog profile IDs (e.g. "mavlink.px4-sitl.mavsdk-smoke"),
	// not hashed entity IDs — these are cross-reference strings into the
	// harnesscatalog, not graph edges to other entities. Plan-reviewer rule
	// scenario.harness_id_unresolved (Move 4) validates each ID resolves into the
	// catalog.
	_ = tw.ReplaceTripleList(ctx, entityID, semspec.ScenarioHarnessProfile, s.HarnessProfileIDs)

	return nil
}

// ValidateScenarioTags enforces the structural rules ADR-041 Move 1 places on
// Scenario.Tags and Scenario.HarnessProfileIDs:
//
//  1. Each tag is non-empty, '@'-prefixed, and contains only alphanumeric +
//     hyphen characters in its body. BDD convention (pytest-bdd rejects '.'
//     and ':'; behave has compat issues with ':') — see ADR-041 §"Why
//     colon-bearing tags fail".
//  2. Exactly one tier tag (@unit/@integration/@smoke/@e2e) is present.
//     Operator-defined facet tags pass through as informational metadata.
//  3. No tag appears more than once.
//  4. Each HarnessProfileID is a non-empty string. Catalog resolution
//     (scenario.harness_id_unresolved) is the plan-reviewer's job per Move 4,
//     not this validator's — this layer only enforces the wire shape.
//
// Cross-entity rules — "every requirement has @unit", "services-bound
// requirement has @integration", "@integration scenario needs at least one
// HarnessProfileID when its architecture binds a services-class profile" —
// belong to plan-reviewer rules (ADR-041 Move 4), not this per-scenario
// validator. The scenario-generator (Move 3) emits valid Tags; plan-reviewer
// gates cross-scenario coverage; the structural-validator (Move 5) gates the
// dev's test scaffolding against HarnessProfileIDs.
func ValidateScenarioTags(s Scenario) error {
	if len(s.Tags) == 0 {
		return fmt.Errorf("scenario %q has no tags; exactly one tier tag is required", s.ID)
	}
	seen := make(map[string]struct{}, len(s.Tags))
	tierCount := 0
	for i, tag := range s.Tags {
		if err := validateTagSyntax(tag); err != nil {
			return fmt.Errorf("scenario %q tag[%d]: %w", s.ID, i, err)
		}
		if _, dup := seen[tag]; dup {
			return fmt.Errorf("scenario %q declares tag %q more than once", s.ID, tag)
		}
		seen[tag] = struct{}{}
		if IsTierTag(tag) {
			tierCount++
		}
	}
	if tierCount == 0 {
		return fmt.Errorf("scenario %q has no tier tag; one of %s/%s/%s/%s is required",
			s.ID, TierUnit, TierIntegration, TierSmoke, TierE2E)
	}
	if tierCount > 1 {
		return fmt.Errorf("scenario %q has %d tier tags; exactly one is required", s.ID, tierCount)
	}
	for i, id := range s.HarnessProfileIDs {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("scenario %q harness_profile_ids[%d] is empty", s.ID, i)
		}
	}
	return nil
}

// validateTagSyntax enforces the alphanumeric + hyphen body rule. Tags must
// start with '@' and contain at least one body character. The strict body
// charset is what guarantees round-trip compatibility across pytest-bdd,
// behave, and karate (ADR-041 §"Why colon-bearing tags fail").
func validateTagSyntax(tag string) error {
	if tag == "" {
		return fmt.Errorf("tag is empty")
	}
	if tag[0] != '@' {
		return fmt.Errorf("tag %q must start with '@'", tag)
	}
	body := tag[1:]
	if body == "" {
		return fmt.Errorf("tag %q has no body after '@'", tag)
	}
	for _, r := range body {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return fmt.Errorf("tag %q body contains disallowed character %q (alphanumeric and '-' only)", tag, r)
		}
	}
	return nil
}
