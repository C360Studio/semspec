import { test, expect } from '@playwright/test';
import { createPlan, deletePlan, executePlan, getPlan, promotePlan, waitForGoal } from './helpers/api';
import type { PlanResponse } from './helpers/api';

/**
 * @mavlink-decode tier: verifies PR #18 catalog-backed harness-profile
 * selection under real LLM.
 *
 * The architect should pick `mavlink.raw-mavlink-direct` from the harness
 * catalog (compatibility tier — no hard-gate evidence-anchor enforcement,
 * so this exercises the selection path without the required-tier wiring).
 * It should classify `gomavlib` (or equivalent Go MAVLink lib) as a
 * `runtime_dep` upstream, not an `integration_target` — the UDP autopilot
 * peer is integration_target territory, but tests use captured frames in
 * testdata so no SITL container is needed.
 *
 * Run with: task e2e:watch:llm -- gemini mavlink-decode
 *
 * Fixture: test/e2e/fixtures/mavlink-heartbeat-go (skeleton main.go + empty
 * go.mod). E2E_FIXTURE=mavlink-heartbeat-go is set by the task wrapper.
 */

const PLAN_PROMPT = `Add a Go HTTP service that listens for MAVLink v2 HEARTBEAT frames over UDP on port 14540 and exposes the most recent heartbeat at GET /heartbeat as JSON containing 'system_id', 'component_id', 'autopilot_type', 'base_mode', and 'received_at'.

Use a real Go MAVLink library (e.g., github.com/bluenviron/gomavlib) for frame parsing — do not hand-roll the MAVLink wire format.

Include unit tests that decode captured MAVLink HEARTBEAT frames from testdata/ files and assert the parsed fields.`;

const CASCADE_TIMEOUT = Number(process.env.CASCADE_TIMEOUT) || 600_000;
const EXECUTION_TIMEOUT = Number(process.env.EXECUTION_TIMEOUT) || 2_400_000;
const POLL_INTERVAL = 3_000;

const TERMINAL_FAILURE_STAGES = ['rejected', 'failed'];

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
			console.log(`[mavlink:${label}] Stage: ${lastStage || '(start)'} -> ${plan.stage} (${((Date.now() - start) / 1000).toFixed(0)}s)`);
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
			console.log(`[mavlink:${label}] Reached ${plan.stage} in ${((Date.now() - start) / 1000).toFixed(1)}s`);
			return plan;
		}

		if (plan.stage === 'reviewed') {
			console.log(`[mavlink:${label}] Promoting at reviewed gate`);
			await promotePlan(slug);
		}
		if (plan.stage === 'scenarios_reviewed') {
			console.log(`[mavlink:${label}] Promoting at scenarios_reviewed gate`);
			await promotePlan(slug);
		}

		await new Promise((r) => setTimeout(r, POLL_INTERVAL));
	}

	const plan = await getPlan(slug);
	throw new Error(`Timed out waiting for [${targetStages}] after ${timeoutMs}ms. Current stage: ${plan.stage}`);
}

test.describe('@t2 @mavlink-decode plan-lifecycle-llm-mavlink', () => {
	let slug: string;

	test.describe.configure({ mode: 'serial' });
	test.setTimeout(EXECUTION_TIMEOUT);

	test.beforeAll(async () => {
		const plan = await createPlan(`${PLAN_PROMPT}\n\nTest run: ${Date.now()}`);
		slug = plan.slug;
		await waitForGoal(slug, CASCADE_TIMEOUT);
	});

	test.afterAll(async () => {
		if (process.env.DEBUG) return;
		if (slug) await deletePlan(slug).catch(() => {});
	});

	test('plan created with goal', async () => {
		const plan = await getPlan(slug);
		expect(plan.goal).toBeTruthy();
		console.log(`[mavlink] Goal: ${plan.goal.slice(0, 80)}...`);
	});

	test('plan reviewed and approved', async () => {
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
		console.log(`[mavlink] Review verdict: ${plan.review_verdict || 'auto-approved'}`);
	});

	test('requirements generated', async () => {
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
		console.log(`[mavlink] ${requirements.length} requirements generated`);
	});

	test('architecture generated with harness profile selection', async () => {
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

		const arch = plan.architecture!;
		expect(Array.isArray(arch.actors)).toBe(true);
		expect(arch.actors.length).toBeGreaterThan(0);
		expect(Array.isArray(arch.integrations)).toBe(true);
		expect(arch.integrations.length).toBeGreaterThan(0);
		expect(arch.test_surface).toBeDefined();

		// Surface harness_profiles for diagnostics. The MAVLink prompt should
		// drive the architect to select `mavlink.raw-mavlink-direct`. We don't
		// hard-assert the specific ID — the lesson is what the architect
		// actually does, not what we hope it does.
		const profiles = (arch as { harness_profiles?: Array<{ profile_id: string }> }).harness_profiles ?? [];
		const profileIds = profiles.map((p) => p.profile_id).join(', ') || '(none)';
		console.log(`[mavlink] Architecture: ${arch.actors.length} actors, ${arch.integrations.length} integrations, harness_profiles=[${profileIds}]`);
	});

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
		console.log(`[mavlink] ${scenarios.length} scenarios generated`);
	});

	test('execution triggered', async () => {
		let plan = await getPlan(slug);

		if (['scenarios_generated', 'scenarios_reviewed'].includes(plan.stage)) {
			await promotePlan(slug);
			plan = await waitForStage(slug, ['ready_for_execution'], 30_000, 'promote-exec');
		}

		if (['implementing', 'executing', 'reviewing_rollup', 'complete'].includes(plan.stage)) {
			console.log(`[mavlink] Already executing: stage=${plan.stage}`);
			return;
		}

		expect(plan.stage).toBe('ready_for_execution');

		console.log('[mavlink] Triggering execution via API');
		await executePlan(slug);

		plan = await waitForStage(
			slug,
			['implementing', 'executing', 'reviewing_rollup', 'complete'],
			60_000,
			'exec-start'
		);

		console.log(`[mavlink] Execution started: stage=${plan.stage}`);
	});

	test('execution completes', async () => {
		const plan = await waitForStage(
			slug,
			['complete'],
			EXECUTION_TIMEOUT,
			'execution'
		);

		expect(plan.stage).toBe('complete');

		if (plan.execution_summary) {
			expect(plan.execution_summary.failed).toBe(0);
			expect(plan.execution_summary.completed).toBe(plan.execution_summary.total);
			console.log(`[mavlink] Execution summary: ${plan.execution_summary.completed}/${plan.execution_summary.total} completed, ${plan.execution_summary.failed} failed`);
		}

		console.log(`[mavlink] Pipeline complete`);
	});

	test('trajectories exist after execution', async () => {
		const loopsRes = await fetch('http://localhost:3000/agentic-dispatch/loops');
		const loops = await loopsRes.json();
		expect(loops.length).toBeGreaterThan(0);
		console.log(`[mavlink] ${loops.length} agent loops after execution`);

		const loopId = loops[0].loop_id;
		const trajRes = await fetch(`http://localhost:3000/agentic-loop/trajectories/${loopId}`);
		const traj = await trajRes.json();
		expect(traj.steps?.length).toBeGreaterThan(0);
		console.log(`[mavlink] Loop ${loopId.slice(0, 8)} has ${traj.steps.length} steps`);
	});
});
