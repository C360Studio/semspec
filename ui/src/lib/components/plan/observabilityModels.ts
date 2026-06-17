import {
	calculateCostAccounting,
	measureSummaryUsage,
	mergeMeasuredUsage,
	type CostAccounting,
	type MeasuredTokenUsage,
	type ProviderRate
} from '$lib/types/costAccounting';
import type { PlanPhaseSummary } from '$lib/types/feed';
import type { PlanWithStatus } from '$lib/types/plan';
import type { TrajectoryListItem } from '$lib/types/trajectory';

type Story = NonNullable<PlanWithStatus['stories']>[number];
type RecoveryDecision = NonNullable<PlanWithStatus['plan_decisions']>[number];

export type ExecutionBlocker = {
	label: string;
	detail?: string;
	kind: 'wait' | 'recovery' | 'error' | 'qa';
};

export type TaskCounts = {
	total: number;
	done: number;
	failed: number;
	active: number;
};

export type AffectedNode = {
	kind: 'Requirement' | 'Story' | 'Contract';
	id: string;
};

export type AutoAcceptSummary = {
	label: string;
	detail: string;
	state: 'success' | 'warning' | 'neutral';
};

export type RoleSummary = {
	role: string;
	loops: number;
	tokens: number;
	duration: number;
};

export type LessonActivityModel = {
	lessonLoops: TrajectoryListItem[];
	lessonUsage: MeasuredTokenUsage;
	costAccounting: CostAccounting;
	roleSummaries: RoleSummary[];
	currentEffect: string;
	futureEffect: string;
};

export type FeedFreshnessSnapshot = {
	currentSlug: string | null;
	connected: boolean;
	streamEverConnected: boolean;
	lastSuccessfulUpdateAt: string | null;
};

export type FreshnessIndicatorState = {
	shouldShow: boolean;
	disconnected: boolean;
	stale: boolean;
	statusLabel: string;
	lastUpdateAt: string | null;
	reason?: string;
	source?: string;
};

export type QAOutcomeState = 'success' | 'warning' | 'error' | 'neutral';

const LESSON_STEPS = new Set(['decompose', 'lesson-decompose', 'lesson-decomposition']);
const LESSON_ROLES = new Set(['lesson-decomposer', 'lesson-curator']);

export function shouldShowPhaseSummaryBanner(summary: PlanPhaseSummary): boolean {
	if (summary.phase === 'terminal') return false;
	if (summary.state === 'active' || summary.state === 'waiting') return true;
	return summary.phase === 'execution' || summary.phase === 'recovery' || summary.phase === 'qa';
}

export function phaseSummaryDetail(summary: PlanPhaseSummary): string | undefined {
	if (summary.detail) return summary.detail;
	if (summary.wait?.policy_reason) return summary.wait.policy_reason;
	if (summary.recovery?.summary) return summary.recovery.summary;
	if (summary.qa?.summary) return summary.qa.summary;
	return undefined;
}

export function executionBlockers(plan: PlanWithStatus): ExecutionBlocker[] {
	const summary = plan.phase_summary ?? null;
	const qaRun = plan.qa_run ?? null;
	const items: ExecutionBlocker[] = [];
	if (summary?.wait) {
		items.push({
			label: summary.wait.reason,
			detail: summary.wait.policy_reason ?? summary.wait.required_action,
			kind: 'wait'
		});
	}
	if (summary?.recovery && summary.recovery.status !== 'accepted') {
		items.push({
			label: `PlanDecision ${summary.recovery.status}`,
			detail: summary.recovery.summary ?? summary.recovery.contract_impact_summary,
			kind: 'recovery'
		});
	}
	if (qaRun && !qaRun.passed) {
		items.push({
			label: 'QA failed',
			detail: qaRun.runner_error ?? qaRun.failures?.[0]?.message,
			kind: 'qa'
		});
	}
	if (plan.last_error) {
		items.push({
			label: 'Plan error',
			detail: plan.last_error,
			kind: 'error'
		});
	}
	return items;
}

export function qaOutcomeState(plan: PlanWithStatus): QAOutcomeState {
	const qaRun = plan.qa_run ?? null;
	const verdict = plan.qa_verdict_summary?.verdict;
	if (qaRun?.passed === false) return 'error';
	if (verdict === 'needs_changes' || verdict === 'rejected') return 'error';
	if (plan.stage === 'failed') return 'error';
	if (plan.stage === 'complete_with_deferrals' || verdict === 'conditionally_approved') return 'warning';
	if (qaRun?.passed === true || verdict === 'approved') return 'success';
	return 'neutral';
}

export function storyTaskCounts(story: Story): TaskCounts {
	const tasks = story.tasks ?? [];
	return {
		total: tasks.length,
		done: tasks.filter((task) => ['complete', 'completed', 'approved'].includes(task.status ?? '')).length,
		failed: tasks.filter((task) => ['failed', 'error', 'rejected'].includes(task.status ?? '')).length,
		active: tasks.filter((task) => ['executing', 'in_progress', 'running'].includes(task.status ?? '')).length
	};
}

export function recoveryAffectedNodes(decision: RecoveryDecision): AffectedNode[] {
	return [
		...(decision.affected_requirement_ids ?? []).map((id) => ({ kind: 'Requirement' as const, id })),
		...(decision.affected_story_ids ?? []).map((id) => ({ kind: 'Story' as const, id })),
		...(decision.contract_impact?.affected_ids ?? []).map((id) => ({ kind: 'Contract' as const, id }))
	];
}

export function inferRecoveryAutoAccept(decision: RecoveryDecision): AutoAcceptSummary {
	const impactKind = decision.contract_impact?.kind;
	const scoped = (decision.affected_requirement_ids?.length ?? 0) > 0 || (decision.affected_story_ids?.length ?? 0) > 0;
	if (decision.status === 'accepted') {
		return {
			label: 'Accepted',
			detail: decision.proposed_by === 'recovery-agent'
				? 'Recovery decision has been accepted; auto-accept may have applied if policy allowed it.'
				: 'Decision has been accepted.',
			state: 'success'
		};
	}
	if (decision.status === 'proposed' || decision.status === 'under_review') {
		if (!impactKind || impactKind === 'change') {
			return {
				label: 'Review required',
				detail: 'Auto-accept is not inferred for missing or contract-changing impact.',
				state: 'warning'
			};
		}
		if (!scoped) {
			return {
				label: 'Review required',
				detail: 'Auto-accept requires scoped affected nodes.',
				state: 'warning'
			};
		}
		return {
			label: 'Policy eligible',
			detail: 'Preserve/refine impact with scoped nodes; final auto-accept depends on recovery policy budget.',
			state: 'neutral'
		};
	}
	return {
		label: 'Not active',
		detail: 'Decision is no longer awaiting recovery policy action.',
		state: 'neutral'
	};
}

export function lessonActivityModel(
	plan: PlanWithStatus,
	trajectoryItems: TrajectoryListItem[] = [],
	providerRates: ProviderRate[] = []
): LessonActivityModel {
	const lessonSummary = plan.phase_summary?.lessons ?? null;
	const lessonLoops = trajectoryItems
		.filter(isLessonTrajectoryItem)
		.sort((a, b) => new Date(a.start_time).getTime() - new Date(b.start_time).getTime());
	const lessonUsage = mergeMeasuredUsage(
		lessonLoops.map((loop) =>
			measureSummaryUsage({
				model: loop.model,
				tokens_in: loop.total_tokens_in,
				tokens_out: loop.total_tokens_out
			})
		)
	);
	const roleSummaries = lessonRoleSummaries(lessonLoops);
	return {
		lessonLoops,
		lessonUsage,
		costAccounting: calculateCostAccounting(lessonUsage, providerRates),
		roleSummaries,
		currentEffect: effectLabel(lessonSummary?.current_run_effect, 'none'),
		futureEffect: effectLabel(lessonSummary?.future_run_effect, 'eligible_for_future_prompts')
	};
}

export function isLessonTrajectoryItem(item: TrajectoryListItem): boolean {
	const taskID = item.task_id ?? '';
	return (
		item.workflow_slug === 'semspec-lesson-decomposition' ||
		LESSON_STEPS.has(item.workflow_step ?? '') ||
		LESSON_ROLES.has(item.role ?? '') ||
		/^(lesson|decompose)-/.test(taskID)
	);
}

export function planFreshnessIndicatorState(
	plan: PlanWithStatus,
	feed: FeedFreshnessSnapshot
): FreshnessIndicatorState {
	const freshness = plan.phase_summary?.freshness ?? null;
	const planScoped = feed.currentSlug === plan.slug;
	const disconnected = planScoped && feed.streamEverConnected && !feed.connected;
	const stale = Boolean(freshness?.stale);
	const shouldShow = stale || disconnected;
	return {
		shouldShow,
		disconnected,
		stale,
		statusLabel: stale && disconnected
			? 'Stale data and stream disconnected'
			: stale
				? 'Stale data'
				: 'Stream disconnected',
		lastUpdateAt: feed.lastSuccessfulUpdateAt ?? freshness?.generated_at ?? null,
		reason: freshness?.reason,
		source: freshness?.source
	};
}

function lessonRoleSummaries(lessonLoops: TrajectoryListItem[]): RoleSummary[] {
	const summaries = new Map<string, RoleSummary>();
	for (const loop of lessonLoops) {
		const role = formatRole(loop.role);
		const current = summaries.get(role) ?? { role, loops: 0, tokens: 0, duration: 0 };
		current.loops += 1;
		current.tokens += loop.total_tokens_in + loop.total_tokens_out;
		current.duration += loop.duration;
		summaries.set(role, current);
	}
	return [...summaries.values()].sort((a, b) => a.role.localeCompare(b.role));
}

export function effectLabel(value: string | undefined, fallback: string): string {
	return formatToken(value ?? fallback);
}

export function formatRole(role: string | undefined): string {
	if (!role) return 'Lesson activity';
	return formatToken(role);
}

export function formatToken(value: string): string {
	return value.replaceAll('_', ' ').replaceAll('-', ' ');
}
