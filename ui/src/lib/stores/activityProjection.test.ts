/**
 * Tests for ActivityEvent → FeedEvent projection used by ActivityFeed when
 * scope="global" (bug #7.2). Ensures the summaries are human-readable and
 * that loop_id segmentation is stable for dedup keys.
 */
import { describe, it, expect } from 'vitest';
import {
	activityEventToFeedEvent,
	projectActivityFeed
} from '$lib/stores/activityProjection';
import type { ActivityEvent } from '$lib/types';

type ActivityOverride = Omit<Partial<ActivityEvent>, 'data'> & {
	type: string;
	data?: unknown;
} & Record<string, unknown>;

function activity(overrides: ActivityOverride): ActivityEvent {
	return {
		loop_id: '01234567-abcd',
		timestamp: '2026-04-21T12:00:00Z',
		...overrides
	} as ActivityEvent;
}

describe('activityEventToFeedEvent', () => {
	it('maps loop_created to a start summary with loop id prefix', () => {
		const fe = activityEventToFeedEvent(activity({ type: 'loop_created' }));
		expect(fe.source).toBe('activity');
		expect(fe.type).toBe('loop_created');
		expect(fe.kind).toBe('activity_loop');
		expect(fe.summary).toMatch(/Loop started/);
		expect(fe.summary).toContain('01234567');
	});

	it('labels semspec-planning loop data as plan activity', () => {
		const fe = activityEventToFeedEvent(activity({
			type: 'loop_updated',
			data: {
				task_id: 'scengen-4',
				workflow_slug: 'semspec-planning',
				workflow_step: 'scenario-generation',
				role: 'scenario-generator'
			}
		}));
		expect(fe.source).toBe('plan');
		expect(fe.kind).toBe('activity_loop');
		expect(fe.data?.workflow_slug).toBe('semspec-planning');
		expect(fe.data?.workflow_step).toBe('scenario-generation');
		expect(fe.data?.task_id).toBe('scengen-4');
		expect(fe.data?.role).toBe('scenario-generator');
	});

	it('labels semspec-execution loop data as execution activity', () => {
		const fe = activityEventToFeedEvent(activity({
			type: 'loop_updated',
			data: {
				task_id: 'reqexec-2',
				workflow_slug: 'semspec-execution',
				workflow_step: 'executing',
				role: 'developer'
			}
		}));
		expect(fe.source).toBe('execution');
		expect(fe.kind).toBe('activity_loop');
	});

	it('labels task-execution workflow loop data as execution activity', () => {
		const fe = activityEventToFeedEvent(activity({
			type: 'loop_updated',
			data: {
				task_id: 'task-run-1',
				workflow_slug: 'semspec-task-execution',
				workflow_step: 'develop',
				role: 'general'
			}
		}));
		expect(fe.source).toBe('execution');
	});

	it('labels lesson-decomposer loops as future-only lesson activity', () => {
		const fe = activityEventToFeedEvent(activity({
			type: 'loop_updated',
			data: {
				task_id: 'decompose-demo-1',
				workflow_slug: 'semspec-lesson-decomposition',
				workflow_step: 'decompose',
				role: 'lesson-decomposer'
			}
		}));
		expect(fe.source).toBe('activity');
		expect(fe.kind).toBe('lesson_activity');
		expect(fe.summary).toContain('Lesson decomposer active');
		expect(fe.summary).toContain('future-only');
		expect(fe.data?.current_run_effect).toBe('none');
		expect(fe.data?.future_run_effect).toBe('eligible_for_future_prompts');
		expect(fe.data?.effect_label).toBe('future-only');
	});

	it('labels lesson-curator metadata as lesson activity', () => {
		const fe = activityEventToFeedEvent(activity({
			type: 'loop_completed',
			data: {
				task_id: 'lesson-curator-sweep',
				workflow_step: 'lesson-decomposition',
				role: 'lesson-curator'
			}
		}));
		expect(fe.kind).toBe('lesson_activity');
		expect(fe.summary).toContain('Lesson curator finished');
		expect(fe.data?.current_run_effect).toBe('none');
	});

	it('maps loop_updated to a tick summary', () => {
		const fe = activityEventToFeedEvent(activity({ type: 'loop_updated' }));
		expect(fe.summary).toMatch(/ticked|tick/i);
	});

	it('maps loop_deleted and loop_completed to a finish summary', () => {
		const deleted = activityEventToFeedEvent(activity({ type: 'loop_deleted' }));
		const completed = activityEventToFeedEvent(activity({ type: 'loop_completed' }));
		expect(deleted.summary).toMatch(/finish/i);
		expect(completed.summary).toMatch(/finish/i);
	});

	it('falls back to type-prefixed summary for unknown types', () => {
		const fe = activityEventToFeedEvent(activity({ type: 'loop_custom_thing' }));
		expect(fe.summary).toContain('loop_custom_thing');
	});

	it('generates stable dedup id from type + loop_id + timestamp', () => {
		const a = activityEventToFeedEvent(
			activity({ type: 'loop_updated', timestamp: '2026-04-21T12:00:01Z' })
		);
		const b = activityEventToFeedEvent(
			activity({ type: 'loop_updated', timestamp: '2026-04-21T12:00:01Z' })
		);
		expect(a.id).toBe(b.id);
	});

	it('preserves loop_id in data for drill-down links', () => {
		const fe = activityEventToFeedEvent(activity({ type: 'loop_updated' }));
		expect(fe.data?.loop_id).toBe('01234567-abcd');
	});

	it('passes requirement_id through to data when present (bug #7.9)', () => {
		// The anchor-pill extractor (getRequirementAnchor) reads
		// event.data?.requirement_id; if the projection drops the field the
		// pill never renders for loop-emitted events even when the backend
		// tags them with a requirement.
		const raw = { ...activity({ type: 'loop_updated' }), requirement_id: 'R4' };
		const fe = activityEventToFeedEvent(raw as ActivityEvent);
		expect(fe.data?.requirement_id).toBe('R4');
	});

	it('passes requirement_id through from loop data when present', () => {
		const fe = activityEventToFeedEvent(activity({
			type: 'loop_updated',
			data: {
				workflow_slug: 'semspec-planning',
				requirement_id: 'R8'
			}
		}));
		expect(fe.data?.requirement_id).toBe('R8');
	});

	it('omits requirement_id when empty string on the raw event', () => {
		// Prevents "R" phantom pills from undefined-backend-data.
		const raw = { ...activity({ type: 'loop_updated' }), requirement_id: '' };
		const fe = activityEventToFeedEvent(raw as ActivityEvent);
		expect(fe.data?.requirement_id).toBeUndefined();
	});
});

describe('projectActivityFeed', () => {
	it('maps every event and preserves order', () => {
		const events = [
			activity({ type: 'loop_created', timestamp: '2026-04-21T12:00:00Z', loop_id: 'a' }),
			activity({ type: 'loop_updated', timestamp: '2026-04-21T12:00:01Z', loop_id: 'b' }),
			activity({ type: 'loop_deleted', timestamp: '2026-04-21T12:00:02Z', loop_id: 'c' })
		];
		const feed = projectActivityFeed(events);
		expect(feed.map((e) => e.data?.loop_id)).toEqual(['a', 'b', 'c']);
	});

	it('caps at maxEvents taking the most recent N', () => {
		const events = Array.from({ length: 20 }, (_, i) =>
			activity({
				type: 'loop_updated',
				timestamp: `2026-04-21T12:00:${String(i).padStart(2, '0')}Z`,
				loop_id: `loop-${i}`
			})
		);
		const feed = projectActivityFeed(events, 5);
		expect(feed).toHaveLength(5);
		expect(feed[0].data?.loop_id).toBe('loop-15');
		expect(feed[4].data?.loop_id).toBe('loop-19');
	});

	it('returns empty for empty input', () => {
		expect(projectActivityFeed([])).toHaveLength(0);
	});
});
