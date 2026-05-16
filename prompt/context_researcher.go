package prompt

// ResearcherPromptContext carries the fields the researcher user-prompt
// fragment renders. The researcher-manager component constructs one of
// these from the inbound ResearchRequest record (workflow.Research) before
// dispatching the researcher via agentic-dispatch.
//
// Kept on prompt/ to avoid the payloads → prompt → payloads dependency
// cycle that would form if workflow.Research were referenced here. The
// minimal projection (question + sources + research_id) is everything the
// renderer needs.
type ResearcherPromptContext struct {
	// ResearchID is the RESEARCH KV record key. The researcher passes it
	// verbatim back via answer_research so the manager can route the
	// answer back to the asking dev's loop.
	ResearchID string

	// Question is the specific upstream-API-surface question to answer.
	// Set by the developer's research() tool call.
	Question string

	// Sources are the dev's starting-point hints (canonical URLs, maven
	// coordinates, repo refs). The researcher MAY consult other sources
	// but begins here.
	Sources []string

	// AskingPlanSlug + AskingTaskID give the researcher context about
	// what the dev is working on. Optional but useful when the researcher
	// has to judge "is this question relevant to the dev's actual goal?".
	AskingPlanSlug string
	AskingTaskID   string
}
