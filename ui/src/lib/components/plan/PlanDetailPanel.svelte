<script lang="ts">
	/**
	 * PlanDetailPanel - Switches between detail views based on selection.
	 *
	 * Renders the appropriate detail component (PlanDetail, PhaseDetail, TaskDetail)
	 * based on the current selection state.
	 */

	import PlanDetail from './PlanDetail.svelte';
	import PhaseDetail from './PhaseDetail.svelte';
	import TaskDetail from './TaskDetail.svelte';
	import type { PlanWithStatus } from '$lib/types/plan';
	import type { Phase } from '$lib/types/phase';
	import type { Task } from '$lib/types/task';
	import type { PlanSelection } from '$lib/stores/planSelection.svelte';

	interface Props {
		selection: PlanSelection | null;
		plan: PlanWithStatus;
		phases: Phase[];
		tasksByPhase: Record<string, Task[]>;
		onRefreshPlan?: () => Promise<void>;
		onRefreshPhases?: () => Promise<void>;
		onRefreshTasks?: () => Promise<void>;
		onApprovePhase?: (phaseId: string) => Promise<void>;
		onRejectPhase?: (phaseId: string, reason: string) => Promise<void>;
		onApproveTask?: (taskId: string) => Promise<void>;
		onRejectTask?: (taskId: string, reason: string) => Promise<void>;
	}

	let {
		selection,
		plan,
		phases,
		tasksByPhase,
		onRefreshPlan,
		onRefreshPhases,
		onRefreshTasks,
		onApprovePhase,
		onRejectPhase,
		onApproveTask,
		onRejectTask
	}: Props = $props();

	// Find selected phase
	const selectedPhase = $derived.by(() => {
		if (!selection?.phaseId) return undefined;
		return phases.find((p) => p.id === selection.phaseId);
	});

	// Find selected task
	const selectedTask = $derived.by(() => {
		if (!selection?.taskId || !selection?.phaseId) return undefined;
		const phaseTasks = tasksByPhase[selection.phaseId] ?? [];
		return phaseTasks.find((t) => t.id === selection.taskId);
	});
</script>

<div class="detail-panel-container">
	{#if !selection || selection.type === 'plan'}
		<PlanDetail
			{plan}
			{phases}
			onRefresh={onRefreshPlan}
			onGeneratePhases={onRefreshPhases}
		/>
	{:else if selection.type === 'phase' && selectedPhase}
		<PhaseDetail
			phase={selectedPhase}
			{plan}
			tasks={tasksByPhase[selectedPhase.id] ?? []}
			allPhases={phases}
			onRefresh={onRefreshPhases}
			onApprove={onApprovePhase}
			onReject={onRejectPhase}
			onRefreshTasks={onRefreshTasks}
		/>
	{:else if selection.type === 'task' && selectedTask && selectedPhase}
		<TaskDetail
			task={selectedTask}
			phase={selectedPhase}
			{plan}
			onRefresh={onRefreshTasks}
			onApprove={onApproveTask}
			onReject={onRejectTask}
		/>
	{:else}
		<!-- Fallback: show plan detail -->
		<PlanDetail
			{plan}
			{phases}
			onRefresh={onRefreshPlan}
			onGeneratePhases={onRefreshPhases}
		/>
	{/if}
</div>

<style>
	.detail-panel-container {
		display: flex;
		flex-direction: column;
		height: 100%;
		overflow: auto;
	}
</style>
