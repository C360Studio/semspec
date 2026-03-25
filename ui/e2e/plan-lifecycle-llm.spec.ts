import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, getPlan, promotePlan } from './helpers/api';
import { startExecutionButton } from './helpers/selectors';

/**
 * @easy tier: health-check scenario with real LLM.
 *
 * Exercises the full plan flow with a real LLM provider.
 * Handles both auto_approve=true (cascade runs automatically) and
 * auto_approve=false (human clicks Create Requirements in UI).
 *
 * Run with: task e2e:ui:test:llm
 * Or: PLAYWRIGHT_TIMEOUT=600000 npx playwright test plan-lifecycle-llm.spec.ts --project cascade --no-deps
 */

const PLAN_PROMPT = `Add a /health endpoint to the Go HTTP service. The endpoint should return JSON with:
"status": "ok", "uptime": seconds since server start, "version": Go runtime version.
Include unit tests for the health handler.`;

const CASCADE_TIMEOUT = 300_000;
const EXECUTION_TIMEOUT = 600_000;
const POLL_INTERVAL = 3_000;

test.describe('@easy @happy-path plan-lifecycle-llm', () => {
	let slug: string;

	test.describe.configure({ mode: 'serial' });
	test.setTimeout(EXECUTION_TIMEOUT);

	test.beforeAll(async () => {
		// Append timestamp to avoid slug collision with previous runs
		const plan = await createPlan(`${PLAN_PROMPT}\n\nTest run: ${Date.now()}`);
		slug = plan.slug;
	});

	test.afterAll(async () => {
		if (slug) await deletePlan(slug).catch(() => {});
	});

	test('plan created with goal', async () => {
		const start = Date.now();
		let plan = await getPlan(slug);
		while (!plan.goal && Date.now() - start < 120_000) {
			await new Promise((r) => setTimeout(r, POLL_INTERVAL));
			plan = await getPlan(slug);
		}
		expect(plan.goal).toBeTruthy();
		console.log(`[easy] Goal generated in ${((Date.now() - start) / 1000).toFixed(1)}s`);
	});

	test('plan reaches scenarios_generated', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		// Check if plan needs manual approval (auto_approve=false)
		// or if cascade already ran (auto_approve=true)
		let plan = await getPlan(slug);

		if (!plan.approved) {
			// Manual approval needed — click Create Requirements button
			console.log('[easy] Manual approval flow — clicking Create Requirements');
			await page.getByRole('button', { name: /Create Requirements/i }).first().click();
		} else {
			console.log(`[easy] Plan already approved (auto_approve=true), stage=${plan.stage}`);
		}

		// Wait for cascade to reach scenarios_generated
		// The button visible depends on the stage:
		// - scenarios_generated: "Approve & Continue"
		// - ready_for_execution: "Start Execution"
		const start = Date.now();
		while (Date.now() - start < CASCADE_TIMEOUT) {
			plan = await getPlan(slug);
			if (['scenarios_generated', 'ready_for_execution'].includes(plan.stage)) break;
			await new Promise((r) => setTimeout(r, POLL_INTERVAL));
		}

		console.log(`[easy] Cascade complete: stage=${plan.stage} in ${((Date.now() - start) / 1000).toFixed(1)}s`);
		expect(['scenarios_generated', 'ready_for_execution']).toContain(plan.stage);
	});

	test('plan has requirements and scenarios', async () => {
		const reqRes = await fetch(`http://localhost:3000/plan-manager/plans/${slug}/requirements`);
		const requirements = await reqRes.json();
		expect(requirements.length).toBeGreaterThan(0);
		console.log(`[easy] ${requirements.length} requirements generated`);

		const scenRes = await fetch(`http://localhost:3000/plan-manager/plans/${slug}/scenarios`);
		const scenarios = await scenRes.json();
		expect(scenarios.length).toBeGreaterThan(0);
		console.log(`[easy] ${scenarios.length} scenarios generated`);
	});

	test('advance to ready_for_execution and execute', async ({ page }) => {
		let plan = await getPlan(slug);

		// Second promote if needed (scenarios_generated → ready_for_execution)
		if (plan.stage === 'scenarios_generated') {
			await promotePlan(slug);
			const start = Date.now();
			while (plan.stage !== 'ready_for_execution' && Date.now() - start < 30_000) {
				await new Promise((r) => setTimeout(r, 1000));
				plan = await getPlan(slug);
			}
		}
		expect(plan.stage).toBe('ready_for_execution');

		// Navigate and execute
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);
		await expect(startExecutionButton(page)).toBeVisible();
		await startExecutionButton(page).click();

		// Wait for execution to advance
		const start = Date.now();
		plan = await getPlan(slug);
		while (
			!['implementing', 'executing', 'reviewing_rollup', 'complete', 'failed'].includes(plan.stage) &&
			Date.now() - start < 30_000
		) {
			await new Promise((r) => setTimeout(r, POLL_INTERVAL));
			plan = await getPlan(slug);
		}

		console.log(`[easy] Execution triggered: stage=${plan.stage}`);
		expect(['implementing', 'executing', 'reviewing_rollup', 'complete', 'failed']).toContain(plan.stage);
	});

	test('execution progresses', async () => {
		const start = Date.now();
		let plan = await getPlan(slug);
		let lastStage = plan.stage;

		while (
			!['complete', 'failed'].includes(plan.stage) &&
			Date.now() - start < EXECUTION_TIMEOUT
		) {
			await new Promise((r) => setTimeout(r, POLL_INTERVAL * 2));
			plan = await getPlan(slug);
			if (plan.stage !== lastStage) {
				console.log(`[easy] Stage: ${lastStage} → ${plan.stage} (${((Date.now() - start) / 1000).toFixed(0)}s)`);
				lastStage = plan.stage;
			}
		}

		console.log(`[easy] Final stage: ${plan.stage} in ${((Date.now() - start) / 1000).toFixed(1)}s`);
		// With real LLM, execution may fail — assert pipeline ran, not that it succeeded
		expect(['implementing', 'executing', 'reviewing_rollup', 'complete', 'failed']).toContain(plan.stage);
	});
});
