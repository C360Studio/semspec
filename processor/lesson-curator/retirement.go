package lessoncurator

import (
	"time"

	"github.com/c360studio/semspec/workflow"
)

// retirementCriteria captures the parameters for the retirement decision.
// Mostly pure data; fileExists and rewriteCheck are the optional
// side-effect plug-ins for Phase 5b's "evidence files all missing" check
// and Phase 5c's "cited region rewritten" check respectively. Tests pass
// in-memory stubs; the component wires real os.Stat and git-blame
// backends. nil disables the corresponding criterion.
type retirementCriteria struct {
	now                time.Time
	idleThreshold      time.Duration
	minAgeBeforeRetire time.Duration
	fileExists         func(path string) bool
	rewriteCheck       func(path string, lineStart, lineEnd int, commitSHA string) (rewritten bool, err error)
}

// shouldRetire evaluates the retirement criteria against a single lesson:
//
//   - If the lesson is already retired → no.
//   - If the lesson is younger than minAgeBeforeRetire → no (grace period).
//   - If the lesson cites EvidenceFiles, ALL of them are missing from
//     disk, and fileExists is wired → yes ("evidence_files_missing",
//     Phase 5b). A lesson with at least one surviving cited path is
//     kept — partial-evidence is still verifiable.
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

	if rc.fileExists != nil && len(l.EvidenceFiles) > 0 {
		anyExists := false
		for _, f := range l.EvidenceFiles {
			if f.Path == "" {
				continue
			}
			if rc.fileExists(f.Path) {
				anyExists = true
				break
			}
		}
		if !anyExists {
			return true, "evidence_files_missing"
		}
	}

	// Phase 5c: file still exists, but the cited region may have been
	// rewritten. We retire only when EVERY cited region with a CommitSHA
	// + line range comes back as fully rewritten — partial survival
	// keeps the lesson because at least one citation is still anchored
	// to its original commit. Entries without a CommitSHA or line range
	// are skipped (whole-file citations are too coarse for this signal).
	if rc.rewriteCheck != nil && len(l.EvidenceFiles) > 0 {
		var checkable, rewrittenCount int
		for _, f := range l.EvidenceFiles {
			if f.Path == "" || f.CommitSHA == "" {
				continue
			}
			if f.LineStart <= 0 || f.LineEnd < f.LineStart {
				continue
			}
			checkable++
			rewritten, err := rc.rewriteCheck(f.Path, f.LineStart, f.LineEnd, f.CommitSHA)
			if err != nil {
				// Treat the entry as inconclusive — same as "still
				// anchored" — so a transient git error never produces
				// a retirement.
				continue
			}
			if rewritten {
				rewrittenCount++
			}
		}
		if checkable > 0 && rewrittenCount == checkable {
			return true, "evidence_regions_rewritten"
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
