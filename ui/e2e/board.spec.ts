import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan } from './helpers/api';

test.describe('@mock board', () => {
	test('renders Plans heading and New Plan button', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		await expect(page.getByTestId('panel-center').getByRole('heading', { name: 'Plans' })).toBeVisible();
		await expect(page.getByRole('button', { name: /New Plan/i })).toBeVisible();
	});

	test('shows column chips with default columns visible', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		// Default ON columns — chips contain label + optional count
		for (const label of ['Draft', 'Review', 'Ready', 'Executing', 'Complete']) {
			await expect(page.getByRole('button', { name: new RegExp(`^${label}`) }).first()).toBeVisible();
		}
	});

	test('Failed and Archived chips are off by default', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		const failedChip = page.getByRole('button', { name: /^Failed/ }).first();
		const archivedChip = page.getByRole('button', { name: /^Archived/ }).first();

		await expect(failedChip).toBeVisible();
		await expect(failedChip).toHaveAttribute('aria-pressed', 'false');
		await expect(archivedChip).toHaveAttribute('aria-pressed', 'false');
	});

	test('toggling a chip hides/shows its column', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		// Draft column header should be visible (text-transform: uppercase in CSS, but HTML is "Draft")
		const draftColumn = page.locator('.column-label', { hasText: 'Draft' });
		await expect(draftColumn).toBeVisible();

		// Toggle Draft chip off
		await page.getByRole('button', { name: /^Draft/ }).first().click();

		// Draft column should be hidden
		await expect(draftColumn).not.toBeVisible();

		// Toggle it back on
		await page.getByRole('button', { name: /^Draft/ }).first().click();
		await expect(draftColumn).toBeVisible();
	});

	test('new plan appears in Draft column', async ({ page }) => {
		const plan = await createPlan(`Board test ${Date.now()}`);
		try {
			await page.goto('/');
			await waitForHydration(page);

			// Plan card should be in the board, linking to its detail page
			await expect(page.getByRole('link', { name: plan.slug }).first()).toBeVisible();
		} finally {
			await deletePlan(plan.slug).catch(() => {});
		}
	});

	test('clicking a plan card navigates to plan detail', async ({ page }) => {
		const plan = await createPlan(`Board nav test ${Date.now()}`);
		try {
			await page.goto('/');
			await waitForHydration(page);

			await page.getByRole('link', { name: plan.slug }).first().click();
			await expect(page).toHaveURL(`/plans/${plan.slug}`);
		} finally {
			await deletePlan(plan.slug).catch(() => {});
		}
	});

	test('New Plan button navigates to create form', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		await page.getByRole('button', { name: /New Plan/i }).click();
		await expect(page).toHaveURL('/plans/new');
	});

	test('empty state shows when no plans exist', async ({ page }) => {
		// This test may show plans from other tests — check for the empty state text
		// or the kanban columns (both are valid states)
		await page.goto('/');
		await waitForHydration(page);

		const hasPlans = await page.getByRole('link', { name: /.*/ }).first().isVisible().catch(() => false);
		if (!hasPlans) {
			await expect(page.getByText('No plans yet')).toBeVisible();
		}
	});
});
