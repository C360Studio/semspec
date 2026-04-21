/**
 * @t0 truth-tests for the /status ops-glass route.
 *
 * Each test seeds a deterministic backend state via stubBoardBackend, navigates
 * to /status, and asserts the DOM matches the fixture. No live backend required.
 *
 * Naming convention for data-testid selectors:
 *   status-empty          — "no runs in flight" state
 *   status-run-banner     — one banner per implementing plan
 *   status-agent-card     — one card per active loop across all implementing plans
 *   status-activity-pulse — last-activity / event-count summary
 *   status-health-dot     — one per SSE stream
 */

import { test, expect } from '@playwright/test';
import { waitForHydration } from './helpers/hydration';
import { planFixture, loopFixture, stubBoardBackend } from './helpers/truth';

test.describe('@t0 status-route', () => {
	// ─── Test 1: empty state ────────────────────────────────────────────────
	test('empty state — no implementing plans shows "No runs in flight"', async ({ page }) => {
		// Seed: only plans in non-implementing stages, or no plans at all.
		await stubBoardBackend(page, {
			plans: [
				planFixture({ slug: 'idle-plan', stage: 'reviewed', approved: false })
			],
			loops: []
		});

		await page.goto('/status');
		await waitForHydration(page);

		await expect(page.getByTestId('status-empty')).toBeVisible();
		await expect(page.getByTestId('status-empty')).toContainText('No runs in flight');

		// Run banners must NOT render.
		await expect(page.getByTestId('status-run-banner')).toHaveCount(0);
	});

	// ─── Test 2: single implementing plan with active agents ────────────────
	test('single implementing plan — banner and agent cards render correctly', async ({ page }) => {
		const slug = 'mortgage-calc';
		await stubBoardBackend(page, {
			plans: [
				planFixture({
					slug,
					stage: 'implementing',
					approved: true,
					execution_summary: { completed: 1, failed: 2, pending: 3, total: 6 },
					active_loops: [
						{ loop_id: 'loop-a', role: 'developer', state: 'executing', iterations: 5, max_iterations: 50 },
						{ loop_id: 'loop-b', role: 'reviewer', state: 'executing', iterations: 2, max_iterations: 50 },
						{ loop_id: 'loop-c', role: 'validator', state: 'executing', iterations: 1, max_iterations: 50 }
					]
				})
			],
			loops: [
				loopFixture({ loop_id: 'loop-a', state: 'executing', workflow_step: 'developer' }),
				loopFixture({ loop_id: 'loop-b', state: 'executing', workflow_step: 'reviewer' }),
				loopFixture({ loop_id: 'loop-c', state: 'executing', workflow_step: 'validator' })
			]
		});

		await page.goto('/status');
		await waitForHydration(page);

		// Exactly one run banner for the one implementing plan.
		const banners = page.getByTestId('status-run-banner');
		await expect(banners).toHaveCount(1);

		const banner = banners.first();
		await expect(banner).toContainText(slug);
		await expect(banner).toContainText('1 done');
		await expect(banner).toContainText('2 failed');
		await expect(banner).toContainText('3 pending');
		await expect(banner).toContainText('6 total');
		// "K of N agents active" — 3 active loops, 6 total reqs is the denominator
		// for requirement count; the agent count is based on active_loops.
		await expect(banner).toContainText('3');

		// Three agent cards — one per loop in active_loops.
		const agentCards = page.getByTestId('status-agent-card');
		await expect(agentCards).toHaveCount(3);

		// First card is the developer loop.
		const devCard = agentCards.nth(0);
		await expect(devCard).toContainText('developer');
		await expect(devCard).toContainText('5');  // iterations
	});

	// ─── Test 3: failed-count visually emphasized ────────────────────────────
	test('failed count is visually emphasized when failed > 0', async ({ page }) => {
		await stubBoardBackend(page, {
			plans: [
				planFixture({
					slug: 'failing-plan',
					stage: 'implementing',
					approved: true,
					execution_summary: { completed: 0, failed: 2, pending: 4, total: 6 },
					active_loops: [
						{ loop_id: 'loop-d', role: 'developer', state: 'executing' }
					]
				})
			],
			loops: []
		});

		await page.goto('/status');
		await waitForHydration(page);

		// The failed count must be rendered inside a dedicated element carrying
		// data-failed="true" or class "failed-count" — we check for the attribute
		// so the assertion doesn't break if we re-skin the color.
		const failedBadge = page.locator('[data-status="failed"]');
		await expect(failedBadge).toBeVisible();
		await expect(failedBadge).toContainText('2');
	});

	// ─── Test 4: multiple plans each get their own banner ───────────────────
	test('multiple implementing plans — each gets its own banner, agents pooled', async ({ page }) => {
		await stubBoardBackend(page, {
			plans: [
				planFixture({
					slug: 'plan-alpha',
					stage: 'implementing',
					approved: true,
					execution_summary: { completed: 2, failed: 0, pending: 1, total: 3 },
					active_loops: [
						{ loop_id: 'loop-alpha-1', role: 'developer', state: 'executing', iterations: 3, max_iterations: 50 }
					]
				}),
				planFixture({
					slug: 'plan-beta',
					stage: 'implementing',
					approved: true,
					execution_summary: { completed: 0, failed: 1, pending: 2, total: 3 },
					active_loops: [
						{ loop_id: 'loop-beta-1', role: 'developer', state: 'executing', iterations: 8, max_iterations: 50 },
						{ loop_id: 'loop-beta-2', role: 'reviewer', state: 'executing', iterations: 1, max_iterations: 50 }
					]
				})
			],
			loops: []
		});

		await page.goto('/status');
		await waitForHydration(page);

		// Two banners — one per implementing plan.
		await expect(page.getByTestId('status-run-banner')).toHaveCount(2);

		// Both plan slugs appear in the banners.
		await expect(page.getByTestId('status-run-banner').first()).toContainText('plan-alpha');
		await expect(page.getByTestId('status-run-banner').last()).toContainText('plan-beta');

		// Total agent cards = 1 (alpha) + 2 (beta) = 3.
		await expect(page.getByTestId('status-agent-card')).toHaveCount(3);
	});

	// ─── Test 5: health dots render ─────────────────────────────────────────
	test('health dots are rendered for SSE streams', async ({ page }) => {
		await stubBoardBackend(page, {
			plans: [],
			loops: [],
			health: { healthy: true, components: [] }
		});

		await page.goto('/status');
		await waitForHydration(page);

		// At least the activity SSE health dot should be present.
		const healthDots = page.getByTestId('status-health-dot');
		await expect(healthDots).toHaveCount(2);  // activity + feed
	});
});
