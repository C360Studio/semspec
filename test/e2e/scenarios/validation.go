package scenarios

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	// maxReviewAttempts is how many times we retry PromotePlan on needs_changes.
	maxReviewAttempts = 3

	// reviewRetryBackoff is the base backoff between review retry attempts.
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

// containsAllCI returns true if text contains all of the keywords (case-insensitive).
func containsAllCI(text string, keywords ...string) bool {
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if !strings.Contains(lower, strings.ToLower(kw)) {
			return false
		}
	}
	return true
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

// --- Task validation helpers ---

// tasksReferenceDir returns true if at least one task references files in the given directory.
// Checks both task.Files list and task.Description for directory mentions.
func tasksReferenceDir(tasks []map[string]any, dirPrefix string) bool {
	lower := strings.ToLower(dirPrefix)
	for _, task := range tasks {
		// Check Files field
		if files, ok := task["files"].([]any); ok {
			for _, f := range files {
				if s, ok := f.(string); ok && strings.Contains(strings.ToLower(s), lower) {
					return true
				}
			}
		}
		// Check Description field
		if desc, ok := task["description"].(string); ok {
			if strings.Contains(strings.ToLower(desc), lower) {
				return true
			}
		}
	}
	return false
}

// tasksCoverBothDirs returns true if tasks collectively reference both directories.
func tasksCoverBothDirs(tasks []map[string]any, dir1, dir2 string) bool {
	return tasksReferenceDir(tasks, dir1) && tasksReferenceDir(tasks, dir2)
}

// tasksHaveType returns true if at least one task has the given type value.
func tasksHaveType(tasks []map[string]any, taskType string) bool {
	lower := strings.ToLower(taskType)
	for _, task := range tasks {
		if t, ok := task["type"].(string); ok {
			if strings.ToLower(t) == lower {
				return true
			}
		}
	}
	return false
}

// tasksHaveKeywordInDescription returns true if at least one task description
// contains any of the given keywords (case-insensitive).
func tasksHaveKeywordInDescription(tasks []map[string]any, keywords ...string) bool {
	for _, task := range tasks {
		if desc, ok := task["description"].(string); ok {
			if containsAnyCI(desc, keywords...) {
				return true
			}
		}
	}
	return false
}

// tasksHaveKeyword returns true if at least one task contains any of the given
// keywords in its description, files list, or acceptance criteria.
// This is broader than tasksHaveKeywordInDescription and should be used for
// SOP compliance checks where the keyword may appear in files or criteria.
func tasksHaveKeyword(tasks []map[string]any, keywords ...string) bool {
	for _, task := range tasks {
		// Check description
		if desc, ok := task["description"].(string); ok {
			if containsAnyCI(desc, keywords...) {
				return true
			}
		}
		// Check files
		if files, ok := task["files"].([]any); ok {
			for _, f := range files {
				if s, ok := f.(string); ok {
					if containsAnyCI(s, keywords...) {
						return true
					}
				}
			}
		}
		// Check acceptance_criteria (Given/When/Then fields)
		if criteria, ok := task["acceptance_criteria"].([]any); ok {
			for _, c := range criteria {
				if cm, ok := c.(map[string]any); ok {
					for _, field := range []string{"given", "when", "then"} {
						if v, ok := cm[field].(string); ok {
							if containsAnyCI(v, keywords...) {
								return true
							}
						}
					}
				}
			}
		}
	}
	return false
}

// tasksAreOrdered returns true if the earliest task referencing dir1 comes before
// the earliest task referencing dir2 based on sequence numbers.
func tasksAreOrdered(tasks []map[string]any, dir1, dir2 string) bool {
	lower1 := strings.ToLower(dir1)
	lower2 := strings.ToLower(dir2)
	var minSeq1, minSeq2 float64
	found1, found2 := false, false

	for _, task := range tasks {
		seq, ok := task["sequence"].(float64) // JSON numbers are float64
		if !ok {
			continue
		}
		taskJSON, _ := json.Marshal(task)
		taskStr := strings.ToLower(string(taskJSON))
		if strings.Contains(taskStr, lower1) && (!found1 || seq < minSeq1) {
			minSeq1 = seq
			found1 = true
		}
		if strings.Contains(taskStr, lower2) && (!found2 || seq < minSeq2) {
			minSeq2 = seq
			found2 = true
		}
	}

	if !found1 || !found2 {
		return false // Can't verify ordering if either dir not found
	}
	return minSeq1 <= minSeq2
}

// tasksReferenceExistingFiles checks that at least minMatches tasks reference files
// from the known file set.
func tasksReferenceExistingFiles(tasks []map[string]any, knownFiles []string, minMatches int) bool {
	matches := 0
	knownSet := make(map[string]bool, len(knownFiles))
	for _, f := range knownFiles {
		knownSet[strings.ToLower(f)] = true
	}

	for _, task := range tasks {
		files, ok := task["files"].([]any)
		if !ok {
			continue
		}
		for _, f := range files {
			if s, ok := f.(string); ok {
				if knownSet[strings.ToLower(s)] {
					matches++
					break // Count each task at most once
				}
			}
		}
	}
	return matches >= minMatches
}
