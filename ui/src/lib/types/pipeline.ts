/**
 * Types for the agent pipeline visualization.
 * Represents the workflow stages from exploration to review.
 */

// =============================================================================
// Stage Types
// =============================================================================

/** Pipeline stage identifiers */
export type PipelineStageId =
	| 'explorer'
	| 'planner'
	| 'task_generator'
	| 'developer'
	| 'spec_reviewer'
	| 'sop_reviewer'
	| 'style_reviewer'
	| 'security_reviewer';

/** Stage execution state */
export type StageState = 'pending' | 'active' | 'complete' | 'failed' | 'skipped';

/** Stage category for grouping */
export type StageCategory = 'exploration' | 'planning' | 'execution' | 'review';

// =============================================================================
// Stage Definition
// =============================================================================

/**
 * Definition of a pipeline stage
 */
export interface PipelineStageDefinition {
	/** Unique stage identifier */
	id: PipelineStageId;
	/** Display label */
	label: string;
	/** Short label for compact view */
	shortLabel: string;
	/** Stage category */
	category: StageCategory;
	/** Icon name (lucide) */
	icon: string;
	/** Whether this stage runs in parallel with others */
	parallel?: boolean;
	/** Parent stage ID for parallel stages */
	parentId?: PipelineStageId;
}

/**
 * Runtime state of a pipeline stage
 */
export interface PipelineStageState {
	/** Stage definition */
	stage: PipelineStageDefinition;
	/** Current execution state */
	state: StageState;
	/** Associated loop ID if active */
	loopId?: string;
	/** Current iteration */
	iterations?: number;
	/** Max iterations */
	maxIterations?: number;
	/** Timestamp when stage started */
	startedAt?: string;
	/** Timestamp when stage completed */
	completedAt?: string;
	/** Error message if failed */
	error?: string;
}

// =============================================================================
// Pipeline Definition
// =============================================================================

/**
 * Full pipeline state for a workflow
 */
export interface PipelineState {
	/** Workflow/plan slug */
	slug: string;
	/** All stage states */
	stages: PipelineStageState[];
	/** Currently active stage IDs */
	activeStages: PipelineStageId[];
	/** Overall pipeline state */
	overallState: 'pending' | 'running' | 'complete' | 'failed';
}

// =============================================================================
// Stage Definitions
// =============================================================================

/** Standard pipeline stage definitions */
export const PIPELINE_STAGES: PipelineStageDefinition[] = [
	{
		id: 'explorer',
		label: 'Explorer',
		shortLabel: 'Explore',
		category: 'exploration',
		icon: 'search'
	},
	{
		id: 'planner',
		label: 'Planner',
		shortLabel: 'Plan',
		category: 'planning',
		icon: 'edit-3'
	},
	{
		id: 'task_generator',
		label: 'Task Generator',
		shortLabel: 'Tasks',
		category: 'planning',
		icon: 'list-checks'
	},
	{
		id: 'developer',
		label: 'Developer',
		shortLabel: 'Dev',
		category: 'execution',
		icon: 'code'
	},
	{
		id: 'spec_reviewer',
		label: 'Spec Compliance',
		shortLabel: 'Spec',
		category: 'review',
		icon: 'check-circle'
	},
	{
		id: 'sop_reviewer',
		label: 'SOP Review',
		shortLabel: 'SOP',
		category: 'review',
		icon: 'book-open',
		parallel: true,
		parentId: 'spec_reviewer'
	},
	{
		id: 'style_reviewer',
		label: 'Style Review',
		shortLabel: 'Style',
		category: 'review',
		icon: 'edit-3',
		parallel: true,
		parentId: 'spec_reviewer'
	},
	{
		id: 'security_reviewer',
		label: 'Security Review',
		shortLabel: 'Security',
		category: 'review',
		icon: 'alert-triangle',
		parallel: true,
		parentId: 'spec_reviewer'
	}
];

// =============================================================================
// Helper Functions
// =============================================================================

/**
 * Get stage definition by ID
 */
export function getStageDefinition(id: PipelineStageId): PipelineStageDefinition | undefined {
	return PIPELINE_STAGES.find((s) => s.id === id);
}

/**
 * Get stages by category
 */
export function getStagesByCategory(category: StageCategory): PipelineStageDefinition[] {
	return PIPELINE_STAGES.filter((s) => s.category === category);
}

/**
 * Get main (non-parallel) stages
 */
export function getMainStages(): PipelineStageDefinition[] {
	return PIPELINE_STAGES.filter((s) => !s.parallel);
}

/**
 * Get parallel stages for a parent stage
 */
export function getParallelStages(parentId: PipelineStageId): PipelineStageDefinition[] {
	return PIPELINE_STAGES.filter((s) => s.parentId === parentId);
}

/**
 * Get icon for stage state
 */
export function getStateIcon(state: StageState): string {
	switch (state) {
		case 'pending':
			return 'circle';
		case 'active':
			return 'loader';
		case 'complete':
			return 'check';
		case 'failed':
			return 'x';
		case 'skipped':
			return 'slash';
	}
}

/**
 * Get CSS class for stage state
 */
export function getStateClass(state: StageState): string {
	switch (state) {
		case 'pending':
			return 'neutral';
		case 'active':
			return 'info';
		case 'complete':
			return 'success';
		case 'failed':
			return 'error';
		case 'skipped':
			return 'neutral';
	}
}

/**
 * Map agent role to pipeline stage ID
 */
export function roleToStageId(role: string): PipelineStageId | undefined {
	const roleMap: Record<string, PipelineStageId> = {
		explorer: 'explorer',
		'explorer-writer': 'explorer',
		planner: 'planner',
		'planner-writer': 'planner',
		'task-generator': 'task_generator',
		'task-generator-writer': 'task_generator',
		developer: 'developer',
		'developer-writer': 'developer',
		spec_reviewer: 'spec_reviewer',
		sop_reviewer: 'sop_reviewer',
		style_reviewer: 'style_reviewer',
		security_reviewer: 'security_reviewer'
	};
	return roleMap[role];
}

/**
 * Create initial pipeline state for a workflow
 */
export function createInitialPipelineState(slug: string): PipelineState {
	return {
		slug,
		stages: PIPELINE_STAGES.map((stage) => ({
			stage,
			state: 'pending'
		})),
		activeStages: [],
		overallState: 'pending'
	};
}

/**
 * Derive pipeline state from active loops
 */
export function derivePipelineFromLoops(
	slug: string,
	loops: Array<{ role?: string; state: string; iterations?: number; max_iterations?: number; loop_id: string }>
): PipelineState {
	const state = createInitialPipelineState(slug);
	const activeStages: PipelineStageId[] = [];

	for (const loop of loops) {
		if (!loop.role) continue;

		const stageId = roleToStageId(loop.role);
		if (!stageId) continue;

		const stageState = state.stages.find((s) => s.stage.id === stageId);
		if (!stageState) continue;

		// Map loop state to stage state
		if (['executing', 'pending', 'exploring'].includes(loop.state)) {
			stageState.state = 'active';
			activeStages.push(stageId);
		} else if (['complete', 'success'].includes(loop.state)) {
			stageState.state = 'complete';
		} else if (['failed', 'cancelled'].includes(loop.state)) {
			stageState.state = 'failed';
		}

		stageState.loopId = loop.loop_id;
		stageState.iterations = loop.iterations;
		stageState.maxIterations = loop.max_iterations;
	}

	state.activeStages = activeStages;
	state.overallState =
		activeStages.length > 0
			? 'running'
			: state.stages.some((s) => s.state === 'failed')
				? 'failed'
				: state.stages.every((s) => s.state === 'complete' || s.state === 'skipped')
					? 'complete'
					: 'pending';

	return state;
}
