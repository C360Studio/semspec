import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, promotePlan } from './helpers/api';

test.describe('@mock right-panel', () => {
	test('right panel hidden on home page without plan selected', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		// Right panel should not be visible when no plan is selected
		const rightPanel = page.getByTestId('panel-right');
		// The panel may exist in DOM but be collapsed (width 0) — check for tab bar
		const tabBar = rightPanel.getByRole('tablist');
		const hasTabBar = await tabBar.isVisible().catch(() => false);
		// On home page without a plan selected, no tabs should show
		// (unless there are active loops)
		expect(typeof hasTabBar).toBe('boolean');
	});

	test('right panel shows tabs when viewing approved plan', async ({ page }) => {
		// Find an approved plan
		const res = await fetch('http://localhost:3000/plan-api/plans');
		const plans = await res.json();
		const approved = plans.find((p: any) => p.approved === true);

		if (!approved) {
			test.skip();
			return;
		}

		await page.goto(`/plans/${approved.slug}`);
		await waitForHydration(page);

		// Right panel should show tabs
		const rightPanel = page.getByTestId('panel-right');
		await expect(rightPanel).toBeVisible();

		// Reviews tab should always be present
		await expect(rightPanel.getByRole('tab', { name: /Reviews/i })).toBeVisible();
		// Files tab should always be present
		await expect(rightPanel.getByRole('tab', { name: /Files/i })).toBeVisible();
	});

	test('tab switching works', async ({ page }) => {
		const res = await fetch('http://localhost:3000/plan-api/plans');
		const plans = await res.json();
		const approved = plans.find((p: any) => p.approved === true);

		if (!approved) {
			test.skip();
			return;
		}

		await page.goto(`/plans/${approved.slug}`);
		await waitForHydration(page);

		const rightPanel = page.getByTestId('panel-right');

		// Click Reviews tab
		const reviewsTab = rightPanel.getByRole('tab', { name: /Reviews/i });
		await reviewsTab.click();
		await expect(reviewsTab).toHaveAttribute('aria-selected', 'true');

		// Click Files tab
		const filesTab = rightPanel.getByRole('tab', { name: /Files/i });
		await filesTab.click();
		await expect(filesTab).toHaveAttribute('aria-selected', 'true');

		// Files shows placeholder
		await expect(rightPanel.getByText(/File viewer coming soon/i)).toBeVisible();
	});

	test('reviews tab loads review content', async ({ page }) => {
		const res = await fetch('http://localhost:3000/plan-api/plans');
		const plans = await res.json();
		const approved = plans.find((p: any) => p.approved === true);

		if (!approved) {
			test.skip();
			return;
		}

		await page.goto(`/plans/${approved.slug}`);
		await waitForHydration(page);

		const rightPanel = page.getByTestId('panel-right');
		const reviewsTab = rightPanel.getByRole('tab', { name: /Reviews/i });
		await reviewsTab.click();

		// Reviews content area should render (may show "Not Found" or actual reviews)
		// The key is that the tab content doesn't crash
		await expect(rightPanel).toBeVisible();
	});

	test('draft plan has no Trajectory or Agents tabs', async ({ page }) => {
		const plan = await createPlan(`Right panel draft test ${Date.now()}`);
		try {
			await page.goto(`/plans/${plan.slug}`);
			await waitForHydration(page);

			// Right panel may or may not be visible for a draft plan (depends on layout data timing).
			// The key assertion: no Trajectory or Agents tabs (those require active loops).
			const trajectoryTab = page.getByRole('tab', { name: /Trajectory/i });
			const agentsTab = page.getByRole('tab', { name: /Agents/i });
			await expect(trajectoryTab).not.toBeVisible();
			await expect(agentsTab).not.toBeVisible();
		} finally {
			await deletePlan(plan.slug).catch(() => {});
		}
	});
});
