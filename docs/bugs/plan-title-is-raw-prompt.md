# Feature: LLM-generated plan title

**Severity**: Low — cosmetic, affects sidebar display
**Component**: `planner` (`processor/planner/`)
**Found during**: UI E2E review (2026-03-28)
**Status**: FIXED

## Summary

Plan `title` is currently set from the raw user prompt in `CreatePlanRequest.Title`.
This means the sidebar shows truncated prompt text like "Add a /health endpoint to
the Go HTTP se…" instead of a concise title like "Health Endpoint".

The planner already generates `goal` and `context` from the prompt. It should also
generate a short `title` (under 60 chars) suitable for display in lists and navigation.

## Expected Behavior

The planner LLM call should return a `title` field alongside `goal`, `context`, and
`scope`. Plan-manager should update the title when the planner result arrives, similar
to how it updates goal/context.

If the LLM doesn't produce a title, fall back to the first 60 chars of the prompt.

## Current Behavior

`title` = raw user prompt (can be 200+ chars of natural language)
`goal` = LLM-synthesized goal (concise but describes the objective, not a title)

## UI Impact

`PlansList.svelte` currently truncates title to 60 chars. Once the backend generates
proper titles, the truncation becomes a safety net rather than the primary mechanism.

## Files

- `processor/planner/component.go` — planner LLM prompt and response parsing
- `processor/plan-manager/http.go` — CreatePlanRequest sets initial title
- `processor/plan-manager/component.go` — plan-manager updates plan from planner result
