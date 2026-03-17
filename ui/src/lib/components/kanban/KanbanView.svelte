<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import KanbanColumn from './KanbanColumn.svelte';
	import StatusFilterChips from './StatusFilterChips.svelte';
	import PlanFilterDropdown from './PlanFilterDropdown.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { kanbanStore } from '$lib/stores/kanban.svelte';
	import {
		KANBAN_COLUMNS,
		taskToKanbanStatus,
		type KanbanCardItem,
		type KanbanStatus
	} from '$lib/types/kanban';
	import { onMount } from 'svelte';

	// Fetch tasks, requirements, and scenarios for all active plans on mount
	onMount(() => {
		fetchAllPlanData();
	});

	// Re-fetch when new plans appear
	$effect(() => {
		fetchAllPlanData();
	});

	function fetchAllPlanData() {
		for (const plan of plansStore.active) {
			if (!plansStore.tasksByPlan[plan.slug]) {
				plansStore.fetchTasks(plan.slug);
			}
			kanbanStore.fetchRequirements(plan.slug);
			kanbanStore.fetchScenarios(plan.slug);
		}
	}

	// Build kanban card items from all plan tasks, enriched with requirement/scenario context
	const allItems = $derived.by(() => {
		const items: KanbanCardItem[] = [];
		const plans = kanbanStore.selectedPlanSlug
			? plansStore.active.filter((p) => p.slug === kanbanStore.selectedPlanSlug)
			: plansStore.active;

		for (const plan of plans) {
			const tasks = plansStore.getTasks(plan.slug);
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
			const scenariosByReqId = new Map<string, typeof scenarios>();
			for (const s of scenarios) {
				const list = scenariosByReqId.get(s.requirement_id) ?? [];
				list.push(s);
				scenariosByReqId.set(s.requirement_id, list);
			}

			// Build a lookup: scenario_id → requirement
			const reqByScenarioId = new Map<string, typeof requirements[number]>();
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
			plans={plansStore.active}
			selectedSlug={kanbanStore.selectedPlanSlug}
			onSelect={(s) => kanbanStore.filterByPlan(s)}
		/>
	</div>

	{#if allItems.length === 0}
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
