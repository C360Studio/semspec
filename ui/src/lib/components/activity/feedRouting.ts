import type { FeedEvent } from '$lib/types/feed';

/**
 * Pick a navigation target for a feed row.
 *
 * Bug #7.8 extended clickability to the whole row; as part of that the
 * routing was broadened so more event types have a destination:
 *   - Anything carrying a loop_id → /trajectories/{loop_id}
 *     (covers task_*, loop_*, and any future loop-bound events)
 *   - Plan events with a slug      → /plans/{slug}
 *   - plan_deleted, question_*     → null (no unambiguous target)
 *
 * When null, the row renders as non-interactive instead of advertising a
 * click that does nothing.
 */
export function getEventHref(event: FeedEvent): string | null {
	const loopId = event.data?.loop_id;
	if (typeof loopId === 'string' && loopId.length > 0) {
		return `/trajectories/${loopId}`;
	}
	if (event.slug && event.type !== 'plan_deleted') {
		return `/plans/${event.slug}`;
	}
	return null;
}

/**
 * Short label for the destination badge inside the row. "trajectory" when
 * routing to /trajectories/{id}; the plan slug otherwise.
 */
export function getEventLinkText(event: FeedEvent): string {
	const loopId = event.data?.loop_id;
	if (typeof loopId === 'string' && loopId.length > 0) {
		return 'trajectory';
	}
	return event.slug ?? 'plan';
}
