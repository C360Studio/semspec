/**
 * @t0 truth-tests for bug #7.5 — per-source counts in the feed dropdown.
 *
 * Before: options read "All events / Plan / Execution / Questions" with no
 * counts, so users couldn't tell at a glance which source dominated. After:
 * each option shows "(N)" totals based on the unfiltered event list.
 *
 * The live test seeds the global activity stream with N events so
 * activityStore.recent populates; the dropdown is wired to show counts
 * derived from that list. Zero-count options still render (greyed-but-present)
 * so the dropdown doesn't jitter when a new source starts firing mid-run.
 */

import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { feedModeRadio } from './helpers/selectors';
import { stubBoardBackend, type ActivityEventSeed } from './helpers/truth';

test.describe('@t0 activity-feed source counts', () => {
	test('all + execution options carry counts matching the seeded stream', async ({ page }) => {
		const events: ActivityEventSeed[] = [
			{ loop_id: 'l-1', type: 'loop_created' },
			{ loop_id: 'l-1', type: 'loop_updated' },
			{ loop_id: 'l-2', type: 'loop_created' },
			{ loop_id: 'l-2', type: 'loop_updated' },
			{ loop_id: 'l-2', type: 'loop_deleted' }
		];
		await stubBoardBackend(page, { plans: [], loops: [], activityEvents: events });

		await page.goto('/');
		await waitForHydration(page);
		await feedModeRadio(page).click();

		// All seeded events project to source='execution' (see activityProjection).
		const allOption = page.locator('[data-testid="feed-source-option"][data-source="all"]');
		const execOption = page.locator('[data-testid="feed-source-option"][data-source="execution"]');
		const planOption = page.locator('[data-testid="feed-source-option"][data-source="plan"]');
		const questionOption = page.locator(
			'[data-testid="feed-source-option"][data-source="question"]'
		);

		await expect(allOption).toHaveAttribute('data-count', '5');
		await expect(execOption).toHaveAttribute('data-count', '5');
		// Zero-count sources must still render so the dropdown shape is stable.
		await expect(planOption).toHaveAttribute('data-count', '0');
		await expect(questionOption).toHaveAttribute('data-count', '0');

		// Label copy includes the count in parentheses — the visible hint.
		await expect(allOption).toHaveText(/All events \(5\)/);
		await expect(execOption).toHaveText(/Execution \(5\)/);
		await expect(planOption).toHaveText(/Plan \(0\)/);
	});

	test('empty stream shows (0) across every option', async ({ page }) => {
		await stubBoardBackend(page, { plans: [], loops: [], activityEvents: [] });

		await page.goto('/');
		await waitForHydration(page);
		await feedModeRadio(page).click();

		const options = page.locator('[data-testid="feed-source-option"]');
		await expect(options).toHaveCount(4);
		for (const src of ['all', 'plan', 'execution', 'question']) {
			await expect(
				page.locator(`[data-testid="feed-source-option"][data-source="${src}"]`)
			).toHaveAttribute('data-count', '0');
		}
	});
});
