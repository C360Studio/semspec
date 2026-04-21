/**
 * Projection from global ActivityEvent (loop ticks) to the FeedEvent shape
 * ActivityFeed.svelte consumes.
 *
 * Bug #7.2: on /board with no selected plan, feedStore.events is empty
 * (feedStore is plan-scoped and only fills when connectPlan runs). The user
 * sees "0 events · Waiting for plan..." while the backend is firing 485+
 * graph mutations and dozens of loop ticks per minute. Rendering global loop
 * events solves that without blurring the plan-scoped feed on plan pages.
 *
 * Pure function — callable from `$derived.by(...)` in the component. No store
 * reads here; the store reads happen at the component's derived boundary.
 */
import type { ActivityEvent } from '$lib/types';
import type { FeedEvent } from '$lib/types/feed';

/** Convert one ActivityEvent to a FeedEvent. Stable `id` enables dedup when
 * the component keys the `{#each}` block on it. */
export function activityEventToFeedEvent(event: ActivityEvent): FeedEvent {
	const loopShort = event.loop_id?.slice(0, 8) ?? 'unknown';
	const summary = summaryFor(event.type, loopShort);

	return {
		id: `${event.type}:${event.loop_id}:${event.timestamp}`,
		timestamp: event.timestamp,
		source: 'execution',
		type: event.type,
		summary,
		data: { loop_id: event.loop_id }
	};
}

/** Bulk projection with optional maxEvents cap (most recent first). */
export function projectActivityFeed(events: ActivityEvent[], maxEvents?: number): FeedEvent[] {
	const sliced = maxEvents !== undefined ? events.slice(-maxEvents) : events;
	return sliced.map(activityEventToFeedEvent);
}

function summaryFor(type: string, loopShort: string): string {
	switch (type) {
		case 'loop_created':
			return `Loop started · ${loopShort}`;
		case 'loop_updated':
			return `Loop ticked · ${loopShort}`;
		case 'loop_deleted':
		case 'loop_completed':
			return `Loop finished · ${loopShort}`;
		default:
			return `${type} · ${loopShort}`;
	}
}
