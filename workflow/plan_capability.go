package workflow

import (
	"context"
	"fmt"
	"path"
	"strings"

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
func writeCapabilityTriples(ctx context.Context, tw *graphutil.TripleWriter, c *Capability, slug, planEntityID string) error {
	if tw == nil {
		return nil
	}
	entityID := CapabilityEntityID(slug, c.Name)

	_ = tw.WriteTriple(ctx, entityID, semspec.CapabilityName, c.Name)
	if err := tw.WriteTriple(ctx, entityID, semspec.CapabilityLifecycle, string(c.Lifecycle)); err != nil {
		return fmt.Errorf("write capability lifecycle: %w", err)
	}
	if c.Description != "" {
		_ = tw.WriteTriple(ctx, entityID, semspec.CapabilityDescription, c.Description)
	}
	_ = tw.WriteTriple(ctx, entityID, semspec.CapabilityPlan, planEntityID)

	// Multi-valued depends_on — one triple per edge, value is the hashed
	// instance ID of the prerequisite Capability (matches the entity-ID
	// suffix scheme used by RequirementDependsOn).
	for _, dep := range c.DependsOn {
		_ = tw.WriteTriple(ctx, entityID, semspec.CapabilityDependsOn, HashInstanceID(slug, dep))
	}

	// Multi-valued surfaces (ADR-041 Move 2). One triple per declared surface;
	// SurfaceUI is the gate for downstream @e2e scenario emission.
	for _, surface := range c.Surfaces {
		_ = tw.WriteTriple(ctx, entityID, semspec.CapabilitySurface, string(surface))
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
		if err := ValidateCapabilitySurfaces(caps[i]); err != nil {
			return err
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
	return detectCapabilityCycle(caps)
}

// ValidateCapabilitySurfaces enforces that every entry in c.Surfaces resolves
// to a defined CapabilitySurface constant and that no surface appears more
// than once. Empty Surfaces is allowed — downstream consumers treat it as
// "unknown" and default to SurfaceAPI. ADR-041 Move 2.
func ValidateCapabilitySurfaces(c Capability) error {
	if len(c.Surfaces) == 0 {
		return nil
	}
	seen := make(map[CapabilitySurface]struct{}, len(c.Surfaces))
	for i, s := range c.Surfaces {
		if !s.IsValid() {
			return fmt.Errorf("capability %q surfaces[%d] %q is not one of ui/api/background", c.Name, i, s)
		}
		if _, dup := seen[s]; dup {
			return fmt.Errorf("capability %q declares surface %q more than once", c.Name, s)
		}
		seen[s] = struct{}{}
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

// ValidateRequirementCapabilityCoverage enforces the ADR-040 Move 2 rule:
// every Requirement's CapabilityName must resolve to a declared Capability,
// and every Capability must own at least one Requirement. Called from
// plan-manager's handleRequirementsMutation when Plan.Exploration is set.
//
// Skipped when exploration is nil — legacy plans without analyst sub-phase
// have no capabilities to validate against. Skipped also when ALL
// requirements have empty CapabilityName: the heuristic is "if the
// requirement-generator was wired for capabilities, every req has one;
// mixed states are a regression and surfaced".
//
// Returns three error categories so plan-reviewer rules can distinguish
// them (and so retry feedback can point at the right fix):
//   - orphan_capability: a Capability with no implementing Requirement
//   - orphan_requirement_cap: a Requirement.CapabilityName not in exploration
//   - inconsistent: mixed state (some reqs with cap, others without)
func ValidateRequirementCapabilityCoverage(exp *Exploration, requirements []Requirement) error {
	if exp == nil || len(exp.Capabilities) == 0 {
		return nil
	}
	if len(requirements) == 0 {
		return nil
	}

	withCap := 0
	for _, r := range requirements {
		if r.CapabilityName != "" {
			withCap++
		}
	}
	if withCap == 0 {
		// All requirements pre-date capability wiring (e.g. mid-cascade
		// upgrade of an in-flight plan). Don't fail — let the operator
		// run regen if they want capability linkage.
		return nil
	}
	if withCap < len(requirements) {
		return fmt.Errorf("inconsistent capability linkage: %d of %d requirements have CapabilityName set (must be all-or-none)",
			withCap, len(requirements))
	}

	declared := make(map[string]bool, len(exp.Capabilities))
	for _, c := range exp.Capabilities {
		declared[c.Name] = true
	}

	// Orphan requirement check: every CapabilityName must resolve.
	for _, r := range requirements {
		if !declared[r.CapabilityName] {
			return fmt.Errorf("requirement %s references capability %q which is not declared in Plan.Exploration",
				r.ID, r.CapabilityName)
		}
	}

	// Orphan capability check: every capability must own ≥1 requirement.
	covered := make(map[string]bool, len(exp.Capabilities))
	for _, r := range requirements {
		covered[r.CapabilityName] = true
	}
	for _, c := range exp.Capabilities {
		if !covered[c.Name] {
			return fmt.Errorf("capability %q has no implementing requirement (capability_orphan)", c.Name)
		}
	}

	return nil
}

// FindUncoveredCapabilities returns the names of capabilities in the
// exploration that have zero implementing requirements. Used by
// plan-reviewer's capability_orphan rule to produce per-capability findings
// rather than the all-or-nothing error from ValidateRequirementCapabilityCoverage.
//
// Empty exploration or empty requirements returns nil.
func FindUncoveredCapabilities(exp *Exploration, requirements []Requirement) []string {
	if exp == nil || len(exp.Capabilities) == 0 {
		return nil
	}
	covered := make(map[string]bool, len(exp.Capabilities))
	for _, r := range requirements {
		if r.CapabilityName != "" {
			covered[r.CapabilityName] = true
		}
	}
	var uncovered []string
	for _, c := range exp.Capabilities {
		if !covered[c.Name] {
			uncovered = append(uncovered, c.Name)
		}
	}
	return uncovered
}

// FindOrphanRequirementCapabilities returns requirements whose CapabilityName
// doesn't resolve to a declared capability. Used by plan-reviewer.
func FindOrphanRequirementCapabilities(exp *Exploration, requirements []Requirement) []Requirement {
	if exp == nil {
		return nil
	}
	declared := make(map[string]bool, len(exp.Capabilities))
	for _, c := range exp.Capabilities {
		declared[c.Name] = true
	}
	var orphans []Requirement
	for _, r := range requirements {
		if r.CapabilityName != "" && !declared[r.CapabilityName] {
			orphans = append(orphans, r)
		}
	}
	return orphans
}

// FindDocsOnlyCapabilities was the Requirement-level run-#3-fingerprint
// detector — it flagged capabilities whose owning Requirements only touched
// documentation files. ADR-043 Move 4 removed Requirement.FilesOwned;
// file ownership moved to Story. The architectural-layer
// equivalent (architecture.component_implementation_files_doc_only, PR 2)
// + the story-layer equivalent (story.docs_only_files_owned, PR 3) catch
// the same shape upstream of where this rule used to fire.
//
// This function returns nil unconditionally now — kept for ABI continuity
// with plan-reviewer's capability_rules.go which still calls it. The rule
// path becomes a no-op for ADR-043 plans; legacy plans (no Stories) also
// get nil because file paths were never recorded on those Requirements in
// post-PR-1 wire shapes.
func FindDocsOnlyCapabilities(exp *Exploration, requirements []Requirement) []string {
	_ = exp
	_ = requirements
	return nil
}

// IsDocumentationPath reports whether a workspace-relative path looks like
// pure documentation. Loose heuristic — false negatives are fine (a
// docs-only capability that escapes this filter is still caught by
// downstream phases that need actual implementation code).
func IsDocumentationPath(p string) bool {
	lower := strings.ToLower(p)
	suffixes := []string{".md", ".txt", ".rst", ".adoc"}
	for _, s := range suffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	// Common readme-shaped filenames without extension.
	bases := []string{"readme", "license", "contributing", "changelog"}
	base := path.Base(lower)
	for _, b := range bases {
		if base == b {
			return true
		}
	}
	return false
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
