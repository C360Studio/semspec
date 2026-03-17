<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import AgentBadge from '$lib/components/board/AgentBadge.svelte';
	import { getTaskStatusInfo, type TaskStatus } from '$lib/types/task';
	import type { KanbanCardItem } from '$lib/types/kanban';

	interface Props {
		item: KanbanCardItem;
		selected?: boolean;
		nested?: boolean;
		onSelect: (id: string) => void;
		onNavigate?: (item: KanbanCardItem) => void;
	}

	let { item, selected = false, nested = false, onSelect, onNavigate }: Props = $props();

	const statusInfo = $derived(
		item.type === 'task'
			? getTaskStatusInfo(item.originalStatus as TaskStatus)
			: { label: item.originalStatus, color: 'gray' as const, icon: 'circle' }
	);

	const typeLabel = $derived(
		item.taskType
			? item.taskType.charAt(0).toUpperCase() + item.taskType.slice(1)
			: item.type === 'scenario'
				? 'Scenario'
				: ''
	);
</script>

<button
	class="kanban-card"
	class:selected
	class:nested
	class:has-rejection={!!item.rejection}
	data-testid="kanban-card"
	data-status={item.originalStatus}
	aria-label="{item.title}, {statusInfo.label}"
	aria-pressed={selected}
	onclick={() => onSelect(item.id)}
	ondblclick={() => onNavigate?.(item)}
>
	<div class="card-top">
		<span class="status-dot status-{statusInfo.color}"></span>
		{#if typeLabel}
			<span class="type-label">{typeLabel}</span>
		{/if}
		<span class="plan-slug">{item.planSlug}</span>
	</div>

	<div class="kanban-title">{item.title}</div>

	{#if item.requirementTitle}
		<div class="requirement-breadcrumb">
			<Icon name="git-branch" size={10} />
			<span>{item.requirementTitle}</span>
		</div>
	{/if}

	{#if item.rejection}
		<div class="rejection-info">
			<Icon name="x-circle" size={12} />
			<span>{item.rejection.reason.slice(0, 60)}{item.rejection.reason.length > 60 ? '...' : ''}</span>
		</div>
	{/if}

	{#if item.iteration !== undefined && item.maxIterations !== undefined}
		<div class="iteration-info">
			Attempt {item.iteration}/{item.maxIterations}
		</div>
	{/if}

	{#if item.agentRole}
		<div class="card-agent">
			<AgentBadge
				role={item.agentRole}
				model={item.agentModel}
				state={item.agentState ?? 'idle'}
			/>
		</div>
	{/if}
</button>

<style>
	.kanban-card {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		width: 100%;
		padding: var(--space-3);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		text-align: left;
		cursor: pointer;
		transition: all 150ms ease;
	}

	.kanban-card:hover {
		border-color: var(--color-accent);
		background: var(--color-bg-tertiary);
	}

	.kanban-card.selected {
		border-color: var(--color-accent);
		box-shadow: 0 0 0 1px var(--color-accent);
	}

	.kanban-card.nested {
		background: var(--color-bg-primary);
		border-color: transparent;
		border-left: 2px solid var(--color-border);
		border-radius: 0 var(--radius-md) var(--radius-md) 0;
	}

	.kanban-card.nested:hover {
		border-left-color: var(--color-accent);
		background: var(--color-bg-secondary);
	}

	.kanban-card.has-rejection {
		border-left: 3px solid var(--color-warning);
	}

	.card-top {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.status-dot {
		width: 6px;
		height: 6px;
		border-radius: var(--radius-full);
		flex-shrink: 0;
	}

	.status-gray { background: var(--color-text-muted); }
	.status-blue { background: var(--color-accent); }
	.status-green { background: var(--color-success); }
	.status-yellow { background: var(--color-warning); }
	.status-red { background: var(--color-error); }
	.status-orange { background: var(--color-warning); }

	.type-label {
		font-weight: var(--font-weight-medium);
		color: var(--color-text-secondary);
	}

	.plan-slug {
		margin-left: auto;
		opacity: 0.7;
	}

	.kanban-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		line-height: var(--line-height-tight);
		text-transform: none;
		display: -webkit-box;
		-webkit-line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}

	.requirement-breadcrumb {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: 0.6875rem;
		color: var(--color-text-muted);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.rejection-info {
		display: flex;
		align-items: flex-start;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-warning);
		line-height: var(--line-height-tight);
	}

	.iteration-info {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-variant-numeric: tabular-nums;
	}

	.card-agent {
		margin-top: var(--space-1);
	}
</style>
