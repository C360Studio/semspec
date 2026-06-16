import type { TrajectoryListItem } from './trajectory';

export type ActivePlanProgress = {
	title: string;
	detail: string;
	startedAt: string | null;
	workflowStep: string;
	loopId: string;
};

const PLANNING_COPY = new Map<string, { title: string; detail: string }>([
	[
		'exploring',
		{
			title: 'Exploring context...',
			detail: 'Analyst is gathering repository context for the plan...'
		}
	],
	[
		'drafting',
		{
			title: 'Drafting plan...',
			detail: 'Planner is composing the plan goal, context, and scope...'
		}
	],
	[
		'reviewing',
		{
			title: 'Reviewing plan...',
			detail: 'Plan reviewer is evaluating the current plan artifacts...'
		}
	],
	[
		'requirement-generation',
		{
			title: 'Generating requirements...',
			detail: 'Decomposing the approved plan into testable requirements...'
		}
	],
	[
		'architecture-generation',
		{
			title: 'Generating architecture...',
			detail: 'Architecture generator is selecting technology and component boundaries...'
		}
	],
	[
		'story-preparation',
		{
			title: 'Preparing stories...',
			detail: 'Preparing implementation stories from requirements and architecture...'
		}
	],
	[
		'scenario-generation',
		{
			title: 'Generating scenarios...',
			detail: 'Generating scenarios for each requirement...'
		}
	]
]);

const EXECUTION_COPY = new Map<string, { title: string; detail: string }>([
	[
		'develop',
		{
			title: 'Implementing...',
			detail: 'Developer is applying code changes for the active task...'
		}
	],
	[
		'developing',
		{
			title: 'Implementing...',
			detail: 'Developer is applying code changes for the active task...'
		}
	],
	[
		'review',
		{
			title: 'Reviewing implementation...',
			detail: 'Reviewer is checking the latest task changes...'
		}
	],
	[
		'reviewing-code',
		{
			title: 'Reviewing implementation...',
			detail: 'Reviewer is checking the latest task changes...'
		}
	],
	[
		'validating',
		{
			title: 'Validating implementation...',
			detail: 'Validator is running checks against the active task...'
		}
	],
	[
		'executing',
		{
			title: 'Executing task...',
			detail: 'Execution agent is working through the active task...'
		}
	]
]);

export function activePlanProgress(
	trajectoryItems: TrajectoryListItem[]
): ActivePlanProgress | null {
	const loop = newestActiveLoop(trajectoryItems, 'semspec-planning', PLANNING_COPY);
	if (!loop || !loop.workflow_step) return null;

	const copy = PLANNING_COPY.get(loop.workflow_step);
	if (!copy) return null;

	return progressFromLoop(loop, copy);
}

export function activePhaseProgress(
	trajectoryItems: TrajectoryListItem[]
): ActivePlanProgress | null {
	return (
		activePlanProgress(trajectoryItems) ??
		activeExecutionProgress(trajectoryItems)
	);
}

function activeExecutionProgress(
	trajectoryItems: TrajectoryListItem[]
): ActivePlanProgress | null {
	const loop = newestActiveLoop(trajectoryItems, 'semspec-task-execution', EXECUTION_COPY);
	if (!loop || !loop.workflow_step) return null;

	const copy = EXECUTION_COPY.get(loop.workflow_step);
	if (!copy) return null;

	return progressFromLoop(loop, copy);
}

function newestActiveLoop(
	trajectoryItems: TrajectoryListItem[],
	workflowSlug: string,
	copy: Map<string, { title: string; detail: string }>
): TrajectoryListItem | null {
	return trajectoryItems
		.filter((item) => {
			if (item.workflow_slug !== workflowSlug) return false;
			if (item.outcome) return false;
			return copy.has(item.workflow_step ?? '');
		})
		.sort((a, b) => startedAtMs(b) - startedAtMs(a))[0] ?? null;
}

function progressFromLoop(
	loop: TrajectoryListItem,
	copy: { title: string; detail: string }
): ActivePlanProgress {
	return {
		...copy,
		startedAt: loop.start_time ?? null,
		workflowStep: loop.workflow_step ?? 'unknown',
		loopId: loop.loop_id
	};
}

function startedAtMs(item: TrajectoryListItem): number {
	return item.start_time ? new Date(item.start_time).getTime() : 0;
}
