/**
 * Tests for computeAttentionItems — specifically the semantic fix for
 * bug #7.1: in-flight TDD retries were surfacing as "Task failed" alarms
 * when they're actually being actively retried (not user-actionable).
 *
 * The new contract: surface a `task_failed` attention item only when a
 * requirement has genuinely exhausted retries — i.e., the plan's
 * execution_summary.failed counter is >0 AND the plan is still
 * `implementing` so the user can intervene. Once the plan rolls up to
 * `failed`/`rejected`/`complete`, the plan card tells the story; no need
 * to duplicate it in the banner.
 */
import { describe, it, expect } from 'vitest';
import { computeAttentionItems } from '$lib/stores/attention.svelte';
import type { PlanWithStatus } from '$lib/types/plan';
import type { Loop } from '$lib/types';

function plan(overrides: Partial<PlanWithStatus> & { slug: string }): PlanWithStatus {
	return {
		title: overrides.slug,
		description: '',
		goal: '',
		stage: 'implementing',
		approved: true,
		created_at: new Date().toISOString(),
		active_loops: [],
		review_verdict: '',
		review_summary: '',
		...overrides
	} as PlanWithStatus;
}

function loop(overrides: Partial<Loop> & { loop_id: string; state: string }): Loop {
	return {
		iterations: 0,
		max_iterations: 50,
		created_at: new Date().toISOString(),
		...overrides
	} as Loop;
}

describe('computeAttentionItems — requirement-level failures only', () => {
	it('does NOT surface in-flight TDD retry losses as attention items', () => {
		// Mortgage-calc run: 3 loops in state=failed (reviewer-rejected dev
		// attempts, superseded by currently-executing retries). Plan stage is
		// still implementing, execution_summary.failed is 0 — retries in flight.
		const plans: PlanWithStatus[] = [
			plan({
				slug: 'mortgage-calc',
				stage: 'implementing',
				execution_summary: { completed: 1, failed: 0, pending: 5, total: 6 }
			})
		];
		const loops: Loop[] = [
			loop({ loop_id: 'old-dev-1', state: 'failed' }),
			loop({ loop_id: 'old-dev-2', state: 'failed' }),
			loop({ loop_id: 'old-dev-3', state: 'failed' })
		];

		const items = computeAttentionItems(plans, loops);
		const taskFailed = items.filter((i) => i.type === 'task_failed');
		expect(taskFailed).toHaveLength(0);
	});

	it('surfaces one attention item per plan with terminal requirement failures', () => {
		const plans: PlanWithStatus[] = [
			plan({
				slug: 'mortgage-calc',
				stage: 'implementing',
				execution_summary: { completed: 1, failed: 2, pending: 3, total: 6 }
			})
		];

		const items = computeAttentionItems(plans, []);
		const taskFailed = items.filter((i) => i.type === 'task_failed');
		expect(taskFailed).toHaveLength(1);
		expect(taskFailed[0].plan_slug).toBe('mortgage-calc');
		expect(taskFailed[0].title).toMatch(/2.*(failed|terminal)/i);
	});

	it('does NOT duplicate attention when plan stage is already failed', () => {
		// Plan has rolled up to failed — its card carries the state; no banner.
		const plans: PlanWithStatus[] = [
			plan({
				slug: 'done-dead',
				stage: 'failed',
				execution_summary: { completed: 0, failed: 6, pending: 0, total: 6 }
			})
		];

		const items = computeAttentionItems(plans, []);
		expect(items).toHaveLength(0);
	});

	it('does NOT duplicate attention when plan stage is rejected', () => {
		const plans: PlanWithStatus[] = [
			plan({
				slug: 'user-rejected',
				stage: 'rejected',
				execution_summary: { completed: 1, failed: 2, pending: 3, total: 6 }
			})
		];

		const items = computeAttentionItems(plans, []);
		expect(items).toHaveLength(0);
	});

	it('does NOT alarm on complete plans', () => {
		const plans: PlanWithStatus[] = [
			plan({
				slug: 'shipped',
				stage: 'complete',
				execution_summary: { completed: 6, failed: 0, pending: 0, total: 6 }
			})
		];

		const items = computeAttentionItems(plans, []);
		expect(items).toHaveLength(0);
	});

	it('does NOT alarm when execution_summary is missing or zeroed', () => {
		const plans: PlanWithStatus[] = [
			plan({ slug: 'pre-execution', stage: 'implementing' }),
			plan({
				slug: 'healthy',
				stage: 'implementing',
				execution_summary: { completed: 2, failed: 0, pending: 4, total: 6 }
			})
		];

		const items = computeAttentionItems(plans, []);
		expect(items.filter((i) => i.type === 'task_failed')).toHaveLength(0);
	});

	it('continues to surface approval_needed when plan is at tasks_approved', () => {
		const plans: PlanWithStatus[] = [
			plan({ slug: 'ready', stage: 'tasks_approved', approved_at: '2026-04-21T10:00:00Z' } as Partial<PlanWithStatus> & { slug: string })
		];
		const items = computeAttentionItems(plans, []);
		expect(items.filter((i) => i.type === 'approval_needed')).toHaveLength(1);
	});
});
