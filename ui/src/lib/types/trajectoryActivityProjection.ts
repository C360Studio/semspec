import type { ActivityEvent } from './index';
import type { TrajectoryListItem } from './trajectory';

type LiveLoopData = {
	completed_at?: string;
	created_at?: string;
	duration?: number;
	end_time?: string | null;
	iterations?: number;
	loop_id?: string;
	metadata?: Record<string, unknown>;
	model?: string;
	outcome?: string;
	role?: string;
	start_time?: string;
	state?: string;
	task_id?: string;
	total_tokens_in?: number;
	total_tokens_out?: number;
	workflow_slug?: string;
	workflow_step?: string;
};

export function mergeLiveTrajectoryItems(
	loadedItems: TrajectoryListItem[],
	activityEvents: ActivityEvent[],
	planSlug: string
): TrajectoryListItem[] {
	if (!planSlug) return loadedItems;

	const byLoop = new Map(loadedItems.map((item) => [item.loop_id, item]));

	for (const event of activityEvents) {
		const loop = loopData(event);
		const loopID = loop?.loop_id ?? event.loop_id;
		if (!loop || !loopID) continue;
		if (loopPlanSlug(loop) !== planSlug) continue;
		if (!isLoopMutation(event.type)) continue;

		const previous = byLoop.get(loopID);
		byLoop.set(loopID, toTrajectoryItem(event, loop, previous));
	}

	return Array.from(byLoop.values()).sort((a, b) => startMs(a) - startMs(b));
}

function loopData(event: ActivityEvent): LiveLoopData | null {
	const raw = (event as ActivityEvent & { data?: unknown }).data;
	if (!raw || typeof raw !== 'object') return null;
	return raw as LiveLoopData;
}

function loopPlanSlug(loop: LiveLoopData): string | null {
	const raw = loop.metadata?.plan_slug;
	return typeof raw === 'string' ? raw : null;
}

function isLoopMutation(type: string): boolean {
	return type === 'loop_created' || type === 'loop_updated' || type === 'loop_completed';
}

function toTrajectoryItem(
	event: ActivityEvent,
	loop: LiveLoopData,
	previous: TrajectoryListItem | undefined
): TrajectoryListItem {
	const startTime = loop.start_time ?? loop.created_at ?? previous?.start_time ?? event.timestamp;
	const endTime = loop.end_time ?? loop.completed_at ?? previous?.end_time ?? null;
	const outcome = loop.outcome ?? previous?.outcome ?? (event.type === 'loop_completed' ? 'success' : undefined);

	return {
		duration: loop.duration ?? previous?.duration ?? durationMs(startTime, endTime),
		end_time: endTime,
		iterations: loop.iterations ?? previous?.iterations ?? 0,
		loop_id: loop.loop_id ?? event.loop_id,
		metadata: loop.metadata ?? previous?.metadata,
		model: stringFrom(loop.model ?? loop.metadata?.model ?? previous?.model),
		outcome,
		role: stringFrom(loop.role ?? loop.metadata?.role ?? previous?.role) || 'general',
		start_time: startTime,
		task_id: stringFrom(loop.task_id ?? loop.metadata?.task_id ?? previous?.task_id),
		total_tokens_in: loop.total_tokens_in ?? previous?.total_tokens_in ?? 0,
		total_tokens_out: loop.total_tokens_out ?? previous?.total_tokens_out ?? 0,
		workflow_slug: loop.workflow_slug ?? previous?.workflow_slug,
		workflow_step: loop.workflow_step ?? previous?.workflow_step
	};
}

function stringFrom(value: unknown): string {
	return typeof value === 'string' ? value : '';
}

function durationMs(startTime: string, endTime: string | null | undefined): number {
	const start = new Date(startTime).getTime();
	if (!Number.isFinite(start)) return 0;
	const end = endTime ? new Date(endTime).getTime() : Date.now();
	if (!Number.isFinite(end)) return 0;
	return Math.max(0, end - start);
}

function startMs(item: TrajectoryListItem): number {
	return new Date(item.start_time).getTime();
}
