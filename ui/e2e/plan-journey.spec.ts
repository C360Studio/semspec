import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, getPlan, promotePlan, waitForGoal } from './helpers/api';
import { MockLLMClient } from './helpers/mock-llm';
import { startExecutionButton, planListItem } from './helpers/selectors';

/**
 * T1 happy-path plan journey: full lifecycle with mock LLM (hello-world scenario).
 *
 * One plan, one journey, serial steps. Mock LLM reset once in beforeAll — fixtures
 * are consumed sequentially as the plan progresses. No mid-journey resets.
 *
 * Flow:
 *   1. Reset mock LLM to hello-world (once)
 *   2. Create plan, wait for goal synthesis
 *   3. Verify "Create Requirements" button on plan detail
 *   4. Click approve → wait for cascade → verify scenarios_generated
 *   5. Verify requirements panel shows active requirements
 *   6. Verify "Approve & Continue" button visible
 *   7. Click "Approve & Continue" → verify ready_for_execution
 *   8. Verify "Start Execution" button visible
 *   9. Click "Start Execution" → verify execution pipeline triggers
 *  10. Wait for complete
 *  11. Verify plan shows in Done filter
 */
test.describe('@t1 @happy-path plan-journey', () => {
	const mockLLM = new MockLLMClient();
	let slug: string;

	test.describe.configure({ mode: 'serial' });

	test.beforeAll(async () => {
		await mockLLM.waitForHealthy();
		await mockLLM.resetScenario('hello-world');
		const plan = await createPlan(`Journey test ${Date.now()}`);
		slug = plan.slug;
		await waitForGoal(slug, 30000);
	});

	test.afterAll(async () => {
		if (slug) await deletePlan(slug).catch(() => {});
	});

	test('plan detail shows Create Requirements button', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(page.getByRole('button', { name: /Create Requirements/i }).first()).toBeVisible();
	});

	test('approve triggers cascade to scenarios_generated', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await page.getByRole('button', { name: /Create Requirements/i }).first().click();

		// UI shows "Approve & Continue" when cascade reaches scenarios_generated
		await expect(
			page.getByRole('button', { name: /Approve & Continue/i })
		).toBeVisible({ timeout: 60000 });

		const plan = await getPlan(slug);
		expect(plan.approved).toBe(true);
		expect(plan.stage).toBe('scenarios_generated');
	});

	test('requirements panel shows active requirements', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		const center = page.getByTestId('panel-center');
		await expect(center.getByText('Requirements', { exact: true })).toBeVisible();
		await expect(center.getByText(/\d+ active/)).toBeVisible();
	});

	test('Approve & Continue button visible at scenarios_generated', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(
			page.getByRole('button', { name: /Approve & Continue/i })
		).toBeVisible();

		// "Start Execution" should NOT be visible yet
		await expect(startExecutionButton(page)).not.toBeVisible();
	});

	test('second approval advances to ready_for_execution', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		const approveBtn = page.getByRole('button', { name: /Approve & Continue/i });
		await expect(approveBtn).toBeVisible();
		await approveBtn.click();

		// After second promote, "Start Execution" should appear
		await expect(startExecutionButton(page)).toBeVisible({ timeout: 15000 });

		const plan = await getPlan(slug);
		expect(plan.stage).toBe('ready_for_execution');
	});

	test('Start Execution button visible', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(page.locator('[data-stage="ready_for_execution"]').first()).toBeVisible();
		await expect(startExecutionButton(page)).toBeVisible();
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

	test('execution reaches complete', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(
			page.getByTestId('panel-center').locator('[data-stage="complete"]')
		).toBeVisible({ timeout: 90000 });

		const plan = await getPlan(slug);
		expect(plan.stage).toBe('complete');
	});

	test('completed plan shows in Done filter', async ({ page }) => {
		await page.goto('/');
		await waitForHydration(page);

		// Ensure Plans mode (may auto-switch to Feed when loops are active)
		const plansRadio = page.getByRole('radio', { name: 'Plans' });
		if ((await plansRadio.getAttribute('aria-checked')) === 'false') {
			await plansRadio.click();
		}

		await page.getByRole('radio', { name: 'Done' }).click();
		await expect(planListItem(page, slug)).toBeVisible();
	});
});
