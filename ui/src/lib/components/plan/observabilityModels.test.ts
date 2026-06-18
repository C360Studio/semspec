import { describe, expect, it } from 'vitest';
import { formatCostLabel, type ProviderRate } from '$lib/types/costAccounting';
import type { PlanPhaseSummary } from '$lib/types/feed';
import type { PlanWithStatus } from '$lib/types/plan';
import type { TrajectoryListItem } from '$lib/types/trajectory';
import {
	executionAttemptModel,
	executionBlockers,
	inferRecoveryAutoAccept,
	isLessonTrajectoryItem,
	lessonActivityModel,
	mergeExecutionTaskSSE,
	persistedLessonSummaries,
	phaseSummaryDetail,
	planFreshnessIndicatorState,
	qaOutcomeState,
	recoveryAffectedNodes,
	shouldShowPhaseSummaryBanner,
	storyTaskCounts,
	type ExecutionTask,
	type Lesson
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

function executionTask(overrides: Partial<ExecutionTask> = {}): ExecutionTask {
	return {
		slug: 'demo',
		task_id: 'node-1',
		requirement_id: 'requirement.demo.1',
		stage: 'approved',
		title: 'Implement raw fallback',
		updated_at: '2026-06-16T12:15:00Z',
		...overrides
	};
}

function lesson(overrides: Partial<Lesson> = {}): Lesson {
	return {
		ID: 'lesson-1',
		Source: 'decomposer',
		ScenarioID: 'node-1',
		Summary: 'Avoid bypassing coverage checks with placeholder code.',
		CategoryIDs: [],
		Role: 'developer',
		CreatedAt: '2026-06-16T12:16:00Z',
		Detail: 'The developer created empty classes instead of satisfying scenarios.',
		InjectionForm: 'Document unsupported features with rationale.',
		EvidenceSteps: [],
		EvidenceFiles: [],
		RootCauseRole: 'developer',
		Positive: false,
		RetiredAt: null,
		LastInjectedAt: null,
		...overrides
	};
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

	it('treats failed QA run as error even when verdict text is approved', () => {
		expect(qaOutcomeState(plan({
			stage: 'complete',
			qa_run: {
				run_id: 'qa-1',
				completed_at: '2026-06-16T12:12:00Z',
				duration_ms: 1200,
				passed: false
			},
			qa_verdict_summary: {
				level: 'integration',
				recorded_at: '2026-06-16T12:13:00Z',
				verdict: 'approved'
			}
		}))).toBe('error');
	});

	it('shows complete-with-deferrals as warning instead of success', () => {
		expect(qaOutcomeState(plan({
			stage: 'complete_with_deferrals',
			qa_run: {
				run_id: 'qa-1',
				completed_at: '2026-06-16T12:12:00Z',
				duration_ms: 1200,
				passed: true
			},
			qa_verdict_summary: {
				level: 'integration',
				recorded_at: '2026-06-16T12:13:00Z',
				verdict: 'conditionally_approved'
			}
		}))).toBe('warning');
	});

	it('shows approved passing QA as success', () => {
		expect(qaOutcomeState(plan({
			stage: 'complete',
			qa_run: {
				run_id: 'qa-1',
				completed_at: '2026-06-16T12:12:00Z',
				duration_ms: 1200,
				passed: true
			},
			qa_verdict_summary: {
				level: 'integration',
				recorded_at: '2026-06-16T12:13:00Z',
				verdict: 'approved'
			}
		}))).toBe('success');
	});

	it('merges live task SSE over durable execution task rows', () => {
		const taskStages = new Map([
			[
				'node-1',
				{
					entity_id: 'entity-1',
					slug: 'demo',
					task_id: 'node-1',
					stage: 'reviewing',
					title: 'Implement raw fallback',
					iteration: 1,
					max_iterations: 5,
					tdd_cycle: 2,
					max_tdd_cycles: 5,
					updated_at: '2026-06-16T12:17:00Z'
				}
			],
			[
				'node-new',
				{
					entity_id: 'entity-new',
					slug: 'demo',
					task_id: 'node-new',
					stage: 'developing',
					title: 'New live task',
					iteration: 0,
					max_iterations: 5,
					updated_at: '2026-06-16T12:18:00Z'
				}
			]
		]);

		const merged = mergeExecutionTaskSSE([executionTask()], taskStages, 'demo');

		expect(merged).toHaveLength(2);
		expect(merged.find((item) => item.task_id === 'node-1')?.stage).toBe('reviewing');
		expect(merged.find((item) => item.task_id === 'node-1')?.tdd_cycle).toBe(2);
		expect(merged.find((item) => item.task_id === 'node-new')?.title).toBe('New live task');
	});

	it('groups replacement attempts and flags stale terminal rows as recovered', () => {
		const model = executionAttemptModel([
			executionTask({
				task_id: 'node-original',
				stage: 'escalated',
				verdict: 'rejected',
				updated_at: '2026-06-16T12:10:00Z'
			}),
			executionTask({
				task_id: 'node-replacement',
				stage: 'approved',
				verdict: 'approved',
				merge_commit: '28e9a4df444b8eaa64b23b500f962be868ff0572',
				updated_at: '2026-06-16T12:20:00Z'
			})
		]);

		expect(model.taskGroups).toHaveLength(1);
		expect(model.taskGroups[0].status).toBe('recovered');
		expect(model.taskGroups[0].attempts.map((attempt) => attempt.taskId)).toEqual([
			'node-original',
			'node-replacement'
		]);
		expect(model.orphanedGroups).toBe(1);
		expect(model.warnings[0].kind).toBe('orphaned-attempt');
	});

	it('matches persisted developer lessons to execution tasks and future-run state', () => {
		const summaries = persistedLessonSummaries(
			plan({ slug: 'demo' }),
			[executionTask({ task_id: 'node-1', title: 'Implement raw fallback' })],
			[trajectory({ loop_id: 'loop-1', task_id: 'node-1' })],
			[
				lesson({ ID: 'matched', ScenarioID: 'node-1', LastInjectedAt: null }),
				lesson({ ID: 'unrelated', ScenarioID: 'node-other', Summary: 'Unrelated' })
			]
		);

		expect(summaries.map((item) => item.id)).toEqual(['matched']);
		expect(summaries[0].futureRunOnly).toBe(true);
		expect(summaries[0].relatedTaskTitle).toBe('Implement raw fallback');
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

	it('carries persisted lesson role and detail for compact UI cards', () => {
		const summaries = persistedLessonSummaries(
			plan(),
			[executionTask({ task_id: 'node-1', title: 'Fix coverage' })],
			[],
			[lesson({
				ScenarioID: 'node-1',
				Summary: 'Short summary',
				Detail: 'Long evidence transcript',
				Role: 'developer'
			})]
		);

		expect(summaries).toHaveLength(1);
		expect(summaries[0]).toMatchObject({
			summary: 'Short summary',
			detail: 'Long evidence transcript',
			role: 'developer',
			relatedTaskTitle: 'Fix coverage'
		});
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

	it('shows question stream failures even when plan feed is still connected', () => {
		const state = planFreshnessIndicatorState(plan({ phase_summary: phaseSummary() }), {
			currentSlug: 'demo',
			connected: true,
			streamEverConnected: true,
			lastSuccessfulUpdateAt: '2026-06-16T12:11:00Z',
			questionsConnected: false,
			questionsEverConnected: true,
			questionsLastSuccessfulUpdateAt: '2026-06-16T12:10:00Z',
			questionsError: 'Questions stream connection error',
			questionsLastErrorAt: '2026-06-16T12:12:00Z'
		});

		expect(state.shouldShow).toBe(true);
		expect(state.disconnected).toBe(true);
		expect(state.statusLabel).toBe('Question stream disconnected');
		expect(state.reason).toBe('Questions stream connection error');
		expect(state.source).toBe('question-manager');
	});
});
