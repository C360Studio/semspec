// Package openspec renders OpenSpec-shaped artifacts from semspec Plan +
// Execution state. Per ADR-040 Move 3, this is the OUTBOUND projection: we
// hydrate OpenSpec from our flow rather than running OpenSpec internally.
// Plan, Exploration, Architecture, Requirements, Scenarios, and Execution
// state remain authoritative on the semspec side; this package produces the
// markdown that OpenSpec adopter tooling (`openspec status`, `openspec
// validate`) consumes.
//
// Layout produced under `.semspec/plans/<slug>/openspec/`:
//
//	proposal.md              — capability list + open questions (Plan.Exploration)
//	specs/<cap-name>/spec.md — per-capability spec with applies_to + scenarios
//	design.md                — architecture decisions + tradeoffs
//	tasks.md                 — execution checkboxes that flip live as
//	                           execution-manager completes nodes
//	.openspec.yaml           — static schema metadata (schema: spec-driven)
//
// Emission is read-only against PLAN_STATES + EXECUTION_STATES. None of the
// renderers mutate semspec state. They return "" when the input plan lacks
// the relevant data (e.g. RenderDesign on a plan without Architecture, or
// any renderer on a plan without Plan.Exploration — legacy plans don't get
// OpenSpec output).
package openspec
