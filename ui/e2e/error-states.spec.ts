import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan } from './helpers/api';
import {
	approvePlanButton,
	errorAlert,
	planGoalInput,
	createPlanButton
} from './helpers/selectors';

test.describe('@mock error-states', () => {
	test('404 plan shows not-found message', async ({ page }) => {
		await page.goto('/plans/nonexistent-plan-slug');
		await waitForHydration(page);

		await expect(page.getByText('Plan not found')).toBeVisible();
		await expect(page.getByRole('link', { name: /Back to Board/i })).toBeVisible();
	});

	test('back to board link works from not-found', async ({ page }) => {
		await page.goto('/plans/nonexistent-plan-slug');
		await waitForHydration(page);

		await page.getByRole('link', { name: /Back to Board/i }).click();
		await expect(page).toHaveURL('/');
	});

	test('create plan API error shows alert', async ({ page }) => {
		await page.goto('/plans/new');
		await waitForHydration(page);

		await page.route('**/plan-manager/plans', (route) => {
			if (route.request().method() === 'POST') {
				route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Database connection failed' })
				});
			} else {
				route.continue();
			}
		});

		await planGoalInput(page).pressSequentially('This will fail');
		await createPlanButton(page).click();

		await expect(errorAlert(page)).toBeVisible();
		await expect(errorAlert(page)).toContainText('Database connection failed');
	});

	test('promote API error shows error banner', async ({ page }) => {
		const plan = await createPlan('Test plan for promote error');
		try {
			await page.goto(`/plans/${plan.slug}`);
			await waitForHydration(page);

			// Mock the promote endpoint to fail
			await page.route(`**/plan-manager/plans/${plan.slug}/promote`, (route) => {
				route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Workflow trigger failed' })
				});
			});

			await approvePlanButton(page).first().click();

			await expect(errorAlert(page)).toBeVisible();
		} finally {
			await deletePlan(plan.slug).catch(() => {});
		}
	});

	test('network failure on create shows error', async ({ page }) => {
		await page.goto('/plans/new');
		await waitForHydration(page);

		await page.route('**/plan-manager/plans', (route) => {
			if (route.request().method() === 'POST') {
				route.abort('connectionrefused');
			} else {
				route.continue();
			}
		});

		await planGoalInput(page).pressSequentially('This will have no network');
		await createPlanButton(page).click();

		await expect(errorAlert(page)).toBeVisible();
	});
});
