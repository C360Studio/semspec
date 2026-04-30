package executionmanager

import (
	"context"
	"encoding/json"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/message"
	"github.com/google/uuid"
)

// checkRejectionPatterns tallies recurring error patterns when the reviewer
// rejects a task. Triggers a structured "Recurring error pattern detected"
// log when any role-scoped error category exceeds the configured threshold.
//
// ADR-033 Phase 3: replaces the keyword-classifier path that wrote a
// `Source="reviewer-feedback"` Lesson on every rejection. The decomposer
// (Phase 2b, see publishLessonDecomposeRequest) is now the sole producer
// for code-review rejections — its evidence-cited Lesson is written
// asynchronously when the agentic loop completes. Threshold detection
// remains synchronous on the rejection path because it surfaces operational
// signal regardless of when the new lesson lands.
func (c *Component) checkRejectionPatterns(ctx context.Context, _ *taskExecution, feedback, verdict string) {
	if c.lessonWriter == nil || verdict != "rejected" || feedback == "" {
		return
	}

	role := "developer"
	if c.errorCategories == nil {
		// No registry → no category match → threshold check is a graph-only
		// scan that still works, just without classifier-derived hints.
		c.checkLessonThreshold(ctx, role, nil)
		return
	}

	matches := c.errorCategories.MatchSignals(feedback)
	if len(matches) == 0 {
		c.checkLessonThreshold(ctx, role, nil)
		return
	}

	categoryIDs := make([]string, 0, len(matches))
	for _, m := range matches {
		categoryIDs = append(categoryIDs, m.Category.ID)
	}
	c.checkLessonThreshold(ctx, role, categoryIDs)
}

// extractStructuralLessons creates one developer-scoped Lesson per failed
// required CheckResult from a structural validation pass. The substrate
// (toolchain stderr/stdout + check name) is deterministic — exit codes and
// real `pytest`/`go build` output — so the keyword classifier here is
// Goodhart-safe in a way reviewer-feedback classification is not. Source is
// "structural-validation" to distinguish from "reviewer-feedback".
//
// Phase 0.4 of ADR-033's lessons chain: bridges the gap between today's
// in-loop validator feedback (re-dispatched developer message) and tomorrow's
// trajectory-decomposed lessons (Phase 1+).
func (c *Component) extractStructuralLessons(ctx context.Context, exec *taskExecution, checks []payloads.CheckResult) {
	if c.lessonWriter == nil {
		return
	}

	role := "developer"
	lessons := buildStructuralLessons(exec.TaskID, checks, c.errorCategories)
	if len(lessons) == 0 {
		return
	}

	allCategoryIDs := map[string]struct{}{}
	for _, lesson := range lessons {
		if err := c.lessonWriter.RecordLesson(ctx, lesson); err != nil {
			c.logger.Warn("Failed to record structural lesson", "role", role, "error", err)
			continue
		}
		for _, id := range lesson.CategoryIDs {
			allCategoryIDs[id] = struct{}{}
		}
	}

	if len(allCategoryIDs) > 0 {
		ids := make([]string, 0, len(allCategoryIDs))
		for id := range allCategoryIDs {
			ids = append(ids, id)
		}
		if err := c.lessonWriter.IncrementRoleLessonCounts(ctx, role, ids); err != nil {
			c.logger.Warn("Failed to increment lesson counts", "role", role, "error", err)
		}
		c.checkLessonThreshold(ctx, role, ids)
	}
}

// buildStructuralLessons is the pure builder for structural-validation
// lessons. One lesson per failed required check. Match substrate is
// `Name + Stderr + Stdout` truncated to 800 runes (enough for pytest/go-test
// failure summaries without inflating the graph) and the stored Summary is
// truncated to 200 runes. Returns nil for empty input or all-passing/
// non-required failures.
//
// ADR-033 Phase 3: each lesson cites `.semspec/checklist.json` in
// EvidenceFiles. The check name is the natural retirement signal — when the
// project retires or renames a check, the cited definition disappears and
// the retirement sweep can expire the lesson. The line range is
// intentionally empty (whole-file citation) because checklist.json is a
// flat list and per-check line numbers shift with edits.
func buildStructuralLessons(taskID string, checks []payloads.CheckResult, registry *workflow.ErrorCategoryRegistry) []workflow.Lesson {
	var lessons []workflow.Lesson
	now := time.Now()
	checklistRef := workflow.FileRef{Path: ".semspec/checklist.json"}
	for _, ck := range checks {
		if ck.Passed || !ck.Required {
			continue
		}
		matchText := ck.Name + "\n" + ck.Stderr + "\n" + ck.Stdout
		matchText = truncateInsight(matchText, 800)

		var categoryIDs []string
		if registry != nil {
			for _, m := range registry.MatchSignals(matchText) {
				categoryIDs = append(categoryIDs, m.Category.ID)
			}
		}

		summarySrc := ck.Name
		if ck.Stderr != "" {
			summarySrc += ": " + ck.Stderr
		} else if ck.Stdout != "" {
			summarySrc += ": " + ck.Stdout
		}

		lessons = append(lessons, workflow.Lesson{
			ID:            uuid.New().String(),
			Source:        "structural-validation",
			ScenarioID:    taskID,
			Summary:       truncateInsight(summarySrc, 200),
			CategoryIDs:   categoryIDs,
			Role:          "developer",
			CreatedAt:     now,
			EvidenceFiles: []workflow.FileRef{checklistRef},
		})
	}
	return lessons
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

// shouldDispatchPositiveLesson returns true when ADR-033 Phase 6's
// first-try-success path should publish a decompose request. Gates:
//
//   - EnablePositiveLessons must be set (default false because every
//     first-try success becomes a decomposer LLM call).
//   - exec.TDDCycle must be 0 — only the first dev→validate→review cycle
//     counts as "first try".
//   - exec.ReviewRetryCount must be 0 — a parse-retry that succeeded on
//     attempt N is not a first-try success.
//
// Pulled out as a method so the boundary is testable without spying on
// NATS publishes.
func (c *Component) shouldDispatchPositiveLesson(exec *taskExecution) bool {
	if exec == nil || !c.config.EnablePositiveLessons {
		return false
	}
	return exec.TDDCycle == 0 && exec.ReviewRetryCount == 0
}

// publishLessonDecomposeRequest signals the lesson-decomposer that a reviewer
// rejection happened. ADR-033 Phase 2a: this fires alongside extractLessons
// (the keyword classifier still runs); Phase 2b's decomposer LLM produces an
// evidence-cited lesson via lessons.Writer when it consumes this. Phase 3 swaps
// extractLessons for the decomposer entirely.
//
// Best-effort — a publish failure logs but does not block the rejection flow.
// The decomposer is non-load-bearing on the hot path.
func (c *Component) publishLessonDecomposeRequest(ctx context.Context, exec *taskExecution, verdict, feedback, reviewerLoopID string) {
	if c.natsClient == nil {
		return
	}
	req := &payloads.LessonDecomposeRequested{
		Slug:            exec.Slug,
		TaskID:          exec.TaskID,
		RequirementID:   exec.RequirementID,
		ScenarioID:      exec.TaskID,
		LoopID:          exec.LoopID,
		DeveloperLoopID: exec.DeveloperLoopID,
		ReviewerLoopID:  reviewerLoopID,
		Verdict:         verdict,
		Feedback:        feedback,
		Source:          "execution-manager",
	}
	if err := req.Validate(); err != nil {
		c.logger.Warn("Skipping lesson decompose publish — invalid payload",
			"slug", exec.Slug, "task_id", exec.TaskID, "error", err)
		return
	}
	envelope := message.NewBaseMessage(req.Schema(), req, "execution-manager")
	data, err := json.Marshal(envelope)
	if err != nil {
		c.logger.Warn("Failed to marshal lesson decompose request",
			"slug", exec.Slug, "task_id", exec.TaskID, "error", err)
		return
	}
	subject := payloads.LessonDecomposeRequestedSubject(exec.Slug)
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		c.logger.Warn("Failed to publish lesson decompose request",
			"slug", exec.Slug, "task_id", exec.TaskID, "subject", subject, "error", err)
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
