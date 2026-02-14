<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { DocCategory } from '$lib/types/source';
	import { CATEGORY_META } from '$lib/types/source';

	interface Props {
		value: DocCategory | '';
		counts?: Record<DocCategory, number>;
		onchange: (category: DocCategory | '') => void;
	}

	let { value, counts, onchange }: Props = $props();

	const categories: { value: DocCategory | ''; label: string }[] = [
		{ value: '', label: 'All Categories' },
		{ value: 'sop', label: 'SOPs' },
		{ value: 'spec', label: 'Specs' },
		{ value: 'datasheet', label: 'Datasheets' },
		{ value: 'reference', label: 'References' },
		{ value: 'api', label: 'API Docs' }
	];

	function handleChange(e: Event) {
		const target = e.target as HTMLSelectElement;
		onchange(target.value as DocCategory | '');
	}

	function getCount(category: DocCategory | ''): number | undefined {
		if (!counts || category === '') return undefined;
		return counts[category];
	}
</script>

<div class="category-filter">
	<Icon name="filter" size={16} />
	<select
		{value}
		onchange={handleChange}
		aria-label="Filter by category"
	>
		{#each categories as cat}
			<option value={cat.value}>
				{cat.label}
				{#if getCount(cat.value) !== undefined}
					({getCount(cat.value)})
				{/if}
			</option>
		{/each}
	</select>
</div>

<style>
	.category-filter {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-muted);
	}

	.category-filter:focus-within {
		border-color: var(--color-accent);
	}

	select {
		flex: 1;
		border: none;
		background: none;
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
		cursor: pointer;
	}

	select:focus {
		outline: none;
	}

	option {
		background: var(--color-bg-primary);
		color: var(--color-text-primary);
	}
</style>
