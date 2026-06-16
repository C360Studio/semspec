import { describe, expect, it, vi } from 'vitest';
import { mergeLiveTrajectoryItems } from './trajectoryActivityProjection';
import type { ActivityEvent } from './index';
import type { TrajectoryListItem } from './trajectory';

type ActivityOverride = Omit<Partial<ActivityEvent>, 'data'> & {
	data?: unknown;
	type?: string;
};

function activity(overrides: ActivityOverride): ActivityEvent {
	return {
		loop_id: 'loop-live',
		timestamp: '2026-06-16T13:10:00Z',
		type: 'loop_updated',
		...overrides
	} as ActivityEvent;
}

function loaded(overrides: Partial<TrajectoryListItem> = {}): TrajectoryListItem {
	return {
		duration: 1000,
		iterations: 1,
		loop_id: 'loop-old',
		model: 'gemini-pro',
		role: 'general',
		start_time: '2026-06-16T13:00:00Z',
		task_id: 'old-task',
		total_tokens_in: 10,
		total_tokens_out: 2,
		workflow_slug: 'semspec-planning',
		workflow_step: 'drafting',
		...overrides
	} as TrajectoryListItem;
}

describe('mergeLiveTrajectoryItems', () => {
	it('adds live execution loops for the current plan', () => {
		const merged = mergeLiveTrajectoryItems([], [
			activity({
				data: {
					created_at: '2026-06-16T13:09:08Z',
					iterations: 4,
					loop_id: 'loop-live',
					metadata: {
						model: 'gpt-5-5',
						plan_slug: 'mavlink',
						role: 'developer',
						task_id: 'node-1'
					},
					task_id: 'dev-run-1',
					workflow_slug: 'semspec-task-execution',
					workflow_step: 'develop'
				}
			})
		], 'mavlink');

		expect(merged).toHaveLength(1);
		expect(merged[0].loop_id).toBe('loop-live');
		expect(merged[0].workflow_slug).toBe('semspec-task-execution');
		expect(merged[0].workflow_step).toBe('develop');
		expect(merged[0].iterations).toBe(4);
		expect(merged[0].model).toBe('gpt-5-5');
		expect(merged[0].role).toBe('developer');
	});

	it('ignores events for other plans', () => {
		const merged = mergeLiveTrajectoryItems([loaded()], [
			activity({
				data: {
					loop_id: 'other-loop',
					metadata: { plan_slug: 'other' },
					workflow_slug: 'semspec-task-execution',
					workflow_step: 'develop'
				}
			})
		], 'mavlink');

		expect(merged.map((item) => item.loop_id)).toEqual(['loop-old']);
	});

	it('overlays live updates onto loader rows by loop id', () => {
		const merged = mergeLiveTrajectoryItems([
			loaded({
				loop_id: 'loop-live',
				iterations: 1,
				total_tokens_in: 20
			})
		], [
			activity({
				loop_id: 'loop-live',
				data: {
					iterations: 5,
					loop_id: 'loop-live',
					metadata: { plan_slug: 'mavlink' },
					workflow_slug: 'semspec-task-execution',
					workflow_step: 'develop'
				}
			})
		], 'mavlink');

		expect(merged).toHaveLength(1);
		expect(merged[0].iterations).toBe(5);
		expect(merged[0].total_tokens_in).toBe(20);
	});

	it('marks loop_completed as success when the event has no explicit outcome', () => {
		const merged = mergeLiveTrajectoryItems([], [
			activity({
				type: 'loop_completed',
				data: {
					completed_at: '2026-06-16T13:11:00Z',
					created_at: '2026-06-16T13:10:00Z',
					loop_id: 'loop-live',
					metadata: { plan_slug: 'mavlink' },
					workflow_slug: 'semspec-task-execution',
					workflow_step: 'review'
				}
			})
		], 'mavlink');

		expect(merged[0].outcome).toBe('success');
		expect(merged[0].duration).toBe(60_000);
	});

	it('uses current time for active-loop duration', () => {
		vi.useFakeTimers();
		vi.setSystemTime(new Date('2026-06-16T13:11:00Z'));

		const merged = mergeLiveTrajectoryItems([], [
			activity({
				data: {
					created_at: '2026-06-16T13:10:00Z',
					loop_id: 'loop-live',
					metadata: { plan_slug: 'mavlink' },
					workflow_slug: 'semspec-task-execution',
					workflow_step: 'develop'
				}
			})
		], 'mavlink');

		expect(merged[0].duration).toBe(60_000);
		vi.useRealTimers();
	});
});
