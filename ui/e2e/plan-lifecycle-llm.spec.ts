import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, executePlan, getPlan, promotePlan, waitForGoal } from './helpers/api';

/**
 * @easy tier: health-check scenario with real LLM.
 *
 * Exercises the full plan flow with a real LLM provider.
 * Handles both auto_approve=true (cascade runs automatically) and
 * auto_approve=false (poll for approval gates and promote via API).
 *
 * The cascade polling loop handles slow local LLMs (Ollama) where the
 * plan may sit in drafting/reviewing_draft for many minutes before
 * reaching an actionable approval gate.
 *
 * Run with: task e2e:ui:test:llm
 * Or: PLAYWRIGHT_TIMEOUT=600000 npx playwright test plan-lifecycle-llm.spec.ts --project t2 --no-deps
 */

const PLAN_PROMPT = `Add a /health endpoint to the Go HTTP service. The endpoint should return JSON with:
"status": "ok", "uptime": seconds since server start, "version": Go runtime version.
Include unit tests for the health handler.`;

const CASCADE_TIMEOUT = Number(process.env.CASCADE_TIMEOUT) || 600_000;
const EXECUTION_TIMEOUT = Number(process.env.EXECUTION_TIMEOUT) || 2_400_000;
const POLL_INTERVAL = 3_000;

/** Stages where the cascade is done and we can proceed to assertions. */
const CASCADE_DONE_STAGES = [
	'scenarios_generated', 'scenarios_reviewed', 'ready_for_execution',
	'implementing', 'executing', 'reviewing_rollup', 'complete',
];

/** Stages where the plan is terminally broken. */
const TERMINAL_FAILURE_STAGES = ['rejected', 'failed'];

test.describe('@t2 @easy plan-lifecycle-llm', () => {
	let slug: string;

	test.describe.configure({ mode: 'serial' });
	test.setTimeout(EXECUTION_TIMEOUT);

	test.beforeAll(async () => {
		// Append timestamp to avoid slug collision with previous runs
		const plan = await createPlan(`${PLAN_PROMPT}\n\nTest run: ${Date.now()}`);
		slug = plan.slug;
		await waitForGoal(slug, 300_000);
	});

	test.afterAll(async () => {
		// Keep plan and artifacts when DEBUG=1 for post-run inspection
		if (process.env.DEBUG) return;
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

		// Poll for cascade completion. With auto_approve=false, the plan stops
		// at approval gates that require human action. We promote via API at each
		// gate rather than clicking UI buttons — this is more reliable with slow
		// LLMs where SSE events and button rendering can race.
		const start = Date.now();
		let lastStage = '';

		while (Date.now() - start < CASCADE_TIMEOUT) {
			const plan = await getPlan(slug);

			if (plan.stage !== lastStage) {
				console.log(`[easy] Stage: ${lastStage || '(start)'} → ${plan.stage} (${((Date.now() - start) / 1000).toFixed(0)}s)`);
				lastStage = plan.stage;
			}

			// Terminal failure — bail immediately.
			if (TERMINAL_FAILURE_STAGES.includes(plan.stage)) {
				throw new Error(`Plan entered terminal failure stage: ${plan.stage}`);
			}

			// Cascade complete — done.
			if (CASCADE_DONE_STAGES.includes(plan.stage)) break;

			// Approval gate: plan reviewed but not yet approved.
			// Only promote at 'reviewed' — 'ready_for_approval' includes
			// 'reviewing_draft' (reviewer still running), which 409s on promote.
			if (!plan.approved && plan.stage === 'reviewed') {
				console.log(`[easy] Promoting at approval gate (stage=${plan.stage})`);
				await promotePlan(slug);
			}

			await new Promise((r) => setTimeout(r, POLL_INTERVAL));
		}

		const plan = await getPlan(slug);
		console.log(`[easy] Cascade complete: stage=${plan.stage} in ${((Date.now() - start) / 1000).toFixed(1)}s`);
		expect(CASCADE_DONE_STAGES).toContain(plan.stage);
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

	test('advance to ready_for_execution and execute', async () => {
		let plan = await getPlan(slug);

		// Second promote if needed (scenarios_reviewed → ready_for_execution)
		if (['scenarios_generated', 'scenarios_reviewed'].includes(plan.stage)) {
			console.log(`[easy] Round 2 approval: promoting from ${plan.stage}`);
			await promotePlan(slug);
			const start = Date.now();
			while (plan.stage !== 'ready_for_execution' && Date.now() - start < 30_000) {
				await new Promise((r) => setTimeout(r, 1000));
				plan = await getPlan(slug);
			}
		}

		// If already executing from auto-cascade, skip.
		if (['implementing', 'executing', 'reviewing_rollup', 'complete'].includes(plan.stage)) {
			console.log(`[easy] Already executing: stage=${plan.stage}`);
			return;
		}

		expect(plan.stage).toBe('ready_for_execution');

		// Trigger execution via API helper. The UI button click can drop the HTTP
		// connection before the JetStream publish completes, leaving the plan stuck.
		// See docs/bugs/execute-context-canceled.md — use API call as workaround.
		console.log('[easy] Triggering execution via API');
		plan = await executePlan(slug);

		// Wait for execution to advance
		const start = Date.now();
		while (
			!['implementing', 'executing', 'reviewing_rollup', 'complete', 'failed'].includes(plan.stage) &&
			Date.now() - start < 30_000
		) {
			await new Promise((r) => setTimeout(r, POLL_INTERVAL));
			plan = await getPlan(slug);
		}

		console.log(`[easy] Execution triggered: stage=${plan.stage}`);
		expect(['implementing', 'executing', 'reviewing_rollup', 'complete', 'failed', 'rejected']).toContain(plan.stage);
	});

	test('execution progresses', async () => {
		const start = Date.now();
		let plan = await getPlan(slug);
		let lastStage = plan.stage;

		while (
			!['complete', 'failed', 'rejected'].includes(plan.stage) &&
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
		expect(['implementing', 'executing', 'reviewing_rollup', 'complete', 'failed', 'rejected']).toContain(plan.stage);
	});
});
