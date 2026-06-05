package scenariogenerator

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

// fakeCatalog returns a Catalog containing the given profiles, indexed by ID
// for the classifier's lookup pattern.
func fakeCatalog(profiles ...harnesscatalog.Profile) *harnesscatalog.Catalog {
	c := &harnesscatalog.Catalog{Profiles: map[string]harnesscatalog.Profile{}}
	for _, p := range profiles {
		c.Profiles[p.ID] = p
	}
	return c
}

func TestClassify_AlwaysEmitsUnit(t *testing.T) {
	// A requirement with no capability, no architecture, no catalog should
	// still produce a @unit emission. @unit is the baseline tier for every
	// requirement per ADR-041.
	emissions := Classify(
		workflow.Requirement{ID: "r1"},
		nil,
		nil,
		nil,
	)
	if len(emissions) != 1 {
		t.Fatalf("expected exactly one emission, got %d: %+v", len(emissions), emissions)
	}
	if emissions[0].Tier != workflow.TierUnit {
		t.Errorf("expected @unit, got %q", emissions[0].Tier)
	}
	if len(emissions[0].HarnessProfileIDs) != 0 {
		t.Errorf("@unit should not carry HarnessProfileIDs, got %v", emissions[0].HarnessProfileIDs)
	}
}

func TestClassify_ServicesProfileIsOperatorTierNoIntegration(t *testing.T) {
	// A services-class profile (live PX4 SITL daemon) is OPERATOR-TIER: the
	// dev sandbox cannot stand it up, so the classifier must NOT make it a
	// gating @integration tier. It reaches the operator via the emitted
	// qa.yml; semspec does not gate the dev on evidence it can't produce.
	// This is the capability gate behind defer-and-note (replaces the old
	// issue-#37 behavior that forced @integration and caused infinite reject).
	cat := fakeCatalog(harnesscatalog.Profile{
		ID:            "mavlink.px4-sitl.mavsdk-smoke",
		Orchestration: harnesscatalog.OrchestrationServices,
	})
	arch := &workflow.ArchitectureDocument{
		HarnessProfiles: []workflow.HarnessProfileSelection{
			{ProfileID: "mavlink.px4-sitl.mavsdk-smoke"},
		},
	}
	emissions := Classify(workflow.Requirement{ID: "r1"}, nil, arch, cat)

	if len(emissions) != 1 || emissions[0].Tier != workflow.TierUnit {
		t.Fatalf("expected only @unit for a services-class (operator-tier) profile, got %+v", emissions)
	}
}

func TestClassify_TestcontainersProfileEmitsIntegration(t *testing.T) {
	// testcontainers-class IS sandbox-runnable, so it gates as @integration.
	cat := fakeCatalog(harnesscatalog.Profile{
		ID:            "db.postgres.testcontainers",
		Orchestration: harnesscatalog.OrchestrationTestcontainers,
	})
	arch := &workflow.ArchitectureDocument{
		HarnessProfiles: []workflow.HarnessProfileSelection{
			{ProfileID: "db.postgres.testcontainers"},
		},
	}
	emissions := Classify(workflow.Requirement{ID: "r1"}, nil, arch, cat)

	if len(emissions) != 2 || emissions[1].Tier != workflow.TierIntegration {
		t.Fatalf("expected @unit + @integration, got %+v", emissions)
	}
	if emissions[1].HarnessProfileIDs[0] != "db.postgres.testcontainers" {
		t.Errorf("expected testcontainers profile binding, got %v", emissions[1].HarnessProfileIDs)
	}
}

func TestClassify_PureFixtureProfileNoIntegration(t *testing.T) {
	// pure-fixture profiles run in-process (captured frames, fixtures); no
	// peer harness is needed. The classifier MUST NOT emit @integration for
	// them — that would force the agent to author tagged integration tests
	// that have no harness to bind to, and the structural-validator (Move 5)
	// would reject them.
	cat := fakeCatalog(harnesscatalog.Profile{
		ID:            "mavlink.raw-mavlink-direct",
		Orchestration: harnesscatalog.OrchestrationPureFixture,
	})
	arch := &workflow.ArchitectureDocument{
		HarnessProfiles: []workflow.HarnessProfileSelection{
			{ProfileID: "mavlink.raw-mavlink-direct"},
		},
	}
	emissions := Classify(workflow.Requirement{ID: "r1"}, nil, arch, cat)
	if len(emissions) != 1 || emissions[0].Tier != workflow.TierUnit {
		t.Errorf("expected only @unit for pure-fixture, got %+v", emissions)
	}
}

func TestClassify_MultipleTestcontainersProfilesEmitPerProfile(t *testing.T) {
	// Architecture binds two testcontainers-class profiles → two @integration
	// emissions, one per profile, so coverage can be demanded per bound
	// sandbox-runnable profile.
	cat := fakeCatalog(
		harnesscatalog.Profile{ID: "a", Orchestration: harnesscatalog.OrchestrationTestcontainers},
		harnesscatalog.Profile{ID: "b", Orchestration: harnesscatalog.OrchestrationTestcontainers},
	)
	arch := &workflow.ArchitectureDocument{
		HarnessProfiles: []workflow.HarnessProfileSelection{
			{ProfileID: "a"},
			{ProfileID: "b"},
		},
	}
	emissions := Classify(workflow.Requirement{ID: "r1"}, nil, arch, cat)
	if len(emissions) != 3 {
		t.Fatalf("expected @unit + 2× @integration, got %d: %+v", len(emissions), emissions)
	}
	if emissions[1].HarnessProfileIDs[0] != "a" || emissions[2].HarnessProfileIDs[0] != "b" {
		t.Errorf("expected per-profile bindings preserving architect order, got %v + %v", emissions[1].HarnessProfileIDs, emissions[2].HarnessProfileIDs)
	}
}

func TestClassify_DuplicateProfileSelectionsDedupe(t *testing.T) {
	// Architecture lists the same profile twice (architect mistake) — the
	// classifier dedupes by profile ID so the agent doesn't produce two
	// identical @integration scenarios.
	cat := fakeCatalog(harnesscatalog.Profile{
		ID:            "a",
		Orchestration: harnesscatalog.OrchestrationTestcontainers,
	})
	arch := &workflow.ArchitectureDocument{
		HarnessProfiles: []workflow.HarnessProfileSelection{
			{ProfileID: "a"},
			{ProfileID: "a"},
		},
	}
	emissions := Classify(workflow.Requirement{ID: "r1"}, nil, arch, cat)
	if len(emissions) != 2 {
		t.Errorf("expected dedup to collapse duplicate selections, got %+v", emissions)
	}
}

func TestClassify_UnresolvedProfileIDSkipped(t *testing.T) {
	// Architect references a profile ID that isn't in the catalog. The
	// classifier silently skips it — the plan-reviewer rule
	// scenario.harness_id_unresolved (Move 4) is the gate for surfacing this
	// to the operator, not the classifier's job.
	cat := fakeCatalog() // empty
	arch := &workflow.ArchitectureDocument{
		HarnessProfiles: []workflow.HarnessProfileSelection{
			{ProfileID: "ghost.profile"},
		},
	}
	emissions := Classify(workflow.Requirement{ID: "r1"}, nil, arch, cat)
	if len(emissions) != 1 || emissions[0].Tier != workflow.TierUnit {
		t.Errorf("expected only @unit when profile unresolved, got %+v", emissions)
	}
}

func TestClassify_UISurfaceEmitsE2E(t *testing.T) {
	// Capability with SurfaceUI → @e2e emission. Mary's analyst sub-phase
	// (Move 2) classifies surfaces; the classifier consumes that signal
	// rather than the fragile prompt-text heuristic the legacy code used.
	caps := []workflow.Capability{
		{
			Name:      "user-login",
			Lifecycle: workflow.CapabilityNew,
			Surfaces:  []workflow.CapabilitySurface{workflow.SurfaceUI},
		},
	}
	req := workflow.Requirement{ID: "r1", CapabilityName: "user-login"}
	emissions := Classify(req, caps, nil, nil)

	if len(emissions) != 2 {
		t.Fatalf("expected @unit + @e2e, got %d: %+v", len(emissions), emissions)
	}
	if emissions[1].Tier != workflow.TierE2E {
		t.Errorf("expected @e2e, got %q", emissions[1].Tier)
	}
	if len(emissions[1].HarnessProfileIDs) != 0 {
		t.Errorf("@e2e carries no HarnessProfileIDs in PR 2, got %v", emissions[1].HarnessProfileIDs)
	}
}

func TestClassify_APIOnlyCapabilityNoE2E(t *testing.T) {
	// Capability with only SurfaceAPI (or no surfaces) MUST NOT emit @e2e.
	// The motivating mavlink-hard run failure (issue #37) had API-only
	// capabilities the legacy heuristic incorrectly tagged as user-facing.
	caps := []workflow.Capability{
		{
			Name:      "mavsdk-lifecycle",
			Lifecycle: workflow.CapabilityNew,
			Surfaces:  []workflow.CapabilitySurface{workflow.SurfaceAPI},
		},
	}
	req := workflow.Requirement{ID: "r1", CapabilityName: "mavsdk-lifecycle"}
	emissions := Classify(req, caps, nil, nil)
	if len(emissions) != 1 {
		t.Errorf("API-only capability should produce only @unit, got %+v", emissions)
	}
}

func TestClassify_FullStackUIAndServicesEmitsAllTiers(t *testing.T) {
	// Capability with SurfaceUI + plan with services-class profile selected
	// → @unit + @integration (with binding) + @e2e. The all-tiers case
	// matches a typical full-stack feature.
	caps := []workflow.Capability{
		{
			Name:      "user-login",
			Lifecycle: workflow.CapabilityNew,
			Surfaces:  []workflow.CapabilitySurface{workflow.SurfaceUI, workflow.SurfaceAPI},
		},
	}
	cat := fakeCatalog(harnesscatalog.Profile{
		ID:            "db.postgres.testcontainers",
		Orchestration: harnesscatalog.OrchestrationTestcontainers,
	})
	arch := &workflow.ArchitectureDocument{
		HarnessProfiles: []workflow.HarnessProfileSelection{
			{ProfileID: "db.postgres.testcontainers"},
		},
	}
	req := workflow.Requirement{ID: "r1", CapabilityName: "user-login"}
	emissions := Classify(req, caps, arch, cat)

	if len(emissions) != 3 {
		t.Fatalf("expected @unit + @integration + @e2e, got %d: %+v", len(emissions), emissions)
	}
	wantTiers := []string{workflow.TierUnit, workflow.TierIntegration, workflow.TierE2E}
	for i, want := range wantTiers {
		if emissions[i].Tier != want {
			t.Errorf("emissions[%d].Tier = %q, want %q", i, emissions[i].Tier, want)
		}
	}
}

func TestClassify_LegacyExplorationless_NoE2E(t *testing.T) {
	// Plan drafted before ADR-040 (no Exploration / no Capabilities). The
	// classifier MUST NOT crash and MUST NOT emit @e2e on the assumption
	// of a UI surface — that would force the agent to author UI tests for
	// API-only code. Falls back to @unit-only.
	req := workflow.Requirement{ID: "r1", CapabilityName: "user-login"}
	emissions := Classify(req, nil, nil, nil)
	if len(emissions) != 1 || emissions[0].Tier != workflow.TierUnit {
		t.Errorf("expected @unit-only fallback, got %+v", emissions)
	}
}

func TestClassify_RequirementWithoutCapabilityName(t *testing.T) {
	// Legacy requirement where CapabilityName is empty. Surface lookup is
	// skipped; only @unit emits unless architecture also binds a services-
	// class profile.
	req := workflow.Requirement{ID: "r1", CapabilityName: ""}
	caps := []workflow.Capability{
		{
			Name:      "user-login",
			Lifecycle: workflow.CapabilityNew,
			Surfaces:  []workflow.CapabilitySurface{workflow.SurfaceUI},
		},
	}
	emissions := Classify(req, caps, nil, nil)
	if len(emissions) != 1 || emissions[0].Tier != workflow.TierUnit {
		t.Errorf("requirement without capability name should not produce @e2e, got %+v", emissions)
	}
}
