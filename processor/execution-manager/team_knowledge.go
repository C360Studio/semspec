package executionmanager

import (
	"context"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/google/uuid"
)

// extractLessons creates role-scoped Lesson entries from reviewer rejection
// feedback and stores them via the lesson writer (TripleWriter → graph-ingest).
// Called after the reviewer completes — only non-approved verdicts produce lessons.
func (c *Component) extractLessons(ctx context.Context, exec *taskExecution, feedback, verdict string) {
	if c.lessonWriter == nil {
		return
	}

	role := "developer" // default role for execution pipeline

	// Rejection lesson from reviewer feedback.
	if verdict == "rejected" && feedback != "" {
		var categoryIDs []string
		if c.errorCategories != nil {
			matches := c.errorCategories.MatchSignals(feedback)
			for _, m := range matches {
				categoryIDs = append(categoryIDs, m.Category.ID)
			}
		}

		lesson := workflow.Lesson{
			ID:          uuid.New().String(),
			Source:      "reviewer-feedback",
			ScenarioID:  exec.TaskID,
			Summary:     truncateInsight(feedback, 200),
			CategoryIDs: categoryIDs,
			Role:        role,
			CreatedAt:   time.Now(),
		}

		if err := c.lessonWriter.RecordLesson(ctx, lesson); err != nil {
			c.logger.Warn("Failed to record lesson", "role", role, "error", err)
		}

		// Increment per-role pattern counts and check threshold.
		if len(categoryIDs) > 0 {
			if err := c.lessonWriter.IncrementRoleLessonCounts(ctx, role, categoryIDs); err != nil {
				c.logger.Warn("Failed to increment lesson counts", "role", role, "error", err)
			}
			c.checkLessonThreshold(ctx, role, categoryIDs)
		}
	}

}

// checkLessonThreshold checks whether any error category for the given role
// has exceeded the configured threshold. If so, emits a structured log warning.
func (c *Component) checkLessonThreshold(ctx context.Context, role string, _ []string) {
	if c.lessonWriter == nil {
		return
	}

	threshold := c.config.LessonThreshold
	if threshold <= 0 {
		threshold = DefaultLessonThreshold
	}

	counts, err := c.lessonWriter.GetRoleLessonCounts(ctx, role)
	if err != nil {
		return
	}

	if catID, exceeded := counts.ExceedsThreshold(threshold); exceeded {
		label := catID
		if c.errorCategories != nil {
			if catDef, ok := c.errorCategories.Get(catID); ok {
				label = catDef.Label
			}
		}
		c.logger.Warn("Recurring error pattern detected",
			"role", role,
			"category", catID,
			"label", label,
			"threshold", threshold,
		)
	}
}

// truncateInsight truncates s to maxLen runes, appending "..." if truncated.
func truncateInsight(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

// summarizeReviewerParseFailure produces a short, prompt-safe rendering of a
// reviewer agent's raw output when it failed parseCodeReviewResult. Used by
// the parse-retry path to thread "this is what came back, and it didn't
// parse" into the next dispatch's user prompt — closes the blind-retry gap
// for code-reviewer parse failures.
//
// The raw output may be JSON-with-extra-text, malformed JSON, prose, or empty.
// We don't try to be clever — just bound the size so the next prompt isn't
// blown out. 800 runes is plenty for the model to recognize the shape its
// previous output took.
func summarizeReviewerParseFailure(rawResult string) string {
	if rawResult == "" {
		return "Reviewer agent returned an empty response. Emit a verdict object with the required fields."
	}
	return truncateInsight(rawResult, 800)
}
