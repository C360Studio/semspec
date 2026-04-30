package lessoncurator

import (
	"time"

	"github.com/c360studio/semspec/workflow"
)

// retirementCriteria captures the parameters for the retirement decision.
// Pure data — no side effects. Constructed once per sweep from Config.
type retirementCriteria struct {
	now                time.Time
	idleThreshold      time.Duration
	minAgeBeforeRetire time.Duration
}

// shouldRetire evaluates the Phase 5a retirement criterion against a
// single lesson:
//
//   - If the lesson is already retired → no.
//   - If the lesson is younger than minAgeBeforeRetire → no (grace period).
//   - If LastInjectedAt is nil and the lesson is older than idleThreshold
//     → yes (was never injected and the grace period has lapsed).
//   - If LastInjectedAt is older than idleThreshold → yes.
//   - Otherwise → no.
//
// Returns (decision, reason). The reason is a short label suitable for
// logs and lesson-retired-event metadata; empty when decision is false.
func (rc retirementCriteria) shouldRetire(l workflow.Lesson) (bool, string) {
	if l.RetiredAt != nil {
		return false, ""
	}

	if !l.CreatedAt.IsZero() {
		age := rc.now.Sub(l.CreatedAt)
		if age < rc.minAgeBeforeRetire {
			return false, ""
		}
	}

	if l.LastInjectedAt == nil {
		// Never injected and out of grace period. The lesson exists but
		// nothing has selected it through Phase 4b rotation — it's
		// either stuck behind too many higher-priority lessons or its
		// role has no producer reading its category. Either way it's
		// dead weight.
		return true, "never_injected_past_grace"
	}

	idle := rc.now.Sub(*l.LastInjectedAt)
	if idle >= rc.idleThreshold {
		return true, "idle_past_threshold"
	}

	return false, ""
}
