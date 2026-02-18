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

		test('shows Active Loops section', async ({ page }) => {
			const loopsHeader = page.locator('.loops-header', { hasText: 'Active Loops' });
			await expect(loopsHeader).toBeVisible();
		});

		test('shows Chat / Commands section', async ({ page }) => {
			const chatHeader = page.locator('h2', { hasText: 'Chat / Commands' });
			await expect(chatHeader).toBeVisible();
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
