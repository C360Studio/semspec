import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';

test.describe('@t0 trajectories', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/trajectories');
		await waitForHydration(page);
	});

	test('renders trajectories page with heading', async ({ page }) => {
		await expect(page.getByTestId('trajectories-heading')).toBeVisible();
		await expect(page.getByTestId('trajectories-heading')).toHaveText('Trajectories');
		await expect(page.getByText('Recent agent loop execution history')).toBeVisible();
	});

	test('shows refresh button', async ({ page }) => {
		await expect(page.getByRole('button', { name: /Refresh/i })).toBeVisible();
	});

	test('shows filter sidebar with Outcome section', async ({ page }) => {
		// Trajectories page has its own left panel with Filters/Outcome
		await expect(page.getByText('Filters')).toBeVisible();
		await expect(page.getByText('Outcome', { exact: true }).first()).toBeVisible();
	});

	test('shows trajectory items or empty state', async ({ page }) => {
		const list = page.getByTestId('trajectory-list');
		const empty = page.getByTestId('trajectories-empty');
		const error = page.getByTestId('trajectories-error');

		// Wait for loading to resolve — one of these should appear
		await expect(list.or(empty).or(error)).toBeVisible();
	});

	test('trajectory items link to detail page', async ({ page }) => {
		const items = page.getByTestId('trajectory-item');
		const count = await items.count();

		if (count > 0) {
			// Each item should be a link to /trajectories/{id}
			const href = await items.first().getAttribute('href');
			expect(href).toMatch(/^\/trajectories\/.+/);

			// Should show loop ID, status badge
			await expect(items.first().getByTestId('trajectory-item-id')).toBeVisible();
			await expect(items.first().getByTestId('trajectory-item-status')).toBeVisible();
		}
	});

	test('refresh button fetches latest data', async ({ page }) => {
		const refreshBtn = page.getByRole('button', { name: /Refresh/i });
		await refreshBtn.click();

		// Button should still be visible after refresh completes
		await expect(refreshBtn).toBeVisible();
	});

	test('outcome filter buttons are clickable', async ({ page }) => {
		// The "All" button in the left panel outcome section
		const allBtn = page.getByTestId('panel-left').getByRole('button', { name: /All/i }).first();
		await expect(allBtn).toBeVisible();
		await allBtn.click();

		// Should still show the page (no navigation away)
		await expect(page.getByTestId('trajectories-heading')).toBeVisible();
	});
});
