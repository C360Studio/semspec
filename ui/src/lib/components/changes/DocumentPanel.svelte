<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import type { DocumentInfo } from '$lib/types/changes';

	interface Props {
		documents: DocumentInfo[];
		selectedType?: string;
		onSelect?: (type: string) => void;
	}

	let { documents, selectedType, onSelect }: Props = $props();

	function getIcon(doc: DocumentInfo): string {
		if (doc.exists) return 'check-circle';
		return 'circle';
	}

	function formatDate(dateString?: string): string {
		if (!dateString) return '';
		return new Date(dateString).toLocaleDateString();
	}
</script>

<div class="document-panel">
	<h3 class="panel-title">Documents</h3>

	<div class="document-list">
		{#each documents as doc}
			<button
				class="document-item"
				class:exists={doc.exists}
				class:selected={selectedType === doc.type}
				onclick={() => onSelect?.(doc.type)}
				disabled={!doc.exists}
			>
				<Icon name={getIcon(doc)} size={16} />
				<span class="doc-name">{doc.type}.md</span>
				{#if doc.exists && doc.model}
					<span class="doc-meta">{doc.model}</span>
				{/if}
			</button>
		{/each}
	</div>
</div>

<style>
	.document-panel {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.panel-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-secondary);
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin: 0;
	}

	.document-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.document-item {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-md);
		text-align: left;
		cursor: pointer;
		transition: all var(--transition-fast);
		color: var(--color-text-muted);
	}

	.document-item:disabled {
		cursor: default;
	}

	.document-item.exists {
		color: var(--color-text-primary);
	}

	.document-item.exists:hover {
		background: var(--color-bg-tertiary);
	}

	.document-item.selected {
		background: var(--color-accent-muted);
		border-color: var(--color-accent);
		color: var(--color-accent);
	}

	.doc-name {
		flex: 1;
		font-size: var(--font-size-sm);
	}

	.doc-meta {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}
</style>
