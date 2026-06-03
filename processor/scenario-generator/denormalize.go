// denormalize.go implements issue #89: when a scenario binds one or more
// harness profiles via HarnessProfileIDs, this denormalizer merges each
// bound profile's Env + RequiredAssertions onto the scenario itself so
// downstream consumers (story-preparer, dev prompt synthesis, qa.yml
// rendering, structural-validator) read this data directly without
// re-resolving the catalog. Pre-fix the data lived only in the catalog
// and downstream consumers had to know to look there — leading to drift
// (story-preparer dropped the binding details out of dev prompts entirely,
// confirmed by smoke 9 forensics 2026-06-02).
//
// Multi-binding semantics: when a scenario references multiple
// HarnessProfileIDs (rare but allowed), Env maps are merged in profile-ID
// declaration order. Duplicate keys with the SAME value pass through;
// duplicate keys with DIFFERENT values surface as an error so the
// generator can retry with a clarified scenario. RequiredAssertions are
// concatenated in declaration order with no dedup — multi-profile
// scenarios genuinely need both assertion sets.
//
// Unknown profile IDs are tolerated silently rather than erroring: the
// existing plan-reviewer rule scenario.harness_id_unresolved (ADR-041
// Move 4) is the right gate for catalog-resolution errors, and we don't
// want this denormalizer to double-fail on the same condition with a
// worse diagnostic.

package scenariogenerator

import (
	"errors"
	"fmt"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

// ErrHarnessEnvConflict is returned when two harness profiles bound to the
// same scenario declare the same env key with different values. The
// scenario cannot resolve to a single value at qa-render time, so the
// generator-time error forces Bob to retry with a clarified binding.
// Wrapped via %w so retry policy in dispatchretry can classify on
// errors.Is and distinguish "model picked incompatible profiles" from
// transient parse failures.
var ErrHarnessEnvConflict = errors.New("harness profile env conflict")

// denormalizeHarnessProfileData populates scenario.Env and
// scenario.RequiredAssertions from the catalog for every profile in
// scenario.HarnessProfileIDs. Returns an error only on env-key conflict
// across multi-binding profiles (different values for the same key —
// ambiguous resolution at qa-render time).
//
// nil catalog or empty HarnessProfileIDs → no-op (scenario unchanged).
// Unknown profile IDs → silently skipped; plan-reviewer's
// scenario.harness_id_unresolved rule is the correct gate.
func denormalizeHarnessProfileData(s *workflow.Scenario, catalog *harnesscatalog.Catalog) error {
	if s == nil || catalog == nil || len(s.HarnessProfileIDs) == 0 {
		return nil
	}

	for _, profileID := range s.HarnessProfileIDs {
		profile, ok := catalog.Profiles[profileID]
		if !ok {
			// Catalog-resolution failures belong to plan-reviewer rule
			// scenario.harness_id_unresolved (ADR-041 Move 4). Skipping
			// here avoids double-failure with a worse diagnostic.
			continue
		}

		for k, v := range profile.Env {
			if s.Env == nil {
				s.Env = make(map[string]string, len(profile.Env))
			}
			existing, present := s.Env[k]
			if present && existing != v {
				return fmt.Errorf("%w on key %q: scenario %q already has %q from a prior profile, profile %q declares %q — multi-profile scenarios cannot resolve to a single value",
					ErrHarnessEnvConflict, k, s.ID, existing, profileID, v)
			}
			s.Env[k] = v
		}

		// Append assertions but dedup byte-identical entries — two profiles
		// in the same domain (e.g. mavlink.px4-sitl.* and
		// mavlink.ardupilot-sitl.*) commonly share the same heartbeat-
		// observation phrasing; duplicating it in the dev prompt is noise.
		// Different wording surfaces as different entries — only exact
		// matches collapse.
		for _, a := range profile.RequiredAssertions {
			if !containsString(s.RequiredAssertions, a) {
				s.RequiredAssertions = append(s.RequiredAssertions, a)
			}
		}
	}

	return nil
}

// containsString returns true when `needle` is byte-identical to any entry
// in `haystack`. Used by RequiredAssertions dedup; O(n) scan is fine since
// per-scenario assertion counts are bounded by catalog profile counts (~5).
func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
