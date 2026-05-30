// Package specimport implements OpenSpec inbound import (ADR-040 Move 4 /
// folded ADR-038). Imports an `openspec/changes/<name>/` directory as a
// semspec Plan with populated Exploration + Requirements, then lets the
// existing semspec pipeline (plan-reviewer → execution → QA) run against
// it. The PR 3 outbound emitter regenerates the OpenSpec markdown after
// execution, closing the round trip.
//
// Three layers per ADR-038 §"Architectural commitment":
//
//	Layer 1 — structural_check.go (this package, deterministic)
//	  Pre-flight: proposal.md exists, schema present, spec dirs match
//	  proposal capabilities. Rejects before LLM tokens are spent.
//
//	Layer 2 — plan-reviewer (existing component, ADR-029)
//	  Semantic review of the translated Plan + scenarios. The plan-reviewer
//	  capability rules (PR 2: capability_orphan / docs_only / cycle /
//	  dep_orphan) automatically apply to imported Plans because they
//	  have populated Plan.Exploration.
//
//	Layer 3 — recovery-agent (existing component, ADR-037)
//	  Wedge-recovery net if a translated Plan stalls at execution time.
//
// External identity is preserved via the `semspec.capability.external_spec`
// and `semspec.requirement.external_spec` triples (registered in PR 1a).
// When the PR 3 outbound emitter writes the change back out, it can
// surface the original source-spec entity IDs via those triples — the
// round-trip is identity-preserving for capability names and applies_to
// globs; only Plan-mutated content (scenarios refined by reviewer, scope
// evolved by execution) reflects semspec-side changes.
package specimport
