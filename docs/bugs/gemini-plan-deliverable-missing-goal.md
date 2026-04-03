# Gemini Planner Sends Deliverable Without goal Field

## Status: FIXED (2026-04-03)

## Severity: Critical (blocks all Gemini plan generation)

## Summary

Gemini (gemini-pro) repeatedly calls `submit_work` with a plan deliverable where
`deliverable.goal` fails the `.(string)` type assertion. After 18+ attempts over ~3
minutes, the loop exhausts iterations and the plan is rejected.

## Evidence

```
WARN submit_work deliverable validation failed deliverable_type=plan error="deliverable.goal is required" call_id=...
```

This error repeats 18+ times. The error message is returned to Gemini each time, but
Gemini never corrects the format.

## Tool Usage (from message logger)

```
submit_work: 18+ calls (all fail validation)
bash: 5 calls (codebase exploration)
graph_summary: 0
graph_search: 0
graph_query: 0
```

No graph tool usage despite prompt saying "MUST use bash or graph_search/graph_summary".

## Diagnosis (2026-04-02, runs 2-4)

Enhanced logging reveals: `deliverable_keys=""`, `deliverable_json={}`. Gemini sends a
**completely empty deliverable object** every time. The plan content is going into the
`summary` field instead. The raw argument logging (c050b62) is at DEBUG level which
doesn't reach the Docker container — LOG_LEVEL isn't forwarded in UI E2E compose.

Key observations across 4 runs:
- `deliverable_json={}` on every call (13+ per run)
- No double-encoding recovery triggered (1633c04 fix never fires)
- `summary` field passes the non-empty check (line 95), so it has content
- Zero graph tool calls across all runs — graph manifest is empty so
  GraphManifestFragment is excluded from the prompt

## Prompt Context

The output-format fragment (`software.planner.output-format`) clearly shows:
```json
{
  "goal": "What we're building or fixing — REQUIRED",
  "context": "Current state, why this matters — REQUIRED",
  "scope": { ... }
}
```

## Fix Applied (2026-04-03)

Root cause: `result` property in submit_work schema was `{type: "object"}` with zero named
properties. Gemini's streaming accumulator can't build JSON without named fields.

Fix: Three-layer approach:
1. **Flatten submit_work** — removed `result` wrapper, fields are now top-level named params
2. **Per-role schemas via TaskMessage.Tools** — each dispatch site passes role-specific
   submit_work schema with only the fields that role needs (plan gets goal/context/scope,
   review gets verdict/feedback, etc.)
3. **Prompt reinforcement** — updated all output format fragments, added Gemini-specific
   directive about putting data in parameters not text

## Found During

UI E2E @easy Gemini test (2026-04-02). Second attempt after duplicate tool fix.
