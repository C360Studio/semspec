// Package observability — tool-recovery predicates.
//
// This file registers RDF predicates for tool-call recovery telemetry.
// When a tool execution returns a recoverable error (e.g. graph_query
// "not found"), the executor can compute candidates and inject a
// RETRY HINT into the result that the agent sees on the next turn.
// Each fire is loud: per-fire prometheus counter + a tool.recovery.incident
// triple set on the SKG so operators can query
// (role, model, tool_name, outcome) recovery rates.
//
// Symmetric to ADR-035's named-quirks list (workflow/jsonutil) and
// the parse.incident emission path (workflow/parseincident), but for
// TOOL-CALL outcomes rather than parse-checkpoint outcomes — that's
// why the namespace is `tool.recovery.*` rather than reusing
// `llm.parse.*`. Operators query each independently; the partition
// keys (role, model) are duplicated under each namespace by design.
package observability

import "github.com/c360studio/semstreams/vocabulary"

// ToolRecoveryNamespace is the base IRI prefix for tool-recovery terms.
const ToolRecoveryNamespace = "https://semspec.dev/ontology/tool-recovery/"

// Tool-recovery predicates. The recovery only fires when a tool's
// natural error path is augmented with a directive hint; happy-path
// successes do not generate recovery triples (no observability value).
const (
	// ToolRecoveryOutcome is the recovery outcome at the tool boundary.
	// Values: "suggested" (we provided directive hints in the error)
	//         "not_suggested" (recovery attempted but no candidates available)
	ToolRecoveryOutcome = "tool.recovery.outcome"

	// ToolRecoveryToolName names the tool that triggered the recovery
	// (e.g. "graph_query"). Future-proofs for if other tools grow
	// recovery-hint paths.
	ToolRecoveryToolName = "tool.recovery.tool_name"

	// ToolRecoveryOriginalQuery retains the query/argument that failed
	// the original lookup. For audit and replay; same role as
	// llm.parse.raw_response on the parse-incident side.
	ToolRecoveryOriginalQuery = "tool.recovery.original_query"

	// ToolRecoveryCandidate is multi-value — one triple per candidate
	// the recovery suggested back to the agent. RDF-correct multi-value
	// (one predicate per candidate, NOT a concatenated string) so
	// per-candidate detectors can slice without parsing.
	ToolRecoveryCandidate = "tool.recovery.candidate"

	// ToolRecoveryIncident is a relation predicate from a call entity
	// to a tool.recovery.incident node. Symmetric to
	// llm.parse.incident — points off to the per-fire detail node.
	ToolRecoveryIncident = "tool.recovery.incident"

	// Partition keys. Same shape as llm.parse.{Role, Model} but under
	// a separate namespace — operators query parse vs recovery rates
	// independently rather than through cross-namespace joins.
	ToolRecoveryRole  = "tool.recovery.role"
	ToolRecoveryModel = "tool.recovery.model"
)

// Outcome values for ToolRecoveryOutcome.
const (
	// ToolRecoveryOutcomeSuggested means the executor produced one or
	// more directive candidate hints in the error returned to the
	// agent. The agent's next turn sees the RETRY HINT; the candidate
	// triples on the SKG record exactly which IDs we suggested.
	ToolRecoveryOutcomeSuggested = "suggested"

	// ToolRecoveryOutcomeNotSuggested means the recovery branch ran
	// but couldn't produce candidates (prefix lookup empty, prefix
	// itself invalid, or recursive call short-circuited). The agent
	// sees the original error unchanged; the triple records that we
	// TRIED to recover and failed — important for tuning the prefix-
	// truncation heuristic over time.
	ToolRecoveryOutcomeNotSuggested = "not_suggested"
)

func init() {
	vocabulary.Register(ToolRecoveryOutcome,
		vocabulary.WithDescription("Tool-call recovery outcome: suggested (directive hint injected) or not_suggested (no candidates)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(ToolRecoveryNamespace+"outcome"))

	vocabulary.Register(ToolRecoveryToolName,
		vocabulary.WithDescription("Tool that triggered the recovery (e.g. graph_query)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(ToolRecoveryNamespace+"toolName"))

	vocabulary.Register(ToolRecoveryOriginalQuery,
		vocabulary.WithDescription("The query or argument that failed the original lookup; retained for audit"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(ToolRecoveryNamespace+"originalQuery"))

	vocabulary.Register(ToolRecoveryCandidate,
		vocabulary.WithDescription("A candidate ID the recovery suggested; one triple per candidate (RDF-correct multi-value)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(ToolRecoveryNamespace+"candidate"))

	vocabulary.Register(ToolRecoveryIncident,
		vocabulary.WithDescription("Relation pointing from a call entity to a tool.recovery.incident node"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(ToolRecoveryNamespace+"incident"))

	vocabulary.Register(ToolRecoveryRole,
		vocabulary.WithDescription("Agentic role making the tool call (planner, developer, lesson-decomposer, etc.)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(ToolRecoveryNamespace+"role"))

	vocabulary.Register(ToolRecoveryModel,
		vocabulary.WithDescription("Endpoint identifier from the model registry"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(ToolRecoveryNamespace+"model"))
}
