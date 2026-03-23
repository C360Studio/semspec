import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, getPlan, promotePlan } from './helpers/api';
import { startExecutionButton, planListItem } from './helpers/selectors';

/**
 * @easy tier: health-check scenario with real LLM.
 *
 * Plan prompt: Add a /health endpoint to a Go HTTP service.
 * Exercises the full UI flow: create → approve → cascade → review scenarios →
 * approve scenarios → execute → complete.
 *
 * Requires:
 * - Real LLM (Ollama, OpenRouter, Claude) configured via LLM_API_URL
 * - UI E2E stack started with: task e2e:ui:up
 * - Workspace initialized with Go project files + .semspec/projects/default/project.json
 *
 * Run with: task e2e:ui:test:llm
 * Or manually: LLM_API_URL=... npx playwright test --grep @easy
 */

const PLAN_PROMPT = `Add a /health endpoint to the Go HTTP service. The endpoint should return JSON with:
"status": "ok", "uptime": seconds since server start, "version": Go runtime version.
Include unit tests for the health handler.`;

// Real LLM timeouts — significantly longer than mock
const CASCADE_TIMEOUT = 300_000; // 5 min for planning + review + requirements + scenarios
const EXECUTION_TIMEOUT = 600_000; // 10 min for execution pipeline
const STAGE_POLL_INTERVAL = 2_000;

test.describe('@easy @happy-path plan-lifecycle-llm', () => {
	let slug: string;

	test.describe.configure({ mode: 'serial' });

	// Use longer per-test timeout for real LLM
	test.setTimeout(EXECUTION_TIMEOUT);

	test.beforeAll(async () => {
		const plan = await createPlan(PLAN_PROMPT);
		slug = plan.slug;
	});

	test.afterAll(async () => {
		if (slug) await deletePlan(slug).catch(() => {});
	});

	test('plan created with goal', async () => {
		// Poll until the plan has a non-empty goal (planner has completed first pass)
		const start = Date.now();
		let plan = await getPlan(slug);
		while (!plan.goal && Date.now() - start < 120_000) {
			await new Promise((r) => setTimeout(r, STAGE_POLL_INTERVAL));
			plan = await getPlan(slug);
		}
		expect(plan.goal).toBeTruthy();
	});

	test('approve plan and wait for cascade', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		// Click approve — triggers planning → review → requirements → scenarios
		await page.getByRole('button', { name: /Approve Plan/i }).first().click();

		// Wait for "Approve & Continue" button (cascade complete, scenarios generated)
		await expect(
			page.getByRole('button', { name: /Approve & Continue/i })
		).toBeVisible({ timeout: CASCADE_TIMEOUT });

		// Verify backend state
		const plan = await getPlan(slug);
		expect(plan.approved).toBe(true);
		expect(plan.stage).toBe('scenarios_generated');
	});

	test('plan has requirements and scenarios', async () => {
		// Verify the LLM produced meaningful output
		const res = await fetch(`http://localhost:3000/plan-api/plans/${slug}/requirements`);
		const requirements = await res.json();
		expect(requirements.length).toBeGreaterThan(0);

		// Check at least one requirement has scenarios
		const scenRes = await fetch(`http://localhost:3000/plan-api/plans/${slug}/scenarios`);
		const scenarios = await scenRes.json();
		expect(scenarios.length).toBeGreaterThan(0);
	});

	test('approve scenarios and reach ready_for_execution', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		// Click "Approve & Continue" for second approval gate
		await page.getByRole('button', { name: /Approve & Continue/i }).click();

		// Wait for "Start Execution" to appear
		await expect(startExecutionButton(page)).toBeVisible({ timeout: 30_000 });

		const plan = await getPlan(slug);
		expect(plan.stage).toBe('ready_for_execution');
	});

	test('execute plan', async ({ page }) => {
		await page.goto(`/plans/${slug}`);
		await waitForHydration(page);

		await expect(startExecutionButton(page)).toBeVisible();
		await startExecutionButton(page).click();

		// Wait for execution to complete — poll backend since UI may not show all states
		const start = Date.now();
		let plan = await getPlan(slug);
		while (
			!['complete', 'failed'].includes(plan.stage) &&
			Date.now() - start < EXECUTION_TIMEOUT
		) {
			await new Promise((r) => setTimeout(r, STAGE_POLL_INTERVAL));
			plan = await getPlan(slug);
		}

		// Log final stage for debugging
		console.log(`[easy] Plan ${slug} final stage: ${plan.stage}`);

		// In real LLM mode, execution may fail due to code quality issues.
		// We assert the pipeline ran (not stuck at ready_for_execution).
		expect(['implementing', 'executing', 'reviewing_rollup', 'complete', 'failed']).toContain(
			plan.stage
		);
	});
});
