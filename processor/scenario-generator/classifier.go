package scenariogenerator

import (
	"github.com/c360studio/semspec/prompt"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
	"github.com/c360studio/semspec/workflow/payloads"
)

// TierEmission tells the scenario-generator what kind of scenarios to emit
// for one requirement at one test-pyramid tier. ADR-041 Move 3.
//
// The classifier produces a TierEmission list per requirement; the prompt
// builder turns each entry into a tier-appropriate instruction block, and
// (in the interim single-dispatch path) the LLM is asked to emit all
// required tiers in one response. The per-tier-dispatch path (ADR-041
// Move 3 strict) is a future refactor.
type TierEmission struct {
	// Tier is one of workflow.TierUnit / TierIntegration / TierSmoke / TierE2E.
	Tier string

	// HarnessProfileIDs lists the catalog profile IDs scenarios at this tier
	// MUST bind to. Empty for @unit and @e2e; populated for @integration
	// with one entry per bound services-class or testcontainers-class
	// profile. The scenario-generator emits at least one @integration
	// scenario per entry.
	HarnessProfileIDs []string
}

// Classify is the deterministic boundary of ADR-041 Move 3. Given a
// requirement, the capability that owns it, the plan's architecture, and
// the harness catalog, it returns the set of tier emissions the
// scenario-generator must produce.
//
// Rules:
//
//   - @unit always emits (every requirement needs unit coverage as a baseline).
//   - @integration emits ONLY for testcontainers-class profiles — the
//     integration tier the dev sandbox can actually execute. Services-class
//     profiles (a live external daemon such as PX4 SITL) are OPERATOR-TIER:
//     the sandbox cannot stand them up, so they are not a gating tier. The
//     architect still selects them by risk and they are emitted into the
//     operator's qa.yml CI contract; semspec does not gate the dev/Story on
//     evidence it structurally cannot produce. This is the capability gate
//     behind the defer-and-note model — services-class proof runs in the
//     operator's CI, not in the sandbox.
//   - @e2e emits when the requirement's capability declares SurfaceUI in its
//     Surfaces list (set by Mary's analyst sub-phase per Move 2). Capabilities
//     without a UI surface produce no @e2e scenarios.
//   - @smoke is operator-directed only — never emitted by this classifier
//     without an explicit signal. ADR-041 §"Tier-emission classifier".
//
// Binding approximation (PR 2 interim): every architecturally-selected
// testcontainers-class profile is treated as relevant to every requirement. The strict "profiles bound to C's components via
// architecture.harness_profiles[].used_by" semantics from the ADR require a
// Capability ↔ Component mapping that doesn't exist on the data model yet
// (ComponentDef has no CapabilityName field and Requirement has no
// ComponentName field). Coarser binding is correct (emits @integration when
// integration is plausibly needed) but emits more @integration scenarios
// than the strict reading. Tighten in a follow-up when capability-component
// binding lands; the classifier's signature is stable for that swap.
//
// Returns the emissions in deterministic order: @unit first, then
// @integration entries sorted by HarnessProfileID, then @e2e. Stable order
// matters for snapshot tests and reviewer-facing diffs.
func Classify(
	req workflow.Requirement,
	caps []workflow.Capability,
	arch *workflow.ArchitectureDocument,
	cat *harnesscatalog.Catalog,
) []TierEmission {
	emissions := []TierEmission{{Tier: workflow.TierUnit}}

	emissions = append(emissions, integrationEmissions(arch, cat)...)

	if capabilityHasSurface(req.CapabilityName, caps, workflow.SurfaceUI) {
		emissions = append(emissions, TierEmission{Tier: workflow.TierE2E})
	}

	return emissions
}

// integrationEmissions walks the architect's selected harness profiles and
// returns one TierEmission per TESTCONTAINERS-class profile — the integration
// tier the dev sandbox can execute. Services-class (live-daemon) and
// pure-fixture profiles are skipped: services-class is operator-tier (runs in
// the operator's CI via the emitted qa.yml, never gated in the sandbox) and
// pure-fixture is covered at @unit. Returns nil when no architecture is
// present, no profiles are selected, or none are testcontainers-class.
func integrationEmissions(arch *workflow.ArchitectureDocument, cat *harnesscatalog.Catalog) []TierEmission {
	if arch == nil || cat == nil || len(arch.HarnessProfiles) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var ids []string
	for _, sel := range arch.HarnessProfiles {
		if _, dup := seen[sel.ProfileID]; dup {
			continue
		}
		profile, ok := cat.Profiles[sel.ProfileID]
		if !ok {
			continue
		}
		// Capability gate: only testcontainers-class integration runs in the
		// sandbox and may gate the dev/Story. Services-class (live SITL etc.)
		// is operator-tier — skip it here; it reaches the operator via qa.yml.
		if profile.EffectiveOrchestration() != harnesscatalog.OrchestrationTestcontainers {
			continue
		}
		seen[sel.ProfileID] = struct{}{}
		ids = append(ids, sel.ProfileID)
	}
	if len(ids) == 0 {
		return nil
	}
	// Sort for deterministic output. Insertion order from the architect's
	// HarnessProfiles is preserved-with-dedup above; an explicit sort would
	// override that. Since architects may set order intentionally (most-
	// important-first), we preserve it — dedup already imposes uniqueness.
	out := make([]TierEmission, 0, len(ids))
	for _, id := range ids {
		out = append(out, TierEmission{
			Tier:              workflow.TierIntegration,
			HarnessProfileIDs: []string{id},
		})
	}
	return out
}

// wireTiersToPromptTiers converts payloads.RequiredTier (wire shape carried
// in ScenarioGeneratorRequest) to prompt.RequiredTier (prompt-context shape
// consumed by the user-prompt renderer). Identity mapping; kept as a
// distinct function so the wire schema and prompt schema can evolve
// independently. Returns nil when the input is empty so the renderer's
// "section silently omitted on empty input" contract holds.
func wireTiersToPromptTiers(wire []payloads.RequiredTier) []prompt.RequiredTier {
	if len(wire) == 0 {
		return nil
	}
	out := make([]prompt.RequiredTier, 0, len(wire))
	for _, t := range wire {
		out = append(out, prompt.RequiredTier{
			Tag:               t.Tag,
			HarnessProfileIDs: t.HarnessProfileIDs,
		})
	}
	return out
}

// capabilityHasSurface reports whether the capability owning requirement
// `capName` declares `target` in its Surfaces. Returns false when no
// capability matches (legacy plans without exploration) or when the
// matched capability has empty Surfaces (unknown — defaults conservatively
// to "no surface match" so we don't emit unwarranted @e2e scenarios).
func capabilityHasSurface(capName string, caps []workflow.Capability, target workflow.CapabilitySurface) bool {
	if capName == "" {
		return false
	}
	for i := range caps {
		if caps[i].Name != capName {
			continue
		}
		for _, s := range caps[i].Surfaces {
			if s == target {
				return true
			}
		}
		return false
	}
	return false
}
