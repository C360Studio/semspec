<script lang="ts">
	import { KANBAN_COLUMNS, type KanbanStatus } from '$lib/types/kanban';

	interface Props {
		activeStatuses: Set<KanbanStatus>;
		counts: Record<KanbanStatus, number>;
		onToggle: (status: KanbanStatus) => void;
	}

	let { activeStatuses, counts, onToggle }: Props = $props();
</script>

<div class="filter-chips" data-testid="kanban-status-filters">
	{#each KANBAN_COLUMNS as col}
		<button
			class="filter-chip"
			class:active={activeStatuses.has(col.status)}
			data-testid="filter-{col.status}"
			aria-pressed={activeStatuses.has(col.status)}
			onclick={() => onToggle(col.status)}
		>
			<span class="chip-label">{col.label}</span>
			<span class="chip-count">{counts[col.status] ?? 0}</span>
		</button>
	{/each}
</div>

<style>
	.filter-chips {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}

	.filter-chip {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-full);
		background: var(--color-bg-primary);
		color: var(--color-text-muted);
		font-size: var(--font-size-xs);
		cursor: pointer;
		transition: all 150ms ease;
	}

	.filter-chip:hover {
		border-color: var(--color-border-focus);
		color: var(--color-text-secondary);
	}

	.filter-chip.active {
		background: var(--color-bg-tertiary);
		border-color: var(--color-accent);
		color: var(--color-text-primary);
		font-weight: var(--font-weight-medium);
	}

	.chip-count {
		font-size: 0.625rem;
		padding: 0 5px;
		border-radius: var(--radius-full);
		background: var(--color-bg-secondary);
		color: var(--color-text-muted);
		min-width: 18px;
		text-align: center;
	}

	.filter-chip.active .chip-count {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}
</style>
