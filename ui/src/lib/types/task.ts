/**
 * Types for Tasks with BDD acceptance criteria.
 *
 * Task/AcceptanceCriterion schemas were removed from the backend (tasks are
 * created at execution time, not stored in plans). These types are now
 * frontend-only definitions used by execution UI components.
 */

// ============================================================================
// Frontend-owned types (no backend schema)
// ============================================================================

/**
 * BDD-style acceptance criterion (Given/When/Then).
 */
export interface AcceptanceCriterion {
	given: string;
	when: string;
	then: string;
}

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
 * - blocked: Blocked by an upstream dependency or ChangeProposal cascade
 * - dirty: A parent Requirement changed; task needs re-evaluation
 */
export type TaskStatus =
	| 'pending'
	| 'pending_approval'
	| 'approved'
	| 'rejected'
	| 'in_progress'
	| 'completed'
	| 'failed'
	| 'blocked'
	| 'dirty';

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
 */
export interface Task {
	id: string;
	plan_id: string;
	sequence: number;
	description: string;
	created_at: string;
	acceptance_criteria: AcceptanceCriterion[];
	title?: string;
	status: TaskStatus;
	type?: TaskType;
	phase_id?: string;
	files?: string[];
	depends_on?: string[];
	rejection_reason?: string;
	approved_by?: string;
	approved_at?: string;
	completed_at?: string;
	assigned_loop_id?: string;
	rejection?: TaskRejection;
	iteration?: number;
	max_iterations?: number;
	scenario_ids?: string[];
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
				label: 'Review Feedback',
				description: 'Minor issues found. Developer agent is retrying.',
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
	color: 'gray' | 'yellow' | 'green' | 'red' | 'blue' | 'orange';
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
		case 'blocked':
			return { label: 'Blocked', color: 'orange', icon: 'lock' };
		case 'dirty':
			return { label: 'Needs Re-evaluation', color: 'yellow', icon: 'alert-circle' };
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
