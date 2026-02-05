<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { Relationship } from '$lib/types';

	interface Props {
		relationships: Relationship[];
		title?: string;
		icon?: string;
		onRelationshipClick?: (rel: Relationship) => void;
	}

	let { relationships, title, icon = 'link', onRelationshipClick }: Props = $props();

	function handleClick(rel: Relationship) {
		onRelationshipClick?.(rel);
	}

	function handleKeyDown(e: KeyboardEvent, rel: Relationship) {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			handleClick(rel);
		}
	}
</script>

{#if relationships.length > 0}
	<section class="relationships-section">
		{#if title}
			<h2>
				<Icon name={icon} size={18} />
				{title}
			</h2>
		{/if}
		<div class="relationships-list">
			{#each relationships as rel}
				<button
					class="relationship-item"
					onclick={() => handleClick(rel)}
					onkeydown={(e) => handleKeyDown(e, rel)}
					type="button"
					aria-label="{rel.direction === 'incoming' ? 'incoming' : 'outgoing'} relationship: {rel.predicateLabel} to {rel.targetName}"
				>
					<span class="rel-direction" class:incoming={rel.direction === 'incoming'}>
						{rel.direction === 'incoming' ? '<-' : '->'}
					</span>
					<span class="rel-predicate">{rel.predicateLabel}</span>
					<span class="rel-target">{rel.targetName}</span>
					<span class="rel-type">{rel.targetType}</span>
				</button>
			{/each}
		</div>
	</section>
{/if}

<style>
	.relationships-section {
		margin-bottom: var(--space-6);
	}

	.relationships-section h2 {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin: 0 0 var(--space-3);
		font-size: var(--font-size-lg);
		color: var(--color-text-primary);
	}

	.relationships-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.relationship-item {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		cursor: pointer;
		transition: all var(--transition-fast);
		text-align: left;
		width: 100%;
	}

	.relationship-item:hover {
		background: var(--color-bg-tertiary);
		border-color: var(--color-accent-muted);
	}

	.relationship-item:focus {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}

	.rel-direction {
		font-family: var(--font-mono);
		font-size: var(--font-size-sm);
		color: var(--color-success);
	}

	.rel-direction.incoming {
		color: var(--color-info);
	}

	.rel-predicate {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.rel-target {
		flex: 1;
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
	}

	.rel-type {
		font-size: var(--font-size-xs);
		padding: 2px 6px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
		text-transform: uppercase;
	}
</style>
