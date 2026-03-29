# Feature: Disable plan action buttons during LLM calls

**Severity**: Low — UX polish
**Component**: UI plan detail page
**Status**: OPEN

## Summary

Plan action buttons (Create Requirements, Execute, etc.) should be disabled
when an LLM call is in flight (e.g. during drafting, requirement generation,
scenario generation). Currently buttons are clickable even when the plan is
being processed, which can cause duplicate triggers or confusing state.

## Expected Behavior

- During `drafting` stage with active loops: disable "Create Requirements" button
- During `generating_requirements` / `generating_scenarios`: disable approve/promote buttons
- Show a spinner or "Processing..." indicator
- Re-enable when the stage transitions

## Files

- `ui/src/routes/plans/[slug]/+page.svelte` — plan detail page with action buttons
- `ui/src/lib/stores/feed.svelte.ts` — active_loops from plan SSE
