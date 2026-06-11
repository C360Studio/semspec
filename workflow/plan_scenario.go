package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/message"

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

// writeScenarioTriples writes all Scenario fields as a single atomic batch to
// ENTITY_STATES via UpsertEntityIfChanged. The whole predicate set for the
// entity is built first, then passed to UpsertEntityIfChanged which:
//  1. Computes a content-hash over (predicate, object) pairs (Phase 3a Lever 1).
//     If the hash matches the last successful write, the NATS call is skipped
//     entirely — suppressing the ~19 unchanged-child re-persists that were the
//     dominant contributor to the ENTITY_STATES write-amplification leak.
//  2. Collapses the per-triple fan-out into one atomic CAS call
//     (graph.mutation.entity.update_with_triples, Phase 3a Lever 2).
//
// RemoveTriples is derived automatically from the distinct predicates in the
// slice (replace-own / preserve-foreign contract). Multi-valued predicates
// (Then, Tags, HarnessProfileIDs) include ALL current values each write; the
// UpsertEntity handler removes the old set and appends the new one atomically.
func writeScenarioTriples(ctx context.Context, tw *graphutil.TripleWriter, s *Scenario) error {
	if tw == nil {
		return nil
	}
	entityID := ScenarioEntityID(s.ID)

	title := s.When
	if len(title) > 100 {
		title = title[:97] + "..."
	}

	// Build the complete predicate set for this entity.
	triples := []message.Triple{
		{Subject: entityID, Predicate: semspec.ScenarioGiven, Object: s.Given},
		{Subject: entityID, Predicate: semspec.ScenarioWhen, Object: s.When},
		{Subject: entityID, Predicate: semspec.ScenarioStatus, Object: string(s.Status)},
		{Subject: entityID, Predicate: semspec.ScenarioRequirement, Object: RequirementEntityID(s.RequirementID)},
		{Subject: entityID, Predicate: semspec.ScenarioCreatedAt, Object: s.CreatedAt.Format(time.RFC3339)},
		{Subject: entityID, Predicate: semspec.DCTitle, Object: title},
	}

	// Multi-valued Then clauses — include the full current list each write.
	for _, then := range s.Then {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.ScenarioThen, Object: then})
	}

	// Tier + facet tags (ADR-041 Move 1). Multi-valued.
	for _, tag := range s.Tags {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.ScenarioTag, Object: tag})
	}

	// Harness profile bindings (ADR-041 Move 1). Multi-valued.
	for _, hpID := range s.HarnessProfileIDs {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.ScenarioHarnessProfile, Object: hpID})
	}

	// Single batched write — skips if content unchanged, replaces own predicates.
	// OwnedPredicates lists every predicate this writer may emit, including the
	// multi-valued ones whose lists may be empty on a given save. Without them,
	// a set→empty transition (Then=[], Tags=[], HarnessProfileIDs=[]) would emit
	// zero triples for that predicate, leaving it absent from RemoveTriples and
	// therefore NOT stripped from the graph (C1 stale-on-empty fix).
	_, err := tw.UpsertEntityIfChanged(ctx, ScenarioEntityType, entityID, triples, graphutil.UpsertOpts{
		OwnedPredicates: []string{
			semspec.ScenarioGiven,
			semspec.ScenarioWhen,
			semspec.ScenarioStatus,
			semspec.ScenarioRequirement,
			semspec.ScenarioCreatedAt,
			semspec.DCTitle,
			semspec.ScenarioThen,
			semspec.ScenarioTag,
			semspec.ScenarioHarnessProfile,
		},
	})
	if err != nil {
		return fmt.Errorf("write scenario %s: %w", s.ID, err)
	}
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
