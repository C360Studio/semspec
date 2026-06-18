import {
	calculateCostAccounting,
	measureSummaryUsage,
	mergeMeasuredUsage,
	type CostAccounting,
	type MeasuredTokenUsage,
	type ProviderRate
} from '$lib/types/costAccounting';
import type { components } from '$lib/types/api.generated';
import type { PlanPhaseSummary } from '$lib/types/feed';
import type { TaskSSEPayload } from '$lib/types/feed';
import type { PlanWithStatus } from '$lib/types/plan';
import type { TrajectoryListItem } from '$lib/types/trajectory';

type Story = NonNullable<PlanWithStatus['stories']>[number];
type RecoveryDecision = NonNullable<PlanWithStatus['plan_decisions']>[number];
export type Lesson = components['schemas']['Lesson'];

export type ExecutionScenario = {
	id?: string;
	title?: string;
	requirement_id?: string;
	story_id?: string;
	tags?: string[];
};

export type ExecutionTask = {
	entity_id?: string;
	slug: string;
	task_id: string;
	requirement_id?: string;
	stage: string;
	tdd_cycle?: number;
	max_tdd_cycles?: number;
	title?: string;
	description?: string;
	project_id?: string;
	prompt?: string;
	model?: string;
	trace_id?: string;
	loop_id?: string;
	request_id?: string;
	task_type?: string;
	worktree_path?: string;
	worktree_branch?: string;
	scenario_branch?: string;
	file_scope?: string[];
	scenarios?: ExecutionScenario[];
	files_modified?: string[];
	tests_passed?: boolean;
	validation_passed?: boolean;
	merge_commit?: string;
	developer_task_id?: string;
	validator_task_id?: string;
	reviewer_task_id?: string;
	verdict?: string;
	rejection_type?: string;
	feedback?: string;
	review_retry_count?: number;
	error_reason?: string;
	error_class?: string;
	escalation_reason?: string;
	created_at?: string;
	updated_at?: string;
};

export type ExecutionTaskGroupStatus = 'active' | 'approved' | 'recovered' | 'blocked' | 'pending';

export type ExecutionTaskAttempt = {
	taskId: string;
	stage: string;
	verdict?: string;
	tddCycle?: number;
	maxTddCycles?: number;
	updatedAt?: string;
	mergeCommit?: string;
	feedback?: string;
	errorReason?: string;
	escalationReason?: string;
};

export type ExecutionTaskGroup = {
	key: string;
	title: string;
	requirementId?: string;
	status: ExecutionTaskGroupStatus;
	attempts: ExecutionTaskAttempt[];
	hasOrphanedTerminalAttempt: boolean;
	latestUpdatedAt?: string;
};

export type ExecutionAttemptWarning = {
	kind: 'orphaned-attempt';
	title: string;
	detail: string;
	relatedId: string;
};

export type ExecutionAttemptModel = {
	taskGroups: ExecutionTaskGroup[];
	warnings: ExecutionAttemptWarning[];
	totalAttempts: number;
	approvedGroups: number;
	activeGroups: number;
	blockedGroups: number;
	orphanedGroups: number;
};

export type PersistedLessonSummary = {
	id: string;
	summary: string;
	detail?: string;
	source: string;
	role?: string;
	positive: boolean;
	createdAt: string;
	lastInjectedAt?: string | null;
	futureRunOnly: boolean;
	relatedTaskTitle?: string;
};

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
	questionsConnected?: boolean;
	questionsEverConnected?: boolean;
	questionsLastSuccessfulUpdateAt?: string | null;
	questionsError?: string | null;
	questionsLastErrorAt?: string | null;
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
const ACTIVE_TASK_STAGES = new Set(['developing', 'validating', 'reviewing', 'testing', 'building']);
const BLOCKED_TASK_STAGES = new Set(['escalated', 'error', 'rejected']);
const TERMINAL_TASK_STAGES = new Set(['approved', ...BLOCKED_TASK_STAGES]);

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

export function mergeExecutionTaskSSE(
	loadedTasks: ExecutionTask[],
	taskStages: Map<string, TaskSSEPayload>,
	planSlug: string
): ExecutionTask[] {
	if (!planSlug) return loadedTasks;

	const byTask = new Map(loadedTasks.map((task) => [task.task_id, task]));
	for (const payload of taskStages.values()) {
		if (payload.slug !== planSlug) continue;
		const previous = byTask.get(payload.task_id);
		byTask.set(payload.task_id, {
			...previous,
			slug: payload.slug,
			task_id: payload.task_id,
			entity_id: payload.entity_id ?? previous?.entity_id,
			stage: payload.stage ?? previous?.stage ?? 'developing',
			title: payload.title ?? previous?.title,
			loop_id: payload.loop_id ?? previous?.loop_id,
			tests_passed: payload.tests_passed ?? previous?.tests_passed,
			validation_passed: payload.validation_passed ?? previous?.validation_passed,
			verdict: payload.verdict ?? previous?.verdict,
			feedback: payload.feedback ?? previous?.feedback,
			tdd_cycle: payload.tdd_cycle ?? previous?.tdd_cycle,
			max_tdd_cycles: payload.max_tdd_cycles ?? previous?.max_tdd_cycles,
			review_retry_count: payload.review_retry_count ?? previous?.review_retry_count,
			created_at: previous?.created_at ?? payload.updated_at,
			updated_at: payload.updated_at ?? previous?.updated_at
		});
	}

	return Array.from(byTask.values()).sort(byExecutionTaskUpdatedAt);
}

export function executionAttemptModel(executionTasks: ExecutionTask[]): ExecutionAttemptModel {
	const taskGroups = groupExecutionTasks(executionTasks);
	const warnings = taskGroups
		.filter((group) => group.hasOrphanedTerminalAttempt)
		.map((group) => ({
			kind: 'orphaned-attempt' as const,
			title: 'Recovered task has old terminal attempt',
			detail: `${group.title} has an approved replacement but an older rejected/escalated row is still present.`,
			relatedId: group.key
		}));

	return {
		taskGroups,
		warnings,
		totalAttempts: executionTasks.length,
		approvedGroups: taskGroups.filter((group) => group.status === 'approved' || group.status === 'recovered').length,
		activeGroups: taskGroups.filter((group) => group.status === 'active').length,
		blockedGroups: taskGroups.filter((group) => group.status === 'blocked').length,
		orphanedGroups: taskGroups.filter((group) => group.hasOrphanedTerminalAttempt).length
	};
}

export function persistedLessonSummaries(
	plan: PlanWithStatus,
	tasks: ExecutionTask[],
	trajectoryItems: TrajectoryListItem[],
	lessons: Lesson[]
): PersistedLessonSummary[] {
	const taskIDs = new Set(tasks.map((task) => task.task_id));
	const scenarioIDs = new Set(
		tasks.flatMap((task) => task.scenarios?.map((scenario) => scenario.id).filter(Boolean) ?? [])
	);
	const loopIDs = new Set(trajectoryItems.map((item) => item.loop_id));
	const taskTitles = new Map(tasks.map((task) => [task.task_id, executionTaskTitle(task)]));

	return lessons
		.filter((lesson) => {
			if (taskIDs.has(lesson.ScenarioID)) return true;
			if (scenarioIDs.has(lesson.ScenarioID)) return true;
			if (lesson.ScenarioID?.includes(plan.slug)) return true;
			return lesson.EvidenceSteps?.some((step) => loopIDs.has(step.loop_id)) ?? false;
		})
		.sort((a, b) => dateMs(b.CreatedAt) - dateMs(a.CreatedAt))
		.map((lesson) => ({
			id: lesson.ID,
			summary: lesson.Summary,
			detail: lesson.Detail,
			source: lesson.Source,
			role: lesson.Role,
			positive: lesson.Positive,
			createdAt: lesson.CreatedAt,
			lastInjectedAt: lesson.LastInjectedAt,
			futureRunOnly: !lesson.LastInjectedAt,
			relatedTaskTitle: taskTitles.get(lesson.ScenarioID)
		}));
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
	const feedDisconnected = planScoped && feed.streamEverConnected && !feed.connected;
	const questionsDisconnected = Boolean(feed.questionsEverConnected && !feed.questionsConnected);
	const disconnected = feedDisconnected || questionsDisconnected;
	const stale = Boolean(freshness?.stale);
	const shouldShow = stale || disconnected;
	const disconnectedReason = questionsDisconnected
		? (feed.questionsError ?? 'Questions stream disconnected')
		: undefined;
	return {
		shouldShow,
		disconnected,
		stale,
		statusLabel: stale && disconnected
			? 'Stale data and stream disconnected'
			: stale
				? 'Stale data'
				: questionsDisconnected
					? 'Question stream disconnected'
					: 'Stream disconnected',
		lastUpdateAt: feed.lastSuccessfulUpdateAt ??
			feed.questionsLastSuccessfulUpdateAt ??
			freshness?.generated_at ??
			null,
		reason: disconnectedReason ?? freshness?.reason,
		source: questionsDisconnected ? 'question-manager' : freshness?.source
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

function groupExecutionTasks(tasks: ExecutionTask[]): ExecutionTaskGroup[] {
	const groups = new Map<string, ExecutionTask[]>();
	for (const task of tasks) {
		const key = executionTaskGroupKey(task);
		groups.set(key, [...(groups.get(key) ?? []), task]);
	}

	return Array.from(groups.entries())
		.map(([key, groupTasks]) => {
			const attempts = groupTasks.sort(byExecutionTaskUpdatedAt).map(taskAttempt);
			const hasApproved = attempts.some((attempt) => attempt.stage === 'approved');
			const hasActive = attempts.some((attempt) => isActiveExecutionTaskStage(attempt.stage));
			const hasBlocked = attempts.some((attempt) => BLOCKED_TASK_STAGES.has(attempt.stage));
			const hasOrphanedTerminalAttempt = hasApproved && hasBlocked;
			const latest = latestExecutionTask(groupTasks);
			return {
				key,
				title: executionTaskTitle(latest ?? groupTasks[0]),
				requirementId: latest?.requirement_id ?? groupTasks[0]?.requirement_id,
				status: executionTaskGroupStatus(hasActive, hasApproved, hasBlocked, hasOrphanedTerminalAttempt),
				attempts,
				hasOrphanedTerminalAttempt,
				latestUpdatedAt: newestTimestamp(groupTasks.map((task) => task.updated_at ?? task.created_at))
			};
		})
		.sort((a, b) => {
			const req = (a.requirementId ?? '').localeCompare(b.requirementId ?? '');
			if (req !== 0) return req;
			return (b.latestUpdatedAt ?? '').localeCompare(a.latestUpdatedAt ?? '');
		});
}

function executionTaskGroupStatus(
	hasActive: boolean,
	hasApproved: boolean,
	hasBlocked: boolean,
	hasOrphanedTerminalAttempt: boolean
): ExecutionTaskGroupStatus {
	if (hasActive) return 'active';
	if (hasApproved && hasOrphanedTerminalAttempt) return 'recovered';
	if (hasApproved) return 'approved';
	if (hasBlocked) return 'blocked';
	return 'pending';
}

function taskAttempt(task: ExecutionTask): ExecutionTaskAttempt {
	return {
		taskId: task.task_id,
		stage: task.stage,
		verdict: task.verdict,
		tddCycle: task.tdd_cycle,
		maxTddCycles: task.max_tdd_cycles,
		updatedAt: task.updated_at ?? task.created_at,
		mergeCommit: task.merge_commit,
		feedback: task.feedback,
		errorReason: task.error_reason,
		escalationReason: task.escalation_reason
	};
}

function executionTaskGroupKey(task: ExecutionTask): string {
	return `${task.requirement_id ?? 'plan'}::${normalizeTaskTitle(executionTaskTitle(task))}`;
}

function executionTaskTitle(task: ExecutionTask | undefined): string {
	if (!task) return 'Untitled task';
	return task.title || task.prompt || task.description || task.task_id;
}

function normalizeTaskTitle(title: string): string {
	return title.trim().toLowerCase().replace(/\s+/g, ' ');
}

function latestExecutionTask(tasks: ExecutionTask[]): ExecutionTask | undefined {
	return [...tasks].sort((a, b) => dateMs(b.updated_at ?? b.created_at) - dateMs(a.updated_at ?? a.created_at))[0];
}

function byExecutionTaskUpdatedAt(a: ExecutionTask, b: ExecutionTask): number {
	return dateMs(a.updated_at ?? a.created_at) - dateMs(b.updated_at ?? b.created_at);
}

function isActiveExecutionTaskStage(stage: string | undefined): boolean {
	if (!stage) return false;
	if (ACTIVE_TASK_STAGES.has(stage)) return true;
	return !TERMINAL_TASK_STAGES.has(stage);
}

function newestTimestamp(values: Array<string | null | undefined>): string | undefined {
	return values
		.filter((value): value is string => Boolean(value))
		.sort((a, b) => dateMs(b) - dateMs(a))[0];
}

function dateMs(value: string | null | undefined): number {
	if (!value) return 0;
	const ms = new Date(value).getTime();
	return Number.isFinite(ms) ? ms : 0;
}
