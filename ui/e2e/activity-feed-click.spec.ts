/**
 * @t0 truth-tests for bug #7.8 — whole-row clickability on ActivityFeed.
 *
 * Before: only the small chip at the end of each row was an <a>. Users
 * instinctively click the row, so interactions felt broken. After:
 *   - Rows with a navigable destination render as a single <a> covering the
 *     whole row (data-testid="activity-feed-row", data-href="/...").
 *   - Rows without a destination stay non-interactive (no <a>).
 *   - Routing broadened so loop_created/updated/deleted with loop_id also
 *     link to /trajectories/{id} (was only task_*).
 *
 * Uses stubActivityStream with preloaded events so the EventSource fills
 * activityStore deterministically — no live backend needed.
 */

import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { feedModeRadio } from './helpers/selectors';
import { stubBoardBackend, type ActivityEventSeed } from './helpers/truth';

test.describe('@t0 activity-feed row clickability', () => {
	test('every loop_* event with loop_id renders as a clickable row', async ({ page }) => {
		const events: ActivityEventSeed[] = [
			{ loop_id: 'loop-aaa111', type: 'loop_created' },
			{ loop_id: 'loop-bbb222', type: 'loop_updated' },
			{ loop_id: 'loop-ccc333', type: 'loop_deleted' }
		];
		await stubBoardBackend(page, { plans: [], loops: [], activityEvents: events });

		await page.goto('/');
		await waitForHydration(page);

		// LeftPanel auto-switches to Feed mode once activity arrives, but be
		// explicit for determinism — matches activity-feed.spec.ts pattern.
		await feedModeRadio(page).click();

		// Three rows, each an <a>. Before the fix: 0 link rows, 3 div rows.
		const rows = page.getByTestId('activity-feed-row');
		await expect(rows).toHaveCount(3);

		// Each row's href points at its trajectory. The fix broadened
		// getEventHref beyond task_* events — the regression pin is that the
		// first row's href is NOT empty. ActivityFeed renders chronologically
		// (oldest → newest), matching activityStore.recent append order.
		await expect(rows.nth(0)).toHaveAttribute('data-href', '/trajectories/loop-aaa111');
		await expect(rows.nth(1)).toHaveAttribute('data-href', '/trajectories/loop-bbb222');
		await expect(rows.nth(2)).toHaveAttribute('data-href', '/trajectories/loop-ccc333');

		// Each row must actually be an <a> (not a div) — prior behaviour only
		// wrapped the chip, leaving the rest of the row unclickable.
		for (let i = 0; i < 3; i++) {
			await expect(rows.nth(i)).toHaveJSProperty('tagName', 'A');
		}
	});

	test('clicking anywhere on a linked row navigates to the destination', async ({ page }) => {
		await stubBoardBackend(page, {
			plans: [],
			loops: [],
			activityEvents: [{ loop_id: 'loop-click-target', type: 'loop_created' }]
		});

		await page.goto('/');
		await waitForHydration(page);
		await feedModeRadio(page).click();

		const row = page.getByTestId('activity-feed-row').first();
		await expect(row).toBeVisible();

		// Click the row itself, not the tiny chip. The test fails if the fix
		// regresses because the click would land on an unhandled <div>.
		await row.click();
		await expect(page).toHaveURL(/\/trajectories\/loop-click-target$/);
	});

	// Bug #7.9 — requirement anchor pill. Rows carrying a requirement_id
	// render a compact [R{n}] pill so the eye can filter by requirement
	// without reading every summary. Rows without a requirement_id do not.
	test('requirement anchor pill appears when event carries a requirement_id', async ({
		page
	}) => {
		await stubBoardBackend(page, {
			plans: [],
			loops: [],
			activityEvents: [
				{ loop_id: 'loop-r7', type: 'loop_updated', requirement_id: 'requirement.demo.7' },
				{ loop_id: 'loop-plain', type: 'loop_updated' }
			]
		});

		await page.goto('/');
		await waitForHydration(page);
		await feedModeRadio(page).click();

		// Exactly one anchor across the two rows.
		const anchors = page.getByTestId('req-anchor');
		await expect(anchors).toHaveCount(1);
		await expect(anchors.first()).toHaveText('R7');
	});
});
