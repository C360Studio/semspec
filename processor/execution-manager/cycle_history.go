package executionmanager

import (
	"fmt"
	"strings"
)

// summarizeCycleHistory renders a structured "PRIOR ATTEMPTS" block from
// exec.PriorCycles. The block is what the developer sees on cycle N>0
// in addition to the most-recent reviewer feedback (exec.Feedback) —
// pattern recognition over the cycle sequence as deterministic prompt
// content.
//
// Empty PriorCycles → "" (cycle 0 has no history; dispatch skips the
// block entirely).
//
// Shape:
//
//	PRIOR ATTEMPTS (N cycles before this one):
//	  Cycle 0: rejected (fixable)
//	    Files changed: a, b
//	    Reviewer said: <feedback excerpt>
//	  Cycle 1: rejected (fixable)
//	    Files changed: a
//	    Reviewer said: <feedback excerpt>
//	  ...
//
//	Pattern across attempts:
//	  - <pattern line if detected>
//
//	Do not repeat any approach above. If the same approach keeps
//	failing for the same reason, change tactics (try a different
//	tool, decompose differently, ask via submit_work).
//
// The pattern lines are deterministic synthesis (no LLM): currently
// detect "all N cycles failed" + "all N cycles touched the same
// files" + "all N cycles had the same reviewer-rejection-type". More
// patterns can be added as we observe real-LLM cycle shapes.
func summarizeCycleHistory(cycles []cycleSnapshot) string {
	if len(cycles) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "PRIOR ATTEMPTS (%d cycle%s before this one):\n",
		len(cycles), pluralS(len(cycles)))

	for _, c := range cycles {
		verdict := c.Verdict
		if c.RejectionType != "" {
			verdict = fmt.Sprintf("%s (%s)", c.Verdict, c.RejectionType)
		}
		fmt.Fprintf(&sb, "  Cycle %d: %s\n", c.Cycle, verdict)
		if len(c.FilesModified) > 0 {
			fmt.Fprintf(&sb, "    Files changed: %s\n", strings.Join(c.FilesModified, ", "))
		}
		if c.Feedback != "" {
			// Indent the feedback so it's visually distinct from the
			// per-cycle metadata. Two-space lead lets the model parse
			// the structure naturally.
			fmt.Fprintf(&sb, "    Reviewer said: %s\n", c.Feedback)
		}
	}

	if patterns := detectPatterns(cycles); len(patterns) > 0 {
		sb.WriteString("\nPattern across attempts:\n")
		for _, p := range patterns {
			fmt.Fprintf(&sb, "  - %s\n", p)
		}
	}

	sb.WriteString("\nDo not repeat any approach above. If the same approach keeps failing for the same reason, change tactics (try a different tool, decompose differently, ask via submit_work).")

	return sb.String()
}

// detectPatterns runs deterministic checks over the cycle history to
// surface "you keep doing X" lines. Returns nil when nothing rises to
// pattern level (e.g. one cycle, or cycles too varied to characterize).
//
// Patterns are intentionally simple — the goal is to surface the
// kinds of insight a manager-role recovery agent would write
// ("3 of 3 cycles failed with the same shell-quoting error class")
// without requiring an LLM call. Adding patterns: keep them
// observation-only ("X happened N times"), not prescriptive ("you
// should do Y") — the prescriptive framing belongs to the recovery
// agent's diagnosis, not the deterministic summarizer.
func detectPatterns(cycles []cycleSnapshot) []string {
	if len(cycles) < 2 {
		return nil
	}
	var patterns []string

	// Pattern 1: all cycles rejected (no approval mixed in).
	allRejected := true
	for _, c := range cycles {
		if c.Verdict != "rejected" {
			allRejected = false
			break
		}
	}
	if allRejected {
		patterns = append(patterns,
			fmt.Sprintf("All %d prior cycles were rejected.", len(cycles)))
	}

	// Pattern 2: every cycle touched exactly the same files. Suggests
	// the dev is editing the same surface repeatedly without changing
	// approach.
	if sameFiles := allCyclesSameFiles(cycles); sameFiles != nil && len(sameFiles) > 0 {
		patterns = append(patterns,
			fmt.Sprintf("All cycles modified the same files: %s", strings.Join(sameFiles, ", ")))
	}

	// Pattern 3: all rejected cycles share the same rejection_type.
	// Distinguishes "you keep producing fixable shape" (apply more
	// detail) from "you keep getting restructure" (your approach is
	// fundamentally wrong; do not produce more code in this shape).
	if rt := commonRejectionType(cycles); rt != "" {
		patterns = append(patterns,
			fmt.Sprintf("All rejections were rejection_type=%s.", rt))
	}

	return patterns
}

// allCyclesSameFiles returns the file list when every cycle modified
// exactly the same set of files (order-insensitive). Returns nil
// otherwise. Empty FilesModified counts as "no files this cycle" and
// disqualifies the pattern (an unrelated cycle can't be part of the
// same-files pattern).
func allCyclesSameFiles(cycles []cycleSnapshot) []string {
	if len(cycles) == 0 || len(cycles[0].FilesModified) == 0 {
		return nil
	}
	want := make(map[string]bool, len(cycles[0].FilesModified))
	for _, f := range cycles[0].FilesModified {
		want[f] = true
	}
	for _, c := range cycles[1:] {
		if len(c.FilesModified) != len(want) {
			return nil
		}
		for _, f := range c.FilesModified {
			if !want[f] {
				return nil
			}
		}
	}
	return cycles[0].FilesModified
}

// commonRejectionType returns the shared rejection_type when every
// rejected cycle has the same one. Approved cycles are skipped (they
// have no rejection_type). Returns "" when cycles are mixed or empty.
func commonRejectionType(cycles []cycleSnapshot) string {
	var common string
	for _, c := range cycles {
		if c.Verdict != "rejected" || c.RejectionType == "" {
			continue
		}
		if common == "" {
			common = c.RejectionType
			continue
		}
		if c.RejectionType != common {
			return ""
		}
	}
	return common
}

// pluralS returns "s" when n != 1, "" otherwise. Tiny English helper
// for the rendered prompt — "1 cycle" vs "3 cycles".
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
