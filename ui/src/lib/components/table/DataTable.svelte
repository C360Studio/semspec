<script lang="ts">
	/**
	 * DataTable Component - Feature-rich sortable/filterable data table
	 *
	 * Features:
	 * - Sortable column headers (click to toggle asc/desc)
	 * - Filter input with configurable searchable fields
	 * - Status filter dropdown
	 * - Pagination with configurable page size
	 * - Expandable rows for detail content
	 * - Row actions column
	 * - Batch selection with select all
	 * - Mobile-responsive cards layout
	 * - Filtered/total count display
	 * - Custom cell rendering via snippet
	 * - ARIA attributes for accessibility
	 * - Sticky header
	 *
	 * Extended from semstreams-ui DataTable with additional features.
	 */

	import type { Snippet } from 'svelte';

	// Generic type for table data
	type T = $$Generic;

	export interface Column<TData> {
		/** Unique key for the column, used for sorting */
		key: string;
		/** Display label for the column header */
		label: string;
		/** Whether the column is sortable */
		sortable?: boolean;
		/** Column width (CSS value) */
		width?: string;
		/** Text alignment: 'left' (default), 'right', 'center' */
		align?: 'left' | 'right' | 'center';
		/** Optional sort comparator for custom sorting */
		compare?: (a: TData, b: TData) => number;
		/** Optional getter for the sortable value (if different from key) */
		getValue?: (item: TData) => unknown;
		/** Hide column on mobile */
		hideOnMobile?: boolean;
	}

	interface StatusOption {
		value: string;
		label: string;
	}

	interface DataTableProps<TData> {
		/** Array of data items to display */
		data: TData[];
		/** Column definitions */
		columns: Column<TData>[];
		/** Placeholder text for filter input */
		filterPlaceholder?: string;
		/** Fields to search when filtering (keys from the data items) */
		filterFields?: string[];
		/** Function to get a unique key for each row */
		getRowKey: (item: TData) => string;
		/** Optional: Label for the table (for accessibility) */
		ariaLabel?: string;
		/** Optional: Show filter input (default: true) */
		showFilter?: boolean;
		/** Optional: Show count (default: true) */
		showCount?: boolean;
		/** Optional: Empty state message */
		emptyMessage?: string;
		/** Optional: No results message (when filter matches nothing) */
		noResultsMessage?: string;
		/** Optional: Label for count (e.g., "tasks" shows "3 tasks" instead of "3 items") */
		countLabel?: string;
		/** Optional: Custom test ID prefix (default: "data-table") */
		testIdPrefix?: string;
		/** Items per page (0 = no pagination) */
		pageSize?: number;
		/** Enable row selection */
		selectable?: boolean;
		/** Enable expandable rows */
		expandable?: boolean;
		/** Status filter options */
		statusOptions?: StatusOption[];
		/** Field to filter by status */
		statusField?: string;
		/** Snippet for rendering cell content */
		cell: Snippet<[Column<TData>, TData]>;
		/** Optional: Snippet for expanded row content */
		expandedRow?: Snippet<[TData]>;
		/** Optional: Snippet for row actions */
		actions?: Snippet<[TData]>;
		/** Optional: Additional info to show in header */
		headerInfo?: Snippet;
		/** Callback when selection changes */
		onSelectionChange?: (selectedKeys: Set<string>) => void;
	}

	let {
		data,
		columns,
		filterPlaceholder = 'Filter...',
		filterFields = [],
		getRowKey,
		ariaLabel = 'Data table',
		showFilter = true,
		showCount = true,
		emptyMessage = 'No data available',
		noResultsMessage,
		countLabel = 'items',
		testIdPrefix = 'data-table',
		pageSize = 0,
		selectable = false,
		expandable = false,
		statusOptions = [],
		statusField = 'status',
		cell,
		expandedRow,
		actions,
		headerInfo,
		onSelectionChange
	}: DataTableProps<T> = $props();

	// Internal state
	let filterText = $state('');
	let statusFilter = $state('');
	let sortColumn = $state<string | null>(null);
	let sortDirection = $state<'asc' | 'desc'>('asc');
	let currentPage = $state(1);
	let selectedKeys = $state<Set<string>>(new Set());
	let expandedKeys = $state<Set<string>>(new Set());

	// Initialize default sort to first sortable column
	$effect(() => {
		if (sortColumn === null && columns.length > 0) {
			const firstSortable = columns.find((c) => c.sortable);
			if (firstSortable) {
				sortColumn = firstSortable.key;
			}
		}
	});

	// Reset page when filters change
	$effect(() => {
		// Access dependencies
		filterText;
		statusFilter;
		// Reset to first page
		currentPage = 1;
	});

	// Filtered and sorted data
	const filteredData = $derived.by(() => {
		let result = [...data];

		// Text filter
		if (filterText && filterFields.length > 0) {
			const searchTerm = filterText.toLowerCase();
			result = result.filter((item) => {
				return filterFields.some((field) => {
					const value = getNestedValue(item, field);
					return String(value).toLowerCase().includes(searchTerm);
				});
			});
		}

		// Status filter
		if (statusFilter && statusField) {
			result = result.filter((item) => {
				const value = getNestedValue(item, statusField);
				return value === statusFilter;
			});
		}

		// Sort
		if (sortColumn) {
			const column = columns.find((c) => c.key === sortColumn);
			if (column) {
				result.sort((a, b) => {
					let cmp = 0;

					if (column.compare) {
						cmp = column.compare(a, b);
					} else {
						const aVal = column.getValue ? column.getValue(a) : getNestedValue(a, column.key);
						const bVal = column.getValue ? column.getValue(b) : getNestedValue(b, column.key);

						if (typeof aVal === 'string' && typeof bVal === 'string') {
							cmp = aVal.localeCompare(bVal);
						} else if (typeof aVal === 'number' && typeof bVal === 'number') {
							cmp = aVal - bVal;
						} else {
							cmp = String(aVal ?? '').localeCompare(String(bVal ?? ''));
						}
					}

					return sortDirection === 'asc' ? cmp : -cmp;
				});
			}
		}

		return result;
	});

	// Paginated data
	const paginatedData = $derived.by(() => {
		if (pageSize <= 0) return filteredData;
		const start = (currentPage - 1) * pageSize;
		return filteredData.slice(start, start + pageSize);
	});

	const totalPages = $derived(pageSize > 0 ? Math.ceil(filteredData.length / pageSize) : 1);

	// Selection helpers
	const allSelected = $derived(
		paginatedData.length > 0 && paginatedData.every((item) => selectedKeys.has(getRowKey(item)))
	);

	const someSelected = $derived(
		paginatedData.some((item) => selectedKeys.has(getRowKey(item))) && !allSelected
	);

	/**
	 * Get nested value from object using dot notation
	 */
	function getNestedValue(obj: unknown, path: string): unknown {
		return path.split('.').reduce((current, key) => {
			if (current && typeof current === 'object' && key in current) {
				return (current as Record<string, unknown>)[key];
			}
			return undefined;
		}, obj);
	}

	/**
	 * Handle column header click for sorting
	 */
	function handleSort(column: Column<T>) {
		if (!column.sortable) return;

		if (sortColumn === column.key) {
			sortDirection = sortDirection === 'asc' ? 'desc' : 'asc';
		} else {
			sortColumn = column.key;
			sortDirection = 'asc';
		}
	}

	/**
	 * Get sort indicator for column
	 */
	function getSortIndicator(columnKey: string): string {
		if (sortColumn !== columnKey) return '';
		return sortDirection === 'asc' ? ' ▲' : ' ▼';
	}

	/**
	 * Get aria-sort value for column
	 */
	function getAriaSort(columnKey: string): 'ascending' | 'descending' | 'none' {
		if (sortColumn !== columnKey) return 'none';
		return sortDirection === 'asc' ? 'ascending' : 'descending';
	}

	/**
	 * Toggle row selection
	 */
	function toggleSelection(key: string) {
		const newSet = new Set(selectedKeys);
		if (newSet.has(key)) {
			newSet.delete(key);
		} else {
			newSet.add(key);
		}
		selectedKeys = newSet;
		onSelectionChange?.(selectedKeys);
	}

	/**
	 * Toggle all visible rows
	 */
	function toggleAll() {
		const newSet = new Set(selectedKeys);
		if (allSelected) {
			// Deselect all on current page
			paginatedData.forEach((item) => newSet.delete(getRowKey(item)));
		} else {
			// Select all on current page
			paginatedData.forEach((item) => newSet.add(getRowKey(item)));
		}
		selectedKeys = newSet;
		onSelectionChange?.(selectedKeys);
	}

	/**
	 * Toggle row expansion
	 */
	function toggleExpansion(key: string) {
		const newSet = new Set(expandedKeys);
		if (newSet.has(key)) {
			newSet.delete(key);
		} else {
			newSet.add(key);
		}
		expandedKeys = newSet;
	}

	/**
	 * Go to a specific page
	 */
	function goToPage(page: number) {
		if (page >= 1 && page <= totalPages) {
			currentPage = page;
		}
	}

	// Compute no results message with filter text
	const computedNoResultsMessage = $derived(
		noResultsMessage ?? `No results match "${filterText}"`
	);

	// Has any active actions column
	const hasActions = $derived(actions !== undefined);

	// Total columns including selection and expand
	const totalColumns = $derived(
		columns.length + (selectable ? 1 : 0) + (expandable ? 1 : 0) + (hasActions ? 1 : 0)
	);
</script>

<div class="data-table" data-testid={testIdPrefix}>
	<!-- Control Bar -->
	{#if showFilter || showCount || headerInfo || statusOptions.length > 0}
		<div class="control-bar">
			<div class="filter-group">
				{#if showFilter}
					<input
						type="text"
						class="filter-input"
						placeholder={filterPlaceholder}
						bind:value={filterText}
						aria-label="Filter table"
						data-testid="{testIdPrefix}-filter"
					/>
				{/if}

				{#if statusOptions.length > 0}
					<select
						class="status-filter"
						bind:value={statusFilter}
						aria-label="Filter by status"
						data-testid="{testIdPrefix}-status-filter"
					>
						<option value="">All statuses</option>
						{#each statusOptions as option (option.value)}
							<option value={option.value}>{option.label}</option>
						{/each}
					</select>
				{/if}
			</div>

			<div class="info-section">
				{#if showCount}
					<span class="info-label" data-testid="{testIdPrefix}-count">
						{#if filterText || statusFilter}
							{filteredData.length} of {data.length}
						{:else}
							{data.length} {countLabel}
						{/if}
					</span>
				{/if}

				{#if selectedKeys.size > 0}
					<span class="selection-count">{selectedKeys.size} selected</span>
				{/if}

				{#if headerInfo}
					{@render headerInfo()}
				{/if}
			</div>
		</div>
	{/if}

	<!-- Table Container (desktop) -->
	<div class="table-container desktop-only">
		{#if data.length === 0}
			<div class="empty-state" data-testid="{testIdPrefix}-empty">
				<p>{emptyMessage}</p>
			</div>
		{:else if filteredData.length === 0 && (filterText || statusFilter)}
			<div class="empty-state" data-testid="{testIdPrefix}-no-results">
				<p>{computedNoResultsMessage}</p>
			</div>
		{:else if paginatedData.length > 0}
			<table aria-label={ariaLabel}>
				<thead>
					<tr>
						{#if selectable}
							<th class="checkbox-col">
								<input
									type="checkbox"
									checked={allSelected}
									indeterminate={someSelected}
									onchange={toggleAll}
									aria-label="Select all"
								/>
							</th>
						{/if}
						{#if expandable}
							<th class="expand-col"></th>
						{/if}
						{#each columns as column (column.key)}
							<th
								scope="col"
								class:sortable={column.sortable}
								class:numeric={column.align === 'right'}
								class:center={column.align === 'center'}
								class:hide-mobile={column.hideOnMobile}
								style:width={column.width}
								onclick={() => handleSort(column)}
								aria-sort={column.sortable ? getAriaSort(column.key) : undefined}
								data-testid="{testIdPrefix}-header-{column.key}"
							>
								{column.label}{column.sortable ? getSortIndicator(column.key) : ''}
							</th>
						{/each}
						{#if hasActions}
							<th class="actions-col">Actions</th>
						{/if}
					</tr>
				</thead>
				<tbody>
					{#each paginatedData as item (getRowKey(item))}
						{@const rowKey = getRowKey(item)}
						{@const isExpanded = expandedKeys.has(rowKey)}
						<tr
							class:selected={selectedKeys.has(rowKey)}
							data-testid="{testIdPrefix}-row"
						>
							{#if selectable}
								<td class="checkbox-col">
									<input
										type="checkbox"
										checked={selectedKeys.has(rowKey)}
										onchange={() => toggleSelection(rowKey)}
										aria-label="Select row"
									/>
								</td>
							{/if}
							{#if expandable}
								<td class="expand-col">
									<button
										type="button"
										class="expand-btn"
										onclick={() => toggleExpansion(rowKey)}
										aria-expanded={isExpanded}
										aria-label={isExpanded ? 'Collapse row' : 'Expand row'}
									>
										<span class="expand-icon" class:expanded={isExpanded}>▶</span>
									</button>
								</td>
							{/if}
							{#each columns as column (column.key)}
								<td
									class:numeric={column.align === 'right'}
									class:center={column.align === 'center'}
									class:hide-mobile={column.hideOnMobile}
								>
									{@render cell(column, item)}
								</td>
							{/each}
							{#if hasActions && actions}
								<td class="actions-col">
									{@render actions(item)}
								</td>
							{/if}
						</tr>
						{#if expandable && isExpanded && expandedRow}
							<tr class="expanded-row">
								<td colspan={totalColumns}>
									<div class="expanded-content">
										{@render expandedRow(item)}
									</div>
								</td>
							</tr>
						{/if}
					{/each}
				</tbody>
			</table>
		{/if}
	</div>

	<!-- Cards Container (mobile) -->
	<div class="cards-container mobile-only">
		{#if data.length === 0}
			<div class="empty-state">
				<p>{emptyMessage}</p>
			</div>
		{:else if filteredData.length === 0 && (filterText || statusFilter)}
			<div class="empty-state">
				<p>{computedNoResultsMessage}</p>
			</div>
		{:else}
			{#each paginatedData as item (getRowKey(item))}
				{@const rowKey = getRowKey(item)}
				{@const isExpanded = expandedKeys.has(rowKey)}
				<div
					class="card"
					class:selected={selectedKeys.has(rowKey)}
					data-testid="{testIdPrefix}-card"
				>
					<div class="card-header">
						{#if selectable}
							<input
								type="checkbox"
								checked={selectedKeys.has(rowKey)}
								onchange={() => toggleSelection(rowKey)}
								aria-label="Select item"
							/>
						{/if}
						<div class="card-title">
							{@render cell(columns[0], item)}
						</div>
						{#if expandable}
							<button
								type="button"
								class="expand-btn"
								onclick={() => toggleExpansion(rowKey)}
								aria-expanded={isExpanded}
							>
								<span class="expand-icon" class:expanded={isExpanded}>▶</span>
							</button>
						{/if}
					</div>

					<div class="card-body">
						{#each columns.slice(1) as column (column.key)}
							<div class="card-field">
								<span class="card-label">{column.label}</span>
								<span class="card-value">
									{@render cell(column, item)}
								</span>
							</div>
						{/each}
					</div>

					{#if hasActions && actions}
						<div class="card-actions">
							{@render actions(item)}
						</div>
					{/if}

					{#if expandable && isExpanded && expandedRow}
						<div class="card-expanded">
							{@render expandedRow(item)}
						</div>
					{/if}
				</div>
			{/each}
		{/if}
	</div>

	<!-- Pagination -->
	{#if pageSize > 0 && totalPages > 1}
		<div class="pagination" data-testid="{testIdPrefix}-pagination">
			<button
				type="button"
				class="page-btn"
				disabled={currentPage === 1}
				onclick={() => goToPage(1)}
				aria-label="First page"
			>
				««
			</button>
			<button
				type="button"
				class="page-btn"
				disabled={currentPage === 1}
				onclick={() => goToPage(currentPage - 1)}
				aria-label="Previous page"
			>
				«
			</button>

			<span class="page-info">
				Page {currentPage} of {totalPages}
			</span>

			<button
				type="button"
				class="page-btn"
				disabled={currentPage === totalPages}
				onclick={() => goToPage(currentPage + 1)}
				aria-label="Next page"
			>
				»
			</button>
			<button
				type="button"
				class="page-btn"
				disabled={currentPage === totalPages}
				onclick={() => goToPage(totalPages)}
				aria-label="Last page"
			>
				»»
			</button>
		</div>
	{/if}
</div>

<style>
	.data-table {
		display: flex;
		flex-direction: column;
		height: 100%;
		background: var(--color-bg-secondary);
	}

	/* Control Bar */
	.control-bar {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: var(--space-3);
		border-bottom: 1px solid var(--color-border);
		background: var(--color-bg-tertiary);
		gap: var(--space-3);
		flex-wrap: wrap;
	}

	.filter-group {
		display: flex;
		gap: var(--space-2);
		flex: 1;
		min-width: 200px;
	}

	.filter-input {
		flex: 1;
		max-width: 300px;
		padding: var(--space-2) var(--space-3);
		font-size: var(--font-size-sm);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-bg-primary);
		color: var(--color-text-primary);
	}

	.filter-input:focus {
		outline: none;
		border-color: var(--color-primary);
		box-shadow: 0 0 0 2px var(--color-accent-muted);
	}

	.filter-input::placeholder {
		color: var(--color-text-muted);
	}

	.status-filter {
		padding: var(--space-2) var(--space-3);
		font-size: var(--font-size-sm);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-bg-primary);
		color: var(--color-text-primary);
		cursor: pointer;
	}

	.info-section {
		display: flex;
		align-items: center;
		gap: var(--space-3);
	}

	.info-label {
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		font-weight: var(--font-weight-medium);
	}

	.selection-count {
		font-size: var(--font-size-sm);
		color: var(--color-accent);
		font-weight: var(--font-weight-medium);
	}

	/* Table Container */
	.table-container {
		flex: 1;
		overflow-y: auto;
		overflow-x: auto;
		background: var(--color-bg-primary);
	}

	.empty-state {
		display: flex;
		align-items: center;
		justify-content: center;
		height: 100%;
		min-height: 150px;
	}

	.empty-state p {
		margin: 0;
		color: var(--color-text-secondary);
		font-size: var(--font-size-sm);
	}

	/* Table Styles */
	table {
		width: 100%;
		border-collapse: collapse;
		font-size: var(--font-size-sm);
	}

	thead {
		position: sticky;
		top: 0;
		background: var(--color-bg-secondary);
		z-index: 1;
		border-bottom: 2px solid var(--color-border);
	}

	th {
		text-align: left;
		padding: var(--space-3);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		white-space: nowrap;
	}

	th.sortable {
		cursor: pointer;
		user-select: none;
		transition: background-color 0.1s;
	}

	th.sortable:hover {
		background: var(--color-bg-tertiary);
	}

	th.numeric {
		text-align: right;
	}

	th.center {
		text-align: center;
	}

	.checkbox-col {
		width: 40px;
		text-align: center;
	}

	.expand-col {
		width: 40px;
		text-align: center;
	}

	.actions-col {
		width: 150px;
		text-align: right;
	}

	tbody tr {
		border-bottom: 1px solid var(--color-border);
		transition: background-color 0.1s;
	}

	tbody tr:hover {
		background: var(--color-bg-secondary);
	}

	tbody tr.selected {
		background: var(--color-accent-muted);
	}

	tbody tr:last-child {
		border-bottom: none;
	}

	td {
		padding: var(--space-3);
		color: var(--color-text-primary);
	}

	td.numeric {
		text-align: right;
	}

	td.center {
		text-align: center;
	}

	/* Expand button */
	.expand-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		padding: 0;
		border: none;
		background: transparent;
		color: var(--color-text-secondary);
		cursor: pointer;
		border-radius: var(--radius-sm);
	}

	.expand-btn:hover {
		background: var(--color-bg-hover);
		color: var(--color-text-primary);
	}

	.expand-icon {
		display: inline-block;
		font-size: 10px;
		transition: transform 0.2s;
	}

	.expand-icon.expanded {
		transform: rotate(90deg);
	}

	/* Expanded row */
	.expanded-row {
		background: var(--color-bg-tertiary);
	}

	.expanded-row td {
		padding: 0;
	}

	.expanded-content {
		padding: var(--space-4);
		border-top: 1px dashed var(--color-border);
	}

	/* Pagination */
	.pagination {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-2);
		padding: var(--space-3);
		border-top: 1px solid var(--color-border);
		background: var(--color-bg-secondary);
	}

	.page-btn {
		padding: var(--space-1) var(--space-2);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		background: var(--color-bg-primary);
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
		cursor: pointer;
		transition: all 0.1s;
	}

	.page-btn:hover:not(:disabled) {
		background: var(--color-bg-tertiary);
		border-color: var(--color-accent);
	}

	.page-btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.page-info {
		padding: 0 var(--space-3);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	/* Mobile cards */
	.cards-container {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-3);
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.card {
		background: var(--color-bg-primary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		overflow: hidden;
	}

	.card.selected {
		border-color: var(--color-accent);
		background: var(--color-accent-muted);
	}

	.card-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-3);
		background: var(--color-bg-secondary);
		border-bottom: 1px solid var(--color-border);
	}

	.card-title {
		flex: 1;
		font-weight: var(--font-weight-semibold);
	}

	.card-body {
		padding: var(--space-3);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.card-field {
		display: flex;
		justify-content: space-between;
		align-items: center;
		font-size: var(--font-size-sm);
	}

	.card-label {
		color: var(--color-text-muted);
	}

	.card-value {
		color: var(--color-text-primary);
	}

	.card-actions {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-2);
		padding: var(--space-3);
		border-top: 1px solid var(--color-border);
		background: var(--color-bg-secondary);
	}

	.card-expanded {
		padding: var(--space-3);
		background: var(--color-bg-tertiary);
		border-top: 1px dashed var(--color-border);
	}

	/* Responsive visibility */
	.desktop-only {
		display: block;
	}

	.mobile-only {
		display: none;
	}

	.hide-mobile {
		display: table-cell;
	}

	@media (max-width: 768px) {
		.desktop-only {
			display: none;
		}

		.mobile-only {
			display: flex;
		}

		.hide-mobile {
			display: none;
		}

		.filter-group {
			flex-direction: column;
			width: 100%;
		}

		.filter-input {
			max-width: none;
		}
	}

	/* Scrollbar styling */
	.table-container::-webkit-scrollbar,
	.cards-container::-webkit-scrollbar {
		width: 8px;
		height: 8px;
	}

	.table-container::-webkit-scrollbar-track,
	.cards-container::-webkit-scrollbar-track {
		background: var(--color-bg-secondary);
	}

	.table-container::-webkit-scrollbar-thumb,
	.cards-container::-webkit-scrollbar-thumb {
		background: var(--color-border);
		border-radius: 4px;
	}

	.table-container::-webkit-scrollbar-thumb:hover,
	.cards-container::-webkit-scrollbar-thumb:hover {
		background: var(--color-text-muted);
	}
</style>
