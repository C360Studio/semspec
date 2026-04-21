import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import {
	createPlan,
	deletePlan,
	executePlan,
	getPlan,
	promotePlan,
	waitForGoal
} from './helpers/api';
import type { PlanResponse } from './helpers/api';

/**
 * @medium tier: mortgage-calc regression scenario.
 *
 * Recreates the early-adopter prompt that exposed a cluster of silent
 * failures: worktrees deleted out from under in-flight reviewers, merge
 * errors swallowed into "Task execution approved", circular agent questions
 * asked verbatim every 5 minutes, activity SSE reconnect churn. Any one of
 * those regressions re-appearing should fail this test.
 *
 * Runtime: 30-90 minutes depending on model. Tuned for local Ollama with
 * a 30b+ coder model.
 *
 * Run with: task e2e:ui:test:llm -- local medium
 */

const PLAN_PROMPT = `Build a Golang CLI mortgage payoff calculator with:
- Input parsing: principal, annual rate, term in years
- Payoff calculation: monthly payment, amortization schedule, total interest
- A test suite covering the happy path and edge cases (zero-rate, invalid input)
- A main.go entrypoint that prints a summary when run from the command line`;

const CASCADE_TIMEOUT = Number(process.env.CASCADE_TIMEOUT) || 1_200_000; // 20 min
const EXECUTION_TIMEOUT = Number(process.env.EXECUTION_TIMEOUT) || 10_800_000; // 3 hours
const POLL_INTERVAL = 5_000;

/** Stages where the plan is terminally broken — fail immediately. */
const TERMINAL_FAILURE_STAGES = ['rejected', 'failed'];

async function waitForStage(
	slug: string,
	targetStages: string[],
	timeoutMs: number,
	label: string
): Promise<PlanResponse> {
	const start = Date.now();
	let lastStage = '';
	let executionKicked = false;

	while (Date.now() - start < timeoutMs) {
		const plan = await getPlan(slug);

		if (plan.stage !== lastStage) {
			console.log(`[medium:${label}] Stage: ${lastStage || '(start)'} -> ${plan.stage} (${((Date.now() - start) / 1000).toFixed(0)}s)`);
			lastStage = plan.stage;
		}

		if (TERMINAL_FAILURE_STAGES.includes(plan.stage)) {
			throw new Error(
				`Plan entered terminal failure stage '${plan.stage}' while waiting for [${targetStages}]. ` +
					`Diagnostics: ${JSON.stringify({
						stage: plan.stage,
						review_verdict: plan.review_verdict,
						execution_summary: plan.execution_summary
					})}`
			);
		}

		if (targetStages.includes(plan.stage)) {
			console.log(`[medium:${label}] Reached ${plan.stage} in ${((Date.now() - start) / 1000).toFixed(1)}s`);
			return plan;
		}

		// Auto-advance at every gate — this scenario is a regression test of the
		// execution phase. Human confirmation is what the UI gives users; here we
		// simulate the happy path clicks so the pipeline can reach implementing.
		if (!plan.approved && plan.stage === 'reviewed') {
			await promotePlan(slug);
		}
		if (plan.stage === 'scenarios_reviewed') {
			await promotePlan(slug);
		}
		if (plan.stage === 'ready_for_execution' && !executionKicked) {
			console.log(`[medium:${label}] Triggering execution at ready_for_execution gate`);
			await executePlan(slug);
			executionKicked = true;
		}

		await new Promise((r) => setTimeout(r, POLL_INTERVAL));
	}

	const plan = await getPlan(slug);
	throw new Error(`Timed out waiting for [${targetStages}] after ${timeoutMs}ms. Current stage: ${plan.stage}`);
}

// ── Regression probes ──────────────────────────────────────────────────

type TaskExecutionState = {
	task_id: string;
	stage: string;
	error_reason?: string;
	verdict?: string;
	requirement_id?: string;
	tdd_cycle?: number;
	max_tdd_cycles?: number;
	updated_at?: string;
};

async function listTaskExecutions(slug: string): Promise<TaskExecutionState[]> {
	const res = await fetch(`http://localhost:3000/execution-manager/plans/${slug}/tasks`);
	if (!res.ok) return [];
	return res.json();
}

type Question = {
	id: string;
	from_agent: string;
	question: string;
	status: string;
	created_at: string;
};

async function listQuestions(): Promise<Question[]> {
	const res = await fetch('http://localhost:3000/question-manager/questions?status=all');
	if (!res.ok) return [];
	return res.json();
}

function normalize(s: string): string {
	return s.replace(/\s+/g, ' ').trim().toLowerCase();
}

test.describe('@t2 @medium mortgage-calc-llm', () => {
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

	// ── Planning cascade ────────────────────────────────────────────────

	test('plan cascade reaches implementing', async () => {
		await waitForStage(
			slug,
			['implementing', 'executing', 'reviewing_rollup', 'complete'],
			CASCADE_TIMEOUT,
			'cascade'
		);
	});

	// ── Execution ───────────────────────────────────────────────────────

	test('execution completes (or lands on a terminal stage with diagnostics)', async () => {
		// Unlike @easy we don't hard-require success here — we want the
		// regression probes below to run even on imperfect completion. What we
		// assert is: the plan reaches SOME terminal stage rather than hanging.
		const plan = await waitForStage(
			slug,
			['complete', 'rejected', 'failed'],
			EXECUTION_TIMEOUT,
			'execution'
		);

		const summary = plan.execution_summary;
		if (summary) {
			console.log(
				`[medium] summary: completed=${summary.completed} failed=${summary.failed} pending=${summary.pending} total=${summary.total}`
			);
			// Pre-fix baseline: all 5 requirements failed. We should do strictly
			// better than that — at least one requirement must make it through.
			// If this assertion ever starts failing it's a real signal, not flake.
			expect(summary.completed).toBeGreaterThan(0);
		}
	});

	// ── Regression probe: no "approved despite merge failure" ──────────

	test('no task approved with merge_failed error_reason', async () => {
		const tasks = await listTaskExecutions(slug);
		expect(tasks.length).toBeGreaterThan(0);

		const approvedWithMergeFailure = tasks.filter(
			(t) => t.stage === 'approved' && (t.error_reason ?? '').includes('merge_failed')
		);

		if (approvedWithMergeFailure.length > 0) {
			throw new Error(
				`Found ${approvedWithMergeFailure.length} tasks marked approved despite merge_failed: ` +
					JSON.stringify(approvedWithMergeFailure.map((t) => ({ id: t.task_id, reason: t.error_reason })))
			);
		}
	});

	// ── Regression probe: no task "approved" when verdict never converged ─

	test('no task approved with empty verdict', async () => {
		const tasks = await listTaskExecutions(slug);
		const bogus = tasks.filter((t) => t.stage === 'approved' && !t.verdict);
		if (bogus.length > 0) {
			throw new Error(
				`Found ${bogus.length} tasks with stage=approved but no verdict: ` +
					JSON.stringify(bogus.map((t) => t.task_id))
			);
		}
	});

	// ── Regression probe: circular questions ────────────────────────────

	test('no agent asked the same question twice within the run', async () => {
		const questions = await listQuestions();

		// Bucket questions by agent + normalized text. Count duplicates within
		// each bucket. Anything > 1 is a regression of the dedupe we shipped.
		const buckets = new Map<string, Question[]>();
		for (const q of questions) {
			const key = `${q.from_agent}::${normalize(q.question)}`;
			const arr = buckets.get(key) ?? [];
			arr.push(q);
			buckets.set(key, arr);
		}

		const duplicates = Array.from(buckets.entries())
			.filter(([, arr]) => arr.length > 1)
			.map(([key, arr]) => ({ key, count: arr.length, ids: arr.map((q) => q.id) }));

		if (duplicates.length > 0) {
			console.error('Duplicate questions detected:', JSON.stringify(duplicates, null, 2));
			throw new Error(
				`Dedupe regression: ${duplicates.length} (agent+text) pairs asked more than once. First: ${JSON.stringify(duplicates[0])}`
			);
		}
	});

	// ── UI probe: PlanCard surfaces cycle badge + age ───────────────────

	test('plan card shows liveness during implementing', async ({ page }) => {
		// Only meaningful while execution is still in flight. If the plan is
		// already complete we skip; the cycle badge is only rendered for
		// `implementing` stage.
		const plan = await getPlan(slug);
		if (plan.stage !== 'implementing') {
			test.skip(true, `plan stage is ${plan.stage}; liveness badge is only shown during implementing`);
			return;
		}

		await page.goto('/board');
		await waitForHydration(page);

		// PlanCard has data-testid="plan-card-liveness" when active_loops has a
		// matching task with updated_at (see PlanCard.svelte edits). Its
		// absence during implementing is the failure mode we fixed.
		const liveness = page
			.locator(`a[href="/plans/${slug}"]`)
			.locator('[data-testid="plan-card-liveness"]');
		await expect(liveness).toBeVisible({ timeout: 30_000 });
		const text = await liveness.textContent();
		console.log(`[medium] liveness text: ${text?.trim()}`);
		// At minimum, expect either a cycle badge or an age label to have text.
		expect(text?.trim().length ?? 0).toBeGreaterThan(0);
	});
});
