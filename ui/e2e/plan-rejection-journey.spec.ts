import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, getPlan, waitForGoal } from './helpers/api';
import { MockLLMClient } from './helpers/mock-llm';
import { startExecutionButton } from './helpers/selectors';

/**
 * T1 rejection-variant plan journey: plan rejected by reviewer once, then approved.
 *
 * One plan, serial steps. Mock LLM reset to plan-reject once in
 * beforeAll — fixtures are consumed sequentially through the retry cycle.
 *
 * Flow:
 *   1. Reset mock LLM to plan-reject (once)
 *   2. Create plan, wait for goal synthesis
 *   3. Approve → reviewer rejects → retry → reaches scenarios_reviewed
 *   4. Second promote via UI → reaches ready_for_execution
 *
 * Pattern: waitForResponse confirms API calls before asserting UI state.
 */
test.describe('@t1 @rejection plan-rejection-journey', () => {
	const mockLLM = new MockLLMClient();
	let slug: string;

	test.describe.configure({ mode: 'serial' });

	test.beforeAll(async () => {
		await mockLLM.waitForHealthy();
		await mockLLM.resetScenario('plan-reject');
		const plan = await createPlan(`Rejection journey test ${Date.now()}`);
		slug = plan.slug;
		await waitForGoal(slug, 30000);
	});

	test.afterAll(async () => {
		if (slug) await deletePlan(slug).catch(() => {});
	});

	test('approve triggers rejection then recovery to scenarios_reviewed', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		// Click "Create Requirements" and wait for promote API response
		const createReqBtn = page.getByRole('button', { name: /Create Requirements/i }).first();
		await Promise.all([
			page.waitForResponse((r) => r.url().includes('/promote') && r.status() === 200),
			createReqBtn.click()
		]);

		// Mock reviewer rejects first, then approves. Full cascade completes with scenarios.
		// Poll API and reload if SSE missed events (mock LLM is fast).
		const start = Date.now();
		while (Date.now() - start < 90000) {
			const approveBtn = page.getByRole('button', { name: /Approve & Continue/i });
			if (await approveBtn.isVisible().catch(() => false)) break;

			const plan = await getPlan(slug);
			if (plan.stage === 'scenarios_reviewed') {
				await page.reload();
				await waitForHydration(page);
				break;
			}
			await new Promise((r) => setTimeout(r, 1000));
		}

		await expect(
			page.getByRole('button', { name: /Approve & Continue/i })
		).toBeVisible({ timeout: 10000 });

		const plan = await getPlan(slug);
		expect(plan.approved).toBe(true);
		expect(plan.stage).toBe('scenarios_reviewed');
	});

	test('second promote reaches ready_for_execution', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		const approveBtn = page.getByRole('button', { name: /Approve & Continue/i });
		await expect(approveBtn).toBeVisible();

		// Click and wait for promote API response
		await Promise.all([
			page.waitForResponse((r) => r.url().includes('/promote') && r.status() === 200),
			approveBtn.click()
		]);

		await expect(startExecutionButton(page)).toBeVisible({ timeout: 15000 });

		const plan = await getPlan(slug);
		expect(plan.stage).toBe('ready_for_execution');
	});
});
