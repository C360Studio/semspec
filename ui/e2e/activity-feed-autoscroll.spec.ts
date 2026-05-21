/**
 * @t0 Activity Feed autoscroll + "N new ↓" pill.
 *
 * Pins 2026-05-21 work — chat-app autoscroll pattern:
 *   - When user is pinned to bottom, new events auto-follow (scrollTop
 *     advances to scrollHeight on each arrival).
 *   - When user scrolls up to read history, scroll position is preserved
 *     AND a "N new" pill appears in the bottom-right of the feed.
 *   - Clicking the pill snaps back to bottom and clears the pill.
 *
 * Uses /e2e-test/activity-feed-autoscroll harness which drives the
 * singleton feedStore directly via buttons. Avoids the SSE roundtrip so
 * the events arrive on a deterministic tick the test can observe.
 */

import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';

test.describe('@t0 activity-feed autoscroll', () => {
	test('new events auto-follow when user is pinned to bottom', async ({ page }) => {
		await page.goto('/e2e-test/activity-feed-autoscroll');
		await waitForHydration(page);

		// Seed 30 events — enough to overflow the 400px feed-pane and create
		// a real scroll context.
		await page.getByTestId('seed-30').click();

		// Scope to the harness wrapper — the page layout also renders an
		// ActivityFeed in the left sidebar (scope=global). Without scoping
		// the `.events-list` locator hits strict-mode violation.
		const harness = page.getByTestId('activity-feed-harness');
		const list = harness.locator('.events-list');
		await expect(list).toBeVisible();
		// First render places us at the bottom because isUserPinnedToBottom
		// defaults to true and the autoscroll $effect scrolls on every length
		// change. Confirm by checking distance-from-bottom is small.
		const initialDistance = await list.evaluate(
			(el) => el.scrollHeight - el.scrollTop - el.clientHeight
		);
		expect(initialDistance).toBeLessThan(50);

		// Append one more event; the autoscroll should keep us at the bottom.
		await page.getByTestId('append-one').click();
		// queueMicrotask + DOM update takes a tick — give it a small buffer.
		// Use the harness root as the scoped query inside waitForFunction so
		// we read the right `.events-list` (not the sidebar's).
		await page.waitForFunction(
			() => {
				const el = document.querySelector(
					'[data-testid="activity-feed-harness"] .events-list'
				) as HTMLElement | null;
				if (!el) return false;
				return el.scrollHeight - el.scrollTop - el.clientHeight < 50;
			},
			{ timeout: 2_000 }
		);

		// Pill must NOT be present in the harness — we're pinned, so nothing
		// is "below". Scoped to harness to avoid matching any sibling pill.
		await expect(harness.getByTestId('new-events-pill')).toHaveCount(0);
	});

	test('scrolling up shows the "N new" pill on append; click snaps back', async ({ page }) => {
		await page.goto('/e2e-test/activity-feed-autoscroll');
		await waitForHydration(page);
		await page.getByTestId('seed-30').click();

		const harness = page.getByTestId('activity-feed-harness');
		const list = harness.locator('.events-list');
		await expect(list).toBeVisible();

		// Scroll user up to the top. Now isUserPinnedToBottom = false.
		await list.evaluate((el) => {
			el.scrollTop = 0;
		});
		// Fire the scroll handler — Playwright's evaluate doesn't dispatch.
		await list.dispatchEvent('scroll');

		// No pill yet because no NEW events arrived after the user scrolled up.
		await expect(harness.getByTestId('new-events-pill')).toHaveCount(0);

		// Two new events arrive. Pill should appear with count "2 new".
		await page.getByTestId('append-one').click();
		await page.getByTestId('append-one').click();

		const pill = harness.getByTestId('new-events-pill');
		await expect(pill).toBeVisible();
		await expect(pill).toContainText(/2 new/);

		// Scroll position MUST NOT have been yanked — user is still reading
		// history. Within a small jitter of scrollTop=0.
		const scrollTopAfter = await list.evaluate((el) => el.scrollTop);
		expect(scrollTopAfter).toBeLessThan(50);

		// Click the pill — snap to bottom, pill disappears, lastSeenIndex
		// catches up so newEventsBelow goes back to 0.
		await pill.click();

		await page.waitForFunction(
			() => {
				const el = document.querySelector(
					'[data-testid="activity-feed-harness"] .events-list'
				) as HTMLElement | null;
				if (!el) return false;
				return el.scrollHeight - el.scrollTop - el.clientHeight < 50;
			},
			{ timeout: 2_000 }
		);
		await expect(harness.getByTestId('new-events-pill')).toHaveCount(0);
	});
});
