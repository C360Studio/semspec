<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import KanbanCard from './KanbanCard.svelte';
	import KanbanGroupCard from './KanbanGroupCard.svelte';
	import { kanbanStore } from '$lib/stores/kanban.svelte';
	import type { KanbanColumnDef, KanbanCardItem } from '$lib/types/kanban';

	interface Props {
		column: KanbanColumnDef;
		items: KanbanCardItem[];
		selectedCardId: string | null;
		onSelectCard: (id: string) => void;
	}

	let { column, items, selectedCardId, onSelectCard }: Props = $props();

	// Group items: items with a requirementId get grouped, ungrouped items render standalone
	interface GroupedEntry {
		type: 'group';
		requirementId: string;
		requirementTitle: string;
		scenarioCount: number;
		items: KanbanCardItem[];
	}

	interface StandaloneEntry {
		type: 'standalone';
		item: KanbanCardItem;
	}

	type ColumnEntry = GroupedEntry | StandaloneEntry;

	const entries = $derived.by(() => {
		const result: ColumnEntry[] = [];
		const byReq = new Map<string, KanbanCardItem[]>();
		const ungrouped: KanbanCardItem[] = [];

		for (const item of items) {
			if (item.requirementId) {
				const list = byReq.get(item.requirementId) ?? [];
				list.push(item);
				byReq.set(item.requirementId, list);
			} else {
				ungrouped.push(item);
			}
		}

		// Render groups (only group if 2+ items share a requirement)
		for (const [reqId, reqItems] of byReq) {
			if (reqItems.length >= 2) {
				const title = reqItems[0].requirementTitle ?? 'Requirement';
				const scenarioCount = kanbanStore.getScenarioCountForRequirement(reqId);
				result.push({ type: 'group', requirementId: reqId, requirementTitle: title, scenarioCount, items: reqItems });
			} else {
				// Single item under a requirement — show as standalone with breadcrumb
				ungrouped.push(...reqItems);
			}
		}

		// Standalone items
		for (const item of ungrouped) {
			result.push({ type: 'standalone', item });
		}

		return result;
	});

	const totalCount = $derived(items.length);
</script>

<div class="kanban-column" data-testid="kanban-column-{column.status}">
	<div class="column-header">
		<div class="header-left">
			<Icon name={column.icon} size={14} />
			<span class="column-label">{column.label}</span>
		</div>
		<span class="column-count">{totalCount}</span>
	</div>

	<div class="column-content">
		{#if entries.length === 0}
			<div class="empty-column">No items</div>
		{:else}
			{#each entries as entry (entry.type === 'group' ? entry.requirementId : entry.item.id)}
				{#if entry.type === 'group'}
					<KanbanGroupCard
						requirementTitle={entry.requirementTitle}
						scenarioCount={entry.scenarioCount}
						items={entry.items}
						{selectedCardId}
						onSelectCard={onSelectCard}
					/>
				{:else}
					<KanbanCard
						item={entry.item}
						selected={selectedCardId === entry.item.id}
						onSelect={onSelectCard}
					/>
				{/if}
			{/each}
		{/if}
	</div>
</div>

<style>
	.kanban-column {
		flex: 0 0 280px;
		display: flex;
		flex-direction: column;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-lg);
		max-height: 100%;
		min-height: 200px;
	}

	.column-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: var(--space-3);
		border-bottom: 1px solid var(--color-border);
	}

	.header-left {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--color-text-secondary);
	}

	.column-label {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
	}

	.column-count {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		padding: 0 var(--space-2);
		background: var(--color-bg-secondary);
		color: var(--color-text-muted);
		border-radius: var(--radius-full);
		min-width: 20px;
		text-align: center;
	}

	.column-content {
		flex: 1;
		padding: var(--space-2);
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.empty-column {
		display: flex;
		align-items: center;
		justify-content: center;
		padding: var(--space-6);
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
	}
</style>
