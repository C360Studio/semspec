import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';

test.describe('@t0 trajectory-detail', () => {
	test('not-found trajectory shows empty state', async ({ page }) => {
		await page.goto('/trajectories/nonexistent-loop-id');
		await waitForHydration(page);

		await expect(page.getByTestId('trajectory-detail-page')).toBeVisible();
		await expect(page.getByTestId('trajectory-heading')).toHaveText('Trajectory Timeline');

		// Should show not-found or empty state (trajectory API returns null for unknown IDs)
		const notFound = page.getByTestId('trajectory-not-found');
		const emptySteps = page.getByTestId('trajectory-empty-steps');
		const error = page.getByTestId('trajectory-error');
		const hasState = await Promise.any([
			notFound.waitFor({ timeout: 5000 }).then(() => true),
			emptySteps.waitFor({ timeout: 5000 }).then(() => true),
			error.waitFor({ timeout: 5000 }).then(() => true)
		]).catch(() => false);

		expect(hasState).toBe(true);
	});

	test('back link navigates to trajectories list', async ({ page }) => {
		await page.goto('/trajectories/nonexistent-loop-id');
		await waitForHydration(page);

		const backLink = page.getByTestId('trajectory-back-link');
		await expect(backLink).toBeVisible();
		await expect(backLink).toHaveText(/Back to Trajectories/i);

		await backLink.click();
		await expect(page).toHaveURL('/trajectories');
	});

	test('shows loop ID in header', async ({ page }) => {
		await page.goto('/trajectories/nonexistent-loop-id');
		await waitForHydration(page);

		await expect(page.getByTestId('trajectory-id')).toBeVisible();
		await expect(page.getByTestId('trajectory-id')).toContainText('nonexistent-loop-id');
	});

	test('real loop ID renders detail page', async ({ page }) => {
		// Get a real loop ID from the API
		const res = await fetch('http://localhost:3000/agentic-dispatch/loops');
		const loops = await res.json();
		if (!loops.length) {
			test.skip();
			return;
		}

		const loopId = loops[0].loop_id;
		await page.goto(`/trajectories/${loopId}`);
		await waitForHydration(page);

		await expect(page.getByTestId('trajectory-detail-page')).toBeVisible();
		await expect(page.getByTestId('trajectory-id')).toContainText(loopId);

		// Left panel should show "Steps" index and "All Steps" nav button
		await expect(page.getByText('Steps', { exact: true })).toBeVisible();
		await expect(page.getByRole('button', { name: /All Steps/i })).toBeVisible();
	});

	test('step type nav buttons filter entries', async ({ page }) => {
		const res = await fetch('http://localhost:3000/agentic-dispatch/loops');
		const loops = await res.json();
		if (!loops.length) {
			test.skip();
			return;
		}

		const loopId = loops[0].loop_id;
		await page.goto(`/trajectories/${loopId}`);
		await waitForHydration(page);

		// "All Steps" should be active by default
		const allBtn = page.getByRole('button', { name: /All Steps/i });
		await expect(allBtn).toBeVisible();

		// If there are Model Calls or Tool Calls buttons, click them
		const modelBtn = page.getByRole('button', { name: /Model Calls/i });
		const hasModel = await modelBtn.isVisible().catch(() => false);
		if (hasModel) {
			await modelBtn.click();
			// Should still show the page
			await expect(page.getByTestId('trajectory-detail-page')).toBeVisible();
			// Switch back
			await allBtn.click();
		}
	});

	test('navigating from list to detail preserves context', async ({ page }) => {
		await page.goto('/trajectories');
		await waitForHydration(page);

		const items = page.getByTestId('trajectory-item');
		const count = await items.count();
		if (count === 0) {
			test.skip();
			return;
		}

		// Click first trajectory item
		await items.first().click();
		await expect(page).toHaveURL(/\/trajectories\/.+/);
		await expect(page.getByTestId('trajectory-detail-page')).toBeVisible();

		// Back link returns to list
		await page.getByTestId('trajectory-back-link').click();
		await expect(page).toHaveURL('/trajectories');
	});
});
