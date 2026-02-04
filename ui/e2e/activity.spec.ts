import { test, expect } from './helpers/setup';

test.describe('Activity Stream', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/');
	});

	test.describe('Connection', () => {
		test('connects to activity stream on page load', async ({ sidebarPage }) => {
			// The sidebar should be visible and show system status
			await sidebarPage.expectVisible();
			// System should eventually report healthy when connected
			await sidebarPage.expectHealthy();
		});

		test('shows active loops count', async ({ sidebarPage }) => {
			// Initially should show 0 active loops (or the current count)
			await expect(sidebarPage.activeLoopsCounter).toBeVisible();
			const text = await sidebarPage.activeLoopsCounter.textContent();
			expect(text).toMatch(/\d+ active loops/);
		});
	});

	test.describe('Real-time Updates', () => {
		test('sidebar reflects current loop state', async ({ chatPage, sidebarPage }) => {
			// Send a command that creates a loop
			await chatPage.sendMessage('/status');
			await chatPage.waitForResponse();

			// The loops counter should be visible
			await expect(sidebarPage.activeLoopsCounter).toBeVisible();
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
			await page.goto('/');

			// Unblock the route
			await page.unroute('**/agentic-dispatch/activity');

			// Wait a bit for reconnection
			await page.waitForTimeout(5000);

			// Should eventually reconnect and show healthy
			await sidebarPage.expectHealthy();
		});
	});
});
