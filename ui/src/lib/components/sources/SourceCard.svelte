<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { DocumentSource } from '$lib/types/source';
	import { CATEGORY_META, STATUS_META } from '$lib/types/source';

	interface Props {
		source: DocumentSource;
		compact?: boolean;
		onclick?: () => void;
	}

	let { source, compact = false, onclick }: Props = $props();

	const categoryMeta = $derived(CATEGORY_META[source.category]);
	const statusMeta = $derived(STATUS_META[source.status]);

	function formatDate(dateStr: string): string {
		return new Date(dateStr).toLocaleDateString();
	}
</script>

<button
	class="source-card"
	class:compact
	onclick={onclick}
	type="button"
	aria-label="View {source.name}"
>
	<div class="source-icon" style="color: {categoryMeta.color}">
		<Icon name={categoryMeta.icon} size={compact ? 16 : 20} />
	</div>

	<div class="source-content">
		<div class="source-header">
			<span class="source-name">{source.name}</span>
			<span class="category-badge" style="background: {categoryMeta.color}20; color: {categoryMeta.color}">
				{categoryMeta.label}
			</span>
		</div>

		{#if !compact}
			<div class="source-meta">
				<span class="status-badge" style="color: {statusMeta.color}">
					<Icon name={statusMeta.icon} size={12} />
					{statusMeta.label}
				</span>
				{#if source.chunkCount !== undefined}
					<span class="chunk-count">
						<Icon name="layers" size={12} />
						{source.chunkCount} chunks
					</span>
				{/if}
				{#if source.project}
					<span class="project-tag">
						<Icon name="folder" size={12} />
						{source.project}
					</span>
				{/if}
			</div>

			{#if source.summary}
				<p class="source-summary">{source.summary}</p>
			{/if}

			<div class="source-footer">
				<span class="filename">{source.filename}</span>
				<span class="added-at">{formatDate(source.addedAt)}</span>
			</div>
		{/if}
	</div>

	<div class="source-arrow">
		<Icon name="chevron-right" size={16} />
	</div>
</button>

<style>
	.source-card {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		cursor: pointer;
		transition: all var(--transition-fast);
		text-align: left;
		width: 100%;
	}

	.source-card:hover {
		background: var(--color-bg-tertiary);
		border-color: var(--color-accent-muted);
	}

	.source-card.compact {
		padding: var(--space-2) var(--space-3);
		align-items: center;
	}

	.source-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
		margin-top: 2px;
	}

	.source-content {
		flex: 1;
		min-width: 0;
	}

	.source-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex-wrap: wrap;
	}

	.source-name {
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.category-badge {
		font-size: var(--font-size-xs);
		padding: 2px 6px;
		border-radius: var(--radius-sm);
		text-transform: uppercase;
		font-weight: var(--font-weight-medium);
	}

	.source-meta {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-3);
		margin-top: var(--space-2);
	}

	.status-badge {
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: var(--font-size-xs);
	}

	.chunk-count,
	.project-tag {
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.source-summary {
		margin: var(--space-2) 0 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		line-height: 1.4;
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}

	.source-footer {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-top: var(--space-2);
	}

	.filename {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-family: var(--font-mono);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.added-at {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		flex-shrink: 0;
	}

	.source-arrow {
		color: var(--color-text-muted);
		flex-shrink: 0;
		align-self: center;
	}
</style>
