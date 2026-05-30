package workflow

import (
	"context"
	"fmt"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// SaveCapabilities writes each Capability in the Exploration to ENTITY_STATES
// as triples. Each capability becomes its own entity (6-part EntityID hashed
// on planSlug + capabilityName), with predicates for name/lifecycle/description
// plus a link back to the owning plan and multi-valued depends_on edges.
//
// ADR-040 Move 1. Called from plan-manager's writeChildTriples whenever
// Plan.Exploration is non-empty.
//
// Idempotency: this function depends on the state-machine guard at
// `handleExploredMutation` to prevent re-entry — `CanTransitionTo(explored)`
// returns false from `explored`, so a duplicate exploration mutation is
// rejected before save() runs. If a future refactor relaxes that guard (e.g.
// allows explored→explored for revisions), the multi-valued `CapabilityDependsOn`
// triples here will append-duplicate edges in the graph. Either guard at the
// state machine OR add a deduplication pass on the depends_on writes before
// flipping that constraint.
func SaveCapabilities(ctx context.Context, tw *graphutil.TripleWriter, exploration *Exploration, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if exploration == nil || len(exploration.Capabilities) == 0 {
		return nil
	}
	if err := ValidateCapabilitySet(exploration.Capabilities); err != nil {
		return fmt.Errorf("invalid capability set: %w", err)
	}

	planEntityID := PlanEntityID(slug)
	for i := range exploration.Capabilities {
		if err := writeCapabilityTriples(ctx, tw, &exploration.Capabilities[i], slug, planEntityID); err != nil {
			return fmt.Errorf("save capability %s: %w", exploration.Capabilities[i].Name, err)
		}
	}
	return nil
}

// writeCapabilityTriples writes the predicate set for a single Capability.
// Hash each depends_on name into the same EntityID suffix scheme so the
// stored object resolves back to the corresponding Capability entity.
func writeCapabilityTriples(ctx context.Context, tw *graphutil.TripleWriter, cap *Capability, slug, planEntityID string) error {
	if tw == nil {
		return nil
	}
	entityID := CapabilityEntityID(slug, cap.Name)

	_ = tw.WriteTriple(ctx, entityID, semspec.CapabilityName, cap.Name)
	if err := tw.WriteTriple(ctx, entityID, semspec.CapabilityLifecycle, string(cap.Lifecycle)); err != nil {
		return fmt.Errorf("write capability lifecycle: %w", err)
	}
	if cap.Description != "" {
		_ = tw.WriteTriple(ctx, entityID, semspec.CapabilityDescription, cap.Description)
	}
	_ = tw.WriteTriple(ctx, entityID, semspec.CapabilityPlan, planEntityID)

	// Multi-valued depends_on — one triple per edge, value is the hashed
	// instance ID of the prerequisite Capability (matches the entity-ID
	// suffix scheme used by RequirementDependsOn).
	for _, dep := range cap.DependsOn {
		_ = tw.WriteTriple(ctx, entityID, semspec.CapabilityDependsOn, HashInstanceID(slug, dep))
	}
	return nil
}

// ValidateCapabilitySet enforces structural rules on the capability list:
// unique kebab-case names, valid lifecycle, and depends_on references that
// resolve within the same set. Mirrors ValidateRequirementDAG for capability
// edges.
//
// PR 2 will add plan-reviewer rules on top of this (capability_orphan,
// capability_dependency_cycle, capability_dependency_orphan) — those rules
// reason about cross-capability + cross-requirement constraints that this
// validator doesn't have visibility into.
func ValidateCapabilitySet(caps []Capability) error {
	if len(caps) == 0 {
		return nil
	}
	names := make(map[string]struct{}, len(caps))
	for i := range caps {
		if caps[i].Name == "" {
			return fmt.Errorf("capability[%d] missing name", i)
		}
		if !caps[i].Lifecycle.IsValid() {
			return fmt.Errorf("capability %q has invalid lifecycle %q", caps[i].Name, caps[i].Lifecycle)
		}
		if _, dup := names[caps[i].Name]; dup {
			return fmt.Errorf("capability %q declared more than once", caps[i].Name)
		}
		names[caps[i].Name] = struct{}{}
	}
	// depends_on resolution + simple cycle detection.
	for _, c := range caps {
		for _, dep := range c.DependsOn {
			if _, ok := names[dep]; !ok {
				return fmt.Errorf("capability %q depends_on %q which is not declared", c.Name, dep)
			}
		}
	}
	if err := detectCapabilityCycle(caps); err != nil {
		return err
	}
	return nil
}

// detectCapabilityCycle performs a DFS over the depends_on edges to flag
// any cycle. Required by the operator's "depends_on is a HARD CONSTRAINT"
// directive in ADR-040: parallel work on cyclically-dependent capabilities
// never converges, so we reject at planning time.
func detectCapabilityCycle(caps []Capability) error {
	byName := make(map[string]*Capability, len(caps))
	for i := range caps {
		byName[caps[i].Name] = &caps[i]
	}
	const (
		white = 0 // unvisited
		gray  = 1 // on current DFS stack
		black = 2 // fully explored
	)
	color := make(map[string]int, len(caps))
	var visit func(name string, stack []string) error
	visit = func(name string, stack []string) error {
		switch color[name] {
		case gray:
			return fmt.Errorf("capability depends_on cycle through %s -> %s", joinNames(stack), name)
		case black:
			return nil
		}
		color[name] = gray
		stack = append(stack, name)
		c := byName[name]
		if c != nil {
			for _, dep := range c.DependsOn {
				if err := visit(dep, stack); err != nil {
					return err
				}
			}
		}
		color[name] = black
		return nil
	}
	for _, c := range caps {
		if color[c.Name] == white {
			if err := visit(c.Name, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += " -> "
		}
		out += n
	}
	return out
}
