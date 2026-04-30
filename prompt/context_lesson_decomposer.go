package prompt

// LessonDecomposerPromptContext carries everything the lesson-decomposer
// user-prompt fragment needs to render. Populated by
// processor/lesson-decomposer at dispatch time (ADR-033 Phase 2b).
//
// The decomposer is a one-shot LLM that turns a single rejection signal
// into an evidence-cited Lesson. Its prompt input is small relative to the
// developer/reviewer prompts — a trajectory summary plus the verdict and
// scenario AC are usually enough to identify the failure pattern.
type LessonDecomposerPromptContext struct {
	// Verdict is the reviewer verdict that triggered this run.
	// "rejected" or "needs_changes" yield a negative lesson; "approved"
	// (Phase 6) yields a positive "best practice" lesson.
	Verdict string

	// Positive marks the run as a positive-lesson dispatch (Phase 6).
	// When true, the renderer frames the prompt as "First-try success
	// — what worked" rather than "Incident — what failed", and the
	// decomposer's output is recorded with workflow.Lesson.Positive=true
	// so the team-knowledge fragment renders [BEST PRACTICE] instead
	// of [AVOID]. Producers normally derive this from Verdict, but the
	// explicit field lets the dispatcher override (e.g. retroactive
	// positive lessons from past approvals once ratings infra exists).
	Positive bool

	// Feedback is the reviewer's free-text feedback. The decomposer reads
	// this to align its narrative with what the reviewer actually said,
	// but should not parrot it — the lesson must explain WHY the failure
	// happened, not just what the reviewer noted.
	Feedback string

	// Source identifies the producer site for routing/metrics: one of
	// "execution-manager" (Phase 2), "plan-reviewer", or "qa-reviewer"
	// (Phase 3). Renderer uses this to tailor the framing line.
	Source string

	// TargetRole is the role the lesson should attach to (where the
	// failure surfaced) — typically "developer" for code-review
	// rejections. Becomes Lesson.Role.
	TargetRole string

	// DeveloperLoopID is the agentic-loop ID for the developer dispatch
	// that produced the rejected code. Required for evidence_steps to
	// cite specific trajectory entries.
	DeveloperLoopID string

	// ReviewerLoopID is the reviewer's loop ID. Optional — included so
	// the decomposer can also cite reviewer trajectory steps when the
	// reviewer's reasoning is itself the evidence (e.g., reviewer flagged
	// a specific tool call by index).
	ReviewerLoopID string

	// DeveloperSteps are pre-summarised one-line renderings of each step
	// in the developer's trajectory (via summarizeStep). The decomposer
	// references these by step_index when filling evidence_steps. Empty
	// slice when the trajectory was unavailable.
	DeveloperSteps []TrajectoryStepSummary

	// ReviewerSteps are pre-summarised steps from the reviewer's
	// trajectory. Optional — included only when the reviewer's chain of
	// reasoning is small enough to fit the prompt budget.
	ReviewerSteps []TrajectoryStepSummary

	// Scenario carries the BDD acceptance criteria the rejected work
	// failed against. Empty Given/When/Then when the rejection wasn't
	// scenario-scoped (rare).
	Scenario *DecomposerScenarioContext

	// FilesModified is the list of files the developer's submit_work
	// claimed. Used by the decomposer to cite specific paths in
	// evidence_files. Empty when the developer never submitted.
	FilesModified []string

	// WorktreeDiffSummary is a short textual digest of the worktree's
	// final state — typically "git status" output or a one-line per-file
	// summary. Optional; the prompt skips the diff section when empty.
	// Phase 2b commit 3 wires this in; commit 2 ships the field with
	// no producer.
	WorktreeDiffSummary string

	// CommitSHA is the commit (or branch tip) SHA the developer's
	// changes land on. Used to populate FileRef.CommitSHA in the
	// emitted lesson. Empty when unknown.
	CommitSHA string

	// ExistingLessons are the role-scoped lessons already in the graph,
	// pre-rendered as one-line summaries. The decomposer reads these to
	// avoid filing a duplicate — when a similar lesson already exists,
	// it should choose categories that match the existing taxonomy
	// rather than inventing new ones.
	ExistingLessons []string

	// CategoryCatalog is the prompt-renderable view of the project's
	// error categories — typically `id: label` lines from
	// configs/error_categories.json. The decomposer picks category_ids
	// from this list rather than inventing new strings.
	CategoryCatalog []string
}

// TrajectoryStepSummary is a prompt-safe one-line rendering of a single
// trajectory step, paired with the index the decomposer cites in
// evidence_steps. Produced by summarizeStep in the lesson-decomposer
// component before the prompt is built.
type TrajectoryStepSummary struct {
	Index   int
	Summary string
}

// DecomposerScenarioContext is the slice of scenario data the decomposer
// renders into its prompt. Lives here (not on workflow.Scenario) so the
// prompt context can stay decoupled from KV-shape evolution in workflow.
type DecomposerScenarioContext struct {
	ID    string
	Given string
	When  string
	Then  []string
}
