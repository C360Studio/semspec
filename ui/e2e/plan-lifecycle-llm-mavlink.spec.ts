import { test, expect } from '@playwright/test';
import { createPlan, deletePlan, executePlan, getPlan, promotePlan, waitForGoal } from './helpers/api';
import type { PlanResponse, PlanDecisionSummary } from './helpers/api';

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

/**
 * REJECTED_GRACE_PERIOD_MS bounds how long the spec keeps polling after
 * seeing stage='rejected' without an in-flight recovery PlanDecision yet
 * showing up on the plan. Covers the race window: plan-manager sets
 * plan.status=rejected and publishes RecoveryRequested in the same
 * mutation; recovery-agent's LLM dispatch lands a PlanDecision on the
 * plan ~5-60s later. During that window plan.stage is rejected with
 * plan_decisions empty (of a recovery-shaped entry); declaring terminal
 * immediately would race the recovery wire we just merged in PR #29.
 *
 * After this window expires, "rejected with no recovery decision" is
 * accepted as actually terminal.
 */
const REJECTED_GRACE_PERIOD_MS = 90_000;

interface WaitForStageOptions {
	/**
	 * allowRecoveryCycles caps how many distinct recovery PlanDecisions the
	 * spec will wait through before declaring the plan terminally failed.
	 * 0 (the default) keeps the original behavior — any TERMINAL_FAILURE_STAGES
	 * value is immediately terminal. Set on the `execution completes` test to
	 * let the autonomous qa-rejection retry chain (PR #29 + #30) actually
	 * run to verdict, which can include multiple QA round-trips.
	 */
	allowRecoveryCycles?: number;
}

interface ActiveRecoveryDecision {
	id: string;
	status: string;
}

/**
 * findActiveRecoveryDecision returns the most-recent recovery-agent-emitted
 * PlanDecision when it's in a not-yet-resolved state (proposed = waiting on
 * auto-accept / human review; accepted = cascade in flight, plan should
 * transition back out of rejected shortly). Anything else (no proposals,
 * decisions all rejected/superseded) returns null so the caller declares
 * terminal.
 */
function findActiveRecoveryDecision(plan: PlanResponse): ActiveRecoveryDecision | null {
	const decisions = (plan.plan_decisions ?? []) as PlanDecisionSummary[];
	const recovery = decisions
		.filter((d) => d.proposed_by === 'recovery-agent')
		.sort((a, b) => b.created_at.localeCompare(a.created_at));
	if (recovery.length === 0) return null;
	const latest = recovery[0];
	if (latest.status === 'proposed' || latest.status === 'accepted') {
		return { id: latest.id, status: latest.status };
	}
	return null;
}

async function waitForStage(
	slug: string,
	targetStages: string[],
	timeoutMs: number,
	label: string,
	options: WaitForStageOptions = {}
): Promise<PlanResponse> {
	const start = Date.now();
	const allowRecoveryCycles = options.allowRecoveryCycles ?? 0;
	let lastStage = '';
	let firstSeenTerminalAt: number | null = null;
	let recoveryCyclesObserved = 0;
	let lastSeenRecoveryDecisionID: string | null = null;

	while (Date.now() - start < timeoutMs) {
		const plan = await getPlan(slug);

		if (plan.stage !== lastStage) {
			console.log(`[mavlink:${label}] Stage: ${lastStage || '(start)'} -> ${plan.stage} (${((Date.now() - start) / 1000).toFixed(0)}s)`);
			// Plan recovered out of a terminal stage — clear the grace timer so
			// a subsequent QA rejection gets a fresh window.
			if (!TERMINAL_FAILURE_STAGES.includes(plan.stage)) {
				firstSeenTerminalAt = null;
			}
			lastStage = plan.stage;
		}

		if (TERMINAL_FAILURE_STAGES.includes(plan.stage)) {
			// Recovery-aware terminal handling. When the caller opted in via
			// allowRecoveryCycles, the spec waits for the autonomous chain
			// (qa-reviewer needs_changes → recovery-agent → auto-accept →
			// requirement-executor resume) to actually finish before declaring
			// the plan dead. See PRs #29 + #30 for the wire that this models.
			if (allowRecoveryCycles > 0) {
				if (firstSeenTerminalAt === null) firstSeenTerminalAt = Date.now();

				const recoveryDecision = findActiveRecoveryDecision(plan);
				if (recoveryDecision !== null) {
					// Track distinct PlanDecisions as cycle boundaries — a new
					// decision ID means recovery-agent has been re-invoked
					// after a previous cycle resolved without reaching the
					// target stage.
					if (recoveryDecision.id !== lastSeenRecoveryDecisionID) {
						recoveryCyclesObserved++;
						lastSeenRecoveryDecisionID = recoveryDecision.id;
						console.log(`[mavlink:${label}] Recovery cycle ${recoveryCyclesObserved}/${allowRecoveryCycles}: PlanDecision ${recoveryDecision.id.slice(-12)} status=${recoveryDecision.status}`);
					}
					if (recoveryCyclesObserved > allowRecoveryCycles) {
						throw new Error(`Plan exhausted ${allowRecoveryCycles} recovery cycles at stage '${plan.stage}' without reaching [${targetStages}]`);
					}
					await new Promise((r) => setTimeout(r, POLL_INTERVAL));
					continue;
				}

				// Terminal stage but no recovery-agent PlanDecision yet — give
				// the dispatch its grace window before declaring dead.
				if (Date.now() - firstSeenTerminalAt < REJECTED_GRACE_PERIOD_MS) {
					await new Promise((r) => setTimeout(r, POLL_INTERVAL));
					continue;
				}

				throw new Error(`Plan stuck at '${plan.stage}' for ${((Date.now() - firstSeenTerminalAt) / 1000).toFixed(0)}s with no recovery PlanDecision; recovery wire may be broken or recovery-agent not running`);
			}

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
		// allowRecoveryCycles: 2 — the @mavlink-decode tier runs at
		// qa_level=integration with real act execution, and gemini-pro
		// has shipped flaky-timing implementations on both prior runs
		// (run #2 2026-05-28 PM: hardcoded time.Sleep; run #3 2026-05-28
		// later: extraneous test.go redeclaring main). The autonomous
		// recovery chain wired in PR #29 + #30 emits a PlanDecision and
		// cascades back to implementing on those rejections. 2 cycles
		// covers one rejection + one successful retry, plus one more
		// for sampling variance.
		const plan = await waitForStage(
			slug,
			['complete'],
			EXECUTION_TIMEOUT,
			'execution',
			{ allowRecoveryCycles: 2 }
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
