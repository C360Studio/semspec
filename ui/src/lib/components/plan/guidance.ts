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
	// Pre-approval LLM-in-progress states: planner or reviewer is actively
	// working. No human action is appropriate — hide the approve button and
	// show a loading hint so the user knows something is happening. Without
	// this branch the generic `!approved` fallback below would enable
	// "Create Requirements" while drafting/review is still in flight, and
	// the empty plan body reads as "stuck" rather than "in progress".
	if (stage === 'drafting') {
		return {
			message: 'Planner is composing the plan goal, context, and scope…',
			showApprove: false,
			isLoading: true
		};
	}
	if (stage === 'drafted') {
		return {
			message: 'Plan drafted. Waiting for plan reviewer…',
			showApprove: false,
			isLoading: true
		};
	}
	if (stage === 'reviewing_draft') {
		return {
			message: 'Plan reviewer is evaluating the draft…',
			showApprove: false,
			isLoading: true
		};
	}

	if (!approved) {
		return {
			message: 'Review the plan details, then create requirements and scenarios.',
			showApprove: true
		};
	}

	// Explicit "generator/reviewer running" stages emitted by the backend.
	// Previously these fell through to indirect heuristics (e.g.
	// `approved + 0 reqs → generating requirements`) which missed real cases —
	// e.g. plan-manager transitions directly to `generating_requirements`
	// before requirementCount has propagated to the UI. The explicit branches
	// guarantee the in-progress panel surfaces consistently for every long
	// LLM call. Ordered to match plan lifecycle.
	if (stage === 'generating_requirements' || (stage === 'approved' && requirementCount === 0)) {
		return {
			message: 'Decomposing the approved plan into testable requirements…',
			showApprove: false,
			isLoading: true
		};
	}

	if (stage === 'generating_architecture') {
		return {
			message: 'Architecture generator is selecting technology and component boundaries…',
			showApprove: false,
			isLoading: true
		};
	}

	if (stage === 'generating_scenarios' || stage === 'requirements_generated') {
		return {
			message: 'Generating scenarios for each requirement…',
			showApprove: false,
			isLoading: true
		};
	}

	if (stage === 'reviewing_scenarios') {
		return {
			message: 'Plan reviewer is evaluating requirements and scenarios…',
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

	if (stage === 'reviewing_qa') {
		return {
			message: 'QA reviewer is checking the release-readiness of the implementation…',
			showApprove: false,
			isLoading: true
		};
	}

	if (stage === 'reviewing_rollup') {
		return {
			message: 'Rollup review is summarising execution results across requirements…',
			showApprove: false,
			isLoading: true
		};
	}

	if (stage === 'complete') {
		return {
			message: 'Plan execution complete.',
			showApprove: false
		};
	}

	// implementing / executing: status is visible in the ExecutionTimeline +
	// AgentPipelineView which already surface per-loop progress. No top-level
	// in-progress panel — would duplicate the existing pipeline UI.
	//
	// `architecture_generated` and `ready_for_qa` also intentionally return
	// null: both are transient auto-advance stages (architecture_generated →
	// generating_scenarios, ready_for_qa → reviewing_qa) that the next
	// dispatch cycle claims within seconds. Surfacing a loading panel for
	// them would flash on and off, which reads as more "broken" than no
	// panel. If either ever becomes a stage that HOLDS, add an isLoading
	// entry here AND a matching stageTitle() case in +page.svelte.
	return null;
}
