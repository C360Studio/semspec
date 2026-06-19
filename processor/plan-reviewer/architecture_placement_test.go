package planreviewer

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// These tests pin the deterministic STRUCTURAL half of #237
// (componentFileNamespaceFindings): a file owned by component A whose path sits
// under component B's distinctive name namespace is flagged as a likely
// misplacement. The SEMANTIC half (a control class under a neutral path) is the
// LLM reviewer's criterion 6a and is intentionally NOT caught here — one test
// below documents that gap so a future reader doesn't mistake it for a bug.

func placementComponent(name string, files ...string) workflow.ComponentDef {
	return workflow.ComponentDef{Name: name, ImplementationFiles: files}
}

func TestComponentFileNamespaceFindings_FlagsForeignNamespace(t *testing.T) {
	arch := &workflow.ArchitectureDocument{
		ComponentBoundaries: []workflow.ComponentDef{
			placementComponent("mavsdk-semantic-datastreams",
				"org/sensorhub/datastreams/Telemetry.java", // own namespace — fine
				"org/sensorhub/controlstreams/PD.java",     // foreign — belongs to controlstreams
			),
			placementComponent("mavsdk-semantic-controlstreams",
				"org/sensorhub/controlstreams/ControlLoop.java",
			),
		},
	}

	findings := componentFileNamespaceFindings(arch)
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1\n%+v", len(findings), findings)
	}
	f := findings[0]
	if f.TargetID != "org/sensorhub/controlstreams/PD.java" {
		t.Errorf("TargetID = %q, want the misplaced control file", f.TargetID)
	}
	if f.Severity != "warning" {
		t.Errorf("Severity = %q, want warning (non-blocking nudge)", f.Severity)
	}
	if f.SOPID != "architecture.file_under_foreign_component_namespace" {
		t.Errorf("SOPID = %q", f.SOPID)
	}
	// The suggestion must point at the better-matching component.
	if !strings.Contains(f.TargetValue, "mavsdk-semantic-controlstreams") {
		t.Errorf("TargetValue = %q, want it to name the controlstreams component", f.TargetValue)
	}
}

func TestComponentFileNamespaceFindings_NoFalsePositives(t *testing.T) {
	tests := []struct {
		name string
		arch *workflow.ArchitectureDocument
	}{
		{
			name: "each component under its own namespace",
			arch: &workflow.ArchitectureDocument{ComponentBoundaries: []workflow.ComponentDef{
				placementComponent("mavsdk-semantic-datastreams", "org/sensorhub/datastreams/Telemetry.java"),
				placementComponent("mavsdk-semantic-controlstreams", "org/sensorhub/controlstreams/ControlLoop.java"),
			}},
		},
		{
			name: "shared tokens (mavsdk/semantic) are not distinctive, so no flag",
			arch: &workflow.ArchitectureDocument{ComponentBoundaries: []workflow.ComponentDef{
				placementComponent("mavsdk-semantic-datastreams", "org/mavsdk/semantic/Telemetry.java"),
				placementComponent("mavsdk-semantic-controlstreams", "org/mavsdk/semantic/ControlLoop.java"),
			}},
		},
		{
			name: "neutral path (the 2026-06-19 live case) is NOT caught structurally — criterion 6a's job",
			arch: &workflow.ArchitectureDocument{ComponentBoundaries: []workflow.ComponentDef{
				placementComponent("mavsdk-semantic-datastreams",
					"processing/ConstAltitudeLLA.java", "processing/PD.java"),
				placementComponent("mavsdk-semantic-controlstreams", "control/ControlLoop.java"),
			}},
		},
		{
			name: "single component cannot misplace into a peer",
			arch: &workflow.ArchitectureDocument{ComponentBoundaries: []workflow.ComponentDef{
				placementComponent("mavsdk-semantic-datastreams", "org/sensorhub/controlstreams/PD.java"),
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if findings := componentFileNamespaceFindings(tt.arch); len(findings) != 0 {
				t.Errorf("got %d findings, want 0 (false positive)\n%+v", len(findings), findings)
			}
		})
	}
}
