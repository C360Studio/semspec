/**
 * Types for Requirements — Plan-scoped intent nodes from ADR-024.
 *
 * Requirements describe the "what" of a plan. They are Plan-scoped (not Phase-scoped)
 * and parent Scenarios which describe observable behavioral contracts.
 */

// ============================================================================
// Status types
// ============================================================================

/**
 * Requirement status in the plan lifecycle.
 * - active: Currently valid and in scope
 * - deprecated: No longer relevant but kept for history
 * - superseded: Replaced by another Requirement via a PlanDecision
 */
export type RequirementStatus = 'active' | 'deprecated' | 'superseded';

// ============================================================================
// Core interface
// ============================================================================

/**
 * A plan-level requirement that defines intent.
 * Requirements are Plan-scoped and have child Scenarios.
 */
export interface Requirement {
	id: string;
	plan_id: string;
	title: string;
	description: string;
	status: RequirementStatus;
	depends_on?: string[];
	created_at: string;
	updated_at: string;
}

// ============================================================================
// Helper functions
// ============================================================================

/**
 * Get display info for a requirement status.
 */
export function getRequirementStatusInfo(status: RequirementStatus): {
	label: string;
	color: 'green' | 'gray' | 'orange';
	icon: string;
} {
	switch (status) {
		case 'active':
			return { label: 'Active', color: 'green', icon: 'check-circle' };
		case 'deprecated':
			return { label: 'Deprecated', color: 'gray', icon: 'archive' };
		case 'superseded':
			return { label: 'Superseded', color: 'orange', icon: 'arrow-right-circle' };
	}
}
