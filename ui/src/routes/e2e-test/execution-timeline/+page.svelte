<script lang="ts">
	/**
	 * Playwright harness for ExecutionTimeline. Renders the component with
	 * hard-coded trajectoryItems so the spec can pin:
	 *   - both sections (Planning + Execution) render even when empty (ghost)
	 *   - architecture-generation lands in Planning, not Execution
	 *   - non-empty sections flip to interactive (chevron + click toggle)
	 *
	 * Scenarios:
	 *   empty                 — no loops; both sections render as ghost
	 *   planning-loops        — Planning has 2 loops; Execution still ghost
	 *   architecture-in-plan  — architecture-generation loop must land in Planning
	 *   execution-loops       — Execution has 1 loop; both sections interactive
	 */
	import ExecutionTimeline from '$lib/components/trajectory/ExecutionTimeline.svelte';
	import type { TrajectoryListItem } from '$lib/types/trajectory';
	import type { PageData } from './$types';

	interface Props {
		data: PageData;
	}

	let { data }: Props = $props();

	const baseItem = (overrides: Partial<TrajectoryListItem>): TrajectoryListItem =>
		({
			loop_id: overrides.loop_id ?? crypto.randomUUID(),
			task_id: overrides.task_id ?? 'task-fixture',
			role: 'general',
			model: 'gemini-flash',
			workflow_slug: 'fixture',
			workflow_step: overrides.workflow_step ?? 'drafting',
			iterations: 1,
			total_tokens_in: 1000,
			total_tokens_out: 100,
			duration: 5000,
			start_time: '2026-05-21T12:00:00Z',
			outcome: 'success',
			...overrides
		}) as TrajectoryListItem;

	const planningLoops: TrajectoryListItem[] = [
		baseItem({ workflow_step: 'drafting' }),
		baseItem({ workflow_step: 'reviewing', role: 'reviewer' })
	];

	const planningWithArchitecture: TrajectoryListItem[] = [
		baseItem({ workflow_step: 'drafting' }),
		baseItem({ workflow_step: 'requirement-generation' }),
		// The smoking gun for the 2026-05-21 fix — without architecture-generation
		// in PLAN_STEPS, this loop landed in the Execution section.
		baseItem({ workflow_step: 'architecture-generation' }),
		baseItem({ workflow_step: 'scenario-generation' })
	];

	const executionLoops: TrajectoryListItem[] = [
		...planningLoops,
		baseItem({ workflow_step: 'execute', role: 'coder' })
	];

	const trajectoryItems = $derived(
		data.scenario === 'planning-loops'
			? planningLoops
			: data.scenario === 'architecture-in-plan'
				? planningWithArchitecture
				: data.scenario === 'execution-loops'
					? executionLoops
					: []
	);
</script>

<div class="harness" data-testid="execution-timeline-harness">
	<ExecutionTimeline slug="fixture-slug" stage="drafting" {trajectoryItems} />
</div>

<style>
	.harness {
		padding: var(--space-4);
		max-width: 900px;
	}
</style>
