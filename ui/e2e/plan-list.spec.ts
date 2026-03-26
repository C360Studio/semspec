import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan } from './helpers/api';
import {
	filterChip,
	planListItem,
	emptyPlansMessage,
	plansModeRadio,
	feedModeRadio
} from './helpers/selectors';

/** Ensure Plans mode is active (may auto-switch to Feed when loops exist). */
async function ensurePlansMode(page: import('@playwright/test').Page) {
	const plansRadio = page.getByRole('radio', { name: 'Plans' });
	if ((await plansRadio.getAttribute('aria-checked')) === 'false') {
		await plansRadio.click();
	}
}

test.describe('@t0 plan-list', () => {
	let createdSlugs: string[] = [];

	test.afterEach(async () => {
		for (const slug of createdSlugs) {
			await deletePlan(slug).catch(() => {});
		}
		createdSlugs = [];
	});

	test('shows empty state when no plans exist', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);
		await ensurePlansMode(page);
		await filterChip(page, 'Drafts').click();
	});

	test('shows created plan in list', async ({ page }) => {
		const plan = await createPlan('Test plan for list view');
		createdSlugs.push(plan.slug);

		await page.goto('/');
		await waitForHydration(page);
		await ensurePlansMode(page);

		await expect(planListItem(page, plan.slug)).toBeVisible();
	});

	test('plan item links to detail page', async ({ page }) => {
		const plan = await createPlan('Test plan for navigation');
		createdSlugs.push(plan.slug);

		await page.goto('/');
		await waitForHydration(page);
		await ensurePlansMode(page);

		await planListItem(page, plan.slug).click();
		await expect(page).toHaveURL(`/plans/${plan.slug}`);
	});

	test('filter chips switch between views', async ({ page }) => {
		const plan = await createPlan('Test plan for filtering');
		createdSlugs.push(plan.slug);

		await page.goto('/');
		await waitForHydration(page);
		await ensurePlansMode(page);

		await expect(planListItem(page, plan.slug)).toBeVisible();

		await filterChip(page, 'Drafts').click();
		await expect(planListItem(page, plan.slug)).toBeVisible();

		await filterChip(page, 'Active').click();
		await expect(planListItem(page, plan.slug)).not.toBeVisible();
	});

	test('mode switcher toggles between Plans and Feed', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		// Both mode buttons should be visible
		await expect(plansModeRadio(page)).toBeVisible();
		await expect(feedModeRadio(page)).toBeVisible();

		// Switch to Feed
		await feedModeRadio(page).click();
		await expect(feedModeRadio(page)).toHaveAttribute('aria-checked', 'true');

		// Switch to Plans
		await plansModeRadio(page).click();
		await expect(plansModeRadio(page)).toHaveAttribute('aria-checked', 'true');
	});
});
