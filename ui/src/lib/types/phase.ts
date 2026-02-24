/**
 * Types for Phase level between Plans and Tasks.
 *
 * Phases enable logical grouping of related tasks (e.g., "Setup", "Implementation", "Testing"),
 * phase-level dependencies, agent team routing per phase, and optional human approval at phase boundaries.
 */

// ============================================================================
// Phase Types
// ============================================================================

/**
 * Phase execution status.
 * - pending: Not yet ready (dependencies not met)
 * - ready: Dependencies met, awaiting start
 * - active: Tasks being executed
 * - complete: All tasks completed
 * - failed: Execution failed
 * - blocked: Blocked by dependency
 */
export type PhaseStatus = 'pending' | 'ready' | 'active' | 'complete' | 'failed' | 'blocked';

/**
 * Agent configuration for a phase.
 * Allows routing specific agent teams or models to different phases.
 */
export interface PhaseAgentConfig {
	/** Agent roles that should work on this phase */
	roles?: string[];
	/** Override default model for this phase */
	model?: string;
	/** Maximum concurrent tasks within this phase */
	max_concurrent?: number;
	/** Review strategy for tasks in this phase */
	review_strategy?: 'parallel' | 'sequential';
}

/**
 * Phase represents a logical grouping of tasks within a plan.
 */
export interface Phase {
	/** Unique identifier */
	id: string;
	/** Parent plan ID */
	plan_id: string;
	/** Order within plan (1-based) */
	sequence: number;
	/** Display name (e.g., "Phase 1: Foundation") */
	name: string;
	/** Purpose and scope description */
	description?: string;

	/** Phase IDs that must complete before this phase can start */
	depends_on?: string[];

	/** Current execution state */
	status: PhaseStatus;

	/** Agent/model routing configuration */
	agent_config?: PhaseAgentConfig;

	/** Whether this phase requires human approval before execution */
	requires_approval?: boolean;
	/** Whether this phase has been approved */
	approved?: boolean;
	/** When the phase was approved */
	approved_at?: string;
	/** Who approved the phase */
	approved_by?: string;

	/** When the phase was created */
	created_at: string;
	/** When the phase execution started */
	started_at?: string;
	/** When the phase completed */
	completed_at?: string;
}

/**
 * Phase statistics for summary display.
 */
export interface PhaseStats {
	total: number;
	pending: number;
	ready: number;
	active: number;
	complete: number;
	failed: number;
	blocked: number;
}

// ============================================================================
// Helper Functions
// ============================================================================

/**
 * Get styling info for a phase status.
 */
export function getPhaseStatusInfo(status: PhaseStatus): {
	label: string;
	color: 'gray' | 'yellow' | 'green' | 'red' | 'blue' | 'orange';
	icon: string;
} {
	switch (status) {
		case 'pending':
			return { label: 'Pending', color: 'gray', icon: 'circle' };
		case 'ready':
			return { label: 'Ready', color: 'yellow', icon: 'play-circle' };
		case 'active':
			return { label: 'Active', color: 'blue', icon: 'loader' };
		case 'complete':
			return { label: 'Complete', color: 'green', icon: 'check-circle' };
		case 'failed':
			return { label: 'Failed', color: 'red', icon: 'x-circle' };
		case 'blocked':
			return { label: 'Blocked', color: 'orange', icon: 'lock' };
	}
}

/**
 * Check if a phase can be approved.
 */
export function canApprovePhase(phase: Phase): boolean {
	return (
		phase.requires_approval === true &&
		!phase.approved &&
		(phase.status === 'pending' || phase.status === 'ready')
	);
}

/**
 * Check if a phase can be edited.
 */
export function canEditPhase(phase: Phase): boolean {
	return phase.status === 'pending' || phase.status === 'ready' || phase.status === 'blocked';
}

/**
 * Check if a phase can be deleted.
 */
export function canDeletePhase(phase: Phase): boolean {
	return phase.status === 'pending' || phase.status === 'ready' || phase.status === 'blocked';
}

/**
 * Check if a phase's dependencies are met.
 */
export function arePhaseDependenciesMet(phase: Phase, allPhases: Phase[]): boolean {
	if (!phase.depends_on || phase.depends_on.length === 0) {
		return true;
	}

	return phase.depends_on.every((depId) => {
		const depPhase = allPhases.find((p) => p.id === depId);
		return depPhase?.status === 'complete';
	});
}

/**
 * Compute task statistics for a phase.
 */
export function computePhaseTaskStats(
	phaseId: string,
	tasks: { phase_id?: string; status: string }[]
): {
	total: number;
	completed: number;
	in_progress: number;
	pending: number;
} {
	const phaseTasks = tasks.filter((t) => t.phase_id === phaseId);
	return {
		total: phaseTasks.length,
		completed: phaseTasks.filter((t) => t.status === 'completed').length,
		in_progress: phaseTasks.filter((t) => t.status === 'in_progress').length,
		pending: phaseTasks.filter(
			(t) =>
				t.status === 'pending' || t.status === 'pending_approval' || t.status === 'approved'
		).length
	};
}
