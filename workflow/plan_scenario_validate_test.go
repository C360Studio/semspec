package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateScenarioTags(t *testing.T) {
	tests := []struct {
		name    string
		s       Scenario
		wantErr string
	}{
		{
			name: "single @unit tag",
			s:    Scenario{ID: "s1", Tags: []string{TierUnit}},
		},
		{
			name: "tier tag plus facet tags",
			s:    Scenario{ID: "s1", Tags: []string{TierIntegration, "@flaky", "@slow"}},
		},
		{
			name: "@integration with harness binding",
			s: Scenario{
				ID:                "s1",
				Tags:              []string{TierIntegration},
				HarnessProfileIDs: []string{"mavlink.px4-sitl.mavsdk-smoke"},
			},
		},
		{
			name: "@integration multi-binding",
			s: Scenario{
				ID:                "s1",
				Tags:              []string{TierIntegration},
				HarnessProfileIDs: []string{"mavlink.px4-sitl.mavsdk-smoke", "mavlink.raw-mavlink-direct"},
			},
		},
		{
			name:    "empty tags rejected",
			s:       Scenario{ID: "s1"},
			wantErr: "no tags",
		},
		{
			name:    "no tier tag rejected",
			s:       Scenario{ID: "s1", Tags: []string{"@flaky"}},
			wantErr: "no tier tag",
		},
		{
			name:    "two tier tags rejected",
			s:       Scenario{ID: "s1", Tags: []string{TierUnit, TierIntegration}},
			wantErr: "2 tier tags",
		},
		{
			name:    "duplicate tag rejected",
			s:       Scenario{ID: "s1", Tags: []string{TierUnit, TierUnit}},
			wantErr: "more than once",
		},
		{
			name:    "tag missing @ rejected",
			s:       Scenario{ID: "s1", Tags: []string{"unit"}},
			wantErr: "must start with '@'",
		},
		{
			name:    "tag with colon rejected (pytest-bdd compat)",
			s:       Scenario{ID: "s1", Tags: []string{TierUnit, "@integration:db"}},
			wantErr: "disallowed character ':'",
		},
		{
			name:    "tag with dot rejected (pytest-bdd compat)",
			s:       Scenario{ID: "s1", Tags: []string{TierUnit, "@a.b"}},
			wantErr: "disallowed character '.'",
		},
		{
			name:    "bare @ rejected",
			s:       Scenario{ID: "s1", Tags: []string{TierUnit, "@"}},
			wantErr: "no body after '@'",
		},
		{
			name: "empty harness profile id rejected",
			s: Scenario{
				ID:                "s1",
				Tags:              []string{TierIntegration},
				HarnessProfileIDs: []string{""},
			},
			wantErr: "is empty",
		},
		{
			name: "whitespace harness profile id rejected",
			s: Scenario{
				ID:                "s1",
				Tags:              []string{TierIntegration},
				HarnessProfileIDs: []string{"   "},
			},
			wantErr: "is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateScenarioTags(tt.s)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected success, got error: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tt.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestIsTierTag(t *testing.T) {
	for _, tag := range []string{TierUnit, TierIntegration, TierSmoke, TierE2E} {
		if !IsTierTag(tag) {
			t.Errorf("%q should be recognized as a tier tag", tag)
		}
	}
	for _, tag := range []string{"@flaky", "@security", "@slow", "unit", "@", ""} {
		if IsTierTag(tag) {
			t.Errorf("%q should not be recognized as a tier tag", tag)
		}
	}
}

// TestScenarioJSONRoundTrip pins the wire shape for the new Tags +
// HarnessProfileIDs fields. Without this, a future refactor that mistypes
// a JSON tag would silently break the OpenSpec emitter contract (ADR-041
// Move 6).
func TestScenarioJSONRoundTrip(t *testing.T) {
	original := Scenario{
		ID:                "scn-1",
		RequirementID:     "req-1",
		Given:             "the env is configured",
		When:              "the driver starts",
		Then:              []string{"the heartbeat is observed"},
		Tags:              []string{TierIntegration, "@flaky"},
		HarnessProfileIDs: []string{"mavlink.px4-sitl.mavsdk-smoke"},
	}
	blob, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	wantSubstrings := []string{
		`"tags":["@integration","@flaky"]`,
		`"harness_profile_ids":["mavlink.px4-sitl.mavsdk-smoke"]`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(string(blob), want) {
			t.Errorf("expected marshalled JSON to contain %q; got: %s", want, blob)
		}
	}

	var decoded Scenario
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Tags) != 2 || decoded.Tags[0] != TierIntegration {
		t.Errorf("Tags not round-tripped: %+v", decoded.Tags)
	}
	if len(decoded.HarnessProfileIDs) != 1 || decoded.HarnessProfileIDs[0] != "mavlink.px4-sitl.mavsdk-smoke" {
		t.Errorf("HarnessProfileIDs not round-tripped: %+v", decoded.HarnessProfileIDs)
	}
}

// TestScenarioJSONOmitsEmptyTagFields guards back-compat: legacy scenarios
// produced before ADR-041 lands have no Tags / no HarnessProfileIDs. Their
// wire output must not gain noisy "tags": null or "harness_profile_ids":
// null entries that would confuse round-trip tooling and OpenSpec emitter
// expectations.
func TestScenarioJSONOmitsEmptyTagFields(t *testing.T) {
	s := Scenario{ID: "s1", RequirementID: "r1", Given: "a", When: "b", Then: []string{"c"}}
	blob, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), `"tags"`) {
		t.Errorf("expected omitempty to drop tags when empty; got: %s", blob)
	}
	if strings.Contains(string(blob), `"harness_profile_ids"`) {
		t.Errorf("expected omitempty to drop harness_profile_ids when empty; got: %s", blob)
	}
}

func TestCapabilityJSONRoundTripWithSurfaces(t *testing.T) {
	original := Capability{
		Name:        "user-auth",
		Lifecycle:   CapabilityNew,
		Description: "Authenticate users.",
		Surfaces:    []CapabilitySurface{SurfaceUI, SurfaceAPI},
	}
	blob, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(blob), `"surfaces":["ui","api"]`) {
		t.Errorf("expected surfaces in JSON; got: %s", blob)
	}

	var decoded Capability
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Surfaces) != 2 || decoded.Surfaces[0] != SurfaceUI {
		t.Errorf("Surfaces not round-tripped: %+v", decoded.Surfaces)
	}
}

func TestCapabilityJSONOmitsEmptySurfaces(t *testing.T) {
	c := Capability{Name: "x", Lifecycle: CapabilityNew}
	blob, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), `"surfaces"`) {
		t.Errorf("expected omitempty to drop surfaces when empty; got: %s", blob)
	}
}
