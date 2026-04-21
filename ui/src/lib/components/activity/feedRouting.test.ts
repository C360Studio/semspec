import { describe, it, expect } from 'vitest';
import { getEventHref, getEventLinkText } from './feedRouting';
import type { FeedEvent } from '$lib/types/feed';

const baseEvent = (over: Partial<FeedEvent> = {}): FeedEvent => ({
	id: 'id-1',
	timestamp: '2026-04-21T00:00:00Z',
	source: 'execution',
	type: 'task_updated',
	summary: 'sum',
	...over
});

describe('getEventHref — bug #7.8 row routing', () => {
	// Any event carrying a loop_id routes to /trajectories/{id}. Prior code
	// only routed task_* events; loop_created/updated/deleted fell through.
	it.each(['task_updated', 'task_completed', 'loop_created', 'loop_updated', 'loop_deleted'])(
		'type=%s with loop_id routes to /trajectories/{id}',
		(type) => {
			const ev = baseEvent({ type, data: { loop_id: 'abc123' } });
			expect(getEventHref(ev)).toBe('/trajectories/abc123');
		}
	);

	it('plan event with slug routes to /plans/{slug}', () => {
		const ev = baseEvent({ source: 'plan', type: 'plan_updated', slug: 'my-plan' });
		expect(getEventHref(ev)).toBe('/plans/my-plan');
	});

	it('plan_deleted returns null even when slug is present', () => {
		// Clicking would land on a 404. Better to render as non-interactive
		// than advertise a dead link.
		const ev = baseEvent({ source: 'plan', type: 'plan_deleted', slug: 'gone' });
		expect(getEventHref(ev)).toBeNull();
	});

	it('question events with no loop_id return null', () => {
		const ev = baseEvent({ source: 'question', type: 'question_asked' });
		expect(getEventHref(ev)).toBeNull();
	});

	it('empty-string loop_id is treated as missing', () => {
		// Guards against backend sending loop_id="" which would yield
		// "/trajectories/" — a broken URL that would 404.
		const ev = baseEvent({ data: { loop_id: '' } });
		expect(getEventHref(ev)).toBeNull();
	});

	it('loop_id takes precedence over slug when both present', () => {
		// Task events can carry both; the trajectory is the more specific
		// landing page for debugging execution, so it wins.
		const ev = baseEvent({ slug: 'p1', data: { loop_id: 'loop-9' } });
		expect(getEventHref(ev)).toBe('/trajectories/loop-9');
	});
});

describe('getEventLinkText — destination badge copy', () => {
	it('"trajectory" when loop_id drives the link', () => {
		const ev = baseEvent({ data: { loop_id: 'x' } });
		expect(getEventLinkText(ev)).toBe('trajectory');
	});

	it('plan slug when routing to a plan', () => {
		const ev = baseEvent({ source: 'plan', type: 'plan_updated', slug: 'mortgage-calc' });
		expect(getEventLinkText(ev)).toBe('mortgage-calc');
	});

	it('"plan" fallback when no slug and no loop_id', () => {
		const ev = baseEvent({ source: 'plan', type: 'plan_updated' });
		expect(getEventLinkText(ev)).toBe('plan');
	});
});
