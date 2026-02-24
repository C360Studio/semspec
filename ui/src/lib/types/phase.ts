/**
 * Types for Phase level between Plans and Tasks.
 *
 * Phases enable logical grouping of related tasks (e.g., "Setup", "Implementation", "Testing"),
 * phase-level dependencies, agent team routing per phase, and optional human approval at phase boundaries.
 *
 * Core types are re-exported from the generated API types for consistency with the backend.
 */

import type { components } from './api.generated';

// ============================================================================
// Phase Types (re-exported from generated API types)
// ============================================================================

/**
 * Phase represents a logical grouping of tasks within a plan.
 * Re-exported from generated API types.
 */
export type Phase = components['schemas']['Phase'];

/**
 * Agent configuration for a phase.
 * Re-exported from generated API types.
 */
export type PhaseAgentConfig = components['schemas']['PhaseAgentConfig'];

/**
 * Phase statistics for summary display.
 * Re-exported from generated API types.
 */
export type PhaseStats = components['schemas']['PhaseStats'];

/**
 * Phase execution status.
 * - pending: Not yet ready (dependencies not met)
 * - ready: Dependencies met, awaiting start
 * - active: Tasks being executed
 * - complete: All tasks completed
 * - failed: Execution failed
 * - blocked: Blocked by dependency
 *
 * Note: The generated type is a string, but we provide a union type for better type safety.
 */
export type PhaseStatus = 'pending' | 'ready' | 'active' | 'complete' | 'failed' | 'blocked';

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
