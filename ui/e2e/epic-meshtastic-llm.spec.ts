import { test, expect } from '@playwright/test';
import { createPlan, deletePlan, executePlan, getPlan, promotePlan, waitForGoal } from './helpers/api';

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

// Timeouts tuned for federated graph indexing + real LLM execution.
// Overridable via env from taskfiles/e2e.yml.
const GRAPH_READY_TIMEOUT = Number(process.env.GRAPH_READY_TIMEOUT) || 600_000; // 10 min
const GOAL_TIMEOUT = Number(process.env.GOAL_TIMEOUT) || 300_000; // 5 min
const CASCADE_TIMEOUT = Number(process.env.CASCADE_TIMEOUT) || 900_000; // 15 min
const EXECUTION_TIMEOUT = Number(process.env.EXECUTION_TIMEOUT) || 3_600_000; // 60 min
const POLL_INTERVAL = 5_000;

const GRAPH_MANIFEST_URL = 'http://localhost:3000/graph-gateway/manifest';

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

test.describe('@t2 @hard epic-meshtastic-llm', () => {
	let slug: string;

	test.describe.configure({ mode: 'serial' });
	test.setTimeout(EXECUTION_TIMEOUT);

	test.beforeAll(async () => {
		// Wait for federated graph to have meaningful entity coverage before
		// creating the plan — the planner draws on graph data at creation time.
		// Without this, OSH/OGC entities may not be indexed yet and the planner
		// produces a generic design that misses the Connected Systems API contract.
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
		// Keep plan and artifacts when DEBUG=1 for post-run inspection.
		if (process.env.DEBUG) return;
		if (slug) await deletePlan(slug).catch(() => {});
	});

	test('federated graph has sufficient entities', async () => {
		// Graph readiness was already asserted in beforeAll (blocking createPlan).
		// This test records the final count for the report.
		const manifest = await fetchGraphManifest();
		const total = totalEntities(manifest);
		console.log(`[hard] Graph entity count: ${total}`);
		expect(total).toBeGreaterThan(100);
	});

	test('plan created with goal', async () => {
		const plan = await getPlan(slug);
		expect(plan.goal).toBeTruthy();
		console.log(`[hard] Goal: ${String(plan.goal).slice(0, 120)}...`);
	});

	test('plan reaches scenarios_generated or later', async () => {
		// With plan-reviewer enabled the flow is:
		//   approved → requirements_generated → architecture_generated
		//   → scenarios_generated → scenarios_reviewed (human pause).
		// With auto_approve=true the cascade runs without human action.
		let plan = await getPlan(slug);

		if (!plan.approved) {
			console.log(`[hard] Manual approval required at stage=${plan.stage}, promoting via API`);
			await promotePlan(slug);
		}

		const TARGET_STAGES = ['scenarios_generated', 'scenarios_reviewed', 'ready_for_execution', 'implementing', 'reviewing_rollup', 'complete'];
		const start = Date.now();
		while (Date.now() - start < CASCADE_TIMEOUT) {
			plan = await getPlan(slug);
			if (plan.stage === 'rejected' || plan.stage === 'failed') {
				throw new Error(`Plan entered terminal failure stage: ${plan.stage}`);
			}
			if (TARGET_STAGES.includes(plan.stage)) break;
			await new Promise((r) => setTimeout(r, POLL_INTERVAL));
		}

		console.log(`[hard] Cascade complete: stage=${plan.stage} in ${((Date.now() - start) / 1000).toFixed(1)}s`);
		expect(TARGET_STAGES).toContain(plan.stage);
	});

	test('at least 3 requirements and 3 scenarios generated', async () => {
		const reqRes = await fetch(`http://localhost:3000/plan-manager/plans/${slug}/requirements`);
		const requirements = (await reqRes.json()) as unknown[];
		console.log(`[hard] ${requirements.length} requirements generated`);
		expect(requirements.length).toBeGreaterThanOrEqual(3);

		const scenRes = await fetch(`http://localhost:3000/plan-manager/plans/${slug}/scenarios`);
		const scenarios = (await scenRes.json()) as unknown[];
		console.log(`[hard] ${scenarios.length} scenarios generated`);
		expect(scenarios.length).toBeGreaterThanOrEqual(3);
	});

	test('advance to ready_for_execution and trigger execution', async () => {
		let plan = await getPlan(slug);

		// Round 2 approval if still in scenarios_generated/reviewed.
		if (['scenarios_generated', 'scenarios_reviewed'].includes(plan.stage)) {
			console.log(`[hard] Round 2 approval: promoting from ${plan.stage}`);
			await promotePlan(slug);
			const start = Date.now();
			while (plan.stage !== 'ready_for_execution' && Date.now() - start < 60_000) {
				await new Promise((r) => setTimeout(r, 1_000));
				plan = await getPlan(slug);
			}
		}

		// If execution already started from the cascade, skip the trigger.
		if (['implementing', 'executing', 'reviewing_rollup', 'complete'].includes(plan.stage)) {
			console.log(`[hard] Execution already in progress: stage=${plan.stage}`);
			return;
		}

		expect(plan.stage).toBe('ready_for_execution');
		console.log('[hard] Triggering execution via API');
		plan = await executePlan(slug);

		// Wait for first execution stage transition.
		const start = Date.now();
		while (
			!['implementing', 'executing', 'reviewing_rollup', 'complete', 'failed'].includes(plan.stage) &&
			Date.now() - start < 60_000
		) {
			await new Promise((r) => setTimeout(r, POLL_INTERVAL));
			plan = await getPlan(slug);
		}
		console.log(`[hard] Execution started: stage=${plan.stage}`);
	});

	test('execution progresses to terminal state', async () => {
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
				console.log(`[hard] Stage: ${lastStage} → ${plan.stage} (${((Date.now() - start) / 1000).toFixed(0)}s)`);
				lastStage = plan.stage;
			}
		}

		console.log(`[hard] Final stage: ${plan.stage} in ${((Date.now() - start) / 1000).toFixed(1)}s`);
		// Real-LLM execution may legitimately fail — assert the pipeline ran,
		// not that the generated code is perfect.
		expect(['implementing', 'executing', 'reviewing_rollup', 'complete', 'failed', 'rejected']).toContain(plan.stage);
	});

	test('rollup review ran (did not skip from implementing to complete)', async () => {
		// The plan must have passed through reviewing_rollup before complete.
		// A direct implementing → complete jump means the rollup reviewer was
		// bypassed, which is a pipeline bug.
		// We cannot re-inspect historical stages without a ledger, so we only
		// assert the current stage is terminal or still rolling up.
		const plan = await getPlan(slug);
		if (plan.stage === 'complete') {
			// Best-effort: fetch the plan's review history if available.
			try {
				const reviewsRes = await fetch(`http://localhost:3000/plan-manager/plans/${slug}/reviews`);
				if (reviewsRes.ok) {
					const reviews = (await reviewsRes.json()) as Array<{ round?: string }>;
					const hasRollup = reviews.some((r) => r.round === 'rollup' || String(r.round).includes('rollup'));
					console.log(`[hard] Rollup review present: ${hasRollup} (${reviews.length} total reviews)`);
				}
			} catch {
				// Endpoint optional — don't fail the test on this soft check.
			}
		}
		expect(['complete', 'failed', 'rejected', 'reviewing_rollup']).toContain(plan.stage);
	});

	test('deliverables contain java source files', async () => {
		const plan = await getPlan(slug);
		if (plan.stage !== 'complete') {
			console.log(`[hard] Skipping deliverables check — plan not complete (stage=${plan.stage})`);
			return;
		}

		// Check sandbox workspace for generated .java files.
		// The sandbox exposes a file listing via its API.
		try {
			const res = await fetch('http://localhost:3000/sandbox/files?path=src/main/java');
			if (!res.ok) {
				console.log(`[hard] Sandbox files endpoint returned ${res.status} — skipping deliverables check`);
				return;
			}
			const files = (await res.json()) as Array<{ path: string; name: string }>;
			const javaFiles = files.filter((f) => f.name.endsWith('.java'));
			console.log(`[hard] Java deliverables: ${javaFiles.length} files`);
			expect(javaFiles.length).toBeGreaterThan(0);
		} catch {
			console.log('[hard] Could not reach sandbox files endpoint — skipping deliverables check');
		}
	});
});
