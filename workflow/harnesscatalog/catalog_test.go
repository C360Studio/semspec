package harnesscatalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestLoadBuiltInLoadsMAVLinkProfiles(t *testing.T) {
	catalog, err := LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn() error = %v", err)
	}

	required, ok := catalog.Profiles["mavlink.px4-sitl.mavsdk-smoke"]
	if !ok {
		t.Fatal("built-in catalog missing mavlink.px4-sitl.mavsdk-smoke")
	}
	if required.Tier != TierRequired {
		t.Fatalf("PX4 smoke tier = %q, want %q", required.Tier, TierRequired)
	}
	for _, anchor := range []string{"mavlink.px4-sitl.mavsdk-smoke", "px4io/px4-sitl", "14540", "HEARTBEAT"} {
		if !contains(required.EvidenceAnchors, anchor) {
			t.Errorf("PX4 smoke evidence anchors missing %q: %v", anchor, required.EvidenceAnchors)
		}
	}
	if catalog.Profiles["mavlink.ardupilot-sitl.compat"].Tier != TierCompatibility {
		t.Error("ArduPilot profile should be compatibility tier")
	}
	if catalog.Profiles["mavlink.px4-gazebo-peripherals"].Tier != TierHeavy {
		t.Error("Gazebo peripheral profile should be heavy tier")
	}
}

func TestLoadWorkspaceOverridesMergeAndReplaceBuiltIn(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".semspec", "harness-catalog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	override := `
profiles:
  - id: mavlink.px4-sitl.mavsdk-smoke
    domain: mavlink
    tier: required
    summary: Workspace-pinned PX4 profile.
    proves: ["workspace override"]
    covers:
      integration_targets: ["PX4"]
    runner_support: ["local-docker"]
    cost: medium
    constraints: ["workspace constraint"]
    required_assertions: ["workspace assertion"]
    evidence_anchors: ["mavlink.px4-sitl.mavsdk-smoke", "workspace-px4-image", "14540"]
  - id: custom.queue.compat
    domain: queue
    tier: compatibility
    summary: Custom queue compatibility profile.
    proves: ["queue compatibility"]
    covers:
      integration_targets: ["queue"]
    runner_support: ["local-docker"]
    cost: low
    constraints: ["none"]
    required_assertions: ["queue starts"]
    evidence_anchors: ["custom.queue.compat", "queue-image"]
`
	if err := os.WriteFile(filepath.Join(dir, "override.yaml"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}

	catalog, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := catalog.Profiles["mavlink.px4-sitl.mavsdk-smoke"].Summary; !strings.Contains(got, "Workspace-pinned") {
		t.Fatalf("override did not replace built-in profile, summary = %q", got)
	}
	if _, ok := catalog.Profiles["custom.queue.compat"]; !ok {
		t.Fatal("workspace-only profile was not merged")
	}
}

func TestLoadWorkspaceRejectsDuplicateOverrideIDs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".semspec", "harness-catalog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeProfile := func(name, id string) {
		t.Helper()
		data := strings.ReplaceAll(validProfileYAML, "$ID", id)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeProfile("a.yaml", "custom.dup")
	writeProfile("b.yaml", "custom.dup")

	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "duplicate harness profile ID") {
		t.Fatalf("Load() error = %v, want duplicate profile ID error", err)
	}
}

func TestLoadRejectsMalformedTierAndEmptyEvidenceAnchors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "malformed tier",
			body: strings.ReplaceAll(validProfileYAML, "tier: required", "tier: someday"),
			want: "malformed tier",
		},
		{
			name: "empty evidence anchor",
			body: strings.ReplaceAll(validProfileYAML, `evidence_anchors: ["$ID", "image"]`, `evidence_anchors: ["$ID", ""]`),
			want: "evidence_anchors[1] is empty",
		},
		{
			name: "missing summary",
			body: strings.ReplaceAll(validProfileYAML, "summary: Valid profile.", "summary: \"\""),
			want: "summary is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".semspec", "harness-catalog")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			body := strings.ReplaceAll(tt.body, "$ID", "custom.bad")
			if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(root)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestResolveSelectionsRejectsUnknownAndSplitsRequired(t *testing.T) {
	catalog, err := LoadBuiltIn()
	if err != nil {
		t.Fatal(err)
	}

	_, err = catalog.ResolveSelections([]workflow.HarnessProfileSelection{{ProfileID: "missing.profile"}})
	if err == nil || !strings.Contains(err.Error(), "unknown harness profile") {
		t.Fatalf("ResolveSelections() error = %v, want unknown profile", err)
	}

	required, err := catalog.RequiredProfiles([]workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.px4-sitl.mavsdk-smoke", Purpose: "required smoke"},
		{ProfileID: "mavlink.ardupilot-sitl.compat", Purpose: "compat"},
	})
	if err != nil {
		t.Fatalf("RequiredProfiles() error = %v", err)
	}
	if len(required) != 1 || required[0].Profile.ID != "mavlink.px4-sitl.mavsdk-smoke" {
		t.Fatalf("RequiredProfiles() = %#v, want only PX4 smoke", required)
	}
}

const validProfileYAML = `
profiles:
  - id: $ID
    domain: test
    tier: required
    summary: Valid profile.
    proves: ["thing"]
    covers:
      integration_targets: ["target"]
    runner_support: ["local-docker"]
    cost: low
    constraints: ["none"]
    required_assertions: ["assertion"]
    evidence_anchors: ["$ID", "image"]
`

func contains(vals []string, want string) bool {
	for _, v := range vals {
		if v == want {
			return true
		}
	}
	return false
}
