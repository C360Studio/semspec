import { expect, type Page } from '@playwright/test';

/**
 * Ensure LeftPanel is in Plans mode and stays in Plans mode for the rest of
 * the test. Click is unconditional because we need `manualOverride=true` in
 * `LeftPanel.svelte` — a one-shot `aria-checked` read can miss the auto-switch
 * `$effect` that fires when `activityStore.loopLastSeen` picks up SSE updates
 * from earlier specs' loops. Wait on the "Filter plans" radiogroup so callers
 * know `PlansList` (not `ActivityFeed`) is actually mounted before reaching for
 * filter chips, plan list items, or the New Plan button.
 *
 * Use this in any spec that interacts with Plans-mode UI in the left panel.
 * History: extracted from `plan-list.spec.ts` after PR #9 fixed the race
 * there; PR follow-up after `health.spec.ts` was caught hitting the same
 * pattern in a cold lifecycle triage run.
 */
export async function ensurePlansMode(page: Page): Promise<void> {
	const plansRadio = page.getByRole('radio', { name: 'Plans' });
	await plansRadio.click();
	await expect(plansRadio).toHaveAttribute('aria-checked', 'true');
	await expect(page.getByRole('radiogroup', { name: 'Filter plans' })).toBeVisible();
}
