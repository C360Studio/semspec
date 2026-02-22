/**
 * Types for the ADR-003 Plan + Tasks workflow model.
 *
 * Core types are derived from the generated OpenAPI spec to prevent drift
 * between Go backend and TypeScript frontend. Frontend-only extensions
 * (GitHubInfo, TaskStats, pipeline derivation) are defined here.
 *
 * Plans start as drafts (approved=false) and can be approved
 * for execution via /promote command.
 */
import type { components } from './api.generated';

// ============================================================================
// Generated types (source of truth from Go backend OpenAPI spec)
// ============================================================================

/** Plan with status — the API response shape, generated from Go structs */
type GeneratedPlanWithStatus = components['schemas']['PlanWithStatus'];

/** Active loop status from the API */
type GeneratedActiveLoopStatus = components['schemas']['ActiveLoopStatus'];

// ============================================================================
// Frontend-only types (not in the Go API)
// ============================================================================

/**
 * PlanStage represents the current phase of a plan's lifecycle.
 * Maps to the `stage` string field from the Go API.
 */
export type PlanStage =
	| 'draft' // Unapproved, gathering information
	| 'drafting' // Go returns "drafting" as well
	| 'ready_for_approval' // Plan has goal/context, ready for approval
	| 'planning' // Approved, finalizing approach
	| 'approved' // Plan explicitly approved
	| 'tasks_generated' // Tasks generated, awaiting approval
	| 'tasks_approved' // All tasks approved, ready for execution
	| 'tasks' // Legacy: Tasks generated
	| 'implementing' // Tasks being implemented
	| 'executing' // Tasks being executed
	| 'complete' // All tasks completed successfully
	| 'archived' // Plan archived (soft deleted)
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
 * GitHub integration metadata for a plan (frontend-only, not in Go API yet)
 */
export interface GitHubInfo {
	epic_number: number;
	epic_url: string;
	repository: string;
	task_issues: Record<string, number>;
}

/**
 * Task completion statistics (frontend-only, not in Go API yet)
 */
export interface TaskStats {
	total: number;
	pending_approval: number;
	approved: number;
	rejected: number;
	in_progress: number;
	completed: number;
	failed: number;
}

/**
 * ActiveLoop extends the generated ActiveLoopStatus with fields the frontend
 * uses that aren't yet in the Go API response.
 *
 * The 3 core fields (loop_id, role, state) come from the Go API.
 * The extra fields (model, iterations, etc.) are populated from agent loop KV data.
 */
export interface ActiveLoop extends GeneratedActiveLoopStatus {
	model?: string;
	iterations?: number;
	max_iterations?: number;
	current_task_id?: string;
}

/**
 * PlanScope is re-exported from the generated type for convenience.
 * Uses the Go API field names (snake_case).
 */
export type PlanScope = NonNullable<GeneratedPlanWithStatus['scope']>;

/**
 * Plan represents a structured development plan.
 * Derived from the generated PlanWithStatus by picking only the base plan fields.
 */
export type Plan = Omit<GeneratedPlanWithStatus, 'stage' | 'active_loops'>;

/**
 * Plan with additional status information for UI display.
 *
 * The core shape comes from the generated OpenAPI spec (Go backend is source of truth).
 * Frontend-only extensions (github, task_stats) are added here.
 */
export interface PlanWithStatus extends Omit<GeneratedPlanWithStatus, 'active_loops' | 'stage'> {
	/** Computed stage based on plan state */
	stage: PlanStage;
	/** GitHub integration metadata (frontend-only) */
	github?: GitHubInfo;
	/** Active agent loops working on this plan */
	active_loops: ActiveLoop[];
	/** Task completion statistics (frontend-only) */
	task_stats?: TaskStats;
}

/**
 * Derive the pipeline state from a plan with status.
 */
export function derivePlanPipeline(plan: PlanWithStatus): PlanPipeline {
	const isGeneratingTasks = (plan.active_loops ?? []).some(
		(l) => l.state === 'executing' && l.role === 'task-generator'
	);
	const isExecuting = (plan.active_loops ?? []).some(
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
