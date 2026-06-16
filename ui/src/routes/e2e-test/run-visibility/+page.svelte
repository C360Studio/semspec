<script lang="ts">
	import RunVisibilityPanel from '$lib/components/plan/RunVisibilityPanel.svelte';
	import type { PlanWithStatus } from '$lib/types/plan';
	import type { ExecutionTask, Lesson } from '$lib/types/runVisibility';
	import type { TrajectoryListItem } from '$lib/types/trajectory';

	const plan = {
		id: 'plan-1',
		slug: '57daa4134abb',
		title: 'Raw MAVLink Fallback Integration',
		project_id: 'project-1',
		status: 'rejected',
		stage: 'rejected',
		approved: true,
		active_loops: [],
		created_at: '2026-06-16T13:54:32Z',
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
			duration_ms: 42156,
			completed_at: '2026-06-16T15:48:45Z',
			skipped_tests: [
				{ name: 'scenario.57daa4134abb.1.1.3: start connects MAVSDK to SITL' },
				{ name: 'scenario.57daa4134abb.3.1.5: SITL takeoff command completes' }
			]
		},
		qa_verdict_summary: {
			verdict: 'needs_changes',
			level: 'integration',
			summary:
				'Executed tests passed, but live SITL tests were skipped. [skip-guard] coerced approved->needs_changes until each skip is classified.',
			recorded_at: '2026-06-16T15:49:54Z'
		}
	} as PlanWithStatus;

	const executionTasks: ExecutionTask[] = [
		{
			slug: '57daa4134abb',
			task_id: 'node-original',
			requirement_id: 'requirement.57daa4134abb.2',
			stage: 'escalated',
			verdict: 'rejected',
			title: 'Verify coverage matrix generation and SITL outputs',
			updated_at: '2026-06-16T15:00:00Z'
		},
		{
			slug: '57daa4134abb',
			task_id: 'node-replacement',
			requirement_id: 'requirement.57daa4134abb.2',
			stage: 'approved',
			verdict: 'approved',
			title: 'Verify coverage matrix generation and SITL outputs',
			merge_commit: '28e9a4df444b8eaa64b23b500f962be868ff0572',
			updated_at: '2026-06-16T15:09:18Z'
		},
		{
			slug: '57daa4134abb',
			task_id: 'node-raw',
			requirement_id: 'requirement.57daa4134abb.4',
			stage: 'approved',
			verdict: 'approved',
			title: 'Verify loading of custom XML dialects without hand-rolled framing',
			merge_commit: '8948d8009182925dd277ec2c9142e20a77f50774',
			updated_at: '2026-06-16T15:47:31Z'
		}
	];

	const trajectoryItems = [
		{
			loop_id: 'loop-1',
			task_id: 'node-replacement',
			role: 'developer',
			model: 'gemini-pro',
			workflow_slug: 'semspec-task-execution',
			workflow_step: 'develop',
			iterations: 12,
			total_tokens_in: 110300,
			total_tokens_out: 4600,
			duration: 68000,
			start_time: '2026-06-16T15:00:00Z',
			outcome: 'success'
		},
		{
			loop_id: 'loop-2',
			task_id: 'node-raw',
			role: 'developer',
			model: 'gemini-pro',
			workflow_slug: 'semspec-task-execution',
			workflow_step: 'develop',
			iterations: 4,
			total_tokens_in: 16000,
			total_tokens_out: 900,
			duration: 20400,
			start_time: '2026-06-16T15:40:00Z',
			outcome: 'success'
		}
	] as TrajectoryListItem[];

	const lessons: Lesson[] = [
		{
			ID: 'lesson-1',
			Source: 'decomposer',
			ScenarioID: 'node-replacement',
			Summary: 'Do not create empty dummy classes to bypass coverage checks.',
			CategoryIDs: [],
			Role: 'developer',
			CreatedAt: '2026-06-16T14:55:04Z',
			Detail: '',
			InjectionForm: 'Document unsupported features with rationale.',
			EvidenceSteps: [],
			EvidenceFiles: [],
			RootCauseRole: 'developer',
			Positive: false,
			RetiredAt: null,
			LastInjectedAt: '2026-06-16T15:42:15Z'
		},
		{
			ID: 'lesson-2',
			Source: 'decomposer',
			ScenarioID: 'node-raw',
			Summary: 'Use test display names to map test methods to acceptance scenario IDs.',
			CategoryIDs: [],
			Role: 'developer',
			CreatedAt: '2026-06-16T15:47:46Z',
			Detail: '',
			InjectionForm: 'Use JUnit display names for scenario traceability.',
			EvidenceSteps: [],
			EvidenceFiles: [],
			RootCauseRole: 'developer',
			Positive: true,
			RetiredAt: null,
			LastInjectedAt: null
		}
	];
</script>

<main class="harness" data-testid="run-visibility-harness">
	<RunVisibilityPanel {plan} {executionTasks} {trajectoryItems} {lessons} />
</main>

<style>
	.harness {
		max-width: 940px;
		padding: var(--space-4);
	}
</style>
