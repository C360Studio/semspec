package workflowdocuments

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestRenderArchitecture_NilReturnsEmpty(t *testing.T) {
	if got := RenderArchitecture(nil); got != "" {
		t.Errorf("RenderArchitecture(nil) = %q, want empty", got)
	}
	plan := &workflow.Plan{Slug: "test"}
	if got := RenderArchitecture(plan); got != "" {
		t.Errorf("RenderArchitecture(plan with no Architecture) = %q, want empty", got)
	}
}

func TestRenderArchitecture_FullDeliverable(t *testing.T) {
	plan := &workflow.Plan{
		Slug:  "arch-test",
		Title: "Driver build",
		Architecture: &workflow.ArchitectureDocument{
			TechnologyChoices: []workflow.TechChoice{
				{Category: "language", Choice: "Go", Rationale: "module already declared"},
			},
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "driver", Responsibility: "Translates protocol", Dependencies: []string{}, UpstreamRefs: []string{"OSH"}},
			},
			DataFlow: "mesh -> driver -> bus",
			Decisions: []workflow.ArchDecision{
				{ID: "ARCH-001", Title: "Use real upstream", Decision: "Pull real jars", Rationale: "no fabrication"},
			},
			Actors: []workflow.ActorDef{
				{Name: "Mesh node", Type: "system", Triggers: []string{"packet received"}},
			},
			Integrations: []workflow.IntegrationPoint{
				{Name: "Mesh network", Direction: "bidirectional", Protocol: "TCP", Contract: "Meshtastic protobuf", ErrorMode: "reconnect"},
			},
			UpstreamResolutions: []workflow.UpstreamResolution{
				{
					Name:       "Meshtastic Daemon",
					Coordinate: "meshtastic/meshtasticd:daily-alpine",
					Role:       "integration_target",
					UsedBy:     []string{"driver"},
				},
			},
			HarnessProfiles: []workflow.HarnessProfileSelection{
				{
					ProfileID: "mavlink.px4-sitl.mavsdk-smoke",
					UsedBy:    []string{"driver"},
					Purpose:   "prove real MAVLink control and telemetry",
					Covers:    []string{"Meshtastic Daemon", "telemetry"},
				},
			},
			TestSurface: &workflow.TestSurface{
				IntegrationFlows: []workflow.IntegrationFlow{
					{Name: "frame-roundtrip", ComponentsInvolved: []string{"driver"}, Description: "encode/decode round-trip"},
				},
			},
		},
	}

	md := RenderArchitecture(plan)
	checks := map[string]bool{
		"# Architecture: Driver build":         true,
		"## Technology choices":                true,
		"| language | Go |":                    true,
		"## Component boundaries":              true,
		"### driver":                           true,
		"Internal dependencies":                false, // empty Dependencies slice
		"Upstream refs":                        true,
		"## Data flow":                         true,
		"mesh -> driver -> bus":                true,
		"## Architectural decisions":           true,
		"### ARCH-001: Use real upstream":      true,
		"**Decision:** Pull real jars":         true,
		"## Actors":                            true,
		"**Mesh node** (system)":               true,
		"## Integrations":                      true,
		"| Mesh network | bidirectional | TCP": true,
		"## Upstream resolutions":              true,
		"### Meshtastic Daemon":                true,
		"`integration_target`":                 true,
		"## Harness profiles":                  true,
		"mavlink.px4-sitl.mavsdk-smoke":        true,
		"prove real MAVLink control":           true,
		"## Test surface":                      true,
		"frame-roundtrip":                      true,
	}
	for needle, want := range checks {
		got := strings.Contains(md, needle)
		if got != want {
			t.Errorf("contains(%q) = %v, want %v\n--- markdown ---\n%s", needle, got, want, md)
		}
	}
}

func TestRenderArchitecture_PureLibraryEmptyIntegrations(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "pure-lib",
		Architecture: &workflow.ArchitectureDocument{
			TechnologyChoices: []workflow.TechChoice{
				{Category: "language", Choice: "Go", Rationale: "test"},
			},
			Integrations: []workflow.IntegrationPoint{}, // empty — pure library
		},
	}
	md := RenderArchitecture(plan)
	if !strings.Contains(md, "*None declared — pure-library shape") {
		t.Errorf("pure-library architecture should render the empty-integrations note. got:\n%s", md)
	}
}

func TestRenderArchitecture_RuntimeDepNoHarnessProfile(t *testing.T) {
	plan := &workflow.Plan{
		Slug: "rt-dep",
		Architecture: &workflow.ArchitectureDocument{
			UpstreamResolutions: []workflow.UpstreamResolution{
				{
					Name: "Lib X", Coordinate: "x:1.0", Role: "runtime_dep",
				},
			},
		},
	}
	md := RenderArchitecture(plan)
	if !strings.Contains(md, "`runtime_dep`") {
		t.Error("missing role rendering")
	}
	if !strings.Contains(md, "## Harness profiles") {
		t.Errorf("architecture should render harness profile section. got:\n%s", md)
	}
	if strings.Contains(md, "Test harness:") || strings.Contains(md, "Test"+"Harness") {
		t.Errorf("legacy harness wording should not render. got:\n%s", md)
	}
}
