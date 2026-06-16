import type { PlanStage, PlanWithStatus } from './plan';

const PLAN_STAGE_ORDER: PlanStage[] = [
	'created',
	'draft',
	'drafting',
	'drafted',
	'reviewing_draft',
	'reviewed',
	'needs_changes',
	'ready_for_approval',
	'planning',
	'approved',
	'generating_requirements',
	'requirements_generated',
	'generating_architecture',
	'architecture_generated',
	'generating_scenarios',
	'scenarios_generated',
	'reviewing_scenarios',
	'scenarios_reviewed',
	'ready_for_execution',
	'phases_generated',
	'phases_approved',
	'tasks_generated',
	'tasks',
	'tasks_approved',
	'implementing',
	'executing',
	'ready_for_qa',
	'reviewing_qa',
	'reviewing_rollup',
	'complete',
	'complete_with_deferrals',
	'failed',
	'archived',
	'rejected'
];

const PLAN_STAGE_RANK = new Map(PLAN_STAGE_ORDER.map((stage, index) => [stage, index]));

export function planStageRank(stage: PlanStage | undefined): number {
	return stage ? (PLAN_STAGE_RANK.get(stage) ?? -1) : -1;
}

export function selectFreshestPlan(
	streamPlan: PlanWithStatus | null | undefined,
	loadedPlan: PlanWithStatus | null | undefined,
	slug: string
): PlanWithStatus | null {
	const scopedStreamPlan = streamPlan?.slug === slug ? streamPlan : null;
	if (!scopedStreamPlan) return loadedPlan ?? null;
	if (!loadedPlan) return scopedStreamPlan;

	if (planStageRank(loadedPlan.stage) > planStageRank(scopedStreamPlan.stage)) {
		return loadedPlan;
	}
	return scopedStreamPlan;
}
