import { describe, expect, it } from 'vitest';
import { selectFreshestPlan } from './planFreshness';
import type { PlanStage, PlanWithStatus } from './plan';

function plan(slug: string, stage: PlanStage): PlanWithStatus {
	return {
		id: `plan.${slug}`,
		slug,
		title: slug,
		status: stage,
		stage,
		approved: true,
		created_at: '2026-06-16T12:00:00Z',
		active_loops: []
	} as unknown as PlanWithStatus;
}

describe('selectFreshestPlan', () => {
	it('prefers loader data when it has advanced beyond a stale stream plan', () => {
		const streamPlan = plan('mavlink', 'generating_scenarios');
		const loadedPlan = plan('mavlink', 'implementing');
		expect(selectFreshestPlan(streamPlan, loadedPlan, 'mavlink')?.stage).toBe('implementing');
	});

	it('keeps stream data when it is ahead of loader data', () => {
		const streamPlan = plan('mavlink', 'implementing');
		const loadedPlan = plan('mavlink', 'generating_scenarios');
		expect(selectFreshestPlan(streamPlan, loadedPlan, 'mavlink')?.stage).toBe('implementing');
	});

	it('ignores stream data for another slug', () => {
		const streamPlan = plan('other', 'implementing');
		const loadedPlan = plan('mavlink', 'generating_scenarios');
		expect(selectFreshestPlan(streamPlan, loadedPlan, 'mavlink')?.slug).toBe('mavlink');
	});

	it('prefers stream data at the same stage so SSE details still win', () => {
		const streamPlan = plan('mavlink', 'generating_scenarios');
		const loadedPlan = { ...plan('mavlink', 'generating_scenarios'), title: 'loader' };
		expect(selectFreshestPlan(streamPlan, loadedPlan, 'mavlink')?.title).toBe('mavlink');
	});
});
