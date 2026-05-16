package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestArchitectureDocument_UpstreamResolutionsRoundTrip locks in the
// upstream-strengthening schema added 2026-05-15. UpstreamResolutions +
// APISurface + ComponentDef.UpstreamRefs must marshal and unmarshal
// preserving every field the architect populates so the dev's downstream
// context-builder (and the eventual reviewer enforcement commit) can
// rely on the shape being intact through the KV write/read cycle.
//
// See [[research-shelved-pivot-to-upstream-strengthening-2026-05-15]] for
// the physics framing — this schema is where K-reduction at the upstream
// layer becomes a structural commitment.
func TestArchitectureDocument_UpstreamResolutionsRoundTrip(t *testing.T) {
	doc := ArchitectureDocument{
		TechnologyChoices: []TechChoice{
			{Category: "build", Choice: "Gradle", Rationale: "build.gradle present"},
		},
		ComponentBoundaries: []ComponentDef{
			{
				Name:           "driver",
				Responsibility: "Meshtastic protocol handler",
				Dependencies:   []string{},
				UpstreamRefs:   []string{"OpenSensorHub Core", "Meshtastic Java"},
			},
		},
		DataFlow: "mesh → driver → bus",
		Decisions: []ArchDecision{
			{ID: "ARCH-001", Title: "Subclass AbstractSensorModule", Decision: "Driver extends ASM", Rationale: "see /sources/.../ASM.java"},
		},
		UpstreamResolutions: []UpstreamResolution{
			{
				Name:       "OpenSensorHub Core",
				Coordinate: "org.sensorhub:sensorhub-core:2.0.0",
				SourceRef:  "https://central.sonatype.com/artifact/org.sensorhub/sensorhub-core/2.0.0",
				APIs: []APISurface{
					{
						Symbol:    "AbstractSensorModule",
						Kind:      "class",
						Signature: "protected AbstractSensorModule(SensorConfig config)",
						Lifecycle: "init(config) -> start() -> stop()",
						Notes:     "must call super.init before IO",
						Citation:  "https://github.com/.../AbstractSensorModule.java#L45-L52",
					},
				},
				UsedBy: []string{"driver"},
			},
		},
	}

	data, err := json.Marshal(&doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed ArchitectureDocument
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(parsed.UpstreamResolutions) != 1 {
		t.Fatalf("UpstreamResolutions count = %d, want 1", len(parsed.UpstreamResolutions))
	}
	r := parsed.UpstreamResolutions[0]
	if r.Coordinate != "org.sensorhub:sensorhub-core:2.0.0" {
		t.Errorf("Coordinate lost: %q", r.Coordinate)
	}
	if len(r.APIs) != 1 {
		t.Fatalf("APIs count = %d, want 1", len(r.APIs))
	}
	api := r.APIs[0]
	if api.Symbol != "AbstractSensorModule" || api.Kind != "class" || api.Lifecycle == "" || api.Citation == "" {
		t.Errorf("APISurface fields lost: %+v", api)
	}
	if len(parsed.ComponentBoundaries) != 1 || len(parsed.ComponentBoundaries[0].UpstreamRefs) != 2 {
		t.Errorf("ComponentDef.UpstreamRefs lost: %+v", parsed.ComponentBoundaries[0])
	}
}

// TestArchitectureDocument_NewFieldsOmittedWhenEmpty verifies the
// omitempty tags work — older plans (pre-2026-05-15) deserialize
// cleanly and freshly-emitted plans without external deps don't
// pollute the JSON with empty arrays.
func TestArchitectureDocument_NewFieldsOmittedWhenEmpty(t *testing.T) {
	doc := ArchitectureDocument{
		TechnologyChoices: []TechChoice{
			{Category: "language", Choice: "Go"},
		},
		ComponentBoundaries: []ComponentDef{
			{Name: "internal", Responsibility: "stdlib only"},
		},
	}
	data, err := json.Marshal(&doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(data)

	if strings.Contains(got, `"upstream_resolutions"`) {
		t.Errorf("upstream_resolutions should be omitted when empty: %s", got)
	}
	if strings.Contains(got, `"upstream_refs"`) {
		t.Errorf("component upstream_refs should be omitted when empty: %s", got)
	}
}

// TestUpstreamResolution_TestHarnessRoundTrip locks in the
// Testcontainers-tier additions from 2026-05-15. Role +
// TestHarness must survive marshal/unmarshal so the reviewer's
// cross-check (declared integration_target ⇒ non-nil TestHarness with
// real image coordinate) has reliable structural data to evaluate.
func TestUpstreamResolution_TestHarnessRoundTrip(t *testing.T) {
	resolution := UpstreamResolution{
		Name:       "Meshtastic daemon",
		Coordinate: "github.com/meshtastic/firmware",
		SourceRef:  "https://meshtastic.org/docs/software/linux-native/",
		APIs: []APISurface{
			{
				Symbol:    "ToRadio",
				Kind:      "type",
				Signature: "message ToRadio { oneof payload_variant { ... } }",
				Citation:  "https://github.com/meshtastic/protobufs/blob/main/meshtastic/mesh.proto",
			},
		},
		UsedBy: []string{"driver"},
		Role:   "integration_target",
		TestHarness: &TestHarness{
			Library:      "testcontainers-java",
			Image:        "meshtastic/meshtasticd:latest",
			AccessMethod: "tcp:4403",
		},
	}

	data, err := json.Marshal(&resolution)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed UpstreamResolution
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.Role != "integration_target" {
		t.Errorf("Role lost: %q", parsed.Role)
	}
	if parsed.TestHarness == nil {
		t.Fatal("TestHarness lost — must round-trip non-nil for integration_target")
	}
	if got, want := parsed.TestHarness.Library, "testcontainers-java"; got != want {
		t.Errorf("TestHarness.Library = %q, want %q", got, want)
	}
	if got, want := parsed.TestHarness.Image, "meshtastic/meshtasticd:latest"; got != want {
		t.Errorf("TestHarness.Image = %q, want %q", got, want)
	}
	if got, want := parsed.TestHarness.AccessMethod, "tcp:4403"; got != want {
		t.Errorf("TestHarness.AccessMethod = %q, want %q", got, want)
	}
}

// TestUpstreamResolution_TestHarnessOmittedForRuntimeDep verifies that
// a runtime_dep resolution (the common case — a library the dev imports
// rather than a service it talks to) omits test_harness from the JSON
// output. omitempty on the pointer field is what makes this work.
func TestUpstreamResolution_TestHarnessOmittedForRuntimeDep(t *testing.T) {
	resolution := UpstreamResolution{
		Name:       "OpenSensorHub Core",
		Coordinate: "org.sensorhub:sensorhub-core:2.0.0",
		SourceRef:  "https://central.sonatype.com/artifact/org.sensorhub/sensorhub-core",
		Role:       "runtime_dep",
	}
	data, err := json.Marshal(&resolution)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(data)
	if strings.Contains(got, `"test_harness"`) {
		t.Errorf("test_harness should be omitted for runtime_dep: %s", got)
	}
}

// TestArchitectureDocument_BackwardCompat verifies a JSON document
// produced before the upstream-strengthening schema landed (no
// upstream_resolutions, no upstream_refs) deserializes cleanly with
// the new fields zero-valued.
func TestArchitectureDocument_BackwardCompat(t *testing.T) {
	legacy := []byte(`{
		"technology_choices": [{"category": "language", "choice": "Go"}],
		"component_boundaries": [{"name": "internal", "responsibility": "x", "dependencies": []}],
		"data_flow": "in -> out",
		"decisions": [{"id": "ARCH-001", "title": "T", "decision": "D", "rationale": "R"}],
		"actors": [],
		"integrations": []
	}`)
	var parsed ArchitectureDocument
	if err := json.Unmarshal(legacy, &parsed); err != nil {
		t.Fatalf("unmarshal legacy doc: %v", err)
	}
	if parsed.UpstreamResolutions != nil {
		t.Errorf("expected nil UpstreamResolutions on legacy doc, got %v", parsed.UpstreamResolutions)
	}
	if parsed.ComponentBoundaries[0].UpstreamRefs != nil {
		t.Errorf("expected nil UpstreamRefs on legacy component, got %v", parsed.ComponentBoundaries[0].UpstreamRefs)
	}
}
