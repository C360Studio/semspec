/**
 * Types for ADR-003 Tasks with BDD acceptance criteria.
 *
 * Core types are derived from the generated OpenAPI spec to prevent drift
 * between Go backend and TypeScript frontend. Frontend-only extensions
 * (RejectionType, TaskRejection, helper functions) are defined here.
 */
import type { components } from './api.generated';

// ============================================================================
// Generated types (source of truth from Go backend OpenAPI spec)
// ============================================================================

/** Task from API — the generated type */
type GeneratedTask = components['schemas']['Task'];

/** AcceptanceCriterion from API */
type GeneratedAcceptanceCriterion = components['schemas']['AcceptanceCriterion'];

// ============================================================================
// Re-exports and type aliases
// ============================================================================

/**
 * BDD-style acceptance criterion (Given/When/Then).
 * Re-exported from generated types for convenience.
 */
export type AcceptanceCriterion = GeneratedAcceptanceCriterion;

// ============================================================================
// Frontend-only types (not in the Go API)
// ============================================================================

/**
 * Task execution status.
 * - pending: Created but not yet submitted for approval
 * - pending_approval: Awaiting human approval before execution
 * - approved: Approved for execution
 * - rejected: Rejected, needs revision
 * - in_progress: Currently being executed
 * - completed: Successfully completed
 * - failed: Execution failed
 */
export type TaskStatus =
	| 'pending'
	| 'pending_approval'
	| 'approved'
	| 'rejected'
	| 'in_progress'
	| 'completed'
	| 'failed';

/**
 * Type of work a task represents.
 */
export type TaskType = 'implement' | 'test' | 'document' | 'review' | 'refactor';

/**
 * Rejection type from reviewer.
 * Determines routing: back to developer, back to plan, or task decomposition.
 */
export type RejectionType =
	| 'fixable' // Minor issues, developer can retry
	| 'misscoped' // Task scope is wrong, back to plan
	| 'architectural' // Architectural issue, back to plan
	| 'too_big'; // Task too large, needs decomposition

/**
 * Rejection information when a task fails review.
 */
export interface TaskRejection {
	type: RejectionType;
	reason: string;
	iteration: number;
	rejected_at: string;
}

/**
 * Task represents an executable unit of work derived from a Plan.
 *
 * The core shape comes from the generated OpenAPI spec (Go backend is source of truth).
 * Frontend-only extensions are added here.
 */
export interface Task extends Omit<GeneratedTask, 'status' | 'type'> {
	/** Current execution state (narrowed from string) */
	status: TaskStatus;
	/** Kind of work (narrowed from string) */
	type?: TaskType;
	/** Active loop working on this task (frontend-only) */
	assigned_loop_id?: string;
	/** Rejection info if task failed review (frontend-only) */
	rejection?: TaskRejection;
	/** Current iteration in developer/reviewer loop (frontend-only) */
	iteration?: number;
	/** Maximum iterations before escalation (frontend-only) */
	max_iterations?: number;
}

// ============================================================================
// Helper functions
// ============================================================================

/**
 * Get a human-readable label for a task type.
 */
export function getTaskTypeLabel(type: TaskType | undefined): string {
	switch (type) {
		case 'implement':
			return 'Implementation';
		case 'test':
			return 'Testing';
		case 'document':
			return 'Documentation';
		case 'review':
			return 'Review';
		case 'refactor':
			return 'Refactoring';
		default:
			return 'Task';
	}
}

/**
 * Get routing guidance based on rejection type.
 */
export function getRejectionRouting(type: RejectionType): {
	label: string;
	description: string;
	action: 'retry' | 'plan' | 'decompose';
} {
	switch (type) {
		case 'fixable':
			return {
				label: 'Fixable',
				description: 'Minor issues found. Developer will retry.',
				action: 'retry'
			};
		case 'misscoped':
			return {
				label: 'Misscoped',
				description: 'Task scope is incorrect. Returning to plan.',
				action: 'plan'
			};
		case 'architectural':
			return {
				label: 'Architectural Issue',
				description: 'Architectural changes needed. Returning to plan.',
				action: 'plan'
			};
		case 'too_big':
			return {
				label: 'Too Large',
				description: 'Task is too large. Needs decomposition.',
				action: 'decompose'
			};
	}
}

/**
 * Get styling info for a task status.
 */
export function getTaskStatusInfo(status: TaskStatus): {
	label: string;
	color: 'gray' | 'yellow' | 'green' | 'red' | 'blue';
	icon: string;
} {
	switch (status) {
		case 'pending':
			return { label: 'Pending', color: 'gray', icon: 'circle' };
		case 'pending_approval':
			return { label: 'Pending Approval', color: 'yellow', icon: 'clock' };
		case 'approved':
			return { label: 'Approved', color: 'green', icon: 'check-circle' };
		case 'rejected':
			return { label: 'Rejected', color: 'red', icon: 'x-circle' };
		case 'in_progress':
			return { label: 'In Progress', color: 'blue', icon: 'loader' };
		case 'completed':
			return { label: 'Completed', color: 'green', icon: 'check' };
		case 'failed':
			return { label: 'Failed', color: 'red', icon: 'x' };
	}
}

/**
 * Check if a task can be approved (is in pending_approval status).
 */
export function canApproveTask(task: Task): boolean {
	return task.status === 'pending_approval';
}

/**
 * Check if a task can be edited (not yet in progress or completed).
 */
export function canEditTask(task: Task): boolean {
	return ['pending', 'pending_approval', 'rejected'].includes(task.status);
}

/**
 * Check if a task can be deleted.
 */
export function canDeleteTask(task: Task): boolean {
	return ['pending', 'pending_approval', 'rejected'].includes(task.status);
}
