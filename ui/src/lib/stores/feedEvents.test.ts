import { describe, expect, it } from 'vitest';
import {
	annotateExecutionSummary,
	classifyExecutionFeedKind,
	classifyPlanFeedKind,
	planFeedEventKey
} from './feedEvents';
import type { PlanSSEPayload, RequirementSSEPayload, TaskSSEPayload } from '$lib/types/feed';

function plan(overrides: Partial<PlanSSEPayload>): Pick<PlanSSEPayload, 'stage' | 'phase_summary'> {
	return {
		stage: 'drafting',
		...overrides
	} as Pick<PlanSSEPayload, 'stage' | 'phase_summary'>;
}

describe('classifyPlanFeedKind', () => {
	it('marks recovery decisions distinctly from ordinary plan updates', () => {
		const kind = classifyPlanFeedKind(
			plan({
				stage: 'implementing',
				phase_summary: {
					stage: 'implementing',
					phase: 'recovery',
					state: 'waiting',
					title: 'Recovery',
					active_loop_count: 0,
					recovery: {
						decision_id: 'pd-1',
						status: 'proposed',
						kind: 'architecture_revise'
					},
					freshness: {
						source: 'plan-manager',
						generated_at: '2026-06-16T12:00:00Z',
						stale: false
					}
				}
			})
		);

		expect(kind).toBe('plan_recovery');
	});

	it('marks waits distinctly from stage changes', () => {
		const kind = classifyPlanFeedKind(
			plan({
				stage: 'ready_for_execution',
				phase_summary: {
					stage: 'ready_for_execution',
					phase: 'waiting',
					state: 'waiting',
					title: 'Ready for execution',
					active_loop_count: 0,
					wait: {
						reason: 'execution_not_started',
						required_action: 'start_execution'
					},
					freshness: {
						source: 'plan-manager',
						generated_at: '2026-06-16T12:00:00Z',
						stale: false
					}
				}
			})
		);

		expect(kind).toBe('plan_wait');
	});

	it('marks stale summaries ahead of other classifications', () => {
		const kind = classifyPlanFeedKind(
			plan({
				stage: 'implementing',
				phase_summary: {
					stage: 'implementing',
					phase: 'execution',
					state: 'active',
					title: 'Execution',
					active_loop_count: 1,
					freshness: {
						source: 'plan-manager',
						generated_at: '2026-06-16T12:00:00Z',
						stale: true,
						reason: 'sse_replay'
					}
				}
			})
		);

		expect(kind).toBe('plan_stale');
	});

	it('marks execution phase as execution-scoped even when delivered by plan SSE', () => {
		const kind = classifyPlanFeedKind(
			plan({
				stage: 'implementing',
				phase_summary: {
					stage: 'implementing',
					phase: 'execution',
					state: 'active',
					title: 'Execution',
					active_loop_count: 1,
					freshness: {
						source: 'plan-manager',
						generated_at: '2026-06-16T12:00:00Z',
						stale: false
					}
				}
			})
		);

		expect(kind).toBe('execution_phase');
	});
});

describe('classifyExecutionFeedKind', () => {
	it('keeps current-slug task rows as execution_task', () => {
		const payload = { slug: 'demo' } as TaskSSEPayload;
		expect(classifyExecutionFeedKind(payload, 'demo', 'execution_task')).toBe('execution_task');
	});

	it('marks mismatched slug rows stale', () => {
		const payload = { slug: 'old-run' } as RequirementSSEPayload;
		expect(classifyExecutionFeedKind(payload, 'current-run', 'execution_requirement')).toBe(
			'execution_stale'
		);
	});

	it('marks missing slug rows orphaned', () => {
		const payload = {} as TaskSSEPayload;
		expect(classifyExecutionFeedKind(payload, 'demo', 'execution_task')).toBe(
			'execution_orphaned'
		);
	});
});

describe('planFeedEventKey', () => {
	it('changes when a same-stage recovery decision appears', () => {
		const base = plan({
			stage: 'implementing',
			phase_summary: {
				stage: 'implementing',
				phase: 'execution',
				state: 'active',
				title: 'Execution',
				active_loop_count: 1,
				freshness: {
					source: 'plan-manager',
					generated_at: '2026-06-16T12:00:00Z',
					stale: false
				}
			}
		});
		const recovery = plan({
			stage: 'implementing',
			phase_summary: {
				stage: 'implementing',
				phase: 'recovery',
				state: 'waiting',
				title: 'Recovery',
				active_loop_count: 0,
				recovery: {
					decision_id: 'pd-1',
					status: 'proposed'
				},
				freshness: {
					source: 'plan-manager',
					generated_at: '2026-06-16T12:00:00Z',
					stale: false
				}
			}
		});

		expect(planFeedEventKey(base)).not.toBe(planFeedEventKey(recovery));
	});
});

describe('annotateExecutionSummary', () => {
	it('prefixes orphaned rows for human scanability', () => {
		expect(annotateExecutionSummary('Task executing: Build', 'execution_orphaned')).toBe(
			'Orphaned execution: Task executing: Build'
		);
	});

	it('leaves ordinary rows unchanged', () => {
		expect(annotateExecutionSummary('Task executing: Build', 'execution_task')).toBe(
			'Task executing: Build'
		);
	});
});
