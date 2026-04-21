import { describe, it, expect } from 'vitest';
import {
	getEventHref,
	getEventLinkText,
	getRequirementAnchor,
	countBySource
} from './feedRouting';
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

describe('getRequirementAnchor — bug #7.9 requirement pill', () => {
	it('returns null when no requirement_id', () => {
		expect(getRequirementAnchor(baseEvent())).toBeNull();
		expect(getRequirementAnchor(baseEvent({ data: {} }))).toBeNull();
	});

	it('returns null for empty string', () => {
		// Empty strings from the wire shouldn't produce a ghost "R" pill.
		expect(getRequirementAnchor(baseEvent({ data: { requirement_id: '' } }))).toBeNull();
	});

	it('uppercases short form "r3" -> "R3"', () => {
		expect(getRequirementAnchor(baseEvent({ data: { requirement_id: 'r3' } }))).toBe('R3');
		expect(getRequirementAnchor(baseEvent({ data: { requirement_id: 'R1' } }))).toBe('R1');
	});

	it('extracts trailing integer from dotted form as R{n}', () => {
		// The backend emits these as "requirement.<plan-slug>.<idx>".
		const ev = baseEvent({ data: { requirement_id: 'requirement.mortgage-calc.3' } });
		expect(getRequirementAnchor(ev)).toBe('R3');
	});

	it('falls back to uppercased tail for non-numeric trailing segments', () => {
		// UUID-style IDs would look ugly as "R..." prefix; show the tail
		// verbatim (still short enough for a pill).
		const ev = baseEvent({ data: { requirement_id: 'requirement.p1.auth-flow' } });
		expect(getRequirementAnchor(ev)).toBe('AUTH-FLOW');
	});

	it('handles numeric-only bare ID (unlikely but safe)', () => {
		expect(getRequirementAnchor(baseEvent({ data: { requirement_id: '7' } }))).toBe('R7');
	});
});

describe('countBySource — bug #7.5 dropdown counts', () => {
	const sources = ['plan', 'execution', 'question'] as const;

	it('sums events per source plus an all-events total', () => {
		const events = [
			baseEvent({ id: '1', source: 'plan' }),
			baseEvent({ id: '2', source: 'plan' }),
			baseEvent({ id: '3', source: 'execution' }),
			baseEvent({ id: '4', source: 'execution' }),
			baseEvent({ id: '5', source: 'execution' }),
			baseEvent({ id: '6', source: 'question' })
		];
		const counts = countBySource(events, sources);
		expect(counts).toEqual({ all: 6, plan: 2, execution: 3, question: 1 });
	});

	it('reports zero for sources with no events (so dropdown can grey them)', () => {
		const events = [baseEvent({ source: 'plan' })];
		const counts = countBySource(events, sources);
		expect(counts.plan).toBe(1);
		expect(counts.execution).toBe(0);
		expect(counts.question).toBe(0);
	});

	it('returns zeros + 0 total for empty input', () => {
		const counts = countBySource([], sources);
		expect(counts).toEqual({ all: 0, plan: 0, execution: 0, question: 0 });
	});

	it('ignores events with unknown sources (defensive against wire drift)', () => {
		// If the backend introduces a new source and we haven't rev'd the UI,
		// we must not silently hide those events from the "all" total.
		const events = [
			baseEvent({ source: 'plan' }),
			baseEvent({ source: 'mystery' as FeedEvent['source'] })
		];
		const counts = countBySource(events, sources);
		// "all" still counts every event — only per-source keys are filtered.
		expect(counts.all).toBe(2);
		expect(counts.plan).toBe(1);
		expect(counts.mystery).toBeUndefined();
	});
});
