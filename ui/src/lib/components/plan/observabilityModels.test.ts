import { describe, expect, it } from 'vitest';
import { formatCostLabel, type ProviderRate } from '$lib/types/costAccounting';
import type { PlanPhaseSummary } from '$lib/types/feed';
import type { PlanWithStatus } from '$lib/types/plan';
import type { TrajectoryListItem } from '$lib/types/trajectory';
import {
	executionBlockers,
	inferRecoveryAutoAccept,
	isLessonTrajectoryItem,
	lessonActivityModel,
	phaseSummaryDetail,
	planFreshnessIndicatorState,
	recoveryAffectedNodes,
	shouldShowPhaseSummaryBanner,
	storyTaskCounts
} from './observabilityModels';

function plan(overrides: Partial<PlanWithStatus> = {}): PlanWithStatus {
	return {
		id: 'plan.demo',
		project_id: 'project.demo',
		slug: 'demo',
		title: 'Demo',
		stage: 'implementing',
		approved: true,
		created_at: '2026-06-16T12:00:00Z',
		active_loops: [],
		...overrides
	} as PlanWithStatus;
}

function phaseSummary(overrides: Partial<PlanPhaseSummary> = {}): PlanPhaseSummary {
	return {
		stage: 'implementing',
		phase: 'execution',
		state: 'active',
		title: 'Execution',
		active_loop_count: 1,
		freshness: {
			source: 'plan-manager',
			generated_at: '2026-06-16T12:10:00Z',
			stale: false
		},
		...overrides
	} as PlanPhaseSummary;
}

function trajectory(overrides: Partial<TrajectoryListItem> = {}): TrajectoryListItem {
	return {
		loop_id: 'loop.lesson',
		workflow_slug: 'semspec-lesson-decomposition',
		workflow_step: 'lesson-decomposition',
		start_time: '2026-06-16T12:11:00Z',
		duration: 1400,
		iterations: 2,
		model: 'gemini-pro',
		role: 'lesson-decomposer',
		task_id: 'lesson-decompose-demo',
		total_tokens_in: 1200,
		total_tokens_out: 300,
		...overrides
	} as TrajectoryListItem;
}

describe('phase banner observability model', () => {
	it('shows non-terminal execution/recovery/qa summaries even after active loop rows finish', () => {
		expect(shouldShowPhaseSummaryBanner(phaseSummary({ phase: 'execution', state: 'complete' }))).toBe(true);
		expect(shouldShowPhaseSummaryBanner(phaseSummary({ phase: 'recovery', state: 'complete' }))).toBe(true);
		expect(shouldShowPhaseSummaryBanner(phaseSummary({ phase: 'terminal', state: 'complete' }))).toBe(false);
	});

	it('prefers wait and recovery detail over generic empty copy', () => {
		expect(phaseSummaryDetail(phaseSummary({
			wait: {
				reason: 'human_gate',
				policy_reason: 'contract change requires review'
			}
		}))).toBe('contract change requires review');

		expect(phaseSummaryDetail(phaseSummary({
			recovery: {
				decision_id: 'pd-1',
				status: 'proposed',
				summary: 'Recovered architecture needs approval'
			}
		}))).toBe('Recovered architecture needs approval');
	});
});

describe('execution detail observability model', () => {
	it('surfaces waits, recovery proposals, QA failures, and plan errors as blockers', () => {
		const blockers = executionBlockers(plan({
			last_error: 'executor wedged',
			phase_summary: phaseSummary({
				wait: {
					reason: 'awaiting_plan_decision',
					policy_reason: 'full-auto policy refused contract change'
				},
				recovery: {
					decision_id: 'pd-1',
					status: 'proposed',
					summary: 'Need topology repair'
				}
			}),
			qa_run: {
				run_id: 'qa-1',
				completed_at: '2026-06-16T12:12:00Z',
				duration_ms: 1200,
				passed: false,
				failures: [{ job_name: 'integration', category: 'topology', message: 'duplicate root element' }]
			}
		}));

		expect(blockers.map((blocker) => blocker.kind)).toEqual(['wait', 'recovery', 'qa', 'error']);
		expect(blockers[0].detail).toContain('full-auto policy refused');
		expect(blockers[2].detail).toContain('duplicate root element');
	});

	it('counts task status groups for Story detail rows', () => {
		const counts = storyTaskCounts({
			id: 'story-1',
			title: 'Story',
			status: 'running',
			tasks: [
				{ id: 't1', status: 'approved' },
				{ id: 't2', status: 'executing' },
				{ id: 't3', status: 'rejected' }
			]
		} as NonNullable<PlanWithStatus['stories']>[number]);

		expect(counts).toEqual({ total: 3, done: 1, active: 1, failed: 1 });
	});
});

describe('recovery detail observability model', () => {
	it('shows affected requirements, stories, and contract nodes', () => {
		const affected = recoveryAffectedNodes({
			id: 'pd-1',
			plan_id: 'plan.demo',
			title: 'Repair topology',
			rationale: 'QA caught composite build mismatch',
			status: 'proposed',
			proposed_by: 'recovery-agent',
			affected_requirement_ids: ['req-1'],
			affected_story_ids: ['story-1'],
			contract_impact: {
				kind: 'refine',
				summary: 'Add dependency API contract',
				affected_ids: ['contract.topology.gradle']
			},
			created_at: '2026-06-16T12:00:00Z'
		});

		expect(affected.map((node) => `${node.kind}:${node.id}`)).toEqual([
			'Requirement:req-1',
			'Story:story-1',
			'Contract:contract.topology.gradle'
		]);
	});

	it('classifies auto-accept status without hiding human-gated contract changes', () => {
		expect(inferRecoveryAutoAccept({
			id: 'pd-change',
			plan_id: 'plan.demo',
			title: 'Change contract',
			rationale: 'Needs bigger scope',
			status: 'proposed',
			proposed_by: 'recovery-agent',
			affected_requirement_ids: ['req-1'],
			contract_impact: { kind: 'change', summary: 'Drop original obligation' },
			created_at: '2026-06-16T12:00:00Z'
		}).label).toBe('Review required');

		expect(inferRecoveryAutoAccept({
			id: 'pd-refine',
			plan_id: 'plan.demo',
			title: 'Refine contract',
			rationale: 'Scope one story',
			status: 'under_review',
			proposed_by: 'recovery-agent',
			affected_requirement_ids: ['req-1'],
			contract_impact: { kind: 'refine', summary: 'Targeted repair' },
			created_at: '2026-06-16T12:00:00Z'
		}).label).toBe('Policy eligible');

		expect(inferRecoveryAutoAccept({
			id: 'pd-accepted',
			plan_id: 'plan.demo',
			title: 'Accepted',
			rationale: 'Policy allowed it',
			status: 'accepted',
			proposed_by: 'recovery-agent',
			affected_requirement_ids: ['req-1'],
			created_at: '2026-06-16T12:00:00Z'
		}).state).toBe('success');
	});
});

describe('lesson activity observability model', () => {
	const rates: ProviderRate[] = [{
		model: 'gemini-pro',
		inputUsdPerMillionTokens: 1,
		outputUsdPerMillionTokens: 2,
		source: 'test-rate-card'
	}];

	it('marks lessons as future-run only and includes measured lesson-loop cost', () => {
		const model = lessonActivityModel(
			plan({
				phase_summary: phaseSummary({
					lessons: {
						state: 'complete',
						current_run_effect: 'none',
						future_run_effect: 'eligible_for_future_prompts'
					}
				})
			}),
			[
				trajectory(),
				trajectory({
					loop_id: 'loop.dev',
					workflow_slug: 'semspec-task-execution',
					workflow_step: 'develop',
					role: 'developer',
					task_id: 'dev-1'
				})
			],
			rates
		);

		expect(model.lessonLoops).toHaveLength(1);
		expect(model.currentEffect).toBe('none');
		expect(model.futureEffect).toBe('eligible for future prompts');
		expect(model.lessonUsage.totalTokens).toBe(1500);
		expect(formatCostLabel(model.costAccounting, true)).toBe('$0.0018 est.');
		expect(model.roleSummaries[0]).toMatchObject({ role: 'lesson decomposer', loops: 1 });
	});

	it('detects lesson loops from role, workflow step, workflow slug, or task id', () => {
		expect(isLessonTrajectoryItem(trajectory({ workflow_slug: 'other', workflow_step: 'other', role: 'lesson-curator' }))).toBe(true);
		expect(isLessonTrajectoryItem(trajectory({ workflow_slug: 'other', workflow_step: 'decompose', role: 'developer' }))).toBe(true);
		expect(isLessonTrajectoryItem(trajectory({ workflow_slug: 'other', workflow_step: 'other', role: 'developer', task_id: 'lesson-pass' }))).toBe(true);
		expect(isLessonTrajectoryItem(trajectory({ workflow_slug: 'semspec-task-execution', workflow_step: 'develop', role: 'developer', task_id: 'dev-1' }))).toBe(false);
	});
});

describe('freshness observability model', () => {
	it('shows stale and disconnected state with the last successful update', () => {
		const state = planFreshnessIndicatorState(
			plan({
				phase_summary: phaseSummary({
					freshness: {
						source: 'plan-manager',
						generated_at: '2026-06-16T12:10:00Z',
						stale: true,
						reason: 'sse_replay'
					}
				})
			}),
			{
				currentSlug: 'demo',
				connected: false,
				streamEverConnected: true,
				lastSuccessfulUpdateAt: '2026-06-16T12:11:00Z'
			}
		);

		expect(state.shouldShow).toBe(true);
		expect(state.statusLabel).toBe('Stale data and stream disconnected');
		expect(state.lastUpdateAt).toBe('2026-06-16T12:11:00Z');
		expect(state.reason).toBe('sse_replay');
	});

	it('hides when the plan is fresh and the stream is live', () => {
		const state = planFreshnessIndicatorState(plan({ phase_summary: phaseSummary() }), {
			currentSlug: 'demo',
			connected: true,
			streamEverConnected: true,
			lastSuccessfulUpdateAt: '2026-06-16T12:11:00Z'
		});

		expect(state.shouldShow).toBe(false);
	});
});
