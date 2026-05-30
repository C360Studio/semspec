package openspec

// RenderOpenSpecYAML returns the static `.openspec.yaml` content that
// declares the OpenSpec schema this directory adheres to. Per ADR-040 Q5,
// semspec emits OpenSpec's default schema (`spec-driven`) — no custom
// semspec schema variant.
//
// This file is per-plan metadata, not per-mutation: callers write it once
// when the openspec/ directory is first created and never overwrite it
// (the value is invariant). Adopter tooling reads it to pick the correct
// validator.
func RenderOpenSpecYAML() string {
	return `# OpenSpec metadata. Emitted by semspec (ADR-040 Move 3).
# Source of truth for capability identity + scenarios is in
# .semspec/plans/<slug>/plan.json — this directory is a derived projection.

schema: spec-driven
`
}
