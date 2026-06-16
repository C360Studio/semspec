import { describe, expect, it } from 'vitest';
import { activePhaseProgress, activePlanProgress } from './activePlanProgress';
import type { TrajectoryListItem } from './trajectory';

function trajectory(overrides: Partial<TrajectoryListItem>): TrajectoryListItem {
	return {
		loop_id: 'loop-1',
		workflow_slug: 'semspec-planning',
		workflow_step: 'architecture-generation',
		start_time: '2026-06-16T12:57:01Z',
		...overrides
	} as TrajectoryListItem;
}

describe('activePlanProgress', () => {
	it('uses the active planning loop to drive the in-progress copy', () => {
		const progress = activePlanProgress([
			trajectory({
				loop_id: 'scenario-done',
				workflow_step: 'scenario-generation',
				outcome: 'success',
				start_time: '2026-06-16T13:00:17Z'
			}),
			trajectory({
				loop_id: 'arch-active',
				workflow_step: 'architecture-generation',
				start_time: '2026-06-16T13:01:46Z'
			})
		]);

		expect(progress?.title).toBe('Generating architecture...');
		expect(progress?.detail).toContain('Architecture generator');
		expect(progress?.startedAt).toBe('2026-06-16T13:01:46Z');
		expect(progress?.loopId).toBe('arch-active');
	});

	it('ignores completed planning loops', () => {
		const progress = activePlanProgress([
			trajectory({ workflow_step: 'scenario-generation', outcome: 'success' })
		]);
		expect(progress).toBeNull();
	});

	it('ignores active execution loops in the planning-only helper', () => {
		const progress = activePlanProgress([
			trajectory({
				workflow_slug: 'semspec-task-execution',
				workflow_step: 'develop'
			})
		]);
		expect(progress).toBeNull();
	});

	it('chooses the newest active planning loop when several are running', () => {
		const progress = activePlanProgress([
			trajectory({
				loop_id: 'old',
				workflow_step: 'architecture-generation',
				start_time: '2026-06-16T12:55:00Z'
			}),
			trajectory({
				loop_id: 'new',
				workflow_step: 'scenario-generation',
				start_time: '2026-06-16T13:00:00Z'
			})
		]);
		expect(progress?.title).toBe('Generating scenarios...');
		expect(progress?.loopId).toBe('new');
	});
});

describe('activePhaseProgress', () => {
	it('uses active execution loops when no planning loop is running', () => {
		const progress = activePhaseProgress([
			trajectory({
				loop_id: 'dev-active',
				workflow_slug: 'semspec-task-execution',
				workflow_step: 'develop',
				start_time: '2026-06-16T13:09:08Z'
			})
		]);

		expect(progress?.title).toBe('Implementing...');
		expect(progress?.detail).toContain('Developer');
		expect(progress?.startedAt).toBe('2026-06-16T13:09:08Z');
		expect(progress?.loopId).toBe('dev-active');
	});

	it('uses active review execution loops', () => {
		const progress = activePhaseProgress([
			trajectory({
				loop_id: 'review-active',
				workflow_slug: 'semspec-task-execution',
				workflow_step: 'review'
			})
		]);

		expect(progress?.title).toBe('Reviewing implementation...');
		expect(progress?.loopId).toBe('review-active');
	});

	it('prefers active planning loops over execution loops during phase overlap', () => {
		const progress = activePhaseProgress([
			trajectory({
				loop_id: 'dev-active',
				workflow_slug: 'semspec-task-execution',
				workflow_step: 'develop',
				start_time: '2026-06-16T13:09:08Z'
			}),
			trajectory({
				loop_id: 'arch-active',
				workflow_slug: 'semspec-planning',
				workflow_step: 'architecture-generation',
				start_time: '2026-06-16T13:01:46Z'
			})
		]);

		expect(progress?.title).toBe('Generating architecture...');
		expect(progress?.loopId).toBe('arch-active');
	});
});
