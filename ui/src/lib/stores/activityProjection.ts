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

type ActivityLoopData = {
	task_id?: string;
	workflow_slug?: string;
	workflow_step?: string;
	role?: string;
	requirement_id?: string;
};

const PLANNING_STEPS = new Set([
	'exploring',
	'drafting',
	'reviewing',
	'requirement-generation',
	'architecture-generation',
	'story-preparation',
	'scenario-generation'
]);

const PLANNING_ROLES = new Set([
	'planner',
	'plan-reviewer',
	'requirement-generator',
	'architect',
	'story-preparer',
	'scenario-generator'
]);

const EXECUTION_STEPS = new Set([
	'decomposing',
	'develop',
	'executing',
	'developing',
	'review',
	'validating',
	'reviewing-code',
	'reviewing_rollup',
	'qa-review'
]);

const EXECUTION_ROLES = new Set([
	'developer',
	'reviewer',
	'validator',
	'qa-reviewer',
	'recovery-agent'
]);

/** Convert one ActivityEvent to a FeedEvent. Stable `id` enables dedup when
 * the component keys the `{#each}` block on it. Carries `requirement_id`
 * through when the raw event includes it so the feed UI can render the
 * per-requirement anchor pill (bug #7.9) without a separate KV lookup. */
export function activityEventToFeedEvent(event: ActivityEvent): FeedEvent {
	const loopShort = event.loop_id?.slice(0, 8) ?? 'unknown';
	const summary = summaryFor(event.type, loopShort);
	const loop = eventData(event);

	const data: Record<string, unknown> = { loop_id: event.loop_id };
	if (loop?.task_id) data.task_id = loop.task_id;
	if (loop?.workflow_slug) data.workflow_slug = loop.workflow_slug;
	if (loop?.workflow_step) data.workflow_step = loop.workflow_step;
	if (loop?.role) data.role = loop.role;
	// The generated ActivityEvent doesn't declare requirement_id, but the wire
	// payload sometimes carries it when the loop is scoped to a requirement.
	// Narrow the intersection so we pull only the field we actually read.
	const reqId =
		(event as ActivityEvent & { requirement_id?: string }).requirement_id ??
		loop?.requirement_id;
	if (typeof reqId === 'string' && reqId.length > 0) {
		data.requirement_id = reqId;
	}

	return {
		id: `${event.type}:${event.loop_id}:${event.timestamp}`,
		timestamp: event.timestamp,
		source: sourceForLoop(loop),
		type: event.type,
		kind: 'activity_loop',
		summary,
		data
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

function eventData(event: ActivityEvent): ActivityLoopData | null {
	const raw = (event as ActivityEvent & { data?: unknown }).data;
	if (!raw || typeof raw !== 'object') return null;
	return raw as ActivityLoopData;
}

function sourceForLoop(loop: ActivityLoopData | null): FeedEvent['source'] {
	if (!loop) return 'activity';
	if (isPlanningLoop(loop)) return 'plan';
	if (isExecutionLoop(loop)) return 'execution';
	return 'activity';
}

function isPlanningLoop(loop: ActivityLoopData): boolean {
	const workflowSlug = loop.workflow_slug ?? '';
	const step = loop.workflow_step ?? '';
	const taskID = loop.task_id ?? '';
	const role = loop.role ?? '';

	return (
		workflowSlug === 'semspec-planning' ||
		PLANNING_STEPS.has(step) ||
		PLANNING_ROLES.has(role) ||
		/^(analyst|plan|review|reqgen|archgen|storyprep|scengen)-/.test(taskID)
	);
}

function isExecutionLoop(loop: ActivityLoopData): boolean {
	const workflowSlug = loop.workflow_slug ?? '';
	const step = loop.workflow_step ?? '';
	const taskID = loop.task_id ?? '';
	const role = loop.role ?? '';

	return (
		workflowSlug === 'semspec-execution' ||
		workflowSlug === 'semspec-task-execution' ||
		EXECUTION_STEPS.has(step) ||
		EXECUTION_ROLES.has(role) ||
		/^(reqexec|exec|dev|reviewer|validator|qa-review|recovery)-/.test(taskID)
	);
}
