// Package observability registers RDF predicate constants for LLM-output
// parse-incident telemetry. See ADR-035 and doc.go for context.
package observability

import "github.com/c360studio/semstreams/vocabulary"

// Namespace is the base IRI prefix for observability vocabulary terms.
const Namespace = "https://semspec.dev/ontology/observability/"

// Checkpoint identifies which gate fired. Step 1 of ADR-035 sequencing
// defines two checkpoints; later steps may add more (e.g. retry-classification).
// Values are listed in the CheckpointResponseParse / CheckpointToolCall
// constants below.
const (
	// Checkpoint is the parse checkpoint that emitted this triple.
	// Values: "response_parse" (CP-1), "tool_call" (CP-2)
	Checkpoint = "llm.parse.checkpoint"

	// Outcome is the strict-parse outcome at this checkpoint.
	// Values: "strict", "tolerated_quirk", "rejected"
	Outcome = "llm.parse.outcome"

	// Incident is a relation predicate pointing from the call entity to a
	// parse-incident node. Used when callers prefer to attach incident
	// detail to a separate node rather than inline triples (e.g. when the
	// raw response is large or has structured fields worth indexing).
	Incident = "llm.parse.incident"

	// RawResponse is the unmodified LLM response string that triggered the
	// incident. Always retained on outcome="rejected" or "tolerated_quirk"
	// so a regression suite can replay against future model/prompt
	// changes without losing the original failure shape.
	RawResponse = "llm.parse.raw_response"

	// Reason is a human-readable description of why the checkpoint
	// rejected (or tolerated) the response. For rejections, this is the
	// same string injected as RETRY HINT into the next loop iteration.
	Reason = "llm.parse.reason"

	// Quirk identifies which named output-quirk allowlist entry handled
	// this response, when Outcome="tolerated_quirk". Empty otherwise.
	// Examples: "chain_of_thought_prefix", "fenced_json_wrapper".
	// Multiple quirk triples may be attached to a single incident node
	// when more than one quirk fired during the parse — one quirk per
	// triple (RDF-correct multi-value), so detectors can slice by
	// individual quirk ID without parsing concatenated values.
	Quirk = "llm.parse.quirk"

	// RawResponseTruncated is a boolean indicating that the raw_response
	// triple stored on the incident node was truncated at the per-call
	// size cap (default 4 KiB). The cap exists because wedged loops can
	// emit 50 KiB+ chain-of-thought blocks and storing the full
	// response in a triple is heavier than the audit value justifies.
	// True when the original response exceeded the cap; false (or
	// absent) otherwise. Read RawResponse alongside this predicate to
	// know whether you're seeing the full or truncated text.
	RawResponseTruncated = "llm.parse.raw_response_truncated"
)

// Partition keys. Aggregating incidents by (Role, Model, PromptVersion)
// is what makes the IncidentRateExceeded detector (ADR-035 step 5)
// possible — and is the same granularity at which fixes get scoped.
const (
	// Role is the agentic role making the LLM call (planner,
	// plan-reviewer, developer, code-reviewer, qa-reviewer, etc.).
	Role = "llm.parse.role"

	// Model is the endpoint identifier as configured in the registry,
	// not the underlying provider model ID. e.g. "openrouter-qwen3-moe",
	// "claude-sonnet". This level of granularity catches per-endpoint
	// regressions (a routing change, a config flip) that the underlying
	// provider model name would miss.
	Model = "llm.parse.model"

	// PromptVersion is the assembled prompt's version identifier,
	// derived from the prompt-pack revision plus per-role fragment hash.
	// Pinning incidents to prompt version is the only way to attribute
	// degradation to a specific prompt edit when correlation across
	// (role, model) doesn't isolate the cause.
	PromptVersion = "llm.parse.prompt_version"
)

// CheckpointResponseParse / CheckpointToolCall are the values that the
// Checkpoint predicate may hold. Use these constants instead of literal
// strings so a future rename or split (e.g. introducing CP-3 for
// retry-classification) is a compile error at every call site, not a
// silent string mismatch.
const (
	// CheckpointResponseParse is CP-1: the LLM-response-to-typed-value
	// step inside the agentic loop. Owned by semstreams; the triple is
	// emitted by whatever wrapper turns the model response into a typed
	// tool-call invocation.
	CheckpointResponseParse = "response_parse"

	// CheckpointToolCall is CP-2: the typed-value-to-tool-execution step
	// inside per-tool executors. Owned by semspec; the triple is emitted
	// by the tool executor's argument-validation step before dispatch.
	CheckpointToolCall = "tool_call"
)

// OutcomeStrict / OutcomeToleratedQuirk / OutcomeRejected are the values
// that the Outcome predicate may hold. As with the checkpoint constants,
// use these instead of literal strings.
const (
	// OutcomeStrict means the response parsed (or validated) without any
	// compensation. This is the contract; everything else is a deviation
	// from the contract that ADR-035 wants visible.
	OutcomeStrict = "strict"

	// OutcomeToleratedQuirk means the response required handling per a
	// model's named output-quirks allowlist entry. The Quirk predicate
	// names which quirk handler fired. Tolerated quirks are the ONE
	// legitimate non-strict path; they exist because some models have
	// documented output behaviors (chain-of-thought prefix, fenced JSON
	// wrappers) that are part of the model's contract, not a defect.
	OutcomeToleratedQuirk = "tolerated_quirk"

	// OutcomeRejected means the response failed strict parse (or
	// semantic validation), and a RETRY HINT was injected into the next
	// loop iteration. This is the loud failure mode that replaces silent
	// compensation. RawResponse and Reason are populated.
	OutcomeRejected = "rejected"
)

func init() {
	registerCheckpointPredicates()
	registerOutcomePredicates()
	registerPartitionPredicates()
}

func registerCheckpointPredicates() {
	vocabulary.Register(Checkpoint,
		vocabulary.WithDescription("Parse checkpoint that emitted this triple: response_parse (CP-1) or tool_call (CP-2)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"checkpoint"))

	vocabulary.Register(Outcome,
		vocabulary.WithDescription("Strict-parse outcome: strict, tolerated_quirk, or rejected"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"outcome"))

	vocabulary.Register(Incident,
		vocabulary.WithDescription("Relation pointing from a call entity to a parse-incident node"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"incident"))
}

func registerOutcomePredicates() {
	vocabulary.Register(RawResponse,
		vocabulary.WithDescription("Unmodified LLM response string that triggered the incident; retained for regression replay"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"rawResponse"))

	vocabulary.Register(Reason,
		vocabulary.WithDescription("Human-readable rejection or tolerance reason; same string injected as RETRY HINT on rejections"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"reason"))

	vocabulary.Register(Quirk,
		vocabulary.WithDescription("Named output-quirk allowlist entry that handled this response; empty unless outcome=tolerated_quirk"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"quirk"))

	vocabulary.Register(RawResponseTruncated,
		vocabulary.WithDescription("True when the raw_response triple on this incident was truncated at the per-call size cap (4 KiB)"),
		vocabulary.WithDataType("boolean"),
		vocabulary.WithIRI(Namespace+"rawResponseTruncated"))
}

func registerPartitionPredicates() {
	vocabulary.Register(Role,
		vocabulary.WithDescription("Agentic role making the LLM call (planner, plan-reviewer, developer, code-reviewer, qa-reviewer, etc.)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"role"))

	vocabulary.Register(Model,
		vocabulary.WithDescription("Endpoint identifier from the model registry (e.g. openrouter-qwen3-moe, claude-sonnet) — NOT the underlying provider model ID"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"model"))

	vocabulary.Register(PromptVersion,
		vocabulary.WithDescription("Assembled prompt version identifier (prompt-pack revision plus per-role fragment hash)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"promptVersion"))
}
