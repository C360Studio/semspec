import type { components } from './api.generated';
import type { TaskSSEPayload } from './feed';
import type { PlanWithStatus } from './plan';
import type { TrajectoryListItem } from './trajectory';

export type Lesson = components['schemas']['Lesson'];

export interface ExecutionScenario {
	id?: string;
	title?: string;
	requirement_id?: string;
	story_id?: string;
	tags?: string[];
}

export interface ExecutionTask {
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
}

export type RunStatusTone = 'neutral' | 'active' | 'success' | 'warning' | 'danger';
export type TaskGroupStatus = 'active' | 'approved' | 'recovered' | 'blocked' | 'pending';
export type RunWarningKind = 'human-review' | 'qa-skip-guard' | 'orphaned-attempt';

export interface RunStatusSummary {
	title: string;
	detail: string;
	tone: RunStatusTone;
	stageLabel: string;
	timestamp?: string | null;
}

export interface TaskAttemptSummary {
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
}

export interface TaskGroupSummary {
	key: string;
	title: string;
	requirementId?: string;
	status: TaskGroupStatus;
	attempts: TaskAttemptSummary[];
	hasOrphanedTerminalAttempt: boolean;
	latestUpdatedAt?: string;
}

export interface QASkippedTest {
	suite?: string;
	name: string;
	file?: string;
}

export interface QAVisibility {
	verdict: string;
	level: string;
	passed: boolean;
	summary?: string;
	durationMs?: number;
	completedAt?: string;
	skippedTests: QASkippedTest[];
	skipGuardTriggered: boolean;
	artifactCount: number;
}

export interface LessonVisibility {
	id: string;
	summary: string;
	source: string;
	positive: boolean;
	createdAt: string;
	lastInjectedAt?: string | null;
	futureRunOnly: boolean;
	relatedTaskTitle?: string;
}

export interface RunWarning {
	kind: RunWarningKind;
	title: string;
	detail: string;
	tone: 'warning' | 'danger';
	relatedId?: string;
}

export interface UsageSummary {
	totalTokens: number;
	inputTokens: number;
	outputTokens: number;
	durationMs: number;
	loopCount: number;
	pricingAvailable: false;
	costLabel: string;
}

export interface RunVisibilitySummary {
	shouldRender: boolean;
	status: RunStatusSummary;
	taskStats: {
		totalGroups: number;
		approvedGroups: number;
		activeGroups: number;
		blockedGroups: number;
		orphanedGroups: number;
		totalAttempts: number;
	};
	taskGroups: TaskGroupSummary[];
	qa: QAVisibility | null;
	warnings: RunWarning[];
	lessons: LessonVisibility[];
	usage: UsageSummary;
}

type FlexibleQaRun = NonNullable<PlanWithStatus['qa_run']> & {
	skipped_tests?: QASkippedTest[];
};

type PlanDecision = NonNullable<PlanWithStatus['plan_decisions']>[number];

const EXECUTION_STAGES = new Set([
	'ready_for_execution',
	'implementing',
	'executing',
	'ready_for_qa',
	'reviewing_qa',
	'reviewing_rollup',
	'complete',
	'complete_with_deferrals',
	'failed'
]);

const ACTIVE_TASK_STAGES = new Set(['developing', 'validating', 'reviewing', 'testing', 'building']);
const TERMINAL_TASK_STAGES = new Set(['approved', 'escalated', 'error', 'rejected']);

export function mergeTaskSse(
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

	return Array.from(byTask.values()).sort(byTaskUpdatedAt);
}

export function summarizeRunVisibility(
	plan: PlanWithStatus,
	executionTasks: ExecutionTask[],
	trajectoryItems: TrajectoryListItem[],
	lessons: Lesson[]
): RunVisibilitySummary {
	const taskGroups = groupTasks(executionTasks);
	const qa = summarizeQA(plan);
	const matchingLessons = summarizeLessons(plan, executionTasks, trajectoryItems, lessons);
	const warnings = summarizeWarnings(plan, taskGroups, qa);
	const usage = summarizeUsage(trajectoryItems);
	const status = summarizeStatus(plan, executionTasks, taskGroups, qa, warnings);

	return {
		shouldRender:
			EXECUTION_STAGES.has(plan.stage) ||
			executionTasks.length > 0 ||
			Boolean(qa) ||
			warnings.length > 0 ||
			matchingLessons.length > 0,
		status,
		taskStats: {
			totalGroups: taskGroups.length,
			approvedGroups: taskGroups.filter((g) => g.status === 'approved' || g.status === 'recovered').length,
			activeGroups: taskGroups.filter((g) => g.status === 'active').length,
			blockedGroups: taskGroups.filter((g) => g.status === 'blocked').length,
			orphanedGroups: taskGroups.filter((g) => g.hasOrphanedTerminalAttempt).length,
			totalAttempts: executionTasks.length
		},
		taskGroups,
		qa,
		warnings,
		lessons: matchingLessons,
		usage
	};
}

function summarizeStatus(
	plan: PlanWithStatus,
	executionTasks: ExecutionTask[],
	taskGroups: TaskGroupSummary[],
	qa: QAVisibility | null,
	warnings: RunWarning[]
): RunStatusSummary {
	const humanDecision = openHumanDecision(plan);
	if (humanDecision) {
		return {
			title: 'Human review needed',
			detail: firstLine(humanDecision.rationale) || humanDecision.title,
			tone: 'warning',
			stageLabel: stageLabel(plan.stage),
			timestamp: humanDecision.created_at
		};
	}

	if (qa?.verdict === 'needs_changes' || qa?.verdict === 'rejected') {
		return {
			title: qa.skipGuardTriggered ? 'QA needs skipped-test classification' : 'QA needs changes',
			detail: firstLine(qa.summary) || 'QA reviewer found work that needs follow-up.',
			tone: qa.verdict === 'rejected' ? 'danger' : 'warning',
			stageLabel: stageLabel(plan.stage),
			timestamp: qa.completedAt
		};
	}

	if (qa?.verdict === 'conditionally_approved') {
		return {
			title: 'Execution complete with deferrals',
			detail: firstLine(qa.summary) || 'QA passed with operator-tier work deferred.',
			tone: 'warning',
			stageLabel: stageLabel(plan.stage),
			timestamp: qa.completedAt
		};
	}

	if (qa?.verdict === 'approved') {
		return {
			title: 'Execution and QA approved',
			detail: firstLine(qa.summary) || 'All execution and QA gates are approved.',
			tone: 'success',
			stageLabel: stageLabel(plan.stage),
			timestamp: qa.completedAt
		};
	}

	const activeTask = latestTask(executionTasks.filter((task) => isActiveTaskStage(task.stage)));
	if (activeTask) {
		return {
			title: `Executing: ${taskTitle(activeTask)}`,
			detail: `Task stage ${activeTask.stage}${cycleLabel(activeTask) ? `, ${cycleLabel(activeTask)}` : ''}.`,
			tone: 'active',
			stageLabel: stageLabel(plan.stage),
			timestamp: activeTask.updated_at
		};
	}

	if (warnings.length > 0 && plan.stage === 'rejected') {
		return {
			title: 'Run rejected',
			detail: warnings[0].detail,
			tone: warnings[0].tone === 'danger' ? 'danger' : 'warning',
			stageLabel: stageLabel(plan.stage),
			timestamp: plan.last_error_at ?? plan.created_at
		};
	}

	const approved = taskGroups.filter((group) => group.status === 'approved' || group.status === 'recovered').length;
	if (taskGroups.length > 0 && approved === taskGroups.length) {
		return {
			title: 'Execution tasks approved',
			detail: `${approved} task group${approved === 1 ? '' : 's'} approved; waiting on final plan gate.`,
			tone: plan.stage === 'complete' ? 'success' : 'neutral',
			stageLabel: stageLabel(plan.stage),
			timestamp: newestTimestamp(taskGroups.map((group) => group.latestUpdatedAt))
		};
	}

	return {
		title: stageLabel(plan.stage),
		detail: plan.last_error || 'No execution task activity has been recorded yet.',
		tone: plan.stage === 'failed' || plan.stage === 'rejected' ? 'danger' : 'neutral',
		stageLabel: stageLabel(plan.stage),
		timestamp: plan.last_error_at ?? plan.created_at
	};
}

function groupTasks(tasks: ExecutionTask[]): TaskGroupSummary[] {
	const groups = new Map<string, ExecutionTask[]>();

	for (const task of tasks) {
		const key = taskGroupKey(task);
		groups.set(key, [...(groups.get(key) ?? []), task]);
	}

	return Array.from(groups.entries())
		.map(([key, groupTasks]) => {
			const attempts = groupTasks.sort(byTaskUpdatedAt).map(taskAttempt);
			const hasApproved = attempts.some((attempt) => attempt.stage === 'approved');
			const hasActive = attempts.some((attempt) => isActiveTaskStage(attempt.stage));
			const hasBlocked = attempts.some((attempt) => ['escalated', 'error', 'rejected'].includes(attempt.stage));
			const hasOrphanedTerminalAttempt = hasApproved && hasBlocked;
			const latest = latestTask(groupTasks);

			return {
				key,
				title: taskTitle(latest ?? groupTasks[0]),
				requirementId: latest?.requirement_id ?? groupTasks[0]?.requirement_id,
				status: taskGroupStatus(hasActive, hasApproved, hasBlocked, hasOrphanedTerminalAttempt),
				attempts,
				hasOrphanedTerminalAttempt,
				latestUpdatedAt: newestTimestamp(groupTasks.map((task) => task.updated_at ?? task.created_at))
			};
		})
		.sort((a, b) => {
			const req = (a.requirementId ?? '').localeCompare(b.requirementId ?? '');
			if (req !== 0) return req;
			return (a.latestUpdatedAt ?? '').localeCompare(b.latestUpdatedAt ?? '');
		});
}

function taskGroupStatus(
	hasActive: boolean,
	hasApproved: boolean,
	hasBlocked: boolean,
	hasOrphanedTerminalAttempt: boolean
): TaskGroupStatus {
	if (hasActive) return 'active';
	if (hasApproved && hasOrphanedTerminalAttempt) return 'recovered';
	if (hasApproved) return 'approved';
	if (hasBlocked) return 'blocked';
	return 'pending';
}

function taskAttempt(task: ExecutionTask): TaskAttemptSummary {
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

function summarizeQA(plan: PlanWithStatus): QAVisibility | null {
	const verdict = plan.qa_verdict_summary;
	const run = plan.qa_run as FlexibleQaRun | null | undefined;
	if (!verdict && !run) return null;

	const skippedTests = run?.skipped_tests ?? [];
	const summary = verdict?.summary;

	return {
		verdict: verdict?.verdict ?? (run?.passed ? 'approved' : 'failed'),
		level: verdict?.level ?? plan.qa_level ?? 'qa',
		passed: run?.passed ?? false,
		summary,
		durationMs: run?.duration_ms,
		completedAt: run?.completed_at,
		skippedTests,
		skipGuardTriggered:
			skippedTests.length > 0 &&
			((summary ?? '').includes('[skip-guard]') || verdict?.verdict === 'needs_changes'),
		artifactCount: run?.artifacts?.length ?? 0
	};
}

function summarizeLessons(
	plan: PlanWithStatus,
	tasks: ExecutionTask[],
	trajectoryItems: TrajectoryListItem[],
	lessons: Lesson[]
): LessonVisibility[] {
	const taskIDs = new Set(tasks.map((task) => task.task_id));
	const scenarioIDs = new Set(
		tasks.flatMap((task) => task.scenarios?.map((scenario) => scenario.id).filter(Boolean) ?? [])
	);
	const loopIDs = new Set(trajectoryItems.map((item) => item.loop_id));
	const taskTitles = new Map(tasks.map((task) => [task.task_id, taskTitle(task)]));

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
			source: lesson.Source,
			positive: lesson.Positive,
			createdAt: lesson.CreatedAt,
			lastInjectedAt: lesson.LastInjectedAt,
			futureRunOnly: !lesson.LastInjectedAt,
			relatedTaskTitle: taskTitles.get(lesson.ScenarioID)
		}));
}

function summarizeWarnings(
	plan: PlanWithStatus,
	taskGroups: TaskGroupSummary[],
	qa: QAVisibility | null
): RunWarning[] {
	const warnings: RunWarning[] = [];
	const decision = openHumanDecision(plan);

	if (decision) {
		warnings.push({
			kind: 'human-review',
			title: decision.title || 'Human review needed',
			detail: firstLine(decision.rationale) || 'Recovery produced an open human-review decision.',
			tone: 'warning',
			relatedId: decision.id
		});
	}

	if (qa?.skipGuardTriggered) {
		warnings.push({
			kind: 'qa-skip-guard',
			title: 'Skipped tests need classification',
			detail: `${qa.skippedTests.length} skipped test${qa.skippedTests.length === 1 ? '' : 's'} kept QA from approving all-green.`,
			tone: 'warning'
		});
	}

	for (const group of taskGroups.filter((g) => g.hasOrphanedTerminalAttempt)) {
		warnings.push({
			kind: 'orphaned-attempt',
			title: 'Recovered task has old terminal attempt',
			detail: `${group.title} has an approved replacement but an older rejected/escalated row is still present.`,
			tone: 'warning',
			relatedId: group.key
		});
	}

	return warnings;
}

function summarizeUsage(trajectoryItems: TrajectoryListItem[]): UsageSummary {
	const inputTokens = trajectoryItems.reduce((sum, item) => sum + (item.total_tokens_in ?? 0), 0);
	const outputTokens = trajectoryItems.reduce((sum, item) => sum + (item.total_tokens_out ?? 0), 0);
	const durationMs = trajectoryItems.reduce((sum, item) => sum + (item.duration ?? 0), 0);
	return {
		totalTokens: inputTokens + outputTokens,
		inputTokens,
		outputTokens,
		durationMs,
		loopCount: trajectoryItems.length,
		pricingAvailable: false,
		costLabel: 'Pricing not configured'
	};
}

function openHumanDecision(plan: PlanWithStatus): PlanDecision | undefined {
	return (plan.plan_decisions ?? []).find((decision) => {
		if (decision.status !== 'proposed' && decision.status !== 'under_review') return false;
		return decision.kind === 'execution_exhausted' || /escalate[_ -]?human/i.test(decision.title);
	});
}

function taskGroupKey(task: ExecutionTask): string {
	return `${task.requirement_id ?? 'plan'}::${normalizeTaskTitle(taskTitle(task))}`;
}

function taskTitle(task: ExecutionTask | undefined): string {
	if (!task) return 'Untitled task';
	return task.title || task.prompt || task.description || task.task_id;
}

function normalizeTaskTitle(title: string): string {
	return title.trim().toLowerCase().replace(/\s+/g, ' ');
}

function latestTask(tasks: ExecutionTask[]): ExecutionTask | undefined {
	return [...tasks].sort((a, b) => dateMs(b.updated_at ?? b.created_at) - dateMs(a.updated_at ?? a.created_at))[0];
}

function byTaskUpdatedAt(a: ExecutionTask, b: ExecutionTask): number {
	return dateMs(a.updated_at ?? a.created_at) - dateMs(b.updated_at ?? b.created_at);
}

function isActiveTaskStage(stage: string | undefined): boolean {
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

function firstLine(text: string | undefined): string {
	return (text ?? '').split('\n').map((line) => line.trim()).find(Boolean) ?? '';
}

function stageLabel(stage: string): string {
	return stage.replace(/_/g, ' ').replace(/\b\w/g, (char) => char.toUpperCase());
}

function cycleLabel(task: Pick<ExecutionTask, 'tdd_cycle' | 'max_tdd_cycles'>): string {
	if (typeof task.tdd_cycle !== 'number') return '';
	const current = task.tdd_cycle + 1;
	if (typeof task.max_tdd_cycles === 'number') return `cycle ${current}/${task.max_tdd_cycles}`;
	return `cycle ${current}`;
}

export function formatTokens(count: number): string {
	if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`;
	if (count >= 1000) return `${(count / 1000).toFixed(1)}k`;
	return String(count);
}

export function formatDuration(ms: number): string {
	if (!Number.isFinite(ms) || ms <= 0) return '0s';
	if (ms < 1000) return `${ms}ms`;
	if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
	const minutes = Math.floor(ms / 60_000);
	const seconds = Math.floor((ms % 60_000) / 1000);
	if (minutes < 60) return `${minutes}m ${seconds.toString().padStart(2, '0')}s`;
	const hours = Math.floor(minutes / 60);
	const remainingMinutes = minutes % 60;
	return `${hours}h ${remainingMinutes.toString().padStart(2, '0')}m`;
}
