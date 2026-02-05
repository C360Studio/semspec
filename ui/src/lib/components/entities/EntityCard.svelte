<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { Entity, EntityType } from '$lib/types';

	interface Props {
		entity: Entity;
		compact?: boolean;
		onclick?: () => void;
	}

	let { entity, compact = false, onclick }: Props = $props();

	// Map entity types to icons
	const typeIcons: Record<EntityType, string> = {
		code: 'file-code',
		proposal: 'lightbulb',
		spec: 'file-text',
		task: 'check-square',
		loop: 'refresh-cw',
		activity: 'activity'
	};

	// Map entity types to colors
	const typeColors: Record<EntityType, string> = {
		code: 'var(--color-info)',
		proposal: 'var(--color-warning)',
		spec: 'var(--color-success)',
		task: 'var(--color-accent)',
		loop: 'var(--color-secondary)',
		activity: 'var(--color-muted)'
	};

	function getIcon(type: EntityType): string {
		return typeIcons[type] || 'circle';
	}

	function getColor(type: EntityType): string {
		return typeColors[type] || 'var(--color-text-muted)';
	}

	// Extract display-relevant predicates
	function getStatus(): string | undefined {
		const statusPred = entity.predicates['semspec.proposal.status'] ||
			entity.predicates['semspec.spec.status'] ||
			entity.predicates['semspec.task.status'] ||
			entity.predicates['agent.loop.status'];
		return statusPred as string | undefined;
	}

	function getPath(): string | undefined {
		return entity.predicates['code.artifact.path'] as string | undefined;
	}

	function getLanguage(): string | undefined {
		return entity.predicates['code.artifact.language'] as string | undefined;
	}
</script>

<button
	class="entity-card"
	class:compact
	onclick={onclick}
	type="button"
	aria-label="View {entity.name}"
>
	<div class="entity-icon" style="color: {getColor(entity.type)}">
		<Icon name={getIcon(entity.type)} size={compact ? 16 : 20} />
	</div>

	<div class="entity-content">
		<div class="entity-header">
			<span class="entity-name">{entity.name}</span>
			<span class="entity-type-badge">{entity.type}</span>
		</div>

		{#if !compact}
			<div class="entity-meta">
				{#if getStatus()}
					<span class="status-badge">{getStatus()}</span>
				{/if}
				{#if getPath()}
					<span class="path-info">{getPath()}</span>
				{/if}
				{#if getLanguage()}
					<span class="language-badge">{getLanguage()}</span>
				{/if}
			</div>

			{#if entity.createdAt}
				<div class="entity-timestamp">
					Created: {new Date(entity.createdAt).toLocaleDateString()}
				</div>
			{/if}
		{/if}
	</div>

	<div class="entity-arrow">
		<Icon name="chevron-right" size={16} />
	</div>
</button>

<style>
	.entity-card {
		display: flex;
		align-items: center;
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

	.entity-card:hover {
		background: var(--color-bg-tertiary);
		border-color: var(--color-accent-muted);
	}

	.entity-card.compact {
		padding: var(--space-2) var(--space-3);
	}

	.entity-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
	}

	.entity-content {
		flex: 1;
		min-width: 0;
	}

	.entity-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.entity-name {
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.entity-type-badge {
		font-size: var(--font-size-xs);
		padding: 2px 6px;
		border-radius: var(--radius-sm);
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
		text-transform: uppercase;
	}

	.entity-meta {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin-top: var(--space-1);
	}

	.status-badge {
		font-size: var(--font-size-xs);
		padding: 2px 6px;
		border-radius: var(--radius-sm);
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.path-info {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-family: var(--font-mono);
	}

	.language-badge {
		font-size: var(--font-size-xs);
		padding: 2px 6px;
		border-radius: var(--radius-sm);
		background: var(--color-info-muted);
		color: var(--color-info);
	}

	.entity-timestamp {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		margin-top: var(--space-1);
	}

	.entity-arrow {
		color: var(--color-text-muted);
		flex-shrink: 0;
	}
</style>
