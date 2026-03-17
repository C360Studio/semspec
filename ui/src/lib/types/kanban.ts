/**
 * Types for the Kanban Board view.
 *
 * Maps semspec task/scenario statuses to kanban columns
 * and provides a unified card item type for cross-plan display.
 */
import type { TaskStatus, TaskType, TaskRejection } from './task';
import type { ScenarioStatus } from './scenario';

/** The original backend status before kanban mapping */
export type OriginalStatus = TaskStatus | ScenarioStatus;

// ============================================================================
// Kanban status and column definitions
// ============================================================================

/**
 * Logical kanban column statuses.
 * Each maps to one or more task/scenario statuses.
 */
export type KanbanStatus =
	| 'backlog'
	| 'in_progress'
	| 'in_review'
	| 'completed'
	| 'needs_attention';

/**
 * Column definition for rendering.
 */
export interface KanbanColumnDef {
	status: KanbanStatus;
	label: string;
	icon: string;
	color: string;
}

/**
 * Default column definitions.
 */
export const KANBAN_COLUMNS: KanbanColumnDef[] = [
	{ status: 'backlog', label: 'Backlog', icon: 'inbox', color: 'neutral' },
	{ status: 'in_progress', label: 'In Progress', icon: 'loader', color: 'accent' },
	{ status: 'in_review', label: 'In Review', icon: 'eye', color: 'info' },
	{ status: 'completed', label: 'Completed', icon: 'check-circle', color: 'success' },
	{ status: 'needs_attention', label: 'Needs Attention', icon: 'alert-triangle', color: 'warning' }
];

/**
 * Default visible columns.
 */
export const DEFAULT_ACTIVE_STATUSES: Set<KanbanStatus> = new Set([
	'backlog',
	'in_progress',
	'in_review',
	'completed',
	'needs_attention'
]);

// ============================================================================
// Status mapping functions
// ============================================================================

/**
 * Map a task status to a kanban column.
 */
export function taskToKanbanStatus(status: TaskStatus): KanbanStatus {
	switch (status) {
		case 'pending':
		case 'approved':
			return 'backlog';
		case 'in_progress':
			return 'in_progress';
		case 'pending_approval':
			return 'in_review';
		case 'completed':
			return 'completed';
		case 'rejected':
		case 'failed':
		case 'blocked':
		case 'dirty':
			return 'needs_attention';
	}
}

/**
 * Map a scenario status to a kanban column.
 */
export function scenarioToKanbanStatus(status: ScenarioStatus): KanbanStatus {
	switch (status) {
		case 'pending':
		case 'skipped':
			return 'backlog';
		case 'passing':
			return 'completed';
		case 'failing':
			return 'needs_attention';
	}
}

// ============================================================================
// Kanban card item (unified display type)
// ============================================================================

/**
 * A unified work item for kanban display.
 * Can represent either a task or a scenario.
 */
export interface KanbanCardItem {
	id: string;
	type: 'task' | 'scenario';
	title: string;
	kanbanStatus: KanbanStatus;
	originalStatus: OriginalStatus;
	planSlug: string;
	requirementId?: string;
	requirementTitle?: string;
	taskType?: TaskType;
	rejection?: TaskRejection;
	iteration?: number;
	maxIterations?: number;
	agentRole?: string;
	agentModel?: string;
	agentState?: string;
	scenarioIds?: string[];
	scenarioGiven?: string;
	scenarioWhen?: string;
	scenarioThen?: string[];
}

// ============================================================================
// Kanban view mode
// ============================================================================

export type BoardViewMode = 'grid' | 'kanban';
