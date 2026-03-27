import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, getPlan, waitForGoal } from './helpers/api';
import { MockLLMClient } from './helpers/mock-llm';
import { startExecutionButton, planListItem } from './helpers/selectors';

/**
 * T1 happy-path plan journey: full lifecycle with mock LLM (hello-world scenario).
 *
 * Two-stage approval:
 *   Round 1: drafted → reviewed (pause) → human clicks "Create Requirements" → approved → cascade
 *   Round 2: scenarios_generated → scenarios_reviewed (pause) → human clicks "Approve & Continue" → ready_for_execution
 *
 * Then: Start Execution → implementing → complete → Done filter
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

	test('first approval triggers cascade to scenarios_reviewed', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await page.getByRole('button', { name: /Create Requirements/i }).first().click();

		// Cascade: approved → requirements_generated → scenarios_generated → scenarios_reviewed
		// At scenarios_reviewed, "Approve & Continue" button appears for round 2
		await expect(
			page.getByRole('button', { name: /Approve & Continue/i })
		).toBeVisible({ timeout: 60000 });

		const plan = await getPlan(slug);
		expect(plan.approved).toBe(true);
		expect(plan.stage).toBe('scenarios_reviewed');
	});

	test('requirements panel shows active requirements', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		const center = page.getByTestId('panel-center');
		await expect(center.getByText('Requirements', { exact: true })).toBeVisible();
		await expect(center.getByText(/\d+ active/)).toBeVisible();
	});

	test('second approval advances to ready_for_execution', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		const approveBtn = page.getByRole('button', { name: /Approve & Continue/i });
		await expect(approveBtn).toBeVisible();
		await approveBtn.click();

		// After round 2 promote, "Start Execution" should appear
		await expect(startExecutionButton(page)).toBeVisible({ timeout: 15000 });

		const plan = await getPlan(slug);
		expect(plan.stage).toBe('ready_for_execution');
	});

	test('execute plan triggers execution pipeline', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(startExecutionButton(page)).toBeVisible();
		await startExecutionButton(page).click();

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

		const plansRadio = page.getByRole('radio', { name: 'Plans' });
		if ((await plansRadio.getAttribute('aria-checked')) === 'false') {
			await plansRadio.click();
		}

		await page.getByRole('radio', { name: 'Done' }).click();
		await expect(planListItem(page, slug)).toBeVisible();
	});
});
