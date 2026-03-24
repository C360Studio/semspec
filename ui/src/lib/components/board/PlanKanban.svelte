<script lang="ts">
	/**
	 * PlanKanban — Plans as cards in stage columns.
	 *
	 * Columns are toggleable via chips at the top. Failed/Archived
	 * are hidden by default. Column visibility persisted to localStorage.
	 */

	import Icon from '$lib/components/shared/Icon.svelte';
	import PlanCard from './PlanCard.svelte';
	import type { PlanWithStatus } from '$lib/types/plan';

	interface Props {
		plans: PlanWithStatus[];
	}

	let { plans }: Props = $props();

	const STORAGE_KEY = 'semspec-kanban-columns';

	interface ColumnDef {
		id: string;
		label: string;
		stages: string[];
		defaultOn: boolean;
	}

	const ALL_COLUMNS: ColumnDef[] = [
		{ id: 'review', label: 'Review', stages: ['draft', 'drafting', 'planning', 'approved', 'requirements_generated', 'scenarios_generated', 'needs_changes', 'rejected', 'ready_for_approval', 'reviewed'], defaultOn: true },
		{ id: 'ready', label: 'Ready', stages: ['ready_for_execution'], defaultOn: true },
		{ id: 'executing', label: 'Executing', stages: ['implementing', 'executing', 'reviewing_rollup'], defaultOn: true },
		{ id: 'complete', label: 'Complete', stages: ['complete'], defaultOn: true },
		{ id: 'failed', label: 'Failed', stages: ['failed'], defaultOn: false },
		{ id: 'archived', label: 'Archived', stages: ['archived'], defaultOn: false }
	];

	// Load persisted column visibility
	function loadVisibility(): Set<string> {
		if (typeof localStorage === 'undefined') {
			return new Set(ALL_COLUMNS.filter((c) => c.defaultOn).map((c) => c.id));
		}
		const stored = localStorage.getItem(STORAGE_KEY);
		if (stored) {
			try {
				return new Set(JSON.parse(stored));
			} catch {
				// fall through to defaults
			}
		}
		return new Set(ALL_COLUMNS.filter((c) => c.defaultOn).map((c) => c.id));
	}

	let activeColumns = $state(loadVisibility());

	function toggleColumn(id: string) {
		const next = new Set(activeColumns);
		if (next.has(id)) {
			if (next.size > 1) next.delete(id);
		} else {
			next.add(id);
		}
		activeColumns = next;
		if (typeof localStorage !== 'undefined') {
			localStorage.setItem(STORAGE_KEY, JSON.stringify([...next]));
		}
	}

	// Count plans per column (for chip badges)
	const columnCounts = $derived.by(() => {
		const counts: Record<string, number> = {};
		for (const col of ALL_COLUMNS) {
			counts[col.id] = plans.filter((p) => col.stages.includes(p.stage)).length;
		}
		return counts;
	});

	// Visible columns with their plans
	const visibleColumns = $derived(
		ALL_COLUMNS.filter((c) => activeColumns.has(c.id)).map((c) => ({
			...c,
			plans: plans.filter((p) => c.stages.includes(p.stage))
		}))
	);
</script>

<div class="plan-kanban">
	<div class="column-chips" role="group" aria-label="Column visibility">
		{#each ALL_COLUMNS as col}
			<button
				class="chip"
				class:active={activeColumns.has(col.id)}
				onclick={() => toggleColumn(col.id)}
				aria-pressed={activeColumns.has(col.id)}
			>
				{col.label}
				{#if columnCounts[col.id] > 0}
					<span class="chip-count">{columnCounts[col.id]}</span>
				{/if}
			</button>
		{/each}
	</div>

	<div class="columns">
		{#each visibleColumns as col (col.id)}
			<div class="column">
				<div class="column-header">
					<span class="column-label">{col.label}</span>
					<span class="column-count">{col.plans.length}</span>
				</div>
				<div class="column-cards">
					{#each col.plans as plan (plan.slug)}
						<PlanCard {plan} />
					{/each}
					{#if col.plans.length === 0}
						<div class="column-empty">No plans</div>
					{/if}
				</div>
			</div>
		{/each}
	</div>
</div>

<style>
	.plan-kanban {
		display: flex;
		flex-direction: column;
		height: 100%;
		gap: var(--space-3);
	}

	.column-chips {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1);
		flex-shrink: 0;
	}

	.chip {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-3);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-full);
		background: transparent;
		color: var(--color-text-muted);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.chip:hover {
		border-color: var(--color-accent);
		color: var(--color-text-primary);
	}

	.chip.active {
		background: var(--color-accent-muted);
		border-color: var(--color-accent);
		color: var(--color-accent);
	}

	.chip-count {
		padding: 0 var(--space-1);
		font-size: 10px;
		font-weight: var(--font-weight-semibold);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-full);
		min-width: 16px;
		text-align: center;
	}

	.chip.active .chip-count {
		background: var(--color-accent);
		color: white;
	}

	.columns {
		display: flex;
		gap: var(--space-3);
		flex: 1;
		overflow-x: auto;
		min-height: 0;
	}

	.column {
		flex: 1;
		min-width: 280px;
		max-width: 400px;
		display: flex;
		flex-direction: column;
		background: var(--color-bg-secondary);
		border-radius: var(--radius-lg);
		border: 1px solid var(--color-border);
		overflow: hidden;
	}

	.column-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: var(--space-2) var(--space-3);
		border-bottom: 1px solid var(--color-border);
		flex-shrink: 0;
	}

	.column-label {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-text-muted);
	}

	.column-count {
		font-size: 10px;
		padding: 0 var(--space-1);
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-full);
		color: var(--color-text-muted);
		min-width: 16px;
		text-align: center;
	}

	.column-cards {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-2);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.column-empty {
		padding: var(--space-4);
		text-align: center;
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}
</style>
