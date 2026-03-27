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
import type { Phase, PhaseStats } from './phase';

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
	| 'drafting' // Plan content being generated
	| 'ready_for_approval' // Plan has goal/context, ready for approval
	| 'reviewed' // Plan reviewed by reviewer (may be approved or need changes)
	| 'needs_changes' // Reviewer requested changes
	| 'planning' // Approved, finalizing approach
	| 'approved' // Plan explicitly approved
	| 'rejected' // Plan rejected
	| 'requirements_generated' // Requirements generated via auto-cascade
	| 'scenarios_generated' // Scenarios generated via auto-cascade
	| 'scenarios_reviewed' // Scenarios reviewed, waiting for human approval (round 2)
	| 'ready_for_execution' // Both approvals done, ready to execute
	| 'phases_generated' // Legacy: Phases generated
	| 'phases_approved' // Legacy: Phases approved
	| 'tasks_generated' // Legacy: Tasks generated
	| 'tasks_approved' // Legacy: All tasks approved
	| 'tasks' // Legacy: Tasks generated
	| 'implementing' // Tasks being implemented
	| 'executing' // Legacy: Tasks being executed
	| 'reviewing_rollup' // Plan rollup review in progress
	| 'complete' // All tasks completed successfully
	| 'archived' // Plan archived (soft deleted)
	| 'failed'; // Execution failed

/**
 * PlanPhaseState represents the state of a single phase in the pipeline.
 */
export type PlanPhaseState = 'none' | 'active' | 'complete' | 'failed';

/**
 * PlanPipeline represents the 3-phase pipeline state.
 * Phases: plan → requirements (auto-cascade) → execute
 */
export interface PlanPipeline {
	plan: PlanPhaseState;
	requirements: PlanPhaseState;
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
 * Frontend-only extensions (github, task_stats, phases) are added here.
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
	/** Phases within this plan, ordered by sequence */
	phases?: Phase[];
	/** Phase completion statistics (frontend-only) */
	phase_stats?: PhaseStats;
}

/**
 * Derive the pipeline state from a plan with status.
 * Pipeline: plan → requirements (auto-cascade) → execute
 */
export function derivePlanPipeline(plan: PlanWithStatus): PlanPipeline {
	const isExecuting = (plan.active_loops ?? []).some(
		(l) => l.state === 'executing' && l.current_task_id
	);

	const stage = plan.stage;

	// Determine plan phase state
	let planState: PlanPhaseState = 'none';
	if (plan.approved) {
		planState = 'complete';
	} else if (stage === 'reviewed' || stage === 'needs_changes' || stage === 'ready_for_approval') {
		planState = 'active';
	} else if (plan.goal || plan.context) {
		planState = 'active';
	}

	// Determine requirements phase state (auto-cascade: approved → requirements → scenarios → ready)
	const reqsDoneStages: PlanStage[] = [
		'ready_for_execution',
		'tasks_approved',
		'implementing',
		'executing',
		'reviewing_rollup',
		'complete'
	];
	const reqsActiveStages: PlanStage[] = [
		'approved',
		'requirements_generated',
		'scenarios_generated',
		// Legacy stages
		'phases_generated',
		'phases_approved',
		'tasks_generated'
	];
	let reqsState: PlanPhaseState = 'none';
	if (reqsDoneStages.includes(stage)) {
		reqsState = 'complete';
	} else if (reqsActiveStages.includes(stage)) {
		reqsState = 'active';
	}

	// Determine execute phase state
	let executeState: PlanPhaseState = 'none';
	if (stage === 'complete') {
		executeState = 'complete';
	} else if (stage === 'failed') {
		executeState = 'failed';
	} else if (stage === 'reviewing_rollup') {
		executeState = 'active'; // rollup review is the final gate before complete
	} else if (isExecuting || stage === 'implementing' || stage === 'executing') {
		executeState = 'active';
	}

	return {
		plan: planState,
		requirements: reqsState,
		execute: executeState
	};
}

/**
 * Human-readable label for a plan stage.
 */
export function getStageLabel(stage: PlanStage): string {
	const labels: Record<string, string> = {
		draft: 'Draft',
		drafting: 'Draft',
		ready_for_approval: 'Ready for Approval',
		reviewed: 'Reviewed',
		needs_changes: 'Needs Changes',
		planning: 'Planning',
		approved: 'Approved',
		rejected: 'Rejected',
		requirements_generated: 'Requirements Generated',
		scenarios_generated: 'Scenarios Generated',
		scenarios_reviewed: 'Scenarios Reviewed',
		ready_for_execution: 'Ready to Execute',
		phases_generated: 'Phases Generated',
		phases_approved: 'Phases Approved',
		tasks_generated: 'Tasks Generated',
		tasks_approved: 'Ready to Execute',
		tasks: 'Ready to Execute',
		implementing: 'Executing',
		executing: 'Executing',
		reviewing_rollup: 'Reviewing',
		complete: 'Complete',
		archived: 'Archived',
		failed: 'Failed'
	};
	return labels[stage] ?? stage;
}
