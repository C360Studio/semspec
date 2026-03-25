/**
 * Types for Phase level between Plans and Tasks.
 *
 * NOTE: Phase schemas were removed from the backend API (replaced by Requirements).
 * These types are kept as local definitions for backwards compatibility with
 * existing components that haven't been cleaned up yet.
 */

// ============================================================================
// Phase Types (local definitions — no longer in generated API types)
// ============================================================================

/**
 * Phase represents a logical grouping of tasks within a plan.
 * @deprecated Phases replaced by Requirements in the current architecture.
 */
export interface Phase {
	id: string;
	slug?: string;
	plan_id?: string;
	plan_slug?: string;
	name?: string;
	title?: string;
	description?: string;
	sequence: number;
	status: PhaseStatus;
	depends_on?: string[];
	requires_approval?: boolean;
	approved?: boolean;
	approved_by?: string;
	approved_at?: string;
	started_at?: string;
	completed_at?: string;
	agent_config?: PhaseAgentConfig;
	created_at?: string;
	updated_at?: string;
}

/**
 * Agent configuration for a phase.
 * @deprecated
 */
export interface PhaseAgentConfig {
	model?: string;
	team?: string;
	roles?: string[];
	max_concurrent?: number;
	review_strategy?: string;
}

/**
 * Phase statistics for summary display.
 * @deprecated
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

/**
 * Phase execution status.
 */
export type PhaseStatus = 'pending' | 'ready' | 'active' | 'complete' | 'failed' | 'blocked';

// ============================================================================
// Helper Functions
// ============================================================================

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

export function canApprovePhase(phase: Phase): boolean {
	return (
		phase.requires_approval === true &&
		!phase.approved &&
		(phase.status === 'pending' || phase.status === 'ready')
	);
}

export function canEditPhase(phase: Phase): boolean {
	return phase.status === 'pending' || phase.status === 'ready' || phase.status === 'blocked';
}

export function canDeletePhase(phase: Phase): boolean {
	return phase.status === 'pending' || phase.status === 'ready' || phase.status === 'blocked';
}

export function arePhaseDependenciesMet(phase: Phase, allPhases: Phase[]): boolean {
	if (!phase.depends_on || phase.depends_on.length === 0) {
		return true;
	}

	return phase.depends_on.every((depId: string) => {
		const depPhase = allPhases.find((p) => p.id === depId);
		return depPhase?.status === 'complete';
	});
}

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
