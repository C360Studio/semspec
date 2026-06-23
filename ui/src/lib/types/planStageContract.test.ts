import { describe, it, expect } from 'vitest';
import {
	derivePlanPipeline,
	getStageLabel,
	STAGE_LABELS,
	type PlanStage,
	type PlanWithStatus,
	type PlanPhaseState,
	type ActiveLoop
} from './plan';

/**
 * #221 INV6 — the operator UI phase must be derived from the authoritative state
 * machine, not stale heuristics. Two properties are pinned here:
 *
 *   1. Totality — every authoritative PlanStage maps to an explicit, human-
 *      readable operator label (no raw snake_case fallthrough). STAGE_LABELS is
 *      `Record<PlanStage, string>`, so a stage added to the union is already a
 *      compile error under `npm run check`; this suite is the runtime mirror.
 *   2. Monotonicity — as the backend stage advances along the happy-path spine,
 *      none of the three derived pipeline phases (plan / requirements / execute)
 *      regresses. The pre-fix derivation dipped requirements back to `none` on
 *      every `generating_*` stage and left `ready_for_qa` / `reviewing_qa` with
 *      `execute = none`; both read to an operator as "went backwards".
 *
 * ALL_PLAN_STAGES is the runtime enumeration of the union, kept authoritative by
 * a compile-time exhaustiveness guard so it cannot silently drift.
 */
const ALL_PLAN_STAGES = [
	'created',
	'draft',
	'exploring',
	'explored',
	'drafting',
	'drafted',
	'reviewing_draft',
	'ready_for_approval',
	'reviewed',
	'needs_changes',
	'planning',
	'approved',
	'rejected',
	'generating_requirements',
	'requirements_generated',
	'reviewing_requirements',
	'requirements_reviewed',
	'generating_architecture',
	'architecture_generated',
	'reviewing_architecture',
	'architecture_reviewed',
	'preparing_stories',
	'stories_generated',
	'generating_scenarios',
	'scenarios_generated',
	'reviewing_scenarios',
	'scenarios_reviewed',
	'awaiting_review',
	'changed',
	'ready_for_execution',
	'phases_generated',
	'phases_approved',
	'tasks_generated',
	'tasks_approved',
	'tasks',
	'implementing',
	'executing',
	'ready_for_qa',
	'reviewing_qa',
	'reviewing_rollup',
	'complete',
	'complete_with_deferrals',
	'archived',
	'failed'
] as const satisfies readonly PlanStage[];

// Compile-time exhaustiveness: if a PlanStage is missing from ALL_PLAN_STAGES,
// `_Missing` is a non-never union and this assignment fails `npm run check`.
type _Missing = Exclude<PlanStage, (typeof ALL_PLAN_STAGES)[number]>;
const _assertExhaustive: [_Missing] extends [never] ? true : { missingStages: _Missing } = true;
void _assertExhaustive;

function makePlan(
	stage: PlanStage,
	opts: { approved?: boolean; loops?: ActiveLoop[] } = {}
): PlanWithStatus {
	return {
		id: 'plan-1',
		slug: 'plan-1',
		project_id: 'default',
		title: 'Contract Plan',
		created_at: '2026-01-01T00:00:00Z',
		approved: opts.approved ?? false,
		stage,
		// Held constant so the test isolates the stage dimension.
		goal: 'ship the thing',
		context: 'brownfield baseline',
		active_loops: opts.loops ?? []
	};
}

describe('plan stage contract (#221 INV6)', () => {
	it('every authoritative stage has an explicit human-readable label', () => {
		for (const stage of ALL_PLAN_STAGES) {
			const label = getStageLabel(stage);
			expect(label, `label for ${stage}`).toBeTruthy();
			// A raw fallthrough returns the stage string itself; an explicit label
			// never equals its snake_case key and never contains an underscore.
			expect(label, `${stage} fell through to the raw stage string`).not.toBe(stage);
			expect(
				label.includes('_'),
				`label for ${stage} (${label}) looks like raw snake_case, not an operator label`
			).toBe(false);
		}
	});

	it('STAGE_LABELS is exactly the authoritative stage set (no missing, no extra)', () => {
		expect(new Set(Object.keys(STAGE_LABELS))).toEqual(new Set(ALL_PLAN_STAGES));
	});

	// Canonical forward (happy-path) spine. Off-path holds (awaiting_review,
	// changed), terminals (rejected, failed, archived), and legacy stages
	// (phases_*, tasks_*, executing, tasks) are excluded — monotonicity is the
	// property of the forward spine. `approved` flips true at the approval gate.
	const HAPPY_PATH: PlanStage[] = [
		'created',
		'exploring',
		'explored',
		'drafting',
		'drafted',
		'reviewing_draft',
		'ready_for_approval',
		'reviewed',
		'planning',
		'approved',
		'generating_requirements',
		'requirements_generated',
		// ADR-051 per-phase review gates (default off; part of the forward spine
		// when enabled — included here to pin pipeline-phase monotonicity through them).
		'reviewing_requirements',
		'requirements_reviewed',
		'generating_architecture',
		'architecture_generated',
		'reviewing_architecture',
		'architecture_reviewed',
		'preparing_stories',
		'stories_generated',
		'generating_scenarios',
		'scenarios_generated',
		'reviewing_scenarios',
		'scenarios_reviewed',
		'ready_for_execution',
		'implementing',
		'ready_for_qa',
		'reviewing_qa',
		'reviewing_rollup',
		'complete'
	];
	const APPROVED_FROM = HAPPY_PATH.indexOf('approved');

	const rank: Record<PlanPhaseState, number> = { none: 0, active: 1, complete: 2, failed: 0 };

	it('pipeline phases never regress along the happy-path spine', () => {
		let prev = { plan: 0, requirements: 0, execute: 0 };
		let prevStage = '(start)';
		HAPPY_PATH.forEach((stage, i) => {
			const pipeline = derivePlanPipeline(makePlan(stage, { approved: i >= APPROVED_FROM }));
			const cur = {
				plan: rank[pipeline.plan],
				requirements: rank[pipeline.requirements],
				execute: rank[pipeline.execute]
			};
			for (const phase of ['plan', 'requirements', 'execute'] as const) {
				expect(
					cur[phase],
					`${phase} regressed ${prevStage} → ${stage} (${prev[phase]} → ${cur[phase]})`
				).toBeGreaterThanOrEqual(prev[phase]);
			}
			prev = cur;
			prevStage = stage;
		});
	});

	it('the happy-path spine actually advances each phase to complete (not a vacuous floor)', () => {
		const last = derivePlanPipeline(makePlan('complete', { approved: true }));
		expect(last.plan).toBe<PlanPhaseState>('complete');
		expect(last.requirements).toBe<PlanPhaseState>('complete');
		expect(last.execute).toBe<PlanPhaseState>('complete');
	});

	it('an executing active loop drives the execute phase active (active-loop dimension)', () => {
		const loops = [{ state: 'executing', current_task_id: 'task-1' } as ActiveLoop];
		const pipeline = derivePlanPipeline(makePlan('ready_for_execution', { approved: true, loops }));
		expect(pipeline.execute).toBe<PlanPhaseState>('active');
	});
});
