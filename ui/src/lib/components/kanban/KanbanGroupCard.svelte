<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import KanbanCard from './KanbanCard.svelte';
	import type { KanbanCardItem } from '$lib/types/kanban';

	interface Props {
		requirementTitle: string;
		scenarioCount: number;
		items: KanbanCardItem[];
		selectedCardId: string | null;
		onSelectCard: (id: string) => void;
	}

	let { requirementTitle, scenarioCount, items, selectedCardId, onSelectCard }: Props = $props();

	let expanded = $state(true);
</script>

<div class="group-card">
	<button
		class="group-header"
		onclick={() => (expanded = !expanded)}
		aria-expanded={expanded}
	>
		<Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={12} />
		<span class="group-title">{requirementTitle}</span>
		<span class="group-counts">
			{items.length} task{items.length !== 1 ? 's' : ''}
			{#if scenarioCount > 0}
				<span class="scenario-count">{scenarioCount} scenario{scenarioCount !== 1 ? 's' : ''}</span>
			{/if}
		</span>
	</button>

	{#if expanded}
		<div class="group-children">
			{#each items as item (item.id)}
				<KanbanCard
					{item}
					nested
					selected={selectedCardId === item.id}
					onSelect={onSelectCard}
				/>
			{/each}
		</div>
	{/if}
</div>

<style>
	.group-card {
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-bg-secondary);
		overflow: hidden;
	}

	.group-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		width: 100%;
		padding: var(--space-2) var(--space-3);
		border: none;
		background: var(--color-bg-elevated);
		color: var(--color-text-secondary);
		font-size: var(--font-size-xs);
		cursor: pointer;
		text-align: left;
		transition: background var(--transition-fast);
	}

	.group-header:hover {
		background: var(--color-bg-tertiary);
	}

	.group-title {
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		flex: 1;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.group-counts {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: 0.6875rem;
		color: var(--color-text-muted);
		white-space: nowrap;
	}

	.scenario-count {
		padding: 0 4px;
		background: var(--color-info-muted);
		color: var(--color-info);
		border-radius: var(--radius-full);
		font-size: 0.625rem;
	}

	.group-children {
		display: flex;
		flex-direction: column;
		gap: 1px;
		padding: var(--space-1) var(--space-1) var(--space-1) var(--space-3);
	}
</style>
