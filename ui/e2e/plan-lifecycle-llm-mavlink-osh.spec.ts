import { test, expect } from '@playwright/test';
import { createPlan, deletePlan, executePlan, getPlan, promotePlan, waitForGoal } from './helpers/api';
import type { PlanResponse, PlanDecisionSummary } from './helpers/api';

/**
 * @mavlink-hard tier: OpenSensorHub MAVSDK/MAVLink driver epic.
 *
 * Drives the architect toward the required-tier `mavlink.px4-sitl.mavsdk-smoke`
 * harness profile in workflow/harnesscatalog/catalog/mavlink.yaml — and
 * optionally the `mavlink.raw-mavlink-direct` compatibility profile for the
 * generic raw-MAVLink coverage path. Validates the full plan→architecture→
 * scenarios→execution pipeline at OSH-driver scope, mirroring the existing
 * `@hard` Meshtastic-driver epic but with a MAVLink/MAVSDK target.
 *
 * Run with: WITH_EPIC=1 task e2e:watch:llm -- hybrid-gpt5 mavlink-hard
 *
 * Fixture: test/e2e/fixtures/osh-driver-mavsdk — Java/Gradle OSH driver
 * skeleton themed for the MAVSDK baseline this prompt extends. E2E_FIXTURE
 * is set by the task wrapper. Epic overlay (WITH_EPIC=1) pre-clones
 * osh-addons (the baseline sensorhub-driver-mavsdk reference impl) +
 * MAVSDK-Java + osh-core + ogc-cs at /sources/ so the agent can read
 * the upstream Java directly instead of fragmentary web_search hits.
 */

const PLAN_PROMPT = `Starting from the existing OpenSensorHub MAVSDK addon at https://github.com/opensensorhub/osh-addons/tree/master/sensors/robotics/sensorhub-driver-mavsdk, design and implement MAVLink/MAVSDK support for OpenSensorHub through the OGC Connected Systems API.

Treat the upstream addon as the baseline, not a clean-room rewrite. Preserve its OSH sensor module patterns, MAVSDK Java integration, mavsdk_server lifecycle, existing telemetry outputs, and existing control inputs unless the architecture explicitly replaces them.

The implementation must provide full Connected Systems API coverage for MAVSDK plugins. For every plugin exposed by the pinned MAVSDK Java/proto version, produce a coverage matrix mapping the plugin's methods and streams to one of:
- CS API DataStream + Observation
- CS API ControlStream + Command + CommandStatus/CommandResult
- SystemEvent
- explicit unsupported/deferred entry with rationale

Prefer typed MAVSDK plugin integrations for semantic APIs. Also evaluate MAVLink-native access and implement a generic MAVLink fallback using MavlinkDirect or a native MAVLink library where needed for raw messages, custom dialects, or plugin gaps. Do not hand-roll MAVLink framing, do not stub MAVSDK/OSH classes, and do not claim full coverage without a machine-checkable coverage inventory.

Acceptance:
1. The driver starts a real mavsdk_server and connects to a real or simulated MAVLink system.
2. CS API exposes typed datastreams for telemetry/status/info/events and typed controlstreams for actions, missions, offboard/manual control, params, camera/gimbal, geofence, FTP/logs, calibration, RTK, shell/tune, transponder/winch/gripper, server-side plugins where applicable.
3. A generic raw MAVLink datastream/controlstream supports subscribe-all, subscribe-by-message-name, send-message, and load-custom-XML dialect.
4. Long-running commands expose status/result resources, not just fire-and-forget acknowledgements.
5. Tests include schema/coverage tests plus at least one live MAVSDK/SITL smoke test.
6. README documents MAVSDK vs native-MAVLink tradeoffs and the coverage matrix.`;

const CASCADE_TIMEOUT = Number(process.env.CASCADE_TIMEOUT) || 1_200_000;
const EXECUTION_TIMEOUT = Number(process.env.EXECUTION_TIMEOUT) || 7_200_000;
const POLL_INTERVAL = 3_000;

const TERMINAL_FAILURE_STAGES = ['rejected', 'failed'];

const REJECTED_GRACE_PERIOD_MS = 90_000;

interface WaitForStageOptions {
	allowRecoveryCycles?: number;
}

interface ActiveRecoveryDecision {
	id: string;
	status: string;
}

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
			console.log(`[mavlink-osh:${label}] Stage: ${lastStage || '(start)'} -> ${plan.stage} (${((Date.now() - start) / 1000).toFixed(0)}s)`);
			if (!TERMINAL_FAILURE_STAGES.includes(plan.stage)) {
				firstSeenTerminalAt = null;
			}
			lastStage = plan.stage;
		}

		if (TERMINAL_FAILURE_STAGES.includes(plan.stage)) {
			if (allowRecoveryCycles > 0) {
				if (firstSeenTerminalAt === null) firstSeenTerminalAt = Date.now();

				const recoveryDecision = findActiveRecoveryDecision(plan);
				if (recoveryDecision !== null) {
					if (recoveryDecision.id !== lastSeenRecoveryDecisionID) {
						recoveryCyclesObserved++;
						lastSeenRecoveryDecisionID = recoveryDecision.id;
						console.log(`[mavlink-osh:${label}] Recovery cycle ${recoveryCyclesObserved}/${allowRecoveryCycles}: PlanDecision ${recoveryDecision.id.slice(-12)} status=${recoveryDecision.status}`);
					}
					if (recoveryCyclesObserved > allowRecoveryCycles) {
						throw new Error(`Plan exhausted ${allowRecoveryCycles} recovery cycles at stage '${plan.stage}' without reaching [${targetStages}]`);
					}
					await new Promise((r) => setTimeout(r, POLL_INTERVAL));
					continue;
				}

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
			console.log(`[mavlink-osh:${label}] Reached ${plan.stage} in ${((Date.now() - start) / 1000).toFixed(1)}s`);
			return plan;
		}

		if (plan.stage === 'reviewed') {
			console.log(`[mavlink-osh:${label}] Promoting at reviewed gate`);
			await promotePlan(slug);
		}
		if (plan.stage === 'scenarios_reviewed') {
			console.log(`[mavlink-osh:${label}] Promoting at scenarios_reviewed gate`);
			await promotePlan(slug);
		}

		await new Promise((r) => setTimeout(r, POLL_INTERVAL));
	}

	const plan = await getPlan(slug);
	throw new Error(`Timed out waiting for [${targetStages}] after ${timeoutMs}ms. Current stage: ${plan.stage}`);
}

test.describe('@t2 @mavlink-hard plan-lifecycle-llm-mavlink-osh', () => {
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
		console.log(`[mavlink-osh] Goal: ${plan.goal.slice(0, 80)}...`);
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
		console.log(`[mavlink-osh] Review verdict: ${plan.review_verdict || 'auto-approved'}`);
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
		console.log(`[mavlink-osh] ${requirements.length} requirements generated`);
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

		// The MAVSDK/OSH prompt should drive the architect toward the
		// required-tier `mavlink.px4-sitl.mavsdk-smoke` profile for the
		// live SITL smoke acceptance criterion. We don't hard-assert the
		// specific ID — the lesson is what the architect actually does,
		// not what we hope it does.
		const profiles = (arch as { harness_profiles?: Array<{ profile_id: string }> }).harness_profiles ?? [];
		const profileIds = profiles.map((p) => p.profile_id).join(', ') || '(none)';
		console.log(`[mavlink-osh] Architecture: ${arch.actors.length} actors, ${arch.integrations.length} integrations, harness_profiles=[${profileIds}]`);
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
		console.log(`[mavlink-osh] ${scenarios.length} scenarios generated`);
	});

	test('execution triggered', async () => {
		let plan = await getPlan(slug);

		if (['scenarios_generated', 'scenarios_reviewed'].includes(plan.stage)) {
			await promotePlan(slug);
			plan = await waitForStage(slug, ['ready_for_execution'], 30_000, 'promote-exec');
		}

		if (['implementing', 'executing', 'reviewing_rollup', 'complete'].includes(plan.stage)) {
			console.log(`[mavlink-osh] Already executing: stage=${plan.stage}`);
			return;
		}

		expect(plan.stage).toBe('ready_for_execution');

		console.log('[mavlink-osh] Triggering execution via API');
		await executePlan(slug);

		plan = await waitForStage(
			slug,
			['implementing', 'executing', 'reviewing_rollup', 'complete'],
			60_000,
			'exec-start'
		);

		console.log(`[mavlink-osh] Execution started: stage=${plan.stage}`);
	});

	test('execution completes', async () => {
		// allowRecoveryCycles: 2 — same shape as @mavlink-decode: this
		// runs at qa_level=integration with real act execution, and any
		// QA needs_changes verdict triggers the autonomous recovery
		// chain (PRs #29 + #30 + #34). 2 cycles covers one rejection +
		// one successful retry plus one for sampling variance.
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
			console.log(`[mavlink-osh] Execution summary: ${plan.execution_summary.completed}/${plan.execution_summary.total} completed, ${plan.execution_summary.failed} failed`);
		}

		console.log(`[mavlink-osh] Pipeline complete`);
	});

	test('trajectories exist after execution', async () => {
		const loopsRes = await fetch('http://localhost:3000/agentic-dispatch/loops');
		const loops = await loopsRes.json();
		expect(loops.length).toBeGreaterThan(0);
		console.log(`[mavlink-osh] ${loops.length} agent loops after execution`);

		const loopId = loops[0].loop_id;
		const trajRes = await fetch(`http://localhost:3000/agentic-loop/trajectories/${loopId}`);
		const traj = await trajRes.json();
		expect(traj.steps?.length).toBeGreaterThan(0);
		console.log(`[mavlink-osh] Loop ${loopId.slice(0, 8)} has ${traj.steps.length} steps`);
	});
});
