<script lang="ts">
	/**
	 * GraphMetrics - Compact stats bar for the knowledge graph visualization
	 *
	 * Displays entity counts broken down by semspec entity type and total
	 * relationship count. Shown at the top of the center panel so users
	 * can see what the graph contains at a glance without inspecting nodes.
	 */

	import type { GraphEntity, GraphRelationship } from '$lib/stores/graphStore.svelte';
	import { ENTITY_COLORS } from '$lib/utils/entity-colors';

	interface GraphMetricsProps {
		entities: GraphEntity[];
		relationships: GraphRelationship[];
	}

	let { entities, relationships }: GraphMetricsProps = $props();

	/** Count entities grouped by semspec entity type. */
	const typeCounts = $derived.by(() => {
		const counts = new Map<string, number>();
		for (const entity of entities) {
			const t = entity.entityType;
			counts.set(t, (counts.get(t) ?? 0) + 1);
		}
		// Return sorted by count descending, unknowns last
		return Array.from(counts.entries())
			.filter(([type]) => type !== 'unknown')
			.sort(([, a], [, b]) => b - a);
	});

	const unknownCount = $derived(entities.filter((e) => e.entityType === 'unknown').length);

	/**
	 * Graph density = edges / (n * (n-1)) for a directed graph.
	 * Only meaningful above ~3 nodes; suppressed below that threshold.
	 */
	const density = $derived.by(() => {
		const n = entities.length;
		if (n < 3) return 0;
		return relationships.length / (n * (n - 1));
	});

	const densityLabel = $derived(density > 0 ? (density * 100).toFixed(1) + '%' : null);
</script>

<div class="graph-metrics" role="status" aria-label="Graph statistics" data-testid="graph-metrics">
	<span class="metrics-label">Graph:</span>

	{#each typeCounts as [type, count] (type)}
		<span
			class="type-chip"
			style="--chip-color: {ENTITY_COLORS[type as keyof typeof ENTITY_COLORS] ?? ENTITY_COLORS.unknown}"
			title="{count} {type} entities"
			data-testid="metrics-type-{type}"
		>
			<span class="chip-dot" aria-hidden="true"></span>
			<span class="chip-type">{type}</span>
			<span class="chip-count">{count}</span>
		</span>
	{/each}

	{#if unknownCount > 0}
		<span
			class="type-chip type-chip-unknown"
			title="{unknownCount} unknown entities"
			data-testid="metrics-type-unknown"
		>
			<span class="chip-dot" aria-hidden="true"></span>
			<span class="chip-type">other</span>
			<span class="chip-count">{unknownCount}</span>
		</span>
	{/if}

	<span class="metrics-sep" aria-hidden="true">|</span>

	<span class="metric-item" data-testid="metrics-relationships">
		<span class="metric-value">{relationships.length}</span>
		<span class="metric-label">edges</span>
	</span>

	{#if densityLabel}
		<span class="metric-item" data-testid="metrics-density">
			<span class="metric-value">{densityLabel}</span>
			<span class="metric-label">density</span>
		</span>
	{/if}
</div>

<style>
	.graph-metrics {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 6px 12px;
		font-size: 11px;
		white-space: nowrap;
		flex-shrink: 0;
	}

	.metrics-label {
		font-weight: 600;
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.5px;
		margin-right: 2px;
	}

	.type-chip {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 2px 7px 2px 5px;
		border-radius: 10px;
		background: color-mix(in srgb, var(--chip-color, #6b7280) 15%, var(--color-bg-primary));
		border: 1px solid color-mix(in srgb, var(--chip-color, #6b7280) 40%, transparent);
	}

	.type-chip-unknown {
		--chip-color: var(--color-text-muted, #6b7280);
	}

	.chip-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--chip-color, #6b7280);
		flex-shrink: 0;
	}

	.chip-type {
		color: var(--color-text-muted);
		text-transform: capitalize;
	}

	.chip-count {
		font-weight: 600;
		color: var(--color-text-primary);
	}

	.metrics-sep {
		color: var(--color-border);
		margin: 0 2px;
	}

	.metric-item {
		display: inline-flex;
		align-items: center;
		gap: 3px;
		color: var(--color-text-muted);
	}

	.metric-value {
		font-weight: 600;
		color: var(--color-text-primary);
	}
</style>
