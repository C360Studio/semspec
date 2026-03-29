import { browser } from '$app/environment';
import { untrack } from 'svelte';
import type {
	FeedEvent,
	PlanSSEPayload,
	TaskSSEPayload,
	RequirementSSEPayload
} from '$lib/types/feed';
import type { Question } from '$lib/types';
import { questionsStore } from './questions.svelte';
import { settingsStore } from './settings.svelte';

/**
 * Plan-scoped feed store. Aggregates plan SSE + execution SSE + questions
 * into a unified FeedEvent stream for the Activity Feed panel.
 *
 * Usage:
 *   feedStore.connectPlan('abc123');  // opens plan + execution SSEs
 *   feedStore.disconnectPlan();       // closes both
 */
class FeedStore {
	events = $state<FeedEvent[]>([]);
	connected = $state(false);
	currentSlug = $state<string | null>(null);

	private planSSE: EventSource | null = null;
	private execSSE: EventSource | null = null;
	private maxEvents = $derived(settingsStore.activityLimit);
	private lastPlanStage: string | null = null;
	private seenQuestionIds = new Set<string>();
	private changeCallbacks: Set<(event: FeedEvent) => void> = new Set();

	/** Subscribe to any data change (plan, task, requirement) for page invalidation. */
	onChange(callback: (event: FeedEvent) => void): () => void {
		this.changeCallbacks.add(callback);
		return () => this.changeCallbacks.delete(callback);
	}

	/** @deprecated Use onChange instead */
	onPlanEvent(callback: (event: FeedEvent) => void): () => void {
		return this.onChange(callback);
	}

	connectPlan(slug: string): void {
		if (!browser) return;

		// untrack prevents callers' $effect from tracking our internal $state reads.
		// Without this, reading this.currentSlug creates a dependency cycle:
		// effect reads state → connect writes state → effect re-runs → cleanup writes state → loop.
		untrack(() => {
			if (this.currentSlug && this.currentSlug !== slug) {
				this.disconnectPlan();
			}
			if (this.currentSlug === slug && this.planSSE) return;

			this.currentSlug = slug;
			this.lastPlanStage = null;
			this.seenQuestionIds.clear();

			// Only connect plan SSE initially. Execution SSE connects lazily
			// when the plan reaches execution stage — avoids consuming browser
			// connections (HTTP/1.1 limit of 6 per origin) during planning phase.
			this.connectPlanSSE(slug);
			this.connected = true;
		});
	}

	disconnectPlan(): void {
		untrack(() => {
			this.planSSE?.close();
			this.planSSE = null;
			this.execSSE?.close();
			this.execSSE = null;
			this.connected = false;
			this.currentSlug = null;
			this.lastPlanStage = null;
			this.seenQuestionIds.clear();
		});
	}

	/** Inject a question event (called from outside when questionsStore updates) */
	addQuestionEvent(question: Question, type: 'question_created' | 'question_answered' | 'question_timeout'): void {
		// Skip if not for current plan
		if (this.currentSlug && question.plan_slug && question.plan_slug !== this.currentSlug) return;

		const key = `${type}-${question.id}`;
		if (this.seenQuestionIds.has(key)) return;
		this.seenQuestionIds.add(key);

		const summaries: Record<string, string> = {
			question_created: `Agent question: ${question.question?.slice(0, 80) ?? '...'}`,
			question_answered: `Question answered: ${question.question?.slice(0, 60) ?? '...'}`,
			question_timeout: `Question timed out: ${question.question?.slice(0, 60) ?? '...'}`
		};

		this.addEvent({
			id: `question-${question.id}-${type}`,
			timestamp: question.answered_at ?? question.created_at ?? new Date().toISOString(),
			source: 'question',
			type,
			summary: summaries[type] ?? type,
			slug: question.plan_slug,
			data: question as unknown as Record<string, unknown>
		});
	}

	clear(): void {
		this.events = [];
		this.lastPlanStage = null;
		this.seenQuestionIds.clear();
	}

	// ── Private ────────────────────────────────────────────────────

	private connectPlanSSE(slug: string): void {
		const sse = new EventSource(`/plan-manager/plans/${slug}/stream`);

		sse.addEventListener('connected', () => {
			// Plan SSE connected
		});

		sse.addEventListener('plan_updated', (event) => {
			const payload = JSON.parse((event as MessageEvent).data) as PlanSSEPayload;
			this.handlePlanUpdated(payload);
		});

		sse.addEventListener('plan_deleted', () => {
			this.addEvent({
				id: `plan-deleted-${Date.now()}`,
				timestamp: new Date().toISOString(),
				source: 'plan',
				type: 'plan_deleted',
				summary: 'Plan deleted',
				slug
			});
		});

		sse.onerror = () => {
			// Will auto-reconnect via EventSource
		};

		this.planSSE = sse;
	}

	private connectExecutionSSE(slug: string): void {
		const sse = new EventSource(`/execution-manager/plans/${slug}/stream`);

		sse.addEventListener('connected', () => {
			// Execution SSE connected
		});

		for (const name of ['task_updated', 'task_completed']) {
			sse.addEventListener(name, (event) => {
				const payload = JSON.parse((event as MessageEvent).data) as TaskSSEPayload;
				this.handleTaskUpdated(payload, name);
			});
		}

		for (const name of ['requirement_updated', 'requirement_completed']) {
			sse.addEventListener(name, (event) => {
				const payload = JSON.parse((event as MessageEvent).data) as RequirementSSEPayload;
				this.handleRequirementUpdated(payload, name);
			});
		}

		sse.onerror = () => {
			// Will auto-reconnect via EventSource
		};

		this.execSSE = sse;
	}

	private handlePlanUpdated(payload: PlanSSEPayload): void {
		const stage = payload.stage;
		const prevStage = this.lastPlanStage;
		this.lastPlanStage = stage;

		// Lazily connect execution SSE when plan reaches execution
		const execStages = ['implementing', 'executing', 'reviewing_rollup'];
		if (execStages.includes(stage) && !this.execSSE && this.currentSlug) {
			this.connectExecutionSSE(this.currentSlug);
		}

		// Skip duplicate stage events in the feed, but still notify subscribers
		// so load functions re-fetch (plan data like active_loops changes within a stage)
		if (prevStage === stage) {
			this.notifyChange({
				id: `plan-${payload.slug}-${stage}-${Date.now()}`,
				timestamp: new Date().toISOString(),
				source: 'plan',
				type: 'plan_updated',
				summary: `Plan: ${formatStage(stage)}`,
				slug: payload.slug,
				data: payload as unknown as Record<string, unknown>
			});
			return;
		}

		const summary = prevStage
			? `Plan: ${formatStage(prevStage)} → ${formatStage(stage)}`
			: `Plan: ${formatStage(stage)}`;

		const event: FeedEvent = {
			id: `plan-${payload.slug}-${stage}-${Date.now()}`,
			timestamp: new Date().toISOString(),
			source: 'plan',
			type: 'plan_updated',
			summary,
			slug: payload.slug,
			data: payload as unknown as Record<string, unknown>
		};
		this.addEvent(event);
		this.notifyChange(event);
	}

	private handleTaskUpdated(payload: TaskSSEPayload, eventType: string): void {
		const title = payload.title?.slice(0, 50) ?? payload.task_id;
		const stage = payload.stage;
		const iter = payload.iteration > 1 ? ` (iter ${payload.iteration})` : '';

		let summary: string;
		if (eventType === 'task_completed') {
			const verdict = payload.verdict ?? stage;
			summary = `Task ${verdict}: ${title}${iter}`;
		} else {
			summary = `Task ${formatTaskStage(stage)}: ${title}${iter}`;
		}

		const event: FeedEvent = {
			id: `task-${payload.task_id}-${stage}-${Date.now()}`,
			timestamp: new Date().toISOString(),
			source: 'execution',
			type: eventType,
			summary,
			slug: payload.slug,
			data: payload as unknown as Record<string, unknown>
		};
		this.addEvent(event);
		this.notifyChange(event);
	}

	private handleRequirementUpdated(payload: RequirementSSEPayload, eventType: string): void {
		const title = payload.title?.slice(0, 50) ?? payload.requirement_id;
		const stage = payload.stage;
		const progress = payload.node_count
			? ` (${(payload.current_node_idx ?? 0) + 1}/${payload.node_count})`
			: '';

		let summary: string;
		if (eventType === 'requirement_completed') {
			summary = `Requirement ${stage}: ${title}`;
		} else {
			summary = `Requirement ${formatReqStage(stage)}: ${title}${progress}`;
		}

		const event: FeedEvent = {
			id: `req-${payload.requirement_id}-${stage}-${Date.now()}`,
			timestamp: new Date().toISOString(),
			source: 'execution',
			type: eventType,
			summary,
			slug: payload.slug,
			data: payload as unknown as Record<string, unknown>
		};
		this.addEvent(event);
		this.notifyChange(event);
	}

	private notifyChange(event: FeedEvent): void {
		for (const cb of this.changeCallbacks) {
			cb(event);
		}
	}

	private addEvent(event: FeedEvent): void {
		this.events = [...this.events.slice(-(this.maxEvents - 1)), event];
	}
}

// ── Formatters ────────────────────────────────────────────────────

function formatStage(stage: string): string {
	const labels: Record<string, string> = {
		drafting: 'Drafting',
		drafted: 'Drafted',
		reviewed: 'Reviewed',
		approved: 'Approved',
		requirements_generated: 'Requirements Generated',
		scenarios_generated: 'Scenarios Generated',
		scenarios_reviewed: 'Scenarios Reviewed',
		ready_for_execution: 'Ready for Execution',
		implementing: 'Executing',
		executing: 'Executing',
		reviewing_rollup: 'Reviewing',
		complete: 'Complete',
		failed: 'Failed'
	};
	return labels[stage] ?? stage.replace(/_/g, ' ');
}

function formatTaskStage(stage: string): string {
	const labels: Record<string, string> = {
		testing: 'testing',
		building: 'building',
		validating: 'validating',
		reviewing: 'reviewing',
		approved: 'approved',
		escalated: 'escalated',
		error: 'error',
		rejected: 'rejected'
	};
	return labels[stage] ?? stage;
}

function formatReqStage(stage: string): string {
	const labels: Record<string, string> = {
		decomposing: 'decomposing',
		executing: 'executing',
		reviewing: 'reviewing',
		completed: 'completed',
		failed: 'failed',
		error: 'error'
	};
	return labels[stage] ?? stage;
}

export const feedStore = new FeedStore();

// Re-export questionsStore integration helper.
// Wire this in the layout or plan page via $effect watching questionsStore.
export function syncQuestionsToFeed(): void {
	// This is a reactive helper — call inside $effect.
	// It watches questionsStore.all and pushes new pending questions to feed.
	for (const q of questionsStore.pending) {
		feedStore.addQuestionEvent(q, 'question_created');
	}
	for (const q of questionsStore.answered) {
		feedStore.addQuestionEvent(q, 'question_answered');
	}
	for (const q of questionsStore.timedOut) {
		feedStore.addQuestionEvent(q, 'question_timeout');
	}
}
