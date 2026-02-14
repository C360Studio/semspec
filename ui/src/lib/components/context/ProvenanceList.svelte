<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { ProvenanceEntry } from '$lib/types/context';
	import {
		getProvenanceLabel,
		getProvenanceIcon,
		formatTokens,
		getSourceShortName
	} from '$lib/types/context';

	interface Props {
		/** Provenance entries to display */
		entries: ProvenanceEntry[];
		/** Maximum items to show before collapsing */
		maxVisible?: number;
		/** Compact mode */
		compact?: boolean;
	}

	let { entries, maxVisible = 8, compact = false }: Props = $props();

	let expanded = $state(false);

	const sortedEntries = $derived(
		[...entries].sort((a, b) => (a.priority ?? 99) - (b.priority ?? 99))
	);

	const visibleEntries = $derived(
		expanded || sortedEntries.length <= maxVisible
			? sortedEntries
			: sortedEntries.slice(0, maxVisible)
	);

	const hasMore = $derived(sortedEntries.length > maxVisible);
	const hiddenCount = $derived(sortedEntries.length - maxVisible);
	const totalTokens = $derived(entries.reduce((sum, e) => sum + e.tokens, 0));
</script>

<div class="provenance-list" class:compact>
	<div class="list-header">
		<span class="list-title">Provenance ({entries.length} sources)</span>
		<span class="total-tokens">{formatTokens(totalTokens)} tokens</span>
	</div>

	{#if entries.length === 0}
		<div class="empty-state">
			<Icon name="inbox" size={20} />
			<span>No sources loaded</span>
		</div>
	{:else}
		<ul class="entries">
			{#each visibleEntries as entry, index}
				<li class="entry" class:truncated={entry.truncated}>
					<div class="entry-priority">P{entry.priority ?? index + 1}</div>
					<div class="entry-type">
						<Icon name={getProvenanceIcon(entry.type)} size={14} />
						<span class="type-label">{getProvenanceLabel(entry.type)}</span>
					</div>
					<div class="entry-source" title={entry.source}>
						{getSourceShortName(entry.source)}
					</div>
					<div class="entry-tokens">
						{formatTokens(entry.tokens)}
						{#if entry.truncated}
							<span class="truncated-badge" title="Content was truncated">!</span>
						{/if}
					</div>
				</li>
			{/each}
		</ul>

		{#if hasMore}
			<button class="show-more-btn" onclick={() => (expanded = !expanded)}>
				<Icon name={expanded ? 'chevron-up' : 'chevron-down'} size={14} />
				{expanded ? 'Show less' : `Show ${hiddenCount} more`}
			</button>
		{/if}
	{/if}
</div>

<style>
	.provenance-list {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		overflow: hidden;
	}

	.list-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: var(--space-3) var(--space-4);
		background: var(--color-bg-tertiary);
		border-bottom: 1px solid var(--color-border);
	}

	.list-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-secondary);
	}

	.total-tokens {
		font-size: var(--font-size-xs);
		font-family: var(--font-family-mono);
		color: var(--color-text-muted);
	}

	.compact .list-header {
		padding: var(--space-2) var(--space-3);
	}

	.compact .list-title {
		font-size: var(--font-size-xs);
	}

	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-6);
		color: var(--color-text-muted);
	}

	.entries {
		list-style: none;
		margin: 0;
		padding: 0;
	}

	.entry {
		display: grid;
		grid-template-columns: 32px 100px 1fr auto;
		gap: var(--space-3);
		align-items: center;
		padding: var(--space-2) var(--space-4);
		border-bottom: 1px solid var(--color-border);
		font-size: var(--font-size-sm);
	}

	.compact .entry {
		grid-template-columns: 24px 80px 1fr auto;
		gap: var(--space-2);
		padding: var(--space-1) var(--space-3);
		font-size: var(--font-size-xs);
	}

	.entry:last-child {
		border-bottom: none;
	}

	.entry.truncated {
		background: var(--color-warning-muted);
	}

	.entry-priority {
		font-size: var(--font-size-xs);
		font-family: var(--font-family-mono);
		color: var(--color-text-muted);
		text-align: center;
	}

	.entry-type {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		color: var(--color-text-secondary);
	}

	.type-label {
		white-space: nowrap;
	}

	.entry-source {
		font-family: var(--font-family-mono);
		color: var(--color-accent);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.entry-tokens {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-family: var(--font-family-mono);
		color: var(--color-text-muted);
		white-space: nowrap;
	}

	.truncated-badge {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 14px;
		height: 14px;
		background: var(--color-warning);
		color: var(--color-bg-primary);
		border-radius: var(--radius-full);
		font-size: 10px;
		font-weight: var(--font-weight-semibold);
	}

	.show-more-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-1);
		width: 100%;
		padding: var(--space-2);
		background: var(--color-bg-tertiary);
		border: none;
		color: var(--color-text-secondary);
		font-size: var(--font-size-xs);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.show-more-btn:hover {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}
</style>
