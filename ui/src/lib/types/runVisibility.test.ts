import { describe, expect, it } from 'vitest';
import {
	mergeTaskSse,
	summarizeRunVisibility,
	type ExecutionTask,
	type Lesson
} from './runVisibility';
import type { PlanWithStatus } from './plan';
import type { TrajectoryListItem } from './trajectory';

function plan(overrides: Partial<PlanWithStatus> = {}): PlanWithStatus {
	return {
		id: 'plan-1',
		slug: '57daa4134abb',
		project_id: 'project-1',
		status: 'rejected',
		stage: 'rejected',
		approved: true,
		active_loops: [],
		created_at: '2026-06-16T13:54:32Z',
		...overrides
	} as PlanWithStatus;
}

function task(overrides: Partial<ExecutionTask> = {}): ExecutionTask {
	return {
		slug: '57daa4134abb',
		task_id: 'node-1',
		requirement_id: 'requirement.57daa4134abb.2',
		stage: 'approved',
		title: 'Verify coverage matrix generation and SITL outputs',
		updated_at: '2026-06-16T15:10:00Z',
		...overrides
	};
}

function lesson(overrides: Partial<Lesson> = {}): Lesson {
	return {
		ID: 'lesson-1',
		Source: 'decomposer',
		ScenarioID: 'node-1',
		Summary: 'Do not create empty dummy classes to bypass coverage checks.',
		CategoryIDs: [],
		Role: 'developer',
		CreatedAt: '2026-06-16T14:55:04Z',
		Detail: 'The developer created empty classes instead of documenting unsupported features.',
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

function trajectory(overrides: Partial<TrajectoryListItem> = {}): TrajectoryListItem {
	return {
		loop_id: 'loop-1',
		start_time: '2026-06-16T15:00:00Z',
		duration: 30_000,
		iterations: 3,
		model: 'gemini-pro',
		role: 'developer',
		task_id: 'node-1',
		total_tokens_in: 10_000,
		total_tokens_out: 500,
		workflow_slug: 'semspec-task-execution',
		workflow_step: 'develop',
		...overrides
	} as TrajectoryListItem;
}

describe('summarizeRunVisibility', () => {
	it('surfaces human escalation ahead of generic rejected state', () => {
		const summary = summarizeRunVisibility(
			plan({
				plan_decisions: [
					{
						id: 'plan-decision.57daa4134abb.recovery.c781429a',
						plan_id: 'plan-1',
						kind: 'execution_exhausted',
						title: 'Recovery: escalate_human',
						rationale:
							'Recommended action: escalate_human\nOriginal wedge: QA verdict needs_changes at level integration',
						status: 'proposed',
						proposed_by: 'recovery-agent',
						affected_requirement_ids: ['requirement.57daa4134abb.1'],
						created_at: '2026-06-16T15:50:01Z'
					}
				],
				qa_level: 'integration',
				qa_run: {
					run_id: 'qa-1',
					passed: true,
					duration_ms: 42_156,
					completed_at: '2026-06-16T15:48:45Z',
					skipped_tests: [{ name: 'scenario.57daa4134abb.1.1.3: SITL smoke' }]
				} as NonNullable<PlanWithStatus['qa_run']>,
				qa_verdict_summary: {
					verdict: 'needs_changes',
					level: 'integration',
					summary:
						'Executed tests passed.\n\n[skip-guard] coerced approved->needs_changes: skipped tests were not classified.',
					recorded_at: '2026-06-16T15:49:54Z'
				}
			}),
			[],
			[],
			[]
		);

		expect(summary.shouldRender).toBe(true);
		expect(summary.status.title).toBe('Human review needed');
		expect(summary.warnings.map((warning) => warning.kind)).toContain('human-review');
		expect(summary.warnings.map((warning) => warning.kind)).toContain('qa-skip-guard');
		expect(summary.qa?.skippedTests).toHaveLength(1);
	});

	it('groups replacement attempts and marks stale rejected rows as recovered', () => {
		const summary = summarizeRunVisibility(
			plan({ stage: 'implementing', status: 'implementing' }),
			[
				task({
					task_id: 'node-original',
					stage: 'escalated',
					verdict: 'rejected',
					updated_at: '2026-06-16T15:00:00Z'
				}),
				task({
					task_id: 'node-replacement',
					stage: 'approved',
					verdict: 'approved',
					merge_commit: '28e9a4df444b8eaa64b23b500f962be868ff0572',
					updated_at: '2026-06-16T15:09:18Z'
				})
			],
			[],
			[]
		);

		expect(summary.taskGroups).toHaveLength(1);
		expect(summary.taskGroups[0].status).toBe('recovered');
		expect(summary.taskGroups[0].attempts.map((attempt) => attempt.taskId)).toEqual([
			'node-original',
			'node-replacement'
		]);
		expect(summary.taskStats.orphanedGroups).toBe(1);
		expect(summary.warnings.some((warning) => warning.kind === 'orphaned-attempt')).toBe(true);
	});

	it('matches lessons to current execution tasks and marks future-run-only lessons', () => {
		const summary = summarizeRunVisibility(
			plan({ stage: 'implementing', status: 'implementing' }),
			[task({ task_id: 'node-1', title: 'Implement raw fallback' })],
			[trajectory({ loop_id: 'loop-1', task_id: 'node-1' })],
			[
				lesson({ ID: 'matched', ScenarioID: 'node-1', LastInjectedAt: null }),
				lesson({
					ID: 'other-plan',
					ScenarioID: 'node-other',
					Summary: 'Unrelated lesson',
					LastInjectedAt: '2026-06-16T15:44:41Z'
				})
			]
		);

		expect(summary.lessons.map((item) => item.id)).toEqual(['matched']);
		expect(summary.lessons[0].futureRunOnly).toBe(true);
		expect(summary.lessons[0].relatedTaskTitle).toBe('Implement raw fallback');
	});

	it('reports token usage without inventing provider cost', () => {
		const summary = summarizeRunVisibility(
			plan({ stage: 'implementing', status: 'implementing' }),
			[task()],
			[
				trajectory({ total_tokens_in: 10_000, total_tokens_out: 500, duration: 30_000 }),
				trajectory({
					loop_id: 'loop-2',
					total_tokens_in: 20_000,
					total_tokens_out: 1000,
					duration: 60_000
				})
			],
			[]
		);

		expect(summary.usage.totalTokens).toBe(31_500);
		expect(summary.usage.durationMs).toBe(90_000);
		expect(summary.usage.pricingAvailable).toBe(false);
		expect(summary.usage.costLabel).toBe('Pricing not configured');
	});
});

describe('mergeTaskSse', () => {
	it('overlays live task payloads and appends newly observed task rows', () => {
		const taskStages = new Map([
			[
				'node-1',
				{
					entity_id: 'entity-1',
					slug: '57daa4134abb',
					task_id: 'node-1',
					stage: 'reviewing',
					title: 'Verify coverage matrix generation and SITL outputs',
					iteration: 1,
					max_iterations: 5,
					tdd_cycle: 2,
					max_tdd_cycles: 5,
					updated_at: '2026-06-16T15:11:00Z'
				}
			],
			[
				'node-new',
				{
					entity_id: 'entity-new',
					slug: '57daa4134abb',
					task_id: 'node-new',
					stage: 'developing',
					title: 'New live task',
					iteration: 0,
					max_iterations: 5,
					updated_at: '2026-06-16T15:12:00Z'
				}
			]
		]);

		const merged = mergeTaskSse([task({ task_id: 'node-1', stage: 'approved' })], taskStages, '57daa4134abb');

		expect(merged).toHaveLength(2);
		expect(merged.find((item) => item.task_id === 'node-1')?.stage).toBe('reviewing');
		expect(merged.find((item) => item.task_id === 'node-1')?.tdd_cycle).toBe(2);
		expect(merged.find((item) => item.task_id === 'node-new')?.title).toBe('New live task');
	});
});
