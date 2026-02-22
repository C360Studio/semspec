import { test, expect } from './helpers/setup';

test.describe('Activity Stream', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/activity');
		// Wait for SvelteKit hydration to complete
		await page.locator('body.hydrated').waitFor({ state: 'attached', timeout: 10000 });
	});

	test.describe('Connection', () => {
		test('connects to activity stream on page load', async ({ sidebarPage }) => {
			// The sidebar should be visible and show system status
			await sidebarPage.expectVisible();
			// System should eventually report healthy when connected
			await sidebarPage.expectHealthy();
		});

		test('shows active loops count in sidebar', async ({ sidebarPage }) => {
			// Initially should show 0 active loops (or the current count)
			await expect(sidebarPage.activeLoopsCounter).toBeVisible();
			const text = await sidebarPage.activeLoopsCounter.textContent();
			expect(text).toMatch(/\d+ active loops/);
		});
	});

	test.describe('Activity Page Layout', () => {
		test('shows view toggle with Feed and Timeline options', async ({ page }) => {
			const feedButton = page.locator('button.toggle-btn', { hasText: 'Feed' });
			const timelineButton = page.locator('button.toggle-btn', { hasText: 'Timeline' });

			await expect(feedButton).toBeVisible();
			await expect(timelineButton).toBeVisible();
		});

		test('shows three collapsible panels', async ({ activityPage }) => {
			await activityPage.expectFeedPanelVisible();
			await activityPage.expectLoopsPanelVisible();
			await activityPage.expectChatPanelVisible();
		});

		test('shows Loops panel with count badge', async ({ page }) => {
			// Loops panel has title "Loops" and count badge
			const loopsPanel = page.locator('[data-panel-id="activity-loops"]');
			await expect(loopsPanel).toBeVisible();
			const loopsCount = page.locator('.loops-count');
			await expect(loopsCount).toBeVisible();
		});

		test('shows Chat panel', async ({ page }) => {
			const chatPanel = page.locator('[data-panel-id="activity-chat"]');
			await expect(chatPanel).toBeVisible();
		});
	});

	test.describe('Collapsible Panels', () => {
		test('can collapse and expand Feed panel', async ({ activityPage }) => {
			await activityPage.expectFeedPanelExpanded();
			await activityPage.toggleFeedPanel();
			await activityPage.expectFeedPanelCollapsed();
			await activityPage.toggleFeedPanel();
			await activityPage.expectFeedPanelExpanded();
		});

		test('can collapse and expand Loops panel', async ({ activityPage }) => {
			await activityPage.expectLoopsPanelExpanded();
			await activityPage.toggleLoopsPanel();
			await activityPage.expectLoopsPanelCollapsed();
			await activityPage.toggleLoopsPanel();
			await activityPage.expectLoopsPanelExpanded();
		});

		test('can collapse and expand Chat panel', async ({ activityPage }) => {
			await activityPage.expectChatPanelExpanded();
			await activityPage.toggleChatPanel();
			await activityPage.expectChatPanelCollapsed();
			await activityPage.toggleChatPanel();
			await activityPage.expectChatPanelExpanded();
		});
	});

	test.describe('Reconnection', () => {
		test('reconnects after connection loss', async ({ page, sidebarPage }) => {
			// Wait for initial healthy state
			await sidebarPage.expectHealthy();

			// Simulate connection loss by blocking the activity endpoint
			await page.route('**/agentic-dispatch/activity', route => {
				route.abort('connectionfailed');
			});

			// Navigate away and back to trigger reconnection attempt
			await page.goto('/settings');
			await page.goto('/activity');

			// Unblock the route
			await page.unroute('**/agentic-dispatch/activity');

			// Wait a bit for reconnection
			await page.waitForTimeout(5000);

			// Should eventually reconnect and show healthy
			await sidebarPage.expectHealthy();
		});
	});
});
