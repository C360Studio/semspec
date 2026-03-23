import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, getPlan, promotePlan } from './helpers/api';
import { MockLLMClient } from './helpers/mock-llm';
import { startExecutionButton, planListItem } from './helpers/selectors';

/**
 * Full plan lifecycle: create → approve → cascade → approve scenarios → execute → complete.
 * Requires mock LLM (hello-world scenario).
 *
 * Two-stage approval: promote #1 approves plan, promote #2 approves scenarios.
 * Serial: each step depends on the previous.
 */
test.describe('@mock @happy-path plan-lifecycle', () => {
	let slug: string;

	test.describe.configure({ mode: 'serial' });

	test.beforeAll(async () => {
		const mockLLM = new MockLLMClient();
		await mockLLM.resetScenario('hello-world');
		const plan = await createPlan(`Lifecycle test ${Date.now()}`);
		slug = plan.slug;
	});

	test.afterAll(async () => {
		if (slug) await deletePlan(slug).catch(() => {});
	});

	test('plan detail shows approve button', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(page.getByRole('button', { name: /Approve Plan/i }).first()).toBeVisible();
	});

	test('approve triggers cascade to scenarios_generated', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await page.getByRole('button', { name: /Approve Plan/i }).first().click();

		// UI shows "Approve & Continue" when cascade reaches scenarios_generated
		await expect(
			page.getByRole('button', { name: /Approve & Continue/i })
		).toBeVisible({ timeout: 60000 });

		// Backend at scenarios_generated, awaiting 2nd promote
		const plan = await getPlan(slug);
		expect(plan.stage).toBe('scenarios_generated');
	});

	test('second promote advances to ready_for_execution', async () => {
		await promotePlan(slug);
		let plan = await getPlan(slug);
		const start = Date.now();
		while (plan.stage !== 'ready_for_execution' && Date.now() - start < 15000) {
			await new Promise((r) => setTimeout(r, 500));
			plan = await getPlan(slug);
		}
		expect(plan.stage).toBe('ready_for_execution');
	});

	test('execute plan triggers execution pipeline', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(startExecutionButton(page)).toBeVisible();
		await startExecutionButton(page).click();

		// Verify the pipeline advances past ready_for_execution
		const start = Date.now();
		let plan = await getPlan(slug);
		while (plan.stage === 'ready_for_execution' && Date.now() - start < 30000) {
			await new Promise((r) => setTimeout(r, 1000));
			plan = await getPlan(slug);
		}
		expect(['implementing', 'executing', 'reviewing_rollup', 'complete']).toContain(plan.stage);
	});

	// TODO: Execution stalls at reviewing_rollup — rollup review mock fixture needed
	test.skip('execution reaches complete', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(
			page.getByTestId('panel-center').locator('[data-stage="complete"]')
		).toBeVisible({ timeout: 90000 });

		const plan = await getPlan(slug);
		expect(plan.stage).toBe('complete');
	});

	test.skip('completed plan shows in Done filter', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		await page.getByRole('radio', { name: 'Done' }).click();
		await expect(planListItem(page, slug)).toBeVisible();
	});
});
