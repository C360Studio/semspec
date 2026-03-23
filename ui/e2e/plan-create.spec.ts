import { test, expect, waitForHydration } from './helpers/setup';

/**
 * Plan creation form (/plans/new).
 *
 * Verifies form rendering, validation, submission, and navigation.
 */
test.describe('Plan Creation', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/plans/new');
		await waitForHydration(page);
	});

	test.describe('Form Rendering', () => {
		test('shows form with title and description', async ({ page }) => {
			await expect(page.getByRole('heading', { name: 'New Plan' })).toBeVisible();
			await expect(page.getByText('Describe what you want to build')).toBeVisible();
		});

		test('shows goal textarea with placeholder', async ({ page }) => {
			const textarea = page.getByRole('textbox', { name: 'What do you want to build?' });
			await expect(textarea).toBeVisible();
			await expect(textarea).toHaveAttribute('placeholder', /Add user authentication/);
		});

		test('shows tips section', async ({ page }) => {
			await expect(page.getByRole('heading', { name: 'Tips for a good plan description' })).toBeVisible();
			await expect(page.getByText('Be specific about the feature')).toBeVisible();
		});

		test('shows cancel link to home', async ({ page }) => {
			const cancel = page.getByRole('link', { name: 'Cancel' });
			await expect(cancel).toBeVisible();
			await expect(cancel).toHaveAttribute('href', '/');
		});
	});

	test.describe('Validation', () => {
		test('submit button is disabled when textarea is empty', async ({ page }) => {
			const submit = page.getByRole('button', { name: 'Create Plan' });
			await expect(submit).toBeDisabled();
		});

		test('submit button enables when text is entered', async ({ page }) => {
			const textarea = page.getByRole('textbox', { name: 'What do you want to build?' });
			const submit = page.getByRole('button', { name: 'Create Plan' });

			await textarea.fill('Add a greeting endpoint');
			await expect(submit).toBeEnabled();
		});

		test('submit button disables when text is cleared', async ({ page }) => {
			const textarea = page.getByRole('textbox', { name: 'What do you want to build?' });
			const submit = page.getByRole('button', { name: 'Create Plan' });

			await textarea.fill('Something');
			await expect(submit).toBeEnabled();

			await textarea.fill('');
			await expect(submit).toBeDisabled();
		});

		test('whitespace-only input keeps button disabled', async ({ page }) => {
			const textarea = page.getByRole('textbox', { name: 'What do you want to build?' });
			const submit = page.getByRole('button', { name: 'Create Plan' });

			await textarea.fill('   ');
			await expect(submit).toBeDisabled();
		});
	});

	test.describe('Submission', () => {
		test('successful submit redirects to plan detail', async ({ page }) => {
			// Mock the create API to return a slug
			await page.route('**/workflow-api/plans', (route) => {
				if (route.request().method() === 'POST') {
					route.fulfill({
						status: 200,
						contentType: 'application/json',
						body: JSON.stringify({
							slug: 'add-greeting-endpoint',
							request_id: 'req-123',
							trace_id: 'trace-123',
							message: 'Plan created'
						})
					});
				} else {
					route.continue();
				}
			});

			// Also mock the plan detail page data so navigation doesn't 404
			await page.route('**/workflow-api/plans/add-greeting-endpoint', (route) => {
				route.fulfill({
					status: 200,
					contentType: 'application/json',
					body: JSON.stringify({
						slug: 'add-greeting-endpoint',
						title: 'Add Greeting Endpoint',
						goal: 'Add a greeting endpoint',
						approved: false,
						stage: 'draft',
						active_loops: []
					})
				});
			});

			const textarea = page.getByRole('textbox', { name: 'What do you want to build?' });
			await textarea.fill('Add a greeting endpoint');
			await page.getByRole('button', { name: 'Create Plan' }).click();

			await expect(page).toHaveURL(/\/plans\/add-greeting-endpoint/);
		});

		test('failed submit shows error message', async ({ page }) => {
			await page.route('**/workflow-api/plans', (route) => {
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

			const textarea = page.getByRole('textbox', { name: 'What do you want to build?' });
			await textarea.fill('Add a greeting endpoint');
			await page.getByRole('button', { name: 'Create Plan' }).click();

			await expect(page.getByRole('alert')).toBeVisible();
			// Should stay on the form page
			await expect(page).toHaveURL(/\/plans\/new/);
		});
	});

	test.describe('Navigation', () => {
		test('cancel navigates back to home', async ({ page }) => {
			await page.getByRole('link', { name: 'Cancel' }).click();
			await expect(page).toHaveURL('/');
		});
	});
});
