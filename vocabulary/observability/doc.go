// Package observability provides RDF predicate constants for LLM-output
// parse-incident telemetry, per ADR-035 (strict-parse discipline — no silent
// compensation in LLM output handling).
//
// # Why this vocabulary exists
//
// LLM output handling has two checkpoints in our system:
//
//   - CP-1 (response_parse): does the LLM output parse to the declared
//     schema? Lives in agentic-loop's response-parse path.
//   - CP-2 (tool_call): does the typed value pass semantic validation
//     against the tool's contract? Lives in tools/<tool>/executor.go.
//
// Both checkpoints reject loudly when they fail, emit RETRY HINT into the
// next loop iteration, and log an incident triple using these predicates.
// Tolerance is permitted only via per-(model, characterized-quirk) allowlist
// in endpoint config; that path emits an outcome of "tolerated_quirk".
//
// # Why graph-native (not metrics-only)
//
// Per ADR-035 constraint #3, parse-incident triples are first-class
// governance signal that downstream agents can reason over via graph_query.
// qa-reviewer (Murat) querying "show me planner incidents from the last 7
// days where checkpoint=tool_call" gets a real answer because incidents
// live in the same graph as everything else, not in a separate metrics
// store.
//
// # Predicate shape
//
// All predicates follow the standard semstreams domain.category.property
// format (exactly 2 dots). The domain is "llm" and the category is "parse"
// because incidents are scoped to LLM output parsing — distinct from
// retry-classification or rate-limit observability that may follow later
// under different categories.
//
// # Usage
//
// Import the package to trigger init-time registration, then use the
// predicate constants when writing triples:
//
//	import obs "github.com/c360studio/semspec/vocabulary/observability"
//
//	// On every LLM call:
//	tw.WriteTriple(callEntityID, obs.Checkpoint, obs.CheckpointResponseParse)
//	tw.WriteTriple(callEntityID, obs.Outcome, obs.OutcomeStrict)
//
//	// On parse failure:
//	tw.WriteTriple(callEntityID, obs.Outcome, obs.OutcomeRejected)
//	tw.WriteTriple(callEntityID, obs.Reason, "missing required field 'verdict'")
//	tw.WriteTriple(callEntityID, obs.RawResponse, raw)
//	tw.WriteTriple(callEntityID, obs.Role, "plan-reviewer")
//	tw.WriteTriple(callEntityID, obs.Model, "openrouter-qwen3-moe")
//	tw.WriteTriple(callEntityID, obs.PromptVersion, "v3.2")
//
// # Cross-references
//
//   - ADR-035: docs/adr/ADR-035-strict-parse-no-silent-compensation.md
//   - ADR-034 detector library (where IncidentRateExceeded will live):
//     pkg/health/detector_*.go
package observability
