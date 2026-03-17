<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import KanbanColumn from './KanbanColumn.svelte';
	import StatusFilterChips from './StatusFilterChips.svelte';
	import PlanFilterDropdown from './PlanFilterDropdown.svelte';
	import { invalidate } from '$app/navigation';
	import { activityStore } from '$lib/stores/activity.svelte';
	import { kanbanStore } from '$lib/stores/kanban.svelte';
	import {
		KANBAN_COLUMNS,
		taskToKanbanStatus,
		type KanbanCardItem,
		type KanbanStatus
	} from '$lib/types/kanban';
	import type { PlanWithStatus } from '$lib/types/plan';
	import type { Task } from '$lib/types/task';
	import { onMount } from 'svelte';

	interface Props {
		plans: PlanWithStatus[];
		tasksByPlan: Record<string, Task[]>;
	}

	let { plans, tasksByPlan }: Props = $props();

	let refreshTimer: ReturnType<typeof setTimeout> | null = null;

	onMount(() => {
		// Fetch kanban-specific data (requirements, scenarios) for enrichment
		initialLoad();

		// Subscribe to SSE activity events for real-time updates.
		const unsubscribe = activityStore.onEvent((event) => {
			if (
				event.type === 'task_status_changed' ||
				event.type === 'plan_updated'
			) {
				scheduleRefresh();
			}
		});

		return () => {
			unsubscribe();
			if (refreshTimer) clearTimeout(refreshTimer);
		};
	});

	function scheduleRefresh() {
		if (refreshTimer) return;
		refreshTimer = setTimeout(() => {
			refreshTimer = null;
			invalidate('app:board');
		}, 2000);
	}

	async function initialLoad() {
		const promises: Promise<void | unknown[]>[] = [];
		for (const plan of plans) {
			promises.push(kanbanStore.fetchRequirements(plan.slug));
			promises.push(kanbanStore.fetchScenarios(plan.slug));
		}
		await Promise.allSettled(promises);
	}

	// Build kanban card items from all plan tasks, enriched with requirement/scenario context
	const allItems = $derived.by(() => {
		const items: KanbanCardItem[] = [];
		const filteredPlans = kanbanStore.selectedPlanSlug
			? plans.filter((p) => p.slug === kanbanStore.selectedPlanSlug)
			: plans;

		for (const plan of filteredPlans) {
			const tasks = tasksByPlan[plan.slug] ?? [];
			const rawReqs = kanbanStore.requirementsByPlan[plan.slug];
			const requirements = Array.isArray(rawReqs) ? rawReqs : [];
			const rawScenarios = kanbanStore.scenariosByPlan[plan.slug];
			const scenarios = Array.isArray(rawScenarios) ? rawScenarios : [];

			// Build lookups
			const loopsByTask = new Map<string, (typeof plan.active_loops)[number]>();
			for (const loop of plan.active_loops ?? []) {
				if (loop.current_task_id) {
					loopsByTask.set(loop.current_task_id, loop);
				}
			}

			const reqById = new Map(requirements.map((r) => [r.id, r]));

			// Build a lookup: scenario_id → requirement
			const reqByScenarioId = new Map<string, (typeof requirements)[number]>();
			for (const s of scenarios) {
				const req = reqById.get(s.requirement_id);
				if (req) reqByScenarioId.set(s.id, req);
			}

			for (const task of tasks) {
				const loop = loopsByTask.get(task.id);

				// Resolve requirement: try scenario_ids first, then fall back to phase_id
				let requirementId: string | undefined;
				let requirementTitle: string | undefined;

				if (task.scenario_ids?.length) {
					const req = reqByScenarioId.get(task.scenario_ids[0]);
					if (req) {
						requirementId = req.id;
						requirementTitle = req.title;
					}
				}
				if (!requirementId && task.phase_id) {
					const req = reqById.get(task.phase_id);
					if (req) {
						requirementId = req.id;
						requirementTitle = req.title;
					}
				}

				items.push({
					id: task.id,
					type: 'task',
					title: task.description,
					kanbanStatus: taskToKanbanStatus(task.status),
					originalStatus: task.status,
					planSlug: plan.slug,
					requirementId,
					requirementTitle,
					taskType: task.type,
					rejection: task.rejection,
					iteration: task.iteration,
					maxIterations: task.max_iterations,
					agentRole: loop?.role,
					agentModel: loop?.model,
					agentState: loop?.state,
					scenarioIds: task.scenario_ids
				});
			}
		}

		return items;
	});

	// Group items by kanban status
	const itemsByStatus = $derived.by(() => {
		const groups: Record<KanbanStatus, KanbanCardItem[]> = {
			backlog: [],
			in_progress: [],
			in_review: [],
			completed: [],
			needs_attention: []
		};
		for (const item of allItems) {
			groups[item.kanbanStatus].push(item);
		}
		return groups;
	});

	// Counts for filter chips
	const statusCounts = $derived.by(() => {
		const counts: Record<KanbanStatus, number> = {
			backlog: 0,
			in_progress: 0,
			in_review: 0,
			completed: 0,
			needs_attention: 0
		};
		for (const item of allItems) {
			counts[item.kanbanStatus]++;
		}
		return counts;
	});

	// Active columns filtered by visibility
	const activeColumns = $derived(
		KANBAN_COLUMNS.filter((col) => kanbanStore.activeStatuses.has(col.status))
	);

	function handleSelectCard(id: string) {
		kanbanStore.selectCard(kanbanStore.selectedCardId === id ? null : id);
	}

</script>

<div class="kanban-view">
	<div class="kanban-toolbar">
		<StatusFilterChips
			activeStatuses={kanbanStore.activeStatuses}
			counts={statusCounts}
			onToggle={(s) => kanbanStore.toggleStatus(s)}
		/>
		<PlanFilterDropdown
			{plans}
			selectedSlug={kanbanStore.selectedPlanSlug}
			onSelect={(s) => kanbanStore.filterByPlan(s)}
		/>
	</div>

	{#if plans.length === 0 && Object.keys(tasksByPlan).length === 0}
		<div class="skeleton-board">
			{#each Array(5) as _}
				<div class="skeleton-column">
					<div class="skeleton-header"></div>
					<div class="skeleton-cards">
						<div class="skeleton-card"></div>
						<div class="skeleton-card short"></div>
						<div class="skeleton-card"></div>
					</div>
				</div>
			{/each}
		</div>
	{:else if allItems.length === 0}
		<div class="empty-state">
			<Icon name="columns" size={48} />
			<h2>No tasks yet</h2>
			<p>Tasks will appear here once plans have requirements, scenarios, and tasks generated.</p>
		</div>
	{:else}
		<div class="kanban-board">
			{#each activeColumns as col (col.status)}
				<KanbanColumn
					column={col}
					items={itemsByStatus[col.status]}
					selectedCardId={kanbanStore.selectedCardId}
					onSelectCard={handleSelectCard}
				/>
			{/each}
		</div>
	{/if}
</div>

<style>
	.kanban-view {
		display: flex;
		flex-direction: column;
		height: 100%;
		gap: var(--space-4);
	}

	.kanban-toolbar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-4);
		flex-wrap: wrap;
	}

	.kanban-board {
		display: flex;
		gap: var(--space-3);
		flex: 1;
		overflow-x: auto;
		padding-bottom: var(--space-2);
	}

	/* Skeleton loading */
	.skeleton-board {
		display: flex;
		gap: var(--space-3);
		flex: 1;
		overflow: hidden;
	}

	.skeleton-column {
		flex: 0 0 280px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-lg);
		overflow: hidden;
	}

	.skeleton-header {
		height: 44px;
		background: linear-gradient(
			90deg,
			var(--color-bg-tertiary) 0%,
			var(--color-bg-elevated) 50%,
			var(--color-bg-tertiary) 100%
		);
		background-size: 200% 100%;
		animation: shimmer 1.5s infinite;
		border-bottom: 1px solid var(--color-border);
	}

	.skeleton-cards {
		padding: var(--space-2);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.skeleton-card {
		height: 72px;
		border-radius: var(--radius-md);
		background: linear-gradient(
			90deg,
			var(--color-bg-secondary) 0%,
			var(--color-bg-elevated) 50%,
			var(--color-bg-secondary) 100%
		);
		background-size: 200% 100%;
		animation: shimmer 1.5s infinite;
	}

	.skeleton-card.short {
		height: 52px;
	}

	@keyframes shimmer {
		0% {
			background-position: 200% 0;
		}
		100% {
			background-position: -200% 0;
		}
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
</style>
