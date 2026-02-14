/**
 * Types for ADR-003 Tasks with BDD acceptance criteria.
 * Tasks are executable units of work derived from a Plan.
 */

/**
 * BDD-style acceptance criterion (Given/When/Then).
 */
export interface AcceptanceCriterion {
	/** Precondition */
	given: string;
	/** Action being performed */
	when: string;
	/** Expected outcome */
	then: string;
}

/**
 * Task execution status.
 */
export type TaskStatus = 'pending' | 'in_progress' | 'completed' | 'failed';

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
 * Maps to workflow.Task in Go backend.
 */
export interface Task {
	/** Unique identifier (format: task.{plan_slug}.{sequence}) */
	id: string;
	/** Parent plan entity ID */
	plan_id: string;
	/** Order within the plan (1-indexed) */
	sequence: number;
	/** What to implement */
	description: string;
	/** Kind of work (implement, test, document, review, refactor) */
	type?: TaskType;
	/** BDD-style conditions for task completion */
	acceptance_criteria: AcceptanceCriterion[];
	/** Files in scope for this task */
	files?: string[];
	/** Current execution state */
	status: TaskStatus;
	/** When the task was created */
	created_at: string;
	/** When the task finished (success or failure) */
	completed_at?: string;
	/** Active loop working on this task */
	assigned_loop_id?: string;
	/** Rejection info if task failed review */
	rejection?: TaskRejection;
	/** Current iteration in developer/reviewer loop */
	iteration?: number;
	/** Maximum iterations before escalation */
	max_iterations?: number;
}

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
