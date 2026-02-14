<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { SourceType } from '$lib/types/source';

	interface Props {
		value: SourceType | '';
		documentCount: number;
		repositoryCount: number;
		webSourceCount?: number;
		onchange: (type: SourceType | '') => void;
	}

	let { value, documentCount, repositoryCount, webSourceCount = 0, onchange }: Props = $props();

	const totalCount = $derived(documentCount + repositoryCount + webSourceCount);

	const options: { value: SourceType | ''; label: string; icon: string }[] = [
		{ value: '', label: 'All', icon: 'layers' },
		{ value: 'document', label: 'Documents', icon: 'file-text' },
		{ value: 'repository', label: 'Repositories', icon: 'git-branch' },
		{ value: 'web', label: 'Web', icon: 'globe' }
	];

	function getCount(type: SourceType | ''): number {
		switch (type) {
			case 'document':
				return documentCount;
			case 'repository':
				return repositoryCount;
			case 'web':
				return webSourceCount;
			default:
				return totalCount;
		}
	}
</script>

<div class="type-filter" role="radiogroup" aria-label="Filter by source type">
	{#each options as option}
		<button
			type="button"
			role="radio"
			class="type-option"
			class:selected={value === option.value}
			onclick={() => onchange(option.value)}
			aria-checked={value === option.value}
			aria-label="{option.label} ({getCount(option.value)} sources)"
		>
			<Icon name={option.icon} size={14} />
			<span class="label">{option.label}</span>
			<span class="count">{getCount(option.value)}</span>
		</button>
	{/each}
</div>

<style>
	.type-filter {
		display: flex;
		gap: var(--space-1);
		padding: 2px;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
	}

	.type-option {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: none;
		border: none;
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.type-option:hover {
		color: var(--color-text-primary);
		background: var(--color-bg-tertiary);
	}

	.type-option:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.type-option.selected {
		background: var(--color-bg-primary);
		color: var(--color-text-primary);
		box-shadow: var(--shadow-sm);
	}

	.label {
		font-weight: var(--font-weight-medium);
	}

	.count {
		padding: 1px 6px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.type-option.selected .count {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}
</style>
