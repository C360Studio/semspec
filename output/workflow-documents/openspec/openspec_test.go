package openspec

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func samplePlan() *workflow.Plan {
	return &workflow.Plan{
		Slug:  "test-plan",
		Title: "Add MAVSDK driver",
		Goal:  "Implement MAVLink/MAVSDK support for the OSH sensorhub.",
		Exploration: &workflow.Exploration{
			Capabilities: []workflow.Capability{
				{
					Name:        "mavsdk-bootstrap",
					Lifecycle:   workflow.CapabilityNew,
					Description: "Boot mavsdk_server and manage peer connection lifecycle.",
				},
				{
					Name:        "telemetry-stream",
					Lifecycle:   workflow.CapabilityNew,
					Description: "Surface MAVSDK telemetry as a CS API DataStream.",
					DependsOn:   []string{"mavsdk-bootstrap"},
				},
				{
					Name:        "legacy-shim",
					Lifecycle:   workflow.CapabilityModified,
					Description: "Extend existing telemetry shim with MAVLink fallback.",
				},
			},
			OpenQuestions: []string{"Static or runtime coverage check?"},
		},
		Requirements: []workflow.Requirement{
			{
				ID:             "r1",
				Title:          "Bootstrap mavsdk_server",
				Description:    "The driver MUST boot mavsdk_server on plan startup.",
				CapabilityName: "mavsdk-bootstrap",
				FilesOwned:     []string{"src/main/java/Bootstrap.java", "src/test/java/BootstrapTest.java"},
			},
			{
				ID:             "r2",
				Title:          "Emit MAVSDK telemetry",
				Description:    "The driver SHALL emit CS DataStream events for MAVSDK telemetry frames.",
				CapabilityName: "telemetry-stream",
				FilesOwned:     []string{"src/main/java/TelemetryStream.java"},
				DependsOn:      []string{"r1"},
			},
		},
		Scenarios: []workflow.Scenario{
			{
				ID:            "s1",
				RequirementID: "r1",
				Given:         "mavsdk_server binary is on PATH",
				When:          "driver.start() is called",
				Then:          []string{"server reaches LISTEN state within 5s", "log emits boot heartbeat"},
				Status:        workflow.ScenarioStatusPassing,
			},
			{
				ID:            "s2",
				RequirementID: "r2",
				Given:         "MAVSDK telemetry frame arrives",
				When:          "driver receives the frame",
				Then:          []string{"CS DataStream emits the telemetry payload"},
				Status:        workflow.ScenarioStatusPending,
			},
		},
		Architecture: &workflow.ArchitectureDocument{
			TechnologyChoices: []workflow.TechChoice{
				{Category: "language", Choice: "Java 17", Rationale: "OSH SDK target"},
			},
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "MavsdkDriver", Responsibility: "MAVLink bridge", UpstreamRefs: []string{"MAVSDK-Java"}},
			},
			Decisions: []workflow.ArchDecision{
				{ID: "ARCH-001", Title: "MAVSDK over raw MAVLink", Decision: "Use mavsdk_server", Rationale: "Better lifecycle"},
			},
			HarnessProfiles: []workflow.HarnessProfileSelection{
				{ProfileID: "mavlink.px4-sitl.mavsdk-smoke", UsedBy: []string{"MavsdkDriver"}, Purpose: "smoke-test integration"},
			},
		},
	}
}

func TestRenderProposal_Happy(t *testing.T) {
	got := RenderProposal(samplePlan())
	required := []string{
		"# Proposal: Add MAVSDK driver",
		"## Why",
		"Implement MAVLink/MAVSDK support",
		"## What Changes",
		"### New Capabilities",
		"`mavsdk-bootstrap`",
		"`telemetry-stream`",
		"### Modified Capabilities",
		"`legacy-shim`",
		"## Capability Dependencies",
		"`telemetry-stream` depends on `mavsdk-bootstrap`",
		"## Open Questions",
		"Static or runtime coverage check?",
	}
	for _, s := range required {
		if !strings.Contains(got, s) {
			t.Errorf("proposal missing %q:\n%s", s, got)
		}
	}
}

func TestRenderProposal_NilOrLegacyReturnsEmpty(t *testing.T) {
	if got := RenderProposal(nil); got != "" {
		t.Errorf("nil plan: got %q, want empty", got)
	}
	if got := RenderProposal(&workflow.Plan{Slug: "legacy"}); got != "" {
		t.Errorf("legacy plan: got %q, want empty", got)
	}
}

func TestRenderSpec_Happy(t *testing.T) {
	got := RenderSpec(samplePlan(), "mavsdk-bootstrap")
	required := []string{
		"# Spec: mavsdk-bootstrap",
		"## Overview",
		"Boot mavsdk_server",
		"## Applies To",
		"`src/main/java/Bootstrap.java`",
		"`src/test/java/BootstrapTest.java`",
		"## Requirements",
		"### Bootstrap mavsdk_server",
		"MUST boot mavsdk_server",
		"#### Scenarios",
		"**GIVEN** mavsdk_server binary is on PATH",
		"**WHEN** driver.start() is called",
		"**THEN** server reaches LISTEN state within 5s",
		"**AND** log emits boot heartbeat",
	}
	for _, s := range required {
		if !strings.Contains(got, s) {
			t.Errorf("spec missing %q:\n%s", s, got)
		}
	}
}

func TestRenderSpec_UnknownCapabilityReturnsEmpty(t *testing.T) {
	if got := RenderSpec(samplePlan(), "no-such-cap"); got != "" {
		t.Errorf("expected empty for unknown cap, got %q", got)
	}
}

func TestRenderSpec_CapabilityWithoutReqReturnsEmpty(t *testing.T) {
	plan := samplePlan()
	// legacy-shim has no implementing requirement in samplePlan()
	if got := RenderSpec(plan, "legacy-shim"); got != "" {
		t.Errorf("expected empty for capability without implementing req, got %q", got)
	}
}

func TestRenderDesign_Happy(t *testing.T) {
	got := RenderDesign(samplePlan())
	required := []string{
		"# Design: Add MAVSDK driver",
		"## Technology Choices",
		"| Java 17 |",
		"## Components",
		"### MavsdkDriver",
		"**Responsibility**: MAVLink bridge",
		"**Upstream refs**: MAVSDK-Java",
		"## Decisions",
		"### ARCH-001: MAVSDK over raw MAVLink",
		"## Test Harness Profiles",
		"`mavlink.px4-sitl.mavsdk-smoke`",
	}
	for _, s := range required {
		if !strings.Contains(got, s) {
			t.Errorf("design missing %q:\n%s", s, got)
		}
	}
}

func TestRenderDesign_NoArchitectureReturnsEmpty(t *testing.T) {
	plan := samplePlan()
	plan.Architecture = nil
	if got := RenderDesign(plan); got != "" {
		t.Errorf("expected empty when Architecture nil, got %q", got)
	}
}

func TestRenderTasks_HappyEmptyExecs(t *testing.T) {
	got := RenderTasks(samplePlan(), nil)
	// All requirements should be unchecked when execs is nil.
	if !strings.Contains(got, "## mavsdk-bootstrap") {
		t.Errorf("tasks missing capability section: %s", got)
	}
	if !strings.Contains(got, "- [ ] Bootstrap mavsdk_server (`r1`)") {
		t.Errorf("tasks expected unchecked r1: %s", got)
	}
	// passing scenario s1 should already render checked even when req execs empty.
	if !strings.Contains(got, "- [x] driver.start() is called") {
		t.Errorf("tasks expected scenario s1 checked: %s", got)
	}
}

func TestRenderTasks_ChecksCompletedReq(t *testing.T) {
	execs := map[string]workflow.RequirementExecution{
		"r1": {RequirementID: "r1", Stage: "completed"},
		"r2": {RequirementID: "r2", Stage: "executing"},
	}
	got := RenderTasks(samplePlan(), execs)
	if !strings.Contains(got, "- [x] Bootstrap mavsdk_server (`r1`)") {
		t.Errorf("expected r1 checked: %s", got)
	}
	if !strings.Contains(got, "- [ ] Emit MAVSDK telemetry (`r2`)") {
		t.Errorf("expected r2 unchecked (stage=executing): %s", got)
	}
}

func TestRenderTasks_CapabilityWithoutReqShowsPlaceholder(t *testing.T) {
	got := RenderTasks(samplePlan(), nil)
	// legacy-shim has no implementing req in samplePlan() — should still
	// render the section header with a "no implementing requirement" note
	// so the gap is visible in tasks.md.
	if !strings.Contains(got, "## legacy-shim") {
		t.Errorf("tasks missing legacy-shim section: %s", got)
	}
	if !strings.Contains(got, "_(no implementing requirement yet)_") {
		t.Errorf("tasks missing placeholder for legacy-shim: %s", got)
	}
}

func TestRenderTasks_LegacyPlanReturnsEmpty(t *testing.T) {
	if got := RenderTasks(&workflow.Plan{Slug: "legacy"}, nil); got != "" {
		t.Errorf("legacy plan should render empty, got %q", got)
	}
}

func TestListCapabilityNames(t *testing.T) {
	got := ListCapabilityNames(samplePlan())
	want := []string{"mavsdk-bootstrap", "telemetry-stream"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i, n := range want {
		if got[i] != n {
			t.Errorf("at idx %d: want %q, got %q", i, n, got[i])
		}
	}
}

func TestRenderOpenSpecYAML_IsStable(t *testing.T) {
	got := RenderOpenSpecYAML()
	if !strings.Contains(got, "schema: spec-driven") {
		t.Errorf("expected schema declaration, got: %s", got)
	}
	// Idempotency check — same call returns same content.
	if got2 := RenderOpenSpecYAML(); got != got2 {
		t.Errorf("RenderOpenSpecYAML should be deterministic across calls")
	}
}
