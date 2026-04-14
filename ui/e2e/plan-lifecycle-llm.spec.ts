import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { createPlan, deletePlan, executePlan, getPlan, promotePlan, waitForGoal } from './helpers/api';
import type { PlanResponse } from './helpers/api';

/**
 * @easy tier: health-check scenario with real LLM.
 *
 * Exercises the FULL plan pipeline with a real LLM provider and asserts on
 * every critical stage. A simple prompt ("add a /health endpoint") should
 * complete the entire pipeline — plan, requirements, architecture, scenarios,
 * execution, rollup, and completion. If it can't, the test fails.
 *
 * Run with: task e2e:ui:test:llm -- local easy
 */

const PLAN_PROMPT = `Add a /health endpoint to the Go HTTP service. The endpoint should return JSON with:
"status": "ok", "uptime": seconds since server start, "version": Go runtime version.
Include unit tests for the health handler.`;

const CASCADE_TIMEOUT = Number(process.env.CASCADE_TIMEOUT) || 600_000;
const EXECUTION_TIMEOUT = Number(process.env.EXECUTION_TIMEOUT) || 2_400_000;
const POLL_INTERVAL = 3_000;

/** Stages where the plan is terminally broken — fail immediately. */
const TERMINAL_FAILURE_STAGES = ['rejected', 'failed'];

/**
 * Poll plan until it reaches one of the target stages or a terminal failure.
 * Promotes at approval gates automatically. Returns the plan at the target stage.
 */
async function waitForStage(
	slug: string,
	targetStages: string[],
	timeoutMs: number,
	label: string
): Promise<PlanResponse> {
	const start = Date.now();
	let lastStage = '';

	while (Date.now() - start < timeoutMs) {
		const plan = await getPlan(slug);

		if (plan.stage !== lastStage) {
			console.log(`[easy:${label}] Stage: ${lastStage || '(start)'} -> ${plan.stage} (${((Date.now() - start) / 1000).toFixed(0)}s)`);
			lastStage = plan.stage;
		}

		if (TERMINAL_FAILURE_STAGES.includes(plan.stage)) {
			const diag = JSON.stringify({
				stage: plan.stage,
				review_verdict: plan.review_verdict,
				execution_summary: plan.execution_summary,
			});
			throw new Error(`Plan entered terminal failure stage '${plan.stage}' while waiting for [${targetStages}]. Diagnostics: ${diag}`);
		}

		if (targetStages.includes(plan.stage)) {
			console.log(`[easy:${label}] Reached ${plan.stage} in ${((Date.now() - start) / 1000).toFixed(1)}s`);
			return plan;
		}

		// Auto-promote at approval gates
		if (!plan.approved && plan.stage === 'reviewed') {
			console.log(`[easy:${label}] Promoting at reviewed gate`);
			await promotePlan(slug);
		}
		if (plan.stage === 'scenarios_reviewed') {
			console.log(`[easy:${label}] Promoting at scenarios_reviewed gate`);
			await promotePlan(slug);
		}

		await new Promise((r) => setTimeout(r, POLL_INTERVAL));
	}

	const plan = await getPlan(slug);
	throw new Error(`Timed out waiting for [${targetStages}] after ${timeoutMs}ms. Current stage: ${plan.stage}`);
}

test.describe('@t2 @easy plan-lifecycle-llm', () => {
	let slug: string;

	test.describe.configure({ mode: 'serial' });
	test.setTimeout(EXECUTION_TIMEOUT);

	test.beforeAll(async () => {
		const plan = await createPlan(`${PLAN_PROMPT}\n\nTest run: ${Date.now()}`);
		slug = plan.slug;
		await waitForGoal(slug, 300_000);
	});

	test.afterAll(async () => {
		if (process.env.DEBUG) return;
		if (slug) await deletePlan(slug).catch(() => {});
	});

	// ── Stage 1: Plan goal synthesized ──────────────────────────────────

	test('plan created with goal', async () => {
		const plan = await getPlan(slug);
		expect(plan.goal).toBeTruthy();
		console.log(`[easy] Goal: ${plan.goal.slice(0, 80)}...`);
	});

	// ── Stage 2: Plan reviewed and approved ─────────────────────────────

	test('plan reviewed and approved', async () => {
		// Wait for review to complete and auto-promote
		const plan = await waitForStage(
			slug,
			['approved', 'generating_requirements', 'requirements_generated',
			 'generating_architecture', 'architecture_generated',
			 'generating_scenarios', 'scenarios_generated', 'scenarios_reviewed',
			 'ready_for_execution', 'implementing'],
			CASCADE_TIMEOUT,
			'review'
		);

		expect(plan.approved).toBe(true);
		console.log(`[easy] Review verdict: ${plan.review_verdict || 'auto-approved'}`);
	});

	// ── Stage 3: Requirements generated ─────────────────────────────────

	test('requirements generated', async () => {
		// Wait for requirements to be generated
		await waitForStage(
			slug,
			['requirements_generated', 'generating_architecture', 'architecture_generated',
			 'generating_scenarios', 'scenarios_generated', 'scenarios_reviewed',
			 'ready_for_execution', 'implementing'],
			CASCADE_TIMEOUT,
			'requirements'
		);

		const res = await fetch(`http://localhost:3000/plan-manager/plans/${slug}/requirements`);
		const requirements = await res.json();
		expect(requirements.length).toBeGreaterThan(0);
		console.log(`[easy] ${requirements.length} requirements generated`);
	});

	// ── Stage 4: Architecture generated ─────────────────────────────────

	test('architecture generated', async () => {
		await waitForStage(
			slug,
			['architecture_generated', 'generating_scenarios', 'scenarios_generated',
			 'scenarios_reviewed', 'ready_for_execution', 'implementing'],
			CASCADE_TIMEOUT,
			'architecture'
		);

		const plan = await getPlan(slug);
		expect(plan.architecture).toBeDefined();
		expect(plan.architecture).not.toBeNull();

		const techChoices = plan.architecture?.technology_choices;
		expect(Array.isArray(techChoices)).toBe(true);
		expect(techChoices!.length).toBeGreaterThan(0);
		console.log(`[easy] Architecture: ${techChoices!.length} technology choices`);
	});

	// ── Stage 5: Scenarios generated ────────────────────────────────────

	test('scenarios generated and reviewed', async () => {
		await waitForStage(
			slug,
			['scenarios_reviewed', 'ready_for_execution', 'implementing'],
			CASCADE_TIMEOUT,
			'scenarios'
		);

		const res = await fetch(`http://localhost:3000/plan-manager/plans/${slug}/scenarios`);
		const scenarios = await res.json();
		expect(scenarios.length).toBeGreaterThan(0);
		console.log(`[easy] ${scenarios.length} scenarios generated`);
	});

	// ── Stage 6: Execution triggered ────────────────────────────────────

	test('execution triggered', async () => {
		let plan = await getPlan(slug);

		// Promote to ready_for_execution if at a gate
		if (['scenarios_generated', 'scenarios_reviewed'].includes(plan.stage)) {
			await promotePlan(slug);
			plan = await waitForStage(slug, ['ready_for_execution'], 30_000, 'promote-exec');
		}

		// Already executing from auto-cascade
		if (['implementing', 'executing', 'reviewing_rollup', 'complete'].includes(plan.stage)) {
			console.log(`[easy] Already executing: stage=${plan.stage}`);
			return;
		}

		expect(plan.stage).toBe('ready_for_execution');

		console.log('[easy] Triggering execution via API');
		await executePlan(slug);

		// Wait for execution to start
		plan = await waitForStage(
			slug,
			['implementing', 'executing', 'reviewing_rollup', 'complete'],
			60_000,
			'exec-start'
		);

		console.log(`[easy] Execution started: stage=${plan.stage}`);
	});

	// ── Stage 7: Execution completes successfully ───────────────────────

	test('execution completes', async () => {
		const plan = await waitForStage(
			slug,
			['complete'],
			EXECUTION_TIMEOUT,
			'execution'
		);

		expect(plan.stage).toBe('complete');

		// Verify execution summary shows all requirements completed
		if (plan.execution_summary) {
			expect(plan.execution_summary.failed).toBe(0);
			expect(plan.execution_summary.completed).toBe(plan.execution_summary.total);
			console.log(`[easy] Execution summary: ${plan.execution_summary.completed}/${plan.execution_summary.total} completed, ${plan.execution_summary.failed} failed`);
		}

		console.log(`[easy] Pipeline complete`);
	});

	// ── Stage 8: Trajectories prove agents actually ran ─────────────────

	test('trajectories exist after execution', async ({ page }) => {
		const loopsRes = await fetch('http://localhost:3000/agentic-dispatch/loops');
		const loops = await loopsRes.json();
		expect(loops.length).toBeGreaterThan(0);
		console.log(`[easy] ${loops.length} agent loops after execution`);

		// At least one loop should have trajectory steps
		const loopId = loops[0].loop_id;
		const trajRes = await fetch(`http://localhost:3000/agentic-loop/trajectories/${loopId}`);
		const traj = await trajRes.json();
		expect(traj.steps?.length).toBeGreaterThan(0);
		console.log(`[easy] Loop ${loopId.slice(0, 8)} has ${traj.steps.length} steps`);

		// Verify trajectory detail page renders in UI
		await page.goto(`/trajectories/${loopId}`);
		await waitForHydration(page);
		await expect(page.getByTestId('trajectory-detail-page')).toBeVisible();
	});
});
