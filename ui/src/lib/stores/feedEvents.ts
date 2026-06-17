import type {
	FeedEventKind,
	PlanSSEPayload,
	RequirementSSEPayload,
	TaskSSEPayload
} from '$lib/types/feed';

export function classifyPlanFeedKind(payload: Pick<PlanSSEPayload, 'stage' | 'phase_summary'>): FeedEventKind {
	const summary = payload.phase_summary;
	if (summary?.freshness?.stale) return 'plan_stale';
	if (summary?.recovery) return 'plan_recovery';
	if (summary?.wait) return 'plan_wait';
	if (summary?.phase === 'execution') return 'execution_phase';
	return 'plan_stage';
}

export function planFeedEventKey(
	payload: Pick<PlanSSEPayload, 'stage' | 'phase_summary'>,
	kind = classifyPlanFeedKind(payload)
): string {
	const summary = payload.phase_summary;
	return [
		payload.stage,
		kind,
		summary?.recovery?.decision_id ?? '',
		summary?.recovery?.status ?? '',
		summary?.wait?.reason ?? '',
		summary?.freshness?.stale ? 'stale' : 'fresh'
	].join(':');
}

export function classifyExecutionFeedKind(
	payload: Pick<TaskSSEPayload | RequirementSSEPayload, 'slug'>,
	currentSlug: string | null,
	nominalKind: 'execution_task' | 'execution_requirement'
): FeedEventKind {
	if (!currentSlug || !payload.slug) return 'execution_orphaned';
	if (payload.slug !== currentSlug) return 'execution_stale';
	return nominalKind;
}

export function annotateExecutionSummary(summary: string, kind: FeedEventKind): string {
	switch (kind) {
		case 'execution_orphaned':
			return `Orphaned execution: ${summary}`;
		case 'execution_stale':
			return `Stale execution: ${summary}`;
		default:
			return summary;
	}
}
