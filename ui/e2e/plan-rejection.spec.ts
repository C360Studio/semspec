import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, getPlan, promotePlan } from './helpers/api';
import { MockLLMClient } from './helpers/mock-llm';
import { startExecutionButton } from './helpers/selectors';

/**
 * Plan rejection flow: plan is rejected by reviewer once, then approved on retry.
 * Switches mock LLM to hello-world-plan-rejection scenario before running.
 * Two-stage approval: promote #1 (plan) → reject → retry → promote #2 (scenarios).
 */
test.describe('@mock @rejection plan-rejection', () => {
	const mockLLM = new MockLLMClient();
	let slug: string;

	test.describe.configure({ mode: 'serial' });

	test.beforeAll(async () => {
		await mockLLM.resetScenario('hello-world-plan-rejection');
		const plan = await createPlan(`Rejection test ${Date.now()}`);
		slug = plan.slug;
	});

	test.afterAll(async () => {
		await mockLLM.resetScenario('hello-world');
		if (slug) await deletePlan(slug).catch(() => {});
	});

	test('plan recovers from rejection and reaches scenarios_generated', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await page.getByRole('button', { name: /Approve Plan/i }).first().click();

		// Mock reviewer rejects first, then approves. Full cycle completes with scenarios.
		await expect(
			page.getByRole('button', { name: /Approve & Continue/i })
		).toBeVisible({ timeout: 90000 });

		const plan = await getPlan(slug);
		expect(plan.approved).toBe(true);
		expect(plan.stage).toBe('scenarios_generated');
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
