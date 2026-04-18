package scenarios

import (
	"fmt"
	"strings"
	"time"
)

const (
	// maxReviewAttempts is the maximum number of plan review cycles before giving up.
	maxReviewAttempts = 3

	// reviewRetryBackoff is the base backoff between review polling attempts.
	reviewRetryBackoff = 5 * time.Second
)

// SemanticCheck represents a named semantic validation with pass/fail result.
type SemanticCheck struct {
	Name   string
	Passed bool
	Detail string
}

// SemanticReport collects checks and produces a summary.
type SemanticReport struct {
	Checks []SemanticCheck
	Errors []string
}

// Add records a check result. Returns the report for chaining.
func (r *SemanticReport) Add(name string, passed bool, detail string) *SemanticReport {
	r.Checks = append(r.Checks, SemanticCheck{Name: name, Passed: passed, Detail: detail})
	if !passed {
		r.Errors = append(r.Errors, fmt.Sprintf("%s: %s", name, detail))
	}
	return r
}

// HasFailures returns true if any check failed.
func (r *SemanticReport) HasFailures() bool {
	return len(r.Errors) > 0
}

// Error returns a combined error message from all failures.
func (r *SemanticReport) Error() string {
	return strings.Join(r.Errors, "; ")
}

// PassRate returns the fraction of checks that passed.
func (r *SemanticReport) PassRate() float64 {
	if len(r.Checks) == 0 {
		return 0
	}
	passed := 0
	for _, c := range r.Checks {
		if c.Passed {
			passed++
		}
	}
	return float64(passed) / float64(len(r.Checks))
}

// --- Low-level string helpers ---

// containsAnyCI returns true if text contains any of the keywords (case-insensitive).
func containsAnyCI(text string, keywords ...string) bool {
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// --- Plan-level validation helpers ---

// planReferencesDir checks if the plan references a directory across multiple fields.
// LLMs often hallucinate scope file paths, so we check goal, context, and scope
// to get a more reliable signal about directory awareness.
func planReferencesDir(plan map[string]any, dirPrefix string) bool {
	lower := strings.ToLower(dirPrefix)

	// Check goal text (most reliable — LLMs often tag sections with [api], [ui])
	if goal, ok := plan["goal"].(string); ok {
		if strings.Contains(strings.ToLower(goal), lower) {
			return true
		}
	}

	// Check context text
	if ctx, ok := plan["context"].(string); ok {
		if strings.Contains(strings.ToLower(ctx), lower) {
			return true
		}
	}

	// Check scope include list
	if scope, ok := plan["scope"].(map[string]any); ok {
		if scopeIncludesDir(scope, dirPrefix) {
			return true
		}
	}

	return false
}

// scopeIncludesDir checks if the plan scope include list references a directory prefix.
func scopeIncludesDir(scope map[string]any, dirPrefix string) bool {
	includes, ok := scope["include"].([]any)
	if !ok {
		return false
	}
	lower := strings.ToLower(dirPrefix)
	for _, inc := range includes {
		if s, ok := inc.(string); ok {
			if strings.Contains(strings.ToLower(s), lower) {
				return true
			}
		}
	}
	return false
}

// scopeHallucinationRate returns the fraction of scope.include entries that are NOT
// present in the actual project files. A high rate means the LLM invented file paths.
func scopeHallucinationRate(scope map[string]any, knownFiles []string) float64 {
	includes, ok := scope["include"].([]any)
	if !ok || len(includes) == 0 {
		return 0
	}

	knownSet := make(map[string]bool, len(knownFiles))
	for _, f := range knownFiles {
		knownSet[strings.ToLower(f)] = true
	}

	hallucinated := 0
	for _, inc := range includes {
		if s, ok := inc.(string); ok {
			if !knownSet[strings.ToLower(s)] {
				hallucinated++
			}
		}
	}
	return float64(hallucinated) / float64(len(includes))
}
