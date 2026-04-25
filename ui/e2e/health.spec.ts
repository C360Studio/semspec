import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { connectionStatus } from './helpers/selectors';

test.describe('@t0 @smoke health check', () => {
	test('page loads and hydrates', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);
	});

	test('shows connected status', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);
		await expect(connectionStatus(page, 'Connected')).toBeVisible();
	});

	test('left panel mode switcher is visible', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);
		// Both Plans and Feed radio buttons should be present
		// (which is active depends on whether loops are running)
		await expect(page.getByRole('radio', { name: 'Plans' })).toBeVisible();
		await expect(page.getByRole('radio', { name: 'Feed' })).toBeVisible();
	});

	test('navigates to new plan form', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);
		// LeftPanel auto-switches to Feed mode when loops exist (c50c87f).
		// Under mock stack, earlier @t0 specs create plans that spawn loops, so
		// this test routinely lands on Feed. Click Plans and wait for the
		// aria-checked flip before asserting on Plans-mode UI — otherwise the
		// getByTitle('New Plan') query hits a stale DOM where PlansList isn't
		// mounted yet.
		const plansRadio = page.getByRole('radio', { name: 'Plans' });
		if ((await plansRadio.getAttribute('aria-checked')) !== 'true') {
			await plansRadio.click();
			await expect(plansRadio).toHaveAttribute('aria-checked', 'true');
		}
		await page.getByTitle('New Plan').click();
		await expect(page).toHaveURL('/plans/new');
		await expect(page.getByLabel('What do you want to build?')).toBeVisible();
	});
});
