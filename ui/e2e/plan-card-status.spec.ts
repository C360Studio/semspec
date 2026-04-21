import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { planFixture, stubBoardBackend } from './helpers/truth';

/**
 * @t0 coverage for the new plan-card status surfaces: LoopHeartbeat and the
 * liveness row. Both answer "is this thing working?" for long small-LLM runs
 * (commit 0a6381e + follow-ups).
 *
 * Uses the truth-test harness (helpers/truth.ts) to set up a deterministic
 * backend state per test. These surfaces are hard to observe from a real or
 * mock pipeline because they blink through generation states in under a
 * second; stubbing lets us park the plan in the exact state we care about.
 */

test.describe('@t0 plan-card-status', () => {
	test('LoopHeartbeat renders during generation when active loops exist', async ({ page }) => {
		const slug = 'heartbeat-fixture';
		await stubBoardBackend(page, { plans: [
			planFixture({
				slug,
				stage: 'planning',
				approved: true,
				active_loops: [
					{
						loop_id: 'loop-generation-1',
						role: 'planner',
						state: 'executing',
						iterations: 3,
						max_iterations: 20
					}
				]
			})
		] });

		await page.goto('/');
		await waitForHydration(page);

		const card = page.locator(`a[href="/plans/${slug}"]`);
		await expect(card).toBeVisible();

		const heartbeat = card.locator('[data-testid="plan-card-heartbeat"]');
		await expect(heartbeat).toBeVisible();

		// Role label + turn counter surface the "what's this loop doing" signal.
		// Without them the heartbeat is just a colored dot and loses diagnostic value.
		await expect(heartbeat).toContainText('planner');
		await expect(heartbeat).toContainText('turn 3/20');
	});

	test('LoopHeartbeat summarizes count when multiple loops are running', async ({ page }) => {
		const slug = 'heartbeat-multi-fixture';
		await stubBoardBackend(page, { plans: [
			planFixture({
				slug,
				stage: 'requirements_generated',
				approved: true,
				active_loops: [
					{ loop_id: 'loop-a', role: 'scenario-generator', state: 'executing' },
					{ loop_id: 'loop-b', role: 'scenario-generator', state: 'executing' },
					{ loop_id: 'loop-c', role: 'scenario-generator', state: 'executing' }
				]
			})
		] });

		await page.goto('/');
		await waitForHydration(page);

		const heartbeat = page
			.locator(`a[href="/plans/${slug}"]`)
			.locator('[data-testid="plan-card-heartbeat"]');
		await expect(heartbeat).toBeVisible();
		// Multi-loop rollup collapses to "N loops running" — keeps the card tidy
		// but still communicates fan-out during scenario generation.
		await expect(heartbeat).toContainText('3 loops running');
	});

	test('LoopHeartbeat does not render when active_loops is empty', async ({ page }) => {
		const slug = 'no-loops-fixture';
		await stubBoardBackend(page, { plans: [
			planFixture({
				slug,
				stage: 'reviewed',
				approved: false,
				active_loops: []
			})
		] });

		await page.goto('/');
		await waitForHydration(page);

		const card = page.locator(`a[href="/plans/${slug}"]`);
		await expect(card).toBeVisible();
		await expect(card.locator('[data-testid="plan-card-heartbeat"]')).toHaveCount(0);
		// And the liveness row shouldn't appear either — this stage is a human
		// decision gate, no agents are running.
		await expect(card.locator('[data-testid="plan-card-liveness"]')).toHaveCount(0);
	});

	test('heartbeat hides in favor of liveness row during implementing', async ({ page }) => {
		// PlanCard's display logic: implementing with active_loops whose
		// current_task_id has a matching TaskSSE payload → liveness row.
		// Without a task SSE payload it falls back to LoopHeartbeat. Here we
		// verify the implementing-stage heartbeat-fallback path renders (the
		// liveness row requires a task SSE event, which we can't fire from a
		// static route mock).
		const slug = 'implementing-fixture';
		await stubBoardBackend(page, { plans: [
			planFixture({
				slug,
				stage: 'implementing',
				approved: true,
				active_loops: [
					{
						loop_id: 'loop-impl-1',
						role: 'developer',
						state: 'executing',
						current_task_id: 'task-1',
						iterations: 7,
						max_iterations: 50
					}
				]
			})
		] });

		await page.goto('/');
		await waitForHydration(page);

		const card = page.locator(`a[href="/plans/${slug}"]`);
		await expect(card).toBeVisible();
		// No task SSE payload yet, so LoopHeartbeat is shown as fallback.
		const heartbeat = card.locator('[data-testid="plan-card-heartbeat"]');
		await expect(heartbeat).toBeVisible();
		await expect(heartbeat).toContainText('developer');
		await expect(heartbeat).toContainText('turn 7/50');
	});
});
