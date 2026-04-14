import { test, expect } from '@playwright/test';
import { createPlan, deletePlan, executePlan, getPlan, promotePlan, waitForGoal } from './helpers/api';
import type { PlanResponse } from './helpers/api';

/**
 * @hard tier: Alpha epic scenario — Meshtastic driver for OpenSensorHub.
 *
 * This is the most complex real-LLM scenario. Three external semsource
 * instances (OSH, Meshtastic, OGC) are federated with the workspace
 * semsource; the planner must draw on all four knowledge domains to
 * design a working Java driver.
 *
 * Validates the full alpha pipeline:
 *   federated knowledge graph → multi-domain planning → execution
 *   → rollup review → deliverables (Java code, tests, README).
 *
 * Infrastructure:
 *   ui/docker-compose.e2e.yml + e2e-llm.yml + e2e-epic.yml
 *   Workspace: test/e2e/fixtures/osh-driver-meshtastic (Maven/Java)
 *
 * Run with: task e2e:ui:test:llm -- claude hard
 *
 * Cost: 150–300k tokens per run. Runtime: 15–30 min with Claude.
 * Do not wire into cost-sensitive CI without a spending cap.
 */

const EPIC_PROMPT = `Design and implement a Meshtastic driver for OpenSensorHub (OSH). The driver must use the Connected Systems API to send and receive messages over the Meshtastic mesh network. Deliver working Java source files, unit tests, and a README with usage examples.`;

const GRAPH_READY_TIMEOUT = Number(process.env.GRAPH_READY_TIMEOUT) || 600_000;
const GOAL_TIMEOUT = Number(process.env.GOAL_TIMEOUT) || 300_000;
const CASCADE_TIMEOUT = Number(process.env.CASCADE_TIMEOUT) || 900_000;
const EXECUTION_TIMEOUT = Number(process.env.EXECUTION_TIMEOUT) || 3_600_000;
const POLL_INTERVAL = 5_000;

const GRAPH_MANIFEST_URL = 'http://localhost:3000/graph-gateway/manifest';
const TERMINAL_FAILURE_STAGES = ['rejected', 'failed'];

interface ManifestResponse {
	sources?: Array<{ name: string; entity_count?: number; phase?: string }>;
	total_entities?: number;
	[key: string]: unknown;
}

async function fetchGraphManifest(): Promise<ManifestResponse | null> {
	try {
		const res = await fetch(GRAPH_MANIFEST_URL);
		if (!res.ok) return null;
		return (await res.json()) as ManifestResponse;
	} catch {
		return null;
	}
}

function totalEntities(m: ManifestResponse | null): number {
	if (!m) return 0;
	if (typeof m.total_entities === 'number') return m.total_entities;
	if (Array.isArray(m.sources)) {
		return m.sources.reduce((sum, s) => sum + (s.entity_count ?? 0), 0);
	}
	return 0;
}

/** Poll until target stage or terminal failure. Auto-promotes at gates. */
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
			console.log(`[hard:${label}] Stage: ${lastStage || '(start)'} -> ${plan.stage} (${((Date.now() - start) / 1000).toFixed(0)}s)`);
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
			console.log(`[hard:${label}] Reached ${plan.stage} in ${((Date.now() - start) / 1000).toFixed(1)}s`);
			return plan;
		}

		if (!plan.approved && plan.stage === 'reviewed') {
			console.log(`[hard:${label}] Promoting at reviewed gate`);
			await promotePlan(slug);
		}
		if (plan.stage === 'scenarios_reviewed') {
			console.log(`[hard:${label}] Promoting at scenarios_reviewed gate`);
			await promotePlan(slug);
		}

		await new Promise((r) => setTimeout(r, POLL_INTERVAL));
	}

	const plan = await getPlan(slug);
	throw new Error(`Timed out waiting for [${targetStages}] after ${timeoutMs}ms. Current stage: ${plan.stage}`);
}

test.describe('@t2 @hard epic-meshtastic-llm', () => {
	let slug: string;

	test.describe.configure({ mode: 'serial' });
	test.setTimeout(EXECUTION_TIMEOUT);

	test.beforeAll(async () => {
		console.log('[hard] Waiting for federated graph readiness (>100 entities)...');
		const gStart = Date.now();
		let lastTotal = 0;
		while (Date.now() - gStart < GRAPH_READY_TIMEOUT) {
			const m = await fetchGraphManifest();
			const total = totalEntities(m);
			if (total !== lastTotal) {
				console.log(`[hard] Graph entities: ${total} (${((Date.now() - gStart) / 1000).toFixed(0)}s)`);
				lastTotal = total;
			}
			if (total > 100) break;
			await new Promise((r) => setTimeout(r, POLL_INTERVAL));
		}
		if (lastTotal <= 100) {
			throw new Error(`Federated graph not ready after ${GRAPH_READY_TIMEOUT / 1000}s (only ${lastTotal} entities)`);
		}

		const plan = await createPlan(`${EPIC_PROMPT}\n\nTest run: ${Date.now()}`);
		slug = plan.slug;
		console.log(`[hard] Plan created: ${slug}`);
		await waitForGoal(slug, GOAL_TIMEOUT);
	});

	test.afterAll(async () => {
		if (process.env.DEBUG) return;
		if (slug) await deletePlan(slug).catch(() => {});
	});

	// ── Pre-flight: federated graph ─────────────────────────────────────

	test('federated graph has sufficient entities', async () => {
		const manifest = await fetchGraphManifest();
		const total = totalEntities(manifest);
		console.log(`[hard] Graph entity count: ${total}`);
		expect(total).toBeGreaterThan(100);
	});

	// ── Stage 1: Plan goal synthesized ──────────────────────────────────

	test('plan created with goal', async () => {
		const plan = await getPlan(slug);
		expect(plan.goal).toBeTruthy();
		console.log(`[hard] Goal: ${String(plan.goal).slice(0, 120)}...`);
	});

	// ── Stage 2: Plan reviewed and approved ─────────────────────────────

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
		console.log(`[hard] Review verdict: ${plan.review_verdict || 'auto-approved'}`);
	});

	// ── Stage 3: Requirements generated ─────────────────────────────────

	test('at least 3 requirements generated', async () => {
		await waitForStage(
			slug,
			['requirements_generated', 'generating_architecture', 'architecture_generated',
			 'generating_scenarios', 'scenarios_generated', 'scenarios_reviewed',
			 'ready_for_execution', 'implementing'],
			CASCADE_TIMEOUT,
			'requirements'
		);

		const res = await fetch(`http://localhost:3000/plan-manager/plans/${slug}/requirements`);
		const requirements = (await res.json()) as unknown[];
		expect(requirements.length).toBeGreaterThanOrEqual(3);
		console.log(`[hard] ${requirements.length} requirements generated`);
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
		console.log(`[hard] Architecture: ${techChoices!.length} technology choices`);
	});

	// ── Stage 5: Scenarios generated and reviewed ───────────────────────

	test('at least 3 scenarios generated and reviewed', async () => {
		await waitForStage(
			slug,
			['scenarios_reviewed', 'ready_for_execution', 'implementing'],
			CASCADE_TIMEOUT,
			'scenarios'
		);

		const res = await fetch(`http://localhost:3000/plan-manager/plans/${slug}/scenarios`);
		const scenarios = (await res.json()) as unknown[];
		expect(scenarios.length).toBeGreaterThanOrEqual(3);
		console.log(`[hard] ${scenarios.length} scenarios generated`);
	});

	// ── Stage 6: Execution triggered ────────────────────────────────────

	test('execution triggered', async () => {
		let plan = await getPlan(slug);

		if (['scenarios_generated', 'scenarios_reviewed'].includes(plan.stage)) {
			await promotePlan(slug);
			const start = Date.now();
			while (plan.stage !== 'ready_for_execution' && Date.now() - start < 60_000) {
				await new Promise((r) => setTimeout(r, 1_000));
				plan = await getPlan(slug);
			}
		}

		if (['implementing', 'executing', 'reviewing_rollup', 'complete'].includes(plan.stage)) {
			console.log(`[hard] Execution already in progress: stage=${plan.stage}`);
			return;
		}

		expect(plan.stage).toBe('ready_for_execution');
		console.log('[hard] Triggering execution via API');
		await executePlan(slug);

		plan = await waitForStage(
			slug,
			['implementing', 'executing', 'reviewing_rollup', 'complete'],
			60_000,
			'exec-start'
		);
		console.log(`[hard] Execution started: stage=${plan.stage}`);
	});

	// ── Stage 7: Execution completes ────────────────────────────────────

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
			console.log(`[hard] Execution: ${plan.execution_summary.completed}/${plan.execution_summary.total} completed`);
		}

		console.log(`[hard] Pipeline complete`);
	});

	// ── Stage 8: Trajectories prove agents ran ──────────────────────────

	test('trajectories exist after execution', async () => {
		const loopsRes = await fetch('http://localhost:3000/agentic-dispatch/loops');
		const loops = await loopsRes.json();
		expect(loops.length).toBeGreaterThan(0);
		console.log(`[hard] ${loops.length} agent loops after execution`);

		const loopId = loops[0].loop_id;
		const trajRes = await fetch(`http://localhost:3000/agentic-loop/trajectories/${loopId}`);
		const traj = await trajRes.json();
		expect(traj.steps?.length).toBeGreaterThan(0);
		console.log(`[hard] Loop ${loopId.slice(0, 8)} has ${traj.steps.length} steps`);
	});
});
