import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { planGoalInput, createPlanButton, errorAlert } from './helpers/selectors';

test.describe('@mock plan-create', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/plans/new');
		await waitForHydration(page);
	});

	test('renders form with label and submit button', async ({ page }) => {
		await expect(planGoalInput(page)).toBeVisible();
		await expect(createPlanButton(page)).toBeVisible();
		await expect(page.getByRole('heading', { name: /New Plan/i })).toBeVisible();
	});

	test('submit button disabled when textarea empty', async ({ page }) => {
		await expect(createPlanButton(page)).toBeDisabled();
	});

	test('submit button enabled after typing goal', async ({ page }) => {
		await planGoalInput(page).pressSequentially('Add user authentication');
		await expect(createPlanButton(page)).toBeEnabled();
	});

	test('successful submit redirects to plan detail', async ({ page }) => {
		await planGoalInput(page).pressSequentially('Add user authentication with JWT');
		await createPlanButton(page).click();

		// Should redirect to /plans/{slug}
		await expect(page).toHaveURL(/\/plans\/[a-z0-9-]+/);
	});

	test('shows error alert on API failure', async ({ page }) => {
		// Mock the create endpoint to fail
		await page.route('**/plan-manager/plans', (route) => {
			if (route.request().method() === 'POST') {
				route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ message: 'Internal server error' })
				});
			} else {
				route.continue();
			}
		});

		await planGoalInput(page).pressSequentially('This will fail');
		await createPlanButton(page).click();

		await expect(errorAlert(page)).toBeVisible();
	});

	test('cancel navigates back to home', async ({ page }) => {
		await page.getByRole('link', { name: /Cancel/i }).click();
		await expect(page).toHaveURL('/');
	});
});
