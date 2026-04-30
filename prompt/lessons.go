package prompt

// LessonsLearned carries role-scoped lesson data for prompt injection.
type LessonsLearned struct {
	// Lessons accumulated for the current role.
	Lessons []LessonEntry
}

// LessonEntry is a single lesson from the role's lesson store.
type LessonEntry struct {
	// Category is the lesson source (e.g., "reviewer-feedback", "plan-review").
	Category string
	// Summary is a one-line description of the lesson. Used as the prompt
	// rendering when InjectionForm is empty.
	Summary string
	// InjectionForm is the decomposer's compressed (≤80 token) rendering of
	// the lesson, framed as concrete advice for the next agent. Preferred
	// over Summary by the team-knowledge prompt fragment when set
	// (ADR-033 Phase 4).
	InjectionForm string
	// Positive marks "best practice" lessons emitted by Phase 6 (approved-
	// on-first-try). When true, the team-knowledge fragment renders the
	// entry with a [BEST PRACTICE] prefix instead of [AVOID].
	Positive bool
	// Role is which role this lesson applies to.
	Role string
	// Guidance is prescriptive remediation text from the error category definition.
	Guidance string
}
