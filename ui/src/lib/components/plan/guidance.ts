import type { PlanStage } from '$lib/types/plan';

/**
 * Guidance rendered above the inline requirements list in PlanDetail.
 * `null` means "do not show the hint" — the stage badge and inline content
 * already communicate state and a hint would be dead text.
 */
export interface PlanGuidance {
	message: string;
	showApprove: boolean;
	isLoading?: boolean;
}

/**
 * Map (approval state + plan stage + requirement count) to the guidance hint,
 * or null when no hint should render.
 *
 * Bug #7.7 removed the implementing/executing hint ("Select a requirement to
 * view progress") and the legacy fallback ("Select a requirement to view its
 * scenarios"). Both pointed at an inline per-requirement timeline that was
 * never built; the plan-wide ExecutionTimeline on the parent page covers the
 * implementing case instead.
 */
export function deriveGuidance(
	approved: boolean,
	stage: PlanStage,
	requirementCount: number
): PlanGuidance | null {
	if (!approved) {
		return {
			message: 'Review the plan details, then create requirements and scenarios.',
			showApprove: true
		};
	}

	if (stage === 'approved' && requirementCount === 0) {
		return {
			message: 'Generating requirements from the approved plan...',
			showApprove: false,
			isLoading: true
		};
	}

	if (stage === 'requirements_generated') {
		return {
			message: 'Requirements generated. Generating scenarios...',
			showApprove: false,
			isLoading: true
		};
	}

	if (stage === 'scenarios_generated') {
		return {
			message:
				'Review the requirements and scenarios below. Edit or deprecate any that need changes, then approve to continue.',
			showApprove: false
		};
	}

	if (stage === 'ready_for_execution') {
		return {
			message: 'Requirements and scenarios approved. Click Execute to start.',
			showApprove: false
		};
	}

	if (stage === 'complete') {
		return {
			message: 'Plan execution complete.',
			showApprove: false
		};
	}

	// implementing / executing / legacy stages: no hint. Status is visible in
	// the header badge and the ExecutionTimeline renders on the parent page.
	return null;
}
