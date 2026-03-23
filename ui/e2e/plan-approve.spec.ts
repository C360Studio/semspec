import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, getPlan, promotePlan } from './helpers/api';
import { MockLLMClient } from './helpers/mock-llm';
import { startExecutionButton } from './helpers/selectors';

/**
 * Plan approval + cascade flow (two-stage approval).
 * Uses mock LLM (hello-world scenario).
 *
 * Flow: promote #1 (approve plan) → cascade → scenarios_generated
 *       promote #2 (approve scenarios) → ready_for_execution
 *
 * Serial: the cascade consumes mock LLM fixtures, so tests must
 * share a single plan and run in order.
 */
test.describe('@mock @happy-path plan-approve', () => {
	let slug: string;

	test.describe.configure({ mode: 'serial' });

	test.beforeAll(async () => {
		const mockLLM = new MockLLMClient();
		await mockLLM.resetScenario('hello-world');
		const plan = await createPlan(`Approve flow test ${Date.now()}`);
		slug = plan.slug;
	});

	test.afterAll(async () => {
		if (slug) await deletePlan(slug).catch(() => {});
	});

	test('shows Approve Plan button for draft plan', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(page.getByRole('button', { name: /Approve Plan/i }).first()).toBeVisible();
	});

	test('first approval triggers cascade to scenarios_generated', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await page.getByRole('button', { name: /Approve Plan/i }).first().click();

		// UI shows "Approve & Continue" when cascade reaches scenarios_generated
		await expect(
			page.getByRole('button', { name: /Approve & Continue/i })
		).toBeVisible({ timeout: 60000 });

		// Backend should be at scenarios_generated (awaiting 2nd approval)
		const plan = await getPlan(slug);
		expect(plan.approved).toBe(true);
		expect(plan.stage).toBe('scenarios_generated');
	});

	test('second approval advances to ready_for_execution', async () => {
		// The backend requires a second promote to approve requirements/scenarios
		// TODO: UI needs a "Review & Approve Scenarios" button at scenarios_generated
		await promotePlan(slug);

		let plan = await getPlan(slug);
		const start = Date.now();
		while (plan.stage !== 'ready_for_execution' && Date.now() - start < 15000) {
			await new Promise((r) => setTimeout(r, 500));
			plan = await getPlan(slug);
		}
		expect(plan.stage).toBe('ready_for_execution');
	});

	test('stage badge shows ready to execute', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(page.locator('[data-stage="ready_for_execution"]').first()).toBeVisible();
		await expect(startExecutionButton(page)).toBeVisible();
	});
});
