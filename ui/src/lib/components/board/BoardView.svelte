<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import AttentionBanner from './AttentionBanner.svelte';
	import PlanCard from './PlanCard.svelte';
	import KanbanView from '$lib/components/kanban/KanbanView.svelte';
	import KanbanDetailPanel from '$lib/components/kanban/KanbanDetailPanel.svelte';
	import ThreePanelLayout from '$lib/components/layout/ThreePanelLayout.svelte';
	import { chatDrawerStore } from '$lib/stores/chatDrawer.svelte';
	import { kanbanStore } from '$lib/stores/kanban.svelte';
	import type { PlanWithStatus } from '$lib/types/plan';
	import type { Task } from '$lib/types/task';
	import type { Loop } from '$lib/types';
	import { taskToKanbanStatus, type KanbanCardItem, type KanbanStatus } from '$lib/types/kanban';

	interface Props {
		plans: PlanWithStatus[];
		loops: Loop[];
		tasksByPlan: Record<string, Task[]>;
		activeLoopCount: number;
		systemHealthy: boolean;
	}

	let { plans, loops, tasksByPlan, activeLoopCount, systemHealthy }: Props = $props();

	const isKanban = $derived(kanbanStore.viewMode === 'kanban');
	const hasSelection = $derived(kanbanStore.selectedCardId !== null);

	// Find the selected card item from the KanbanView's allItems
	const selectedItem = $derived.by((): KanbanCardItem | null => {
		if (!kanbanStore.selectedCardId) return null;
		const id = kanbanStore.selectedCardId;
		for (const plan of plans) {
			const tasks = tasksByPlan[plan.slug] ?? [];
			for (const task of tasks) {
				if (task.id === id) {
					const loop = (plan.active_loops ?? []).find((l) => l.current_task_id === id);
					return {
						id: task.id,
						type: 'task',
						title: task.description,
						kanbanStatus: taskToKanbanStatus(task.status),
						originalStatus: task.status,
						planSlug: plan.slug,
						requirementId: undefined,
						requirementTitle: kanbanStore.getRequirementTitle(task.phase_id ?? ''),
						taskType: task.type,
						rejection: task.rejection,
						iteration: task.iteration,
						maxIterations: task.max_iterations,
						agentRole: loop?.role,
						agentModel: loop?.model,
						agentState: loop?.state,
						scenarioIds: task.scenario_ids
					};
				}
			}
		}
		return null;
	});

	function handleNewPlan() {
		chatDrawerStore.open({ type: 'global' });
	}
</script>

<div class="board-view">
	<AttentionBanner {plans} {loops} {tasksByPlan} />

	<div class="board-header">
		<h1>{isKanban ? 'Task Board' : 'Active Plans'}</h1>
		<div class="header-actions">
			<div class="view-toggle" role="radiogroup" aria-label="Board view mode">
				<button
					class="toggle-btn"
					class:active={!isKanban}
					aria-checked={!isKanban}
					role="radio"
					title="Grid view"
					onclick={() => kanbanStore.setViewMode('grid')}
				>
					<Icon name="layout-grid" size={16} />
				</button>
				<button
					class="toggle-btn"
					class:active={isKanban}
					aria-checked={isKanban}
					role="radio"
					title="Kanban view"
					onclick={() => kanbanStore.setViewMode('kanban')}
				>
					<Icon name="columns" size={16} />
				</button>
			</div>
			<button class="new-plan-btn" onclick={handleNewPlan}>
				<Icon name="plus" size={16} />
				<span>New Plan</span>
			</button>
		</div>
	</div>

	{#if plans.length === 0}
		<div class="empty-state">
			<Icon name="inbox" size={48} />
			<h2>No active plans</h2>
			<p>Click "New Plan" above to describe what you'd like to build.</p>
			<button class="start-btn" onclick={handleNewPlan}>Create Your First Plan</button>
		</div>
	{:else if isKanban}
		<div class="kanban-layout">
			<ThreePanelLayout
				id="kanban-board"
				leftOpen={false}
				rightOpen={true}
				leftWidth={0}
				rightWidth={320}
			>
				{#snippet leftPanel()}
					<!-- Reserved for future swimlane/grouping controls -->
				{/snippet}
				{#snippet centerPanel()}
					<KanbanView {plans} {tasksByPlan} />
				{/snippet}
				{#snippet rightPanel()}
					<KanbanDetailPanel item={selectedItem} />
				{/snippet}
			</ThreePanelLayout>
		</div>
	{:else}
		<div class="plans-grid">
			{#each plans as plan (plan.slug)}
				<PlanCard {plan} tasks={tasksByPlan[plan.slug] ?? []} />
			{/each}
		</div>
	{/if}

	<footer class="board-footer">
		<div class="status-item">
			<div class="status-dot" class:healthy={systemHealthy}></div>
			<span>{systemHealthy ? 'Connected' : 'Disconnected'}</span>
		</div>
		<div class="status-item">
			<Icon name="activity" size={14} />
			<span>{activeLoopCount} active loop{activeLoopCount !== 1 ? 's' : ''}</span>
		</div>
	</footer>
</div>

<style>
	.board-view {
		height: 100%;
		display: flex;
		flex-direction: column;
		padding: var(--space-6);
	}

	.board-view:not(:has(.kanban-view)) {
		max-width: 1200px;
		margin: 0 auto;
	}

	/* Fallback for browsers without :has() */
	@supports not selector(:has(*)) {
		.board-view {
			max-width: 1200px;
			margin: 0 auto;
		}
	}

	.board-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: var(--space-4);
	}

	.board-header h1 {
		font-size: var(--font-size-xl);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
	}

	.header-actions {
		display: flex;
		align-items: center;
		gap: var(--space-3);
	}

	.view-toggle {
		display: flex;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-md);
		border: 1px solid var(--color-border);
		overflow: hidden;
	}

	.toggle-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		padding: var(--space-1) var(--space-2);
		border: none;
		background: none;
		color: var(--color-text-muted);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.toggle-btn:hover {
		color: var(--color-text-primary);
		background: var(--color-bg-elevated);
	}

	.toggle-btn.active {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.new-plan-btn {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-accent);
		color: var(--color-bg-primary);
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: opacity var(--transition-fast);
	}

	.new-plan-btn:hover {
		opacity: 0.9;
	}

	.kanban-layout {
		flex: 1;
		min-height: 0;
	}

	.plans-grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(350px, 1fr));
		gap: var(--space-4);
		flex: 1;
		overflow-y: auto;
	}

	.empty-state {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-3);
		color: var(--color-text-muted);
		text-align: center;
	}

	.empty-state h2 {
		margin: 0;
		font-size: var(--font-size-lg);
		color: var(--color-text-primary);
	}

	.empty-state p {
		margin: 0;
		max-width: 320px;
	}

	.start-btn {
		padding: var(--space-2) var(--space-4);
		background: var(--color-accent);
		color: var(--color-bg-primary);
		border: none;
		border-radius: var(--radius-md);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: opacity var(--transition-fast);
	}

	.start-btn:hover {
		opacity: 0.9;
	}

	.board-footer {
		display: flex;
		gap: var(--space-4);
		padding-top: var(--space-4);
		border-top: 1px solid var(--color-border);
		margin-top: var(--space-4);
	}

	.status-item {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.status-dot {
		width: 8px;
		height: 8px;
		border-radius: var(--radius-full);
		background: var(--color-error);
	}

	.status-dot.healthy {
		background: var(--color-success);
	}

	:global(.spin) {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		from {
			transform: rotate(0deg);
		}
		to {
			transform: rotate(360deg);
		}
	}
</style>
