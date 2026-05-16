# Operator rules — what the run had to obey

This directory contains the two operator-declared rule files the agent's
plan-reviewer and structural-validator enforced during take-30. These
files are committed in the project fixture; the agent reads them at
startup and their content shapes every deliverable the agent produces.

## Files

- **`standards.json`** — Operator's SOPs (Standard Operating Procedures).
  Plan-reviewer treats these as compliance criteria. The agent's plan
  + architecture + requirements + scenarios must demonstrate adherence
  or the reviewer raises a finding. 17 items, ~680 tokens, six
  categories: security (5), testing (4), error handling (3), code
  style (3), observability (1), documentation (1).

- **`checklist.json`** — Per-project structural checks that
  structural-validator runs at every scenario merge. For this Java
  project: `./gradlew dependencies` (must resolve to real artifacts)
  and `./gradlew test` (must pass). `required: true` on both — failure
  rejects the scenario.

## How these interact with the rest of the system

The full picture (agent roles, phase flow, the six layers of
deterministic gating, and where these two files fit) is in
`../flow-overview.md`. The short version:

- **`standards.json`** is Layer 6 — applied by the LLM plan-reviewer
  as compliance criteria against every plan-phase artifact.
- **`checklist.json`** is Layer 4 — opaque shell commands the
  structural-validator runs in the sandbox after each scenario merge.

Same machinery supports arbitrary additional checks per project
(`go vet`, `npm audit`, `cargo clippy`, custom scripts) — the
framework treats checklist items as commands with a timeout and a
required/advisory bit.
