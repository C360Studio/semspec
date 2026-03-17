<script lang="ts">
	/**
	 * GraphFilters - 2-row toolbar for the knowledge graph visualization
	 *
	 * Row 1: Full-width search/NLQ input.
	 *   - On Enter with natural language query (contains spaces or NLQ keywords):
	 *     calls onNlqSearch callback to trigger a backend NLQ search.
	 *   - Otherwise filters visible nodes client-side via onSearchChange.
	 *   - Shows a classification badge when NLQ returns metadata (tier, confidence, intent).
	 *
	 * Row 2: Entity type filter chips (one per present type, colored by type) +
	 *   "All | None" quick-select buttons + node count badge "Showing N of M".
	 *
	 * Filter state is owned by the parent (graphStore). This component only
	 * dispatches changes via callback props to keep it stateless and testable.
	 */

	import type { SvelteSet } from 'svelte/reactivity';
	import type { ClassificationMeta } from '$lib/api/graph-types';
	import { ENTITY_TYPE_COLORS } from '$lib/utils/entity-colors';

	interface Props {
		/** Set of currently visible entity types from graphStore.visibleTypes */
		visibleTypes: SvelteSet<string>;
		/** Entity types present in the current data set */
		presentTypes: string[];
		/** Current client-side search string */
		search: string;
		/** Number of entities currently visible after filtering */
		visibleCount: number;
		/** Total number of entities in the store */
		totalCount: number;
		/** NLQ classification metadata returned by the last globalSearch */
		classification?: ClassificationMeta | null;
		/** Whether an NLQ search is in progress */
		searching?: boolean;
		/** Callback when user toggles a type chip */
		onToggleType: (type: string) => void;
		/** Callback when user changes the client-side search text */
		onSearchChange: (search: string) => void;
		/** Callback when user submits an NLQ query (Enter on a NLQ-like string) */
		onNlqSearch: (query: string) => void;
		/** Callback to show all types */
		onShowAll: () => void;
		/** Callback to hide all types */
		onHideAll: () => void;
	}

	let {
		visibleTypes,
		presentTypes,
		search,
		visibleCount,
		totalCount,
		classification = null,
		searching = false,
		onToggleType,
		onSearchChange,
		onNlqSearch,
		onShowAll,
		onHideAll
	}: Props = $props();

	/**
	 * Local input value — decoupled from `search` so we can detect NLQ on Enter.
	 * Initialized empty; synced from `search` prop via $effect below.
	 */
	let inputValue = $state('');

	// Keep inputValue in sync when parent resets the search externally
	$effect(() => {
		inputValue = search;
	});

	/**
	 * Detect if the current input looks like a natural language query.
	 * Heuristics:
	 * - Contains at least one space (multi-word)
	 * - OR starts with a NLQ keyword: find, show, what, list, get, search, which, how
	 */
	function isNlqQuery(query: string): boolean {
		const trimmed = query.trim();
		if (!trimmed) return false;
		if (trimmed.includes(' ')) return true;
		const nlqPrefixes = ['find', 'show', 'what', 'list', 'get', 'search', 'which', 'how'];
		const lower = trimmed.toLowerCase();
		return nlqPrefixes.some((p) => lower.startsWith(p));
	}

	function handleInput(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		inputValue = input.value;
		// Live client-side filtering (not NLQ)
		onSearchChange(input.value);
	}

	function handleKeydown(event: KeyboardEvent) {
		if (event.key === 'Escape') {
			inputValue = '';
			onSearchChange('');
		} else if (event.key === 'Enter') {
			const trimmed = inputValue.trim();
			if (!trimmed) return;
			if (isNlqQuery(trimmed)) {
				onNlqSearch(trimmed);
			} else {
				onSearchChange(trimmed);
			}
		}
	}

	function handleClearSearch() {
		inputValue = '';
		onSearchChange('');
	}

	/** Tier label for the NLQ classification badge. */
	function tierLabel(tier: number): string {
		switch (tier) {
			case 0:
				return 'local';
			case 1:
				return 'community';
			case 2:
				return 'global';
			default:
				return `tier${tier}`;
		}
	}
</script>

<div class="graph-filters" data-testid="graph-filters">
	<!-- Row 1: Search / NLQ input -->
	<div class="filters-row filters-row-search">
		<div class="search-wrapper" class:searching>
			<span class="search-icon" aria-hidden="true">
				{#if searching}
					<span class="spin-icon">&#x21bb;</span>
				{:else}
					&#x2315;
				{/if}
			</span>
			<input
				type="search"
				class="search-input"
				placeholder="Filter by ID, name… or type a question and press Enter"
				value={inputValue}
				oninput={handleInput}
				onkeydown={handleKeydown}
				aria-label="Filter entities or search with natural language"
				data-testid="graph-search-input"
			/>
			{#if inputValue}
				<button
					class="clear-search"
					onclick={handleClearSearch}
					aria-label="Clear search"
					title="Clear"
				>
					×
				</button>
			{/if}
		</div>

		<!-- NLQ classification badge (appears after NLQ search) -->
		{#if classification}
			<div class="nlq-badge" title="NLQ classification: {classification.intent}" data-testid="nlq-badge">
				<span class="nlq-tier">{tierLabel(classification.tier)}</span>
				<span class="nlq-confidence">{(classification.confidence * 100).toFixed(0)}%</span>
				<span class="nlq-intent">{classification.intent}</span>
			</div>
		{/if}
	</div>

	<!-- Row 2: Type chips + count badge -->
	<div class="filters-row filters-row-types">
		<!-- Entity type filter chips -->
		<div class="type-list" role="group" aria-label="Entity type filters">
			{#each presentTypes as type (type)}
				{@const checked = visibleTypes.has(type)}
				{@const color = ENTITY_TYPE_COLORS[type] ?? ENTITY_TYPE_COLORS.unknown}
				<label
					class="type-chip"
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
					<span class="type-name">{type.replaceAll('_', ' ')}</span>
				</label>
			{/each}
		</div>

		<!-- Quick actions -->
		<div class="quick-actions">
			<button class="quick-btn" onclick={onShowAll} title="Show all entity types">All</button>
			<button class="quick-btn" onclick={onHideAll} title="Hide all entity types">None</button>
		</div>

		<!-- Spacer -->
		<span class="type-spacer" aria-hidden="true"></span>

		<!-- Node count badge -->
		<div class="count-badge" aria-label="Showing {visibleCount} of {totalCount} entities" data-testid="graph-count-badge">
			<span class="count-visible">{visibleCount}</span>
			<span class="count-sep">/</span>
			<span class="count-total">{totalCount}</span>
			<span class="count-label">entities</span>
		</div>
	</div>
</div>

<style>
	.graph-filters {
		display: flex;
		flex-direction: column;
		background: var(--color-bg-secondary);
		font-size: 12px;
		flex-shrink: 0;
		border-bottom: 1px solid var(--color-border);
	}

	/* Shared row styles */
	.filters-row {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 12px;
	}

	/* Row 1: Search */
	.filters-row-search {
		border-bottom: 1px solid var(--color-border);
		min-height: 36px;
	}

	.search-wrapper {
		position: relative;
		flex: 1;
		display: flex;
		align-items: center;
	}

	.search-icon {
		position: absolute;
		left: 8px;
		color: var(--color-text-muted);
		font-size: 14px;
		pointer-events: none;
		z-index: 1;
	}

	.spin-icon {
		display: inline-block;
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}

	.search-input {
		width: 100%;
		padding: 5px 28px 5px 28px;
		font-size: 12px;
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm, 4px);
		color: var(--color-text-primary);
		outline: none;
		transition: border-color 150ms ease;
		box-sizing: border-box;
	}

	.search-input:focus {
		border-color: var(--color-accent);
	}

	.search-input::placeholder {
		color: var(--color-text-muted);
	}

	/* Remove browser default search styling */
	.search-input::-webkit-search-decoration,
	.search-input::-webkit-search-cancel-button {
		-webkit-appearance: none;
	}

	.clear-search {
		position: absolute;
		right: 6px;
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

	/* NLQ classification badge */
	.nlq-badge {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 3px 8px;
		background: color-mix(in srgb, var(--color-accent, #4a9eff) 12%, transparent);
		border: 1px solid color-mix(in srgb, var(--color-accent, #4a9eff) 40%, transparent);
		border-radius: 10px;
		font-size: 10px;
		white-space: nowrap;
		flex-shrink: 0;
	}

	.nlq-tier {
		font-weight: 600;
		color: var(--color-accent, #4a9eff);
		text-transform: uppercase;
	}

	.nlq-confidence {
		color: var(--color-text-primary);
		font-weight: 500;
	}

	.nlq-intent {
		color: var(--color-text-muted);
		text-transform: capitalize;
		max-width: 120px;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	/* Row 2: Types + count */
	.filters-row-types {
		min-height: 32px;
		overflow-x: auto;
	}

	/* Type chips — horizontal row */
	.type-list {
		display: flex;
		align-items: center;
		gap: 2px;
		flex-shrink: 0;
	}

	.type-chip {
		display: flex;
		align-items: center;
		gap: 4px;
		padding: 3px 8px;
		border-radius: 12px;
		cursor: pointer;
		transition: background-color 150ms ease;
		font-size: 11px;
		color: var(--color-text-muted);
		white-space: nowrap;
		user-select: none;
	}

	.type-chip:hover {
		background: var(--color-bg-tertiary);
	}

	.type-chip.type-checked {
		color: var(--color-text-primary);
		background: color-mix(in srgb, var(--type-color, #6b7280) 12%, transparent);
	}

	.type-chip input[type='checkbox'] {
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
		transition: background-color 150ms ease;
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
		flex-shrink: 0;
	}

	.quick-btn {
		font-size: 10px;
		padding: 2px 6px;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm, 3px);
		background: var(--color-bg-primary);
		color: var(--color-text-muted);
		cursor: pointer;
		transition: background-color 150ms ease, border-color 150ms ease;
	}

	.quick-btn:hover {
		background: var(--color-bg-tertiary);
		border-color: var(--color-accent);
		color: var(--color-text-primary);
	}

	/* Spacer */
	.type-spacer {
		flex: 1;
	}

	/* Count badge */
	.count-badge {
		display: flex;
		align-items: center;
		gap: 3px;
		font-size: 11px;
		flex-shrink: 0;
		padding: 2px 8px;
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm, 4px);
	}

	.count-visible {
		font-weight: 600;
		color: var(--color-text-primary);
	}

	.count-sep {
		color: var(--color-text-muted);
	}

	.count-total {
		color: var(--color-text-muted);
	}

	.count-label {
		color: var(--color-text-muted);
		margin-left: 2px;
	}
</style>
