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

/**
 * Count events by `source` field for the filter dropdown (bug #7.5).
 * Returns totals for "all" plus each known source, so the dropdown can show
 * "All events (42) / Plan (5) / Execution (37) / Questions (0)" and the user
 * can tell at a glance which source dominates.
 *
 * `knownSources` is typed off FeedEvent['source'] so the compiler fails the
 * component build if the wire-level union grows and the component's filter
 * options aren't updated alongside.
 */
export function countBySource(
	events: FeedEvent[],
	knownSources: readonly FeedEvent['source'][]
): Record<string, number> {
	const counts: Record<string, number> = { all: events.length };
	for (const src of knownSources) {
		counts[src] = 0;
	}
	for (const event of events) {
		if (event.source in counts) counts[event.source]++;
	}
	return counts;
}

/**
 * Compact requirement anchor (e.g. "R3", "R12") for events carrying a
 * requirement_id. Returns null when the event isn't requirement-scoped so
 * the template can conditionally render the pill.
 *
 * Requirement IDs arrive in two shapes:
 *   - Short: "R1", "R3", "r12" — kept as-is, upper-cased.
 *   - Long:  "requirement.<slug>.3" — reduced to the trailing segment.
 *
 * Bug #7.9 — the feed is visually uniform until you read every summary.
 * A requirement pill lets the eye filter rows at a glance.
 */
export function getRequirementAnchor(event: FeedEvent): string | null {
	const raw = event.data?.requirement_id;
	if (typeof raw !== 'string' || raw.length === 0) return null;

	const shortPattern = /^r\d+$/i;
	if (shortPattern.test(raw)) return raw.toUpperCase();

	// Long-form: take the trailing segment. "requirement.slug.3" -> "R3".
	const parts = raw.split('.');
	const tail = parts[parts.length - 1];
	if (!tail) return null;
	// If the tail is numeric-ish, prefix with R. Otherwise show the tail
	// as-is (covers UUID-style requirement IDs without producing a junk label).
	return /^\d+$/.test(tail) ? `R${tail}` : tail.toUpperCase();
}
