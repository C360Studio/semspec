import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { connectionStatus } from './helpers/selectors';

test.describe('@mock @smoke health check', () => {
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
		// Switch to Plans mode if Feed is active (auto-switches when loops exist)
		const plansRadio = page.getByRole('radio', { name: 'Plans' });
		if ((await plansRadio.getAttribute('aria-checked')) === 'false') {
			await plansRadio.click();
		}
		// The "+" button is a link with title "New Plan"
		await page.getByTitle('New Plan').click();
		await expect(page).toHaveURL('/plans/new');
		await expect(page.getByLabel('What do you want to build?')).toBeVisible();
	});
});
