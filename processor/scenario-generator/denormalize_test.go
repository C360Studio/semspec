package scenariogenerator

import (
	"errors"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

// denormCatalog returns a minimal catalog with profiles shaped to exercise
// the denormalizer's paths. Reuses the package-level fakeCatalog helper
// (classifier_test.go:12) which accepts varargs and builds the indexed
// map. Profiles below mirror the YAML shape from
// workflow/harnesscatalog/catalog/mavlink.yaml — values are hand-curated
// to keep the test self-contained and exercise multi-binding conflict.
func denormCatalog() *harnesscatalog.Catalog {
	return fakeCatalog(
		harnesscatalog.Profile{
			ID:  "mavlink.px4-sitl.mavsdk-smoke",
			Env: map[string]string{"PX4_SIM_MODEL": "iris"},
			RequiredAssertions: []string{
				"Observe a MAVLink heartbeat from the SITL target.",
				"Assert MAVSDK reports a connected vehicle before plugin calls run.",
			},
		},
		harnesscatalog.Profile{
			ID:                 "mavlink.raw-mavlink-direct",
			Env:                map[string]string{"MAVLINK_PORT": "14550"},
			RequiredAssertions: []string{"Send and receive raw MAVLink message frames."},
		},
		harnesscatalog.Profile{
			// Same Env key as the smoke profile but with a different value.
			// Used to exercise the multi-binding conflict path.
			ID:  "mavlink.px4-sitl.alt-env",
			Env: map[string]string{"PX4_SIM_MODEL": "standard_vtol"},
		},
	)
}

func TestDenormalizeHarnessProfileData(t *testing.T) {
	t.Run("nil scenario is no-op", func(t *testing.T) {
		if err := denormalizeHarnessProfileData(nil, denormCatalog()); err != nil {
			t.Errorf("nil scenario should not error: %v", err)
		}
	})

	t.Run("nil catalog is no-op", func(t *testing.T) {
		s := &workflow.Scenario{
			ID:                "scen.1",
			HarnessProfileIDs: []string{"mavlink.px4-sitl.mavsdk-smoke"},
		}
		if err := denormalizeHarnessProfileData(s, nil); err != nil {
			t.Errorf("nil catalog should not error: %v", err)
		}
		if len(s.Env) != 0 || len(s.RequiredAssertions) != 0 {
			t.Errorf("nil catalog should leave scenario unchanged; got env=%v assertions=%v",
				s.Env, s.RequiredAssertions)
		}
	})

	t.Run("empty HarnessProfileIDs is no-op", func(t *testing.T) {
		s := &workflow.Scenario{ID: "scen.1"}
		if err := denormalizeHarnessProfileData(s, denormCatalog()); err != nil {
			t.Errorf("empty profile list should not error: %v", err)
		}
		if len(s.Env) != 0 || len(s.RequiredAssertions) != 0 {
			t.Errorf("empty profile list should leave scenario unchanged; got env=%v assertions=%v",
				s.Env, s.RequiredAssertions)
		}
	})

	t.Run("single profile populates env and assertions", func(t *testing.T) {
		s := &workflow.Scenario{
			ID:                "scen.1",
			HarnessProfileIDs: []string{"mavlink.px4-sitl.mavsdk-smoke"},
		}
		if err := denormalizeHarnessProfileData(s, denormCatalog()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.Env["PX4_SIM_MODEL"] != "iris" {
			t.Errorf("env.PX4_SIM_MODEL = %q, want iris", s.Env["PX4_SIM_MODEL"])
		}
		if len(s.RequiredAssertions) != 2 {
			t.Errorf("RequiredAssertions len = %d, want 2", len(s.RequiredAssertions))
		}
	})

	t.Run("multi-profile merges env and concatenates assertions", func(t *testing.T) {
		s := &workflow.Scenario{
			ID: "scen.1",
			HarnessProfileIDs: []string{
				"mavlink.px4-sitl.mavsdk-smoke",
				"mavlink.raw-mavlink-direct",
			},
		}
		if err := denormalizeHarnessProfileData(s, denormCatalog()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.Env["PX4_SIM_MODEL"] != "iris" {
			t.Errorf("env.PX4_SIM_MODEL missing from first profile: %v", s.Env)
		}
		if s.Env["MAVLINK_PORT"] != "14550" {
			t.Errorf("env.MAVLINK_PORT missing from second profile: %v", s.Env)
		}
		if len(s.RequiredAssertions) != 3 {
			t.Errorf("RequiredAssertions len = %d, want 3 (2 from first + 1 from second)", len(s.RequiredAssertions))
		}
	})

	t.Run("multi-profile env conflict surfaces as error", func(t *testing.T) {
		s := &workflow.Scenario{
			ID: "scen.1",
			HarnessProfileIDs: []string{
				"mavlink.px4-sitl.mavsdk-smoke", // PX4_SIM_MODEL=iris
				"mavlink.px4-sitl.alt-env",      // PX4_SIM_MODEL=standard_vtol
			},
		}
		err := denormalizeHarnessProfileData(s, denormCatalog())
		if err == nil {
			t.Fatal("expected env conflict error, got nil")
		}
		if !errors.Is(err, ErrHarnessEnvConflict) {
			t.Errorf("error should wrap ErrHarnessEnvConflict sentinel: %v", err)
		}
		if !strings.Contains(err.Error(), "PX4_SIM_MODEL") {
			t.Errorf("error should name the conflicting key: %v", err)
		}
		if !strings.Contains(err.Error(), "iris") || !strings.Contains(err.Error(), "standard_vtol") {
			t.Errorf("error should name both conflicting values: %v", err)
		}
	})

	t.Run("identical assertion strings across profiles dedup", func(t *testing.T) {
		// Two profiles both declare the same assertion phrase verbatim.
		// Denormalizer must NOT duplicate it on the scenario; otherwise
		// Sarah surfaces "Observe a heartbeat" twice in the dev prompt.
		shared := "Observe a MAVLink heartbeat from the SITL target."
		cat := fakeCatalog(
			harnesscatalog.Profile{ID: "p1", RequiredAssertions: []string{shared, "p1-only"}},
			harnesscatalog.Profile{ID: "p2", RequiredAssertions: []string{shared, "p2-only"}},
		)
		s := &workflow.Scenario{
			ID:                "scen.1",
			HarnessProfileIDs: []string{"p1", "p2"},
		}
		if err := denormalizeHarnessProfileData(s, cat); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Expect 3 unique assertions: shared + p1-only + p2-only.
		if len(s.RequiredAssertions) != 3 {
			t.Errorf("RequiredAssertions = %v, want 3 unique (deduped), got %d entries", s.RequiredAssertions, len(s.RequiredAssertions))
		}
		// Confirm `shared` appears exactly once.
		count := 0
		for _, a := range s.RequiredAssertions {
			if a == shared {
				count++
			}
		}
		if count != 1 {
			t.Errorf("shared assertion appears %d times, want exactly 1", count)
		}
	})

	t.Run("unknown profile id is silently skipped", func(t *testing.T) {
		// plan-reviewer rule scenario.harness_id_unresolved owns this
		// failure; denormalizer must not double-fail.
		s := &workflow.Scenario{
			ID: "scen.1",
			HarnessProfileIDs: []string{
				"mavlink.px4-sitl.mavsdk-smoke",
				"profile.does.not.exist",
			},
		}
		if err := denormalizeHarnessProfileData(s, denormCatalog()); err != nil {
			t.Errorf("unknown profile should be silently skipped, got %v", err)
		}
		// Known profile's data should still land.
		if s.Env["PX4_SIM_MODEL"] != "iris" {
			t.Errorf("known profile's env should still populate: %v", s.Env)
		}
	})

	t.Run("profile with empty env and assertions is fine", func(t *testing.T) {
		minimal := &harnesscatalog.Catalog{
			Profiles: map[string]harnesscatalog.Profile{
				"empty.profile": {ID: "empty.profile"},
			},
		}
		s := &workflow.Scenario{
			ID:                "scen.1",
			HarnessProfileIDs: []string{"empty.profile"},
		}
		if err := denormalizeHarnessProfileData(s, minimal); err != nil {
			t.Errorf("empty profile should not error: %v", err)
		}
		if len(s.Env) != 0 || len(s.RequiredAssertions) != 0 {
			t.Errorf("empty profile should leave scenario unchanged: env=%v assertions=%v",
				s.Env, s.RequiredAssertions)
		}
	})

	t.Run("multi-profile same key+value is fine", func(t *testing.T) {
		cat := &harnesscatalog.Catalog{
			Profiles: map[string]harnesscatalog.Profile{
				"p1": {ID: "p1", Env: map[string]string{"COMMON": "value"}},
				"p2": {ID: "p2", Env: map[string]string{"COMMON": "value"}},
			},
		}
		s := &workflow.Scenario{
			ID:                "scen.1",
			HarnessProfileIDs: []string{"p1", "p2"},
		}
		if err := denormalizeHarnessProfileData(s, cat); err != nil {
			t.Errorf("matching env values should not conflict: %v", err)
		}
		if s.Env["COMMON"] != "value" {
			t.Errorf("env value not set: %v", s.Env)
		}
	})
}
