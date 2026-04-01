package executionmanager

import (
	"context"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/google/uuid"
)

// extractLessons creates role-scoped Lesson entries from reviewer feedback and
// stores them via the agentHelper lesson CRUD. Called after the reviewer
// completes — verdict determines whether to extract rejection lessons or
// positive-pattern insights.
func (c *Component) extractLessons(ctx context.Context, exec *taskExecution, feedback, verdict string) {
	if c.agentHelper == nil {
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

		if err := c.agentHelper.RecordLesson(ctx, lesson); err != nil {
			c.logger.Warn("Failed to record lesson", "role", role, "error", err)
		}

		// Increment per-role pattern counts and check threshold.
		if len(categoryIDs) > 0 {
			if err := c.agentHelper.IncrementRoleLessonCounts(ctx, role, categoryIDs); err != nil {
				c.logger.Warn("Failed to increment lesson counts", "role", role, "error", err)
			}
			c.checkLessonThreshold(ctx, role, categoryIDs)
		}
	}

	// Approval insight: capture positive patterns from approved work.
	if verdict == "approved" && feedback != "" {
		lesson := workflow.Lesson{
			ID:         uuid.New().String(),
			Source:     "approved-pattern",
			ScenarioID: exec.TaskID,
			Summary:    truncateInsight(feedback, 200),
			Role:       role,
			CreatedAt:  time.Now(),
		}
		if err := c.agentHelper.RecordLesson(ctx, lesson); err != nil {
			c.logger.Warn("Failed to record approval lesson", "role", role, "error", err)
		}
	}
}

// checkLessonThreshold checks whether any error category for the given role
// has exceeded the configured threshold. If so, emits a structured log warning.
// NATS notification will be added in a follow-up step.
func (c *Component) checkLessonThreshold(ctx context.Context, role string, _ []string) {
	if c.agentHelper == nil {
		return
	}

	threshold := c.config.LessonThreshold
	if threshold <= 0 {
		threshold = DefaultLessonThreshold
	}

	counts, err := c.agentHelper.GetRoleLessonCounts(ctx, role)
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
// Operates on runes to avoid splitting multi-byte UTF-8 characters.
func truncateInsight(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}
