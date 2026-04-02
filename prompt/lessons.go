package prompt

// LessonsLearned carries role-scoped lesson data for prompt injection.
type LessonsLearned struct {
	// Lessons accumulated for the current role.
	Lessons []LessonEntry
}

// LessonEntry is a single lesson from the role's lesson store.
type LessonEntry struct {
	// Category is the lesson source (e.g., "reviewer-feedback", "approved-pattern").
	Category string
	// Summary is a one-line description of the lesson.
	Summary string
	// Role is which role this lesson applies to.
	Role string
	// Guidance is prescriptive remediation text from the error category definition.
	Guidance string
}
