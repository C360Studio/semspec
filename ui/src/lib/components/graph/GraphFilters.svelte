<script lang="ts">
	/**
	 * GraphFilters - Horizontal filter toolbar for the knowledge graph visualization
	 *
	 * Provides:
	 * - Entity type checkboxes (code/spec/task/loop/proposal/activity)
	 *   that map to graphStore.toggleType
	 * - Text search input for filtering by entity ID substring
	 * - "All / None" quick-select buttons
	 *
	 * The filter state is owned by graphStore; this component only dispatches
	 * changes via callback props to keep it stateless and testable.
	 */

	import type { EntityType } from '$lib/types';
	import { ENTITY_COLORS } from '$lib/utils/entity-colors';

	interface GraphFiltersProps {
		/** Set of currently visible entity types from graphStore.visibleTypes */
		visibleTypes: Set<EntityType>;
		/** Current search string from graphStore.searchQuery */
		search: string;
		/** Callback when user toggles a type checkbox */
		onToggleType: (type: EntityType) => void;
		/** Callback when user changes the search text */
		onSearchChange: (search: string) => void;
		/** Callback to show all types */
		onShowAll: () => void;
		/** Callback to hide all types */
		onHideAll: () => void;
	}

	let {
		visibleTypes,
		search,
		onToggleType,
		onSearchChange,
		onShowAll,
		onHideAll
	}: GraphFiltersProps = $props();

	const SEMSPEC_TYPES: EntityType[] = ['code', 'spec', 'task', 'loop', 'proposal', 'activity'];

	function handleSearchInput(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		onSearchChange(input.value);
	}

	function handleSearchKeydown(event: KeyboardEvent) {
		if (event.key === 'Escape') {
			onSearchChange('');
		}
	}
</script>

<div class="graph-filters" data-testid="graph-filters">
	<!-- Entity type toggles -->
	<div class="type-list" role="group" aria-label="Entity type filters">
		{#each SEMSPEC_TYPES as type (type)}
			{@const checked = visibleTypes.has(type)}
			{@const color = ENTITY_COLORS[type] ?? ENTITY_COLORS.unknown}
			<label
				class="type-checkbox"
				class:type-checked={checked}
				style="--type-color: {color}"
				data-testid="filter-type-{type}"
			>
				<input
					type="checkbox"
					{checked}
					onchange={() => onToggleType(type)}
					aria-label="Show {type} entities"
				/>
				<span class="type-dot" aria-hidden="true"></span>
				<span class="type-name">{type}</span>
			</label>
		{/each}
	</div>

	<!-- Quick actions -->
	<div class="quick-actions">
		<button class="quick-btn" onclick={onShowAll} title="Show all entity types">All</button>
		<button class="quick-btn" onclick={onHideAll} title="Hide all entity types">None</button>
	</div>

	<span class="filter-sep" aria-hidden="true">|</span>

	<!-- Search -->
	<div class="search-wrapper">
		<input
			id="graph-search"
			type="search"
			class="search-input"
			placeholder="Filter by ID or name…"
			value={search}
			oninput={handleSearchInput}
			onkeydown={handleSearchKeydown}
			aria-label="Filter entities by ID or name"
		/>
		{#if search}
			<button
				class="clear-search"
				onclick={() => onSearchChange('')}
				aria-label="Clear search"
				title="Clear"
			>
				×
			</button>
		{/if}
	</div>
</div>

<style>
	.graph-filters {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 12px;
		background: var(--color-bg-secondary);
		font-size: 12px;
		flex-shrink: 0;
		min-height: 36px;
	}

	/* Type checkboxes — horizontal row */
	.type-list {
		display: flex;
		align-items: center;
		gap: 2px;
		flex-wrap: wrap;
	}

	.type-checkbox {
		display: flex;
		align-items: center;
		gap: 4px;
		padding: 3px 8px;
		border-radius: 12px;
		cursor: pointer;
		transition: background-color var(--transition-fast, 150ms ease);
		font-size: 11px;
		color: var(--color-text-muted);
		white-space: nowrap;
		user-select: none;
	}

	.type-checkbox:hover {
		background: var(--color-bg-tertiary);
	}

	.type-checkbox.type-checked {
		color: var(--color-text-primary);
		background: color-mix(in srgb, var(--type-color, #6b7280) 12%, transparent);
	}

	.type-checkbox input[type='checkbox'] {
		/* Visually hidden but accessible */
		position: absolute;
		width: 1px;
		height: 1px;
		opacity: 0;
		margin: 0;
	}

	.type-dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		border: 2px solid var(--type-color, #6b7280);
		flex-shrink: 0;
		background: transparent;
		transition: background-color var(--transition-fast, 150ms ease);
	}

	.type-checked .type-dot {
		background: var(--type-color, #6b7280);
	}

	.type-name {
		text-transform: capitalize;
	}

	/* Quick-action buttons */
	.quick-actions {
		display: flex;
		gap: 2px;
	}

	.quick-btn {
		font-size: 10px;
		padding: 2px 6px;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm, 3px);
		background: var(--color-bg-primary);
		color: var(--color-text-secondary);
		cursor: pointer;
		transition: background-color var(--transition-fast, 150ms ease),
			border-color var(--transition-fast, 150ms ease);
	}

	.quick-btn:hover {
		background: var(--color-bg-tertiary);
		border-color: var(--color-accent);
		color: var(--color-text-primary);
	}

	.filter-sep {
		color: var(--color-border);
		margin: 0 2px;
	}

	/* Search */
	.search-wrapper {
		position: relative;
		min-width: 160px;
		max-width: 240px;
	}

	.search-input {
		width: 100%;
		padding: 4px 28px 4px 8px;
		font-size: 11px;
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm, 4px);
		color: var(--color-text-primary);
		outline: none;
		transition: border-color var(--transition-fast, 150ms ease);
		box-sizing: border-box;
	}

	.search-input:focus {
		border-color: var(--color-accent);
	}

	.search-input::placeholder {
		color: var(--color-text-muted);
	}

	/* Override browser default search input styling */
	.search-input::-webkit-search-decoration,
	.search-input::-webkit-search-cancel-button {
		-webkit-appearance: none;
	}

	.clear-search {
		position: absolute;
		right: 4px;
		top: 50%;
		transform: translateY(-50%);
		width: 18px;
		height: 18px;
		border: none;
		background: transparent;
		color: var(--color-text-muted);
		font-size: 14px;
		cursor: pointer;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: var(--radius-sm, 3px);
	}

	.clear-search:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}
</style>
