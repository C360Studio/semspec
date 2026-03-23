import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, getPlan, promotePlan } from './helpers/api';
import { MockLLMClient } from './helpers/mock-llm';
import { startExecutionButton } from './helpers/selectors';

/**
 * Plan review UX: the second approval gate at scenarios_generated.
 * Tests the "Approve & Continue" button, requirement panel, and deprecated section.
 *
 * Serial: shares a plan through the two-stage approval flow.
 */
test.describe('@mock @happy-path plan-review', () => {
	const mockLLM = new MockLLMClient();
	let slug: string;

	test.describe.configure({ mode: 'serial' });

	test.beforeAll(async () => {
		await mockLLM.resetScenario('hello-world');
		const plan = await createPlan(`Review UX test ${Date.now()}`);
		slug = plan.slug;

		// Drive the plan to scenarios_generated via API
		await promotePlan(slug);
		// Wait for cascade
		const start = Date.now();
		let plan2 = await getPlan(slug);
		while (plan2.stage !== 'scenarios_generated' && Date.now() - start < 30000) {
			await new Promise((r) => setTimeout(r, 500));
			plan2 = await getPlan(slug);
		}
	});

	test.afterAll(async () => {
		if (slug) await deletePlan(slug).catch(() => {});
	});

	test('shows Approve & Continue button at scenarios_generated', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(
			page.getByRole('button', { name: /Approve & Continue/i })
		).toBeVisible();

		// "Start Execution" should NOT be visible yet
		await expect(startExecutionButton(page)).not.toBeVisible();
	});

	test('guidance text prompts review', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(page.getByText(/Review the requirements and scenarios/i)).toBeVisible();
	});

	test('requirements panel shows active requirements', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		// Requirements heading should be visible in center panel
		const center = page.getByTestId('panel-center');
		await expect(center.getByText('Requirements', { exact: true })).toBeVisible();

		// Should show active count badge
		await expect(center.getByText(/\d+ active/)).toBeVisible();
	});

	test('clicking Approve & Continue calls second promote', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		const approveBtn = page.getByRole('button', { name: /Approve & Continue/i });
		await expect(approveBtn).toBeVisible();
		await approveBtn.click();

		// After second promote, "Start Execution" should appear
		await expect(startExecutionButton(page)).toBeVisible({ timeout: 15000 });

		// Backend should be at ready_for_execution
		const plan = await getPlan(slug);
		expect(plan.stage).toBe('ready_for_execution');
	});
});
