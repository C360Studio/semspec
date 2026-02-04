import { test, expect } from './helpers/setup';

test.describe('System Health', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/');
	});

	test.describe('Health Indicator', () => {
		test('shows healthy status when backend is available', async ({ sidebarPage }) => {
			await sidebarPage.expectVisible();
			await sidebarPage.expectHealthy();
		});

		test('health indicator has correct visual styling', async ({ sidebarPage }) => {
			// When healthy, indicator should have the healthy class (green color)
			await expect(sidebarPage.healthIndicator).toHaveClass(/healthy/);
		});

		test('status text shows "System healthy"', async ({ sidebarPage }) => {
			const statusText = sidebarPage.systemStatus.locator('.status-text');
			await expect(statusText).toHaveText('System healthy');
		});
	});

	test.describe('Unhealthy State', () => {
		test('shows unhealthy status when health check fails', async ({ sidebarPage, page }) => {
			// Mock a failed health check
			await page.route('**/agentic-dispatch/health', route => {
				route.fulfill({
					status: 503,
					contentType: 'application/json',
					body: JSON.stringify({ status: 'unhealthy', error: 'Backend unavailable' })
				});
			});

			// Reload to trigger health check
			await page.reload();

			// Should show unhealthy state
			await sidebarPage.expectUnhealthy();
		});

		test('unhealthy indicator has correct visual styling', async ({ sidebarPage, page }) => {
			await page.route('**/agentic-dispatch/health', route => {
				route.fulfill({
					status: 503,
					contentType: 'application/json',
					body: JSON.stringify({ status: 'unhealthy' })
				});
			});

			await page.reload();

			// When unhealthy, indicator should NOT have the healthy class (red color)
			await expect(sidebarPage.healthIndicator).not.toHaveClass(/healthy/);
		});

		test('status text shows "System issues" when unhealthy', async ({ sidebarPage, page }) => {
			await page.route('**/agentic-dispatch/health', route => {
				route.fulfill({
					status: 503,
					contentType: 'application/json',
					body: JSON.stringify({ status: 'unhealthy' })
				});
			});

			await page.reload();

			const statusText = sidebarPage.systemStatus.locator('.status-text');
			await expect(statusText).toHaveText('System issues');
		});
	});

	test.describe('Health Recovery', () => {
		test('recovers to healthy state when backend becomes available', async ({ sidebarPage, page }) => {
			// Start with unhealthy state
			await page.route('**/agentic-dispatch/health', route => {
				route.fulfill({
					status: 503,
					contentType: 'application/json',
					body: JSON.stringify({ status: 'unhealthy' })
				});
			});

			await page.reload();
			await sidebarPage.expectUnhealthy();

			// Remove the route to restore normal behavior
			await page.unroute('**/agentic-dispatch/health');

			// Reload to trigger fresh health check
			await page.reload();

			// Should show healthy state
			await sidebarPage.expectHealthy();
		});
	});

	test.describe('Health Status Accessibility', () => {
		test('system status has live region for screen readers', async ({ sidebarPage }) => {
			await expect(sidebarPage.systemStatus).toHaveAttribute('role', 'status');
			await expect(sidebarPage.systemStatus).toHaveAttribute('aria-live', 'polite');
		});

		test('active loops has status role', async ({ sidebarPage }) => {
			await expect(sidebarPage.activeLoopsCounter).toHaveAttribute('role', 'status');
		});
	});
});
