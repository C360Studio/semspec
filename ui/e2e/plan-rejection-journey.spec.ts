import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, getPlan, promotePlan, waitForGoal } from './helpers/api';
import { MockLLMClient } from './helpers/mock-llm';

/**
 * T1 rejection-variant plan journey: plan rejected by reviewer once, then approved.
 *
 * One plan, serial steps. Mock LLM reset to hello-world-plan-rejection once in
 * beforeAll — fixtures are consumed sequentially through the retry cycle.
 *
 * Flow:
 *   1. Reset mock LLM to hello-world-plan-rejection (once)
 *   2. Create plan, wait for goal synthesis
 *   3. Approve → reviewer rejects → retry → reaches scenarios_generated
 *   4. Second promote → reaches ready_for_execution
 */
test.describe('@t1 @rejection plan-rejection-journey', () => {
	const mockLLM = new MockLLMClient();
	let slug: string;

	test.describe.configure({ mode: 'serial' });

	test.beforeAll(async () => {
		await mockLLM.waitForHealthy();
		await mockLLM.resetScenario('hello-world-plan-rejection');
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

		await page.getByRole('button', { name: /Create Requirements/i }).first().click();

		// Mock reviewer rejects first, then approves. Full cycle completes with scenarios.
		// With mock LLM the cascade can complete before the SSE connection is established,
		// so we poll the API and reload if the stage advances before the UI catches up.
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

	test('second promote reaches ready_for_execution', async () => {
		await promotePlan(slug);
		let plan = await getPlan(slug);
		const start = Date.now();
		while (plan.stage !== 'ready_for_execution' && Date.now() - start < 15000) {
			await new Promise((r) => setTimeout(r, 500));
			plan = await getPlan(slug);
		}
		expect(plan.stage).toBe('ready_for_execution');
	});
});
