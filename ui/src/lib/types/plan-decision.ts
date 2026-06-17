/**
 * Types for PlanDecisions — mid-stream requirement mutation nodes from ADR-024.
 *
 * A PlanDecision represents a proposed change to one or more Requirements.
 * On acceptance, the reactive workflow cascades changes to affected Scenarios and Tasks.
 */

import type { components } from './api.generated';

// ============================================================================
// Status types
// ============================================================================

/**
 * PlanDecision lifecycle status.
 * - proposed: Submitted, awaiting review
 * - under_review: Being evaluated
 * - accepted: Approved, cascade in progress or complete
 * - rejected: Declined, no changes made
 * - archived: Historical record, no longer actionable
 */
export type PlanDecisionStatus =
	| 'proposed'
	| 'under_review'
	| 'accepted'
	| 'rejected'
	| 'archived';

type GeneratedPlanDecision = NonNullable<components['schemas']['PlanWithStatus']['plan_decisions']>[number];

/**
 * A mid-stream change proposal that mutates one or more Requirements.
 */
export interface PlanDecision extends Omit<GeneratedPlanDecision, 'status'> {
	status: PlanDecisionStatus;
}

// ============================================================================
// Helper functions
// ============================================================================

/**
 * Get display info for a change proposal status.
 */
export function getPlanDecisionStatusInfo(status: PlanDecisionStatus): {
	label: string;
	color: 'blue' | 'orange' | 'green' | 'red' | 'gray';
	icon: string;
} {
	switch (status) {
		case 'proposed':
			return { label: 'Proposed', color: 'blue', icon: 'file-plus' };
		case 'under_review':
			return { label: 'Under Review', color: 'orange', icon: 'eye' };
		case 'accepted':
			return { label: 'Accepted', color: 'green', icon: 'check-circle' };
		case 'rejected':
			return { label: 'Rejected', color: 'red', icon: 'x-circle' };
		case 'archived':
			return { label: 'Archived', color: 'gray', icon: 'archive' };
	}
}
