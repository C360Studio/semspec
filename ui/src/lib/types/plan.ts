/**
 * Types for the ADR-003 Plan + Tasks workflow model.
 * Plans start as drafts (approved=false) and can be approved
 * for execution via /promote command.
 */

/**
 * Scope defines file/directory boundaries for a plan.
 */
export interface PlanScope {
	/** Files/directories in scope for this plan */
	include: string[];
	/** Files/directories explicitly out of scope */
	exclude: string[];
	/** Protected files/directories that must not be modified */
	do_not_touch: string[];
}

/**
 * Plan represents a structured development plan.
 * Maps to workflow.Plan in Go backend.
 */
export interface Plan {
	/** Unique identifier for the plan entity */
	id: string;
	/** URL-friendly identifier (used for file paths) */
	slug: string;
	/** Human-readable title */
	title: string;
	/** false = draft plan, true = approved for execution */
	approved: boolean;
	/** When the plan was created */
	created_at: string;
	/** When the plan was approved */
	approved_at?: string;
	/** What we're building or fixing */
	goal?: string;
	/** Current state and why this matters */
	context?: string;
	/** File/directory boundaries for this plan */
	scope: PlanScope;
	/** Project ID this plan belongs to (defaults to "default") */
	projectId: string;
}

/**
 * PlanStage represents the current phase of a plan's lifecycle.
 */
export type PlanStage =
	| 'draft' // Unapproved, gathering information
	| 'planning' // Approved, finalizing approach
	| 'tasks' // Tasks generated, ready for execution
	| 'executing' // Tasks being executed
	| 'complete' // All tasks completed successfully
	| 'failed'; // Execution failed

/**
 * PlanPhaseState represents the state of a single phase in the pipeline.
 */
export type PlanPhaseState = 'none' | 'active' | 'complete' | 'failed';

/**
 * PlanPipeline represents the 3-phase pipeline state.
 */
export interface PlanPipeline {
	plan: PlanPhaseState;
	tasks: PlanPhaseState;
	execute: PlanPhaseState;
}

/**
 * GitHub integration metadata for a plan
 */
export interface GitHubInfo {
	epic_number: number;
	epic_url: string;
	repository: string;
	task_issues: Record<string, number>;
}

/**
 * Task completion statistics
 */
export interface TaskStats {
	total: number;
	completed: number;
	failed: number;
	in_progress: number;
}

/**
 * Loop associated with a plan, showing active agent work
 */
export interface ActiveLoop {
	loop_id: string;
	role: string;
	model: string;
	state: string;
	iterations: number;
	max_iterations: number;
	current_task_id?: string;
}

/**
 * Plan with additional status information for UI display.
 */
export interface PlanWithStatus extends Plan {
	/** Computed stage based on plan state */
	stage: PlanStage;
	/** GitHub integration metadata */
	github?: GitHubInfo;
	/** Active agent loops working on this plan */
	active_loops: ActiveLoop[];
	/** Task completion statistics */
	task_stats?: TaskStats;
}

/**
 * Derive the pipeline state from a plan with status.
 */
export function derivePlanPipeline(plan: PlanWithStatus): PlanPipeline {
	const isGeneratingTasks = plan.active_loops.some(
		(l) => l.state === 'executing' && l.role === 'task-generator'
	);
	const isExecuting = plan.active_loops.some(
		(l) => l.state === 'executing' && l.current_task_id
	);

	// Determine plan phase state
	let planState: PlanPhaseState = 'none';
	if (plan.approved) {
		planState = 'complete';
	} else if (plan.goal || plan.context) {
		planState = 'active';
	}

	// Determine tasks phase state
	let tasksState: PlanPhaseState = 'none';
	if (plan.task_stats && plan.task_stats.total > 0) {
		tasksState = 'complete';
	} else if (isGeneratingTasks) {
		tasksState = 'active';
	}

	// Determine execute phase state
	let executeState: PlanPhaseState = 'none';
	if (plan.stage === 'complete') {
		executeState = 'complete';
	} else if (plan.stage === 'failed') {
		executeState = 'failed';
	} else if (isExecuting || plan.stage === 'executing') {
		executeState = 'active';
	}

	return {
		plan: planState,
		tasks: tasksState,
		execute: executeState
	};
}
