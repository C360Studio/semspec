package specimport

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semspec/graph"
	"github.com/c360studio/semspec/vocabulary/spec"
	"github.com/c360studio/semspec/workflow"
)

// TranslateOptions controls how Translate composes the imported Plan.
// All fields optional; sensible defaults apply.
type TranslateOptions struct {
	// Slug is the slug to assign to the imported Plan. When empty,
	// derived from the change name. Slug uniqueness is the caller's
	// responsibility (plan-manager's create rejects collisions).
	Slug string

	// Title is the human-readable plan title. When empty, derived from
	// "Import: <change-name>".
	Title string

	// ProjectID is the parent project entity ID. When empty, plan-manager
	// applies its default-project rule when persisting.
	ProjectID string

	// GraphReadinessBudget is how long to wait for the graph to be
	// queryable. Zero falls back to 30s — semsource indexing latency
	// is usually sub-second, but a fresh stack startup can take longer.
	GraphReadinessBudget time.Duration
}

// Translate reads OpenSpec entities from the SKG and produces a
// workflow.Plan ready for plan-manager to persist. Per ADR-040 Move 4:
//
//   - One spec entity → one Capability (kebab-case name from file path)
//   - One spec requirement entity → one workflow.Requirement with
//     CapabilityName set to the spec's capability identifier
//   - One spec scenario entity → one workflow.Scenario
//
// The structural pre-check from StructuralCheck supplies the expected
// capability set; entities not corresponding to those capabilities are
// ignored. This is the load-bearing correlation that lets us import
// from a multi-change semsource without mixing capabilities from other
// changes into the same Plan.
//
// Round-trip identity preservation: the external entity IDs from the
// source graph are stitched into Capability/Requirement via the
// `external_spec` triples emitted by SaveCapabilities / SaveRequirements
// (predicates registered in PR 1a). Storage of those identifiers is the
// caller's responsibility — see ExternalRefs on the returned Plan.
type TranslateResult struct {
	Plan *workflow.Plan `json:"plan"`
	// ExternalRefs maps the canonical name (capability or requirement ID)
	// to the source graph's entity ID. Plan-manager writes these as
	// external_spec triples post-persist so the round-trip emitter can
	// surface the original identity. Empty map allowed.
	ExternalRefs map[string]string `json:"external_refs,omitempty"`
}

// Translate runs the graph→Plan translation. The structural pre-check
// MUST have run first; pass its result to bound the expected capability
// set. Returns an error when no requirements were found for any
// capability — that's a sign that semsource hasn't indexed the change
// yet (caller can retry).
func Translate(ctx context.Context, q graph.Querier, sr *StructuralResult, opts TranslateOptions) (*TranslateResult, error) {
	if q == nil {
		return nil, fmt.Errorf("graph querier required")
	}
	if sr == nil || !sr.OK {
		return nil, fmt.Errorf("structural pre-check must pass before Translate")
	}
	if len(sr.Proposal.CapabilityNames) == 0 {
		return nil, fmt.Errorf("no capabilities declared in proposal — nothing to translate")
	}

	if opts.GraphReadinessBudget <= 0 {
		opts.GraphReadinessBudget = 30 * time.Second
	}
	readyCtx, cancel := context.WithTimeout(ctx, opts.GraphReadinessBudget)
	defer cancel()
	if err := q.WaitForReady(readyCtx, opts.GraphReadinessBudget); err != nil {
		return nil, fmt.Errorf("graph not ready within budget: %w", err)
	}

	slug := opts.Slug
	if slug == "" {
		slug = sr.ChangeName
	}
	title := opts.Title
	if title == "" {
		title = "Import: " + sr.ChangeName
	}

	plan := &workflow.Plan{
		Slug:      slug,
		Title:     title,
		ProjectID: opts.ProjectID,
		Status:    workflow.StatusExplored, // imports skip the analyst sub-phase
		Goal:      readProposalGoal(sr),
		Context:   fmt.Sprintf("Imported from OpenSpec change %s (%s).", sr.ChangeName, sr.ChangePath),
		Exploration: &workflow.Exploration{
			Capabilities: make([]workflow.Capability, 0, len(sr.Proposal.CapabilityNames)),
		},
	}

	externalRefs := make(map[string]string)

	// Map each declared capability to a graph specification entity.
	specsByCap, err := loadSpecEntitiesByCapability(ctx, q, sr)
	if err != nil {
		return nil, fmt.Errorf("load spec entities: %w", err)
	}

	// Build capabilities (one per declared name; lifecycle defaults to
	// "modified" when the spec exists in the source OpenSpec store,
	// "new" when our import is introducing it semspec-side). For an
	// inbound import everything is "modified" from semspec's perspective
	// because the source already has the spec — but the PR 3 outbound
	// emitter writes both new + modified, so we preserve the lifecycle
	// hint if the proposal declared it. Default to "modified" because
	// inbound imports adopt existing specs.
	for _, capName := range sr.Proposal.CapabilityNames {
		cap := workflow.Capability{
			Name:        capName,
			Lifecycle:   workflow.CapabilityModified,
			Description: capabilityDescription(specsByCap[capName], capName),
		}
		plan.Exploration.Capabilities = append(plan.Exploration.Capabilities, cap)
		if e := specsByCap[capName]; e != nil {
			externalRefs["capability:"+capName] = e.ID
		}
	}

	// Translate requirements + scenarios.
	if err := translateRequirements(ctx, q, plan, specsByCap, externalRefs); err != nil {
		return nil, err
	}
	if len(plan.Requirements) == 0 {
		return nil, fmt.Errorf("graph returned no requirement entities for change %q — semsource may not have indexed it yet", sr.ChangeName)
	}

	return &TranslateResult{Plan: plan, ExternalRefs: externalRefs}, nil
}

// loadSpecEntitiesByCapability returns a map of capability name →
// specification graph entity. Capability names not found in the graph
// map to nil entries.
func loadSpecEntitiesByCapability(ctx context.Context, q graph.Querier, sr *StructuralResult) (map[string]*graph.Entity, error) {
	out := make(map[string]*graph.Entity, len(sr.Proposal.CapabilityNames))
	for _, capName := range sr.Proposal.CapabilityNames {
		out[capName] = nil
	}

	// Pull all entities that have the spec.meta.type predicate, then
	// filter to those whose file_path falls under this change's
	// specs/<capName>/ directory.
	entities, err := q.QueryEntitiesByPredicate(ctx, spec.SpecType)
	if err != nil {
		return nil, fmt.Errorf("query spec entities: %w", err)
	}
	wantDir := filepath.Join(sr.ChangePath, "specs")
	for i := range entities {
		e := &entities[i]
		if entityStringTriple(e, spec.SpecType) != "specification" {
			continue
		}
		filePath := entityStringTriple(e, spec.SpecFilePath)
		if filePath == "" {
			continue
		}
		capName := capabilityNameFromSpecPath(filePath, wantDir)
		if capName == "" {
			continue
		}
		if _, want := out[capName]; !want {
			continue
		}
		out[capName] = e
	}
	return out, nil
}

// capabilityNameFromSpecPath extracts the kebab-case capability name from
// a spec file path. Expected shape: `<changePath>/specs/<capName>/spec.md`.
// Returns "" when the path doesn't match the expected layout.
func capabilityNameFromSpecPath(filePath, specsDir string) string {
	// Allow both absolute and relative file paths in the graph.
	cleaned := filepath.Clean(filePath)
	if !strings.HasSuffix(cleaned, string(filepath.Separator)+"spec.md") &&
		!strings.HasSuffix(cleaned, "/spec.md") {
		return ""
	}
	dir := filepath.Dir(cleaned)
	// dir is .../specs/<capName>; we want the last segment.
	if !strings.Contains(dir, "/specs/") && !strings.HasSuffix(filepath.Dir(dir), "specs") {
		return ""
	}
	return filepath.Base(dir)
}

// translateRequirements iterates each capability's specification entity,
// walks the spec.rel.has_requirement edges to load Requirement entities,
// then walks spec.rel.has_scenario edges to load Scenarios.
func translateRequirements(ctx context.Context, q graph.Querier, plan *workflow.Plan, specsByCap map[string]*graph.Entity, externalRefs map[string]string) error {
	for capName, specEntity := range specsByCap {
		if specEntity == nil {
			continue
		}
		reqEntities, err := q.TraverseRelationships(ctx, specEntity.ID, spec.HasRequirement, "outgoing", 1)
		if err != nil {
			return fmt.Errorf("traverse requirements for %s: %w", capName, err)
		}
		for ri := range reqEntities {
			re := &reqEntities[ri]
			reqName := entityStringTriple(re, spec.RequirementName)
			if reqName == "" {
				continue
			}
			req := workflow.Requirement{
				ID:             requirementIDFromName(plan.Slug, capName, reqName),
				Title:          reqName,
				Description:    entityStringTriple(re, spec.RequirementDescription),
				Status:         workflow.RequirementStatusActive,
				CapabilityName: capName,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			}
			plan.Requirements = append(plan.Requirements, req)
			externalRefs["requirement:"+req.ID] = re.ID

			// Scenarios under this requirement.
			scenEntities, err := q.TraverseRelationships(ctx, re.ID, spec.HasScenario, "outgoing", 1)
			if err != nil {
				return fmt.Errorf("traverse scenarios for %s.%s: %w", capName, reqName, err)
			}
			for si := range scenEntities {
				se := &scenEntities[si]
				scen := workflow.Scenario{
					ID:            scenarioIDFromName(req.ID, entityStringTriple(se, spec.ScenarioName)),
					RequirementID: req.ID,
					Given:         entityStringTriple(se, spec.ScenarioGiven),
					When:          entityStringTriple(se, spec.ScenarioWhen),
					Then:          entityStringSliceTriple(se, spec.ScenarioThen),
					Status:        workflow.ScenarioStatusPending,
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}
				plan.Scenarios = append(plan.Scenarios, scen)
			}
		}
	}
	// Stable order so re-imports are diff-friendly.
	sort.SliceStable(plan.Requirements, func(i, j int) bool {
		return plan.Requirements[i].ID < plan.Requirements[j].ID
	})
	sort.SliceStable(plan.Scenarios, func(i, j int) bool {
		return plan.Scenarios[i].ID < plan.Scenarios[j].ID
	})
	return nil
}

// readProposalGoal returns the first paragraph after a `## Why` header if
// the structural check found one; otherwise empty. Goal can stay empty
// — operator can edit the imported Plan post-creation.
func readProposalGoal(sr *StructuralResult) string {
	if !sr.Proposal.HasWhySection {
		return ""
	}
	// The structural check only flagged presence; we don't extract body
	// from here because it'd duplicate parsing. plan-manager surfaces an
	// empty Goal until the operator fills it in. Future enhancement:
	// parse a leading paragraph into Goal during structural check and
	// expose it on StructuralResult.
	return ""
}

func capabilityDescription(e *graph.Entity, fallback string) string {
	if e == nil {
		return fallback + " (imported capability)"
	}
	desc := entityStringTriple(e, spec.RequirementDescription)
	if desc == "" {
		desc = fallback + " (imported capability)"
	}
	return desc
}

// entityStringTriple returns the first triple object matching predicate
// as a string. Returns "" when missing or non-string.
func entityStringTriple(e *graph.Entity, predicate string) string {
	if e == nil {
		return ""
	}
	for _, t := range e.Triples {
		if t.Predicate != predicate {
			continue
		}
		if s, ok := t.Object.(string); ok {
			return s
		}
	}
	return ""
}

// entityStringSliceTriple collects all triple objects matching predicate
// as strings. Handles both single multi-valued triples (array object) and
// multiple per-predicate triples.
func entityStringSliceTriple(e *graph.Entity, predicate string) []string {
	if e == nil {
		return nil
	}
	var out []string
	for _, t := range e.Triples {
		if t.Predicate != predicate {
			continue
		}
		switch v := t.Object.(type) {
		case string:
			out = append(out, v)
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// requirementIDFromName produces a deterministic ID for an imported
// Requirement. Distinct shape from requirement-generator's
// `requirement.<slug>.<seq>` (3 parts) — imports use
// `requirement.<slug>.<capName>.<slugified-reqName>` (4 parts) so
// re-imports of the same source spec produce stable IDs without
// dependency on insertion order.
//
// Downstream code (executor, reviewer) treats both shapes identically
// because RequirementEntityID hashes the logical ID opaquely. The
// difference matters only for human-readable diagnostics in logs.
//
// PlanID is NOT set here — callers (Translate) leave it empty and rely
// on plan-manager's writeRequirementTriples to fill PlanID from
// PlanEntityID(slug) when triples are emitted.
func requirementIDFromName(slug, capName, reqName string) string {
	return fmt.Sprintf("requirement.%s.%s.%s", slug, capName, slugifyForID(reqName))
}

func scenarioIDFromName(reqID, scenName string) string {
	return fmt.Sprintf("scenario.%s.%s", reqID, slugifyForID(scenName))
}

// slugifyForID coerces an arbitrary string into a stable ID-safe slug.
// Lowercases, replaces non-alphanumerics with hyphens, collapses runs.
func slugifyForID(s string) string {
	low := strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevHyphen := false
	for _, r := range low {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen && b.Len() > 0 {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	out := b.String()
	out = strings.TrimRight(out, "-")
	if out == "" {
		return "x"
	}
	return out
}
