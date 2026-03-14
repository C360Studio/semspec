<script lang="ts">
	/**
	 * Entity Graph Explorer
	 *
	 * Replaces the flat entity list with a Sigma.js knowledge graph visualization
	 * using ThreePanelLayout:
	 * - Left panel:   Entity type filters (GraphFilters vertical variant)
	 * - Center panel: GraphFilters toolbar (search) + SigmaCanvas
	 * - Right panel:  GraphDetail for the selected entity
	 *
	 * Data flow:
	 * - On mount, calls graphStore.loadInitialGraph via a graphApiAdapter that
	 *   bridges api.entities.* to the GraphStoreAdapter interface.
	 * - Selection, hover, and expand events update graphStore state directly.
	 * - Filtered entities/relationships from graphStore feed SigmaCanvas.
	 */

	import ThreePanelLayout from '$lib/components/layout/ThreePanelLayout.svelte';
	import SigmaCanvas from '$lib/components/graph/SigmaCanvas.svelte';
	import GraphFilters from '$lib/components/graph/GraphFilters.svelte';
	import GraphDetail from '$lib/components/graph/GraphDetail.svelte';
	import GraphMetrics from '$lib/components/graph/GraphMetrics.svelte';

	import { graphStore } from '$lib/stores/graphStore.svelte';
	import type { GraphStoreAdapter } from '$lib/stores/graphStore.svelte';
	import { graphqlRequest } from '$lib/api/graphql';
	import type { EntityType } from '$lib/types';
	import type { RawEntity, RawRelationship } from '$lib/api/transforms';
	import { getEntityColor } from '$lib/utils/entity-colors';

	// ---------------------------------------------------------------------------
	// GraphStoreAdapter
	//
	// Bridges the semspec graphql API (api.entities.*) to the GraphStoreAdapter
	// interface that graphStore.loadInitialGraph / expandEntity expect.
	// ---------------------------------------------------------------------------

	/** Raw entity with both triples and relationships from the graph API. */
	interface RawEntityWithRels extends RawEntity {
		relationships?: RawRelationship[];
	}

	/**
	 * Convert a raw entity from the GraphQL API into the normalized shape
	 * the graphStore expects (properties, outgoing, incoming).
	 */
	function toAdapterEntity(raw: RawEntityWithRels) {
		const properties = (raw.triples ?? []).map((t) => ({
			predicate: t.predicate,
			object: t.object,
			confidence: 1,
			source: 'graph',
			timestamp: Date.now()
		}));

		const outgoing: Array<{
			predicate: string;
			targetId: string;
			confidence: number;
		}> = [];
		const incoming: Array<{
			predicate: string;
			sourceId: string;
			confidence: number;
		}> = [];

		for (const rel of raw.relationships ?? []) {
			if (rel.direction === 'outgoing') {
				outgoing.push({ predicate: rel.predicate, targetId: rel.to, confidence: 1 });
			} else {
				incoming.push({ predicate: rel.predicate, sourceId: rel.from, confidence: 1 });
			}
		}

		return { id: raw.id, properties, outgoing, incoming };
	}

	const graphApiAdapter: GraphStoreAdapter = {
		async listEntities({ prefix = '', limit = 200 }) {
			const result = await graphqlRequest<{
				entitiesByPrefix: RawEntityWithRels[];
			}>(
				`
				query($prefix: String!, $limit: Int) {
					entitiesByPrefix(prefix: $prefix, limit: $limit) {
						id
						triples { subject predicate object }
					}
				}
			`,
				{ prefix, limit }
			);

			const entities = (result.entitiesByPrefix ?? []).map(toAdapterEntity);
			return { entities };
		},

		async getEntityNeighbors(entityId: string) {
			// Fetch the entity and its direct relationships, then fetch neighbor entities
			const result = await graphqlRequest<{
				entity: RawEntity;
				relationships: RawRelationship[];
			}>(
				`
				query($id: String!) {
					entity(id: $id) {
						id
						triples { subject predicate object }
					}
					relationships(entityId: $id) {
						from
						to
						predicate
						direction
					}
				}
			`,
				{ id: entityId }
			);

			const rels = result.relationships ?? [];

			// Collect neighbor IDs
			const neighborIds = new Set<string>();
			for (const r of rels) {
				if (r.direction === 'outgoing') neighborIds.add(r.to);
				else neighborIds.add(r.from);
			}

			// Fetch all neighbor entities in parallel rather than sequentially
			// to avoid N+1 sequential GraphQL requests on expand.
			const neighborEntities: RawEntityWithRels[] = [];
			if (result.entity) {
				neighborEntities.push({ ...result.entity, relationships: rels });
			}

			const neighborResults = await Promise.allSettled(
				Array.from(neighborIds).map((nId) =>
					graphqlRequest<{ entity: RawEntity }>(
						`
						query($id: String!) {
							entity(id: $id) {
								id
								triples { subject predicate object }
							}
						}
					`,
						{ id: nId }
					)
				)
			);

			for (const settled of neighborResults) {
				if (settled.status === 'fulfilled' && settled.value.entity) {
					neighborEntities.push({ ...settled.value.entity, relationships: [] });
				}
				// Rejected promises are silently skipped — entity may not exist yet.
			}

			const entities = neighborEntities.map(toAdapterEntity);
			return { entities };
		},

		async searchEntities({ query, limit = 100 }) {
			const result = await graphqlRequest<{
				entitiesByPrefix: RawEntityWithRels[];
			}>(
				`
				query($prefix: String!, $limit: Int) {
					entitiesByPrefix(prefix: $prefix, limit: $limit) {
						id
						triples { subject predicate object }
					}
				}
			`,
				{ prefix: query, limit }
			);

			const entities = (result.entitiesByPrefix ?? []).map(toAdapterEntity);
			return { entities };
		}
	};

	// ---------------------------------------------------------------------------
	// Load initial graph on first mount.
	// If the user navigates away and back, existing store data is preserved
	// rather than silently resetting exploration. Refresh provides an explicit reload.
	// ---------------------------------------------------------------------------
	$effect(() => {
		if (graphStore.entities.size > 0) return;
		graphStore.loadInitialGraph(graphApiAdapter);
	});

	// ---------------------------------------------------------------------------
	// Derived passthrough values from the store
	// ---------------------------------------------------------------------------
	const filteredEntities = $derived(graphStore.filteredEntities);
	const filteredRelationships = $derived(graphStore.filteredRelationships);
	const selectedEntity = $derived(graphStore.selectedEntity);

	// Precompute entity counts per type so the template doesn't filter on every render.
	const typeCounts = $derived.by(() => {
		const counts: Record<string, number> = {};
		for (const entity of graphStore.entities.values()) {
			counts[entity.entityType] = (counts[entity.entityType] ?? 0) + 1;
		}
		return counts;
	});

	// ---------------------------------------------------------------------------
	// Event handlers — delegate straight to graphStore mutations
	// ---------------------------------------------------------------------------
	function handleEntitySelect(entityId: string | null) {
		graphStore.selectEntity(entityId);
	}

	function handleEntityHover(entityId: string | null) {
		graphStore.setHoveredEntity(entityId);
	}

	async function handleEntityExpand(entityId: string) {
		await graphStore.expandEntity(graphApiAdapter, entityId);
	}

	async function handleRefresh() {
		graphStore.clearEntities();
		await graphStore.loadInitialGraph(graphApiAdapter);
	}

	function handleToggleType(type: EntityType) {
		graphStore.toggleType(type);
	}

	function handleSearchChange(search: string) {
		graphStore.setSearch(search);
	}

	// Navigate to a related entity from the detail panel — select it and
	// expand it so its neighbors are visible in the graph.
	function handleRelatedEntitySelect(entityId: string) {
		graphStore.selectEntity(entityId);
		void graphStore.expandEntity(graphApiAdapter, entityId);
	}
</script>

<svelte:head>
	<title>Knowledge Graph - Semspec</title>
</svelte:head>

<ThreePanelLayout
	id="entities-graph"
	leftOpen={true}
	rightOpen={true}
	leftWidth={240}
	rightWidth={320}
>
	{#snippet leftPanel()}
		<div class="left-panel">
			<div class="left-panel-header">
				<h2 class="left-panel-title">Entity Types</h2>
			</div>
			<div class="left-type-filters" role="group" aria-label="Entity type filters">
				{#each (['code', 'spec', 'task', 'loop', 'proposal', 'activity'] as EntityType[]) as type (type)}
					{@const checked = graphStore.visibleTypes.has(type)}
					<label
						class="type-filter-row"
						class:checked
						data-testid="filter-type-{type}"
					>
						<input
							type="checkbox"
							{checked}
							onchange={() => handleToggleType(type)}
							aria-label="Show {type} entities"
						/>
						<span
							class="type-dot"
							style="background: {getEntityColor(type)}"
							aria-hidden="true"
						></span>
						<span class="type-label">{type}</span>
						<span class="type-count" aria-label="{type} entity count">
							{typeCounts[type] ?? 0}
						</span>
					</label>
				{/each}
			</div>
			<div class="left-quick-actions">
				<button class="quick-link" onclick={() => graphStore.showAllTypes()}>All</button>
				<button class="quick-link" onclick={() => graphStore.hideAllTypes()}>None</button>
			</div>
		</div>
	{/snippet}

	{#snippet centerPanel()}
		<div class="graph-page" data-testid="graph-page">
			<!-- Top bar: filters + metrics -->
			<div class="graph-toolbar">
				<GraphFilters
					visibleTypes={graphStore.visibleTypes}
					search={graphStore.searchQuery}
					onToggleType={handleToggleType}
					onSearchChange={handleSearchChange}
					onShowAll={() => graphStore.showAllTypes()}
					onHideAll={() => graphStore.hideAllTypes()}
				/>
				<div class="toolbar-spacer"></div>
				<GraphMetrics
					entities={filteredEntities}
					relationships={filteredRelationships}
				/>
			</div>

			{#if graphStore.error}
				<div class="error-banner" role="alert" data-testid="graph-error">
					<span class="error-icon" aria-hidden="true">!</span>
					{graphStore.error}
					<button
						class="error-dismiss"
						onclick={() => graphStore.setError(null)}
						aria-label="Dismiss error"
					>
						×
					</button>
				</div>
			{/if}

			<!-- Graph canvas — fills remaining height -->
			<div class="canvas-wrapper">
				<SigmaCanvas
					entities={filteredEntities}
					relationships={filteredRelationships}
					selectedEntityId={graphStore.selectedEntityId}
					hoveredEntityId={graphStore.hoveredEntityId}
					onEntitySelect={handleEntitySelect}
					onEntityHover={handleEntityHover}
					onEntityExpand={handleEntityExpand}
					onRefresh={handleRefresh}
					loading={graphStore.loading}
				/>
			</div>
		</div>
	{/snippet}

	{#snippet rightPanel()}
		<GraphDetail
			entity={selectedEntity ?? null}
			onEntitySelect={handleRelatedEntitySelect}
		/>
	{/snippet}
</ThreePanelLayout>

<style>
	/* ========================================================================
	 * Page layout
	 * ======================================================================== */

	.graph-page {
		height: 100%;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	/* Toolbar: single horizontal row across the top of center panel */
	.graph-toolbar {
		display: flex;
		align-items: center;
		flex-shrink: 0;
		border-bottom: 1px solid var(--color-border);
		background: var(--color-bg-secondary);
		overflow-x: auto;
	}

	.toolbar-spacer {
		flex: 1;
	}

	/* Canvas fills all remaining height below the toolbar */
	.canvas-wrapper {
		flex: 1;
		min-height: 0;
		position: relative;
	}

	/* Error banner */
	.error-banner {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 12px;
		background: color-mix(in srgb, var(--color-error, #f87171) 15%, var(--color-bg-primary));
		color: var(--color-error, #f87171);
		font-size: 12px;
		border-bottom: 1px solid var(--color-border);
	}

	.error-icon {
		font-weight: 700;
		flex-shrink: 0;
	}

	.error-dismiss {
		margin-left: auto;
		background: transparent;
		border: none;
		color: inherit;
		font-size: 16px;
		cursor: pointer;
		padding: 0 4px;
		opacity: 0.7;
		line-height: 1;
	}

	.error-dismiss:hover {
		opacity: 1;
	}

	/* ========================================================================
	 * Left panel
	 * ======================================================================== */

	.left-panel {
		display: flex;
		flex-direction: column;
		height: 100%;
		padding: var(--space-3, 12px);
		gap: var(--space-2, 8px);
		overflow-y: auto;
	}

	.left-panel-header {
		flex-shrink: 0;
	}

	.left-panel-title {
		margin: 0;
		font-size: var(--font-size-sm, 13px);
		font-weight: var(--font-weight-semibold, 600);
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.5px;
	}

	/* Vertical type filter list */
	.left-type-filters {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.type-filter-row {
		display: flex;
		align-items: center;
		gap: var(--space-2, 8px);
		padding: var(--space-1, 4px) var(--space-2, 8px);
		border-radius: var(--radius-sm, 4px);
		cursor: pointer;
		font-size: var(--font-size-sm, 13px);
		color: var(--color-text-muted);
		transition: background-color var(--transition-fast, 150ms ease);
		user-select: none;
	}

	.type-filter-row:hover {
		background: var(--color-bg-tertiary);
	}

	.type-filter-row.checked {
		color: var(--color-text-primary);
	}

	.type-filter-row input[type='checkbox'] {
		/* Visually hidden but accessible */
		position: absolute;
		width: 1px;
		height: 1px;
		opacity: 0;
		margin: 0;
	}

	.type-dot {
		width: 10px;
		height: 10px;
		border-radius: 50%;
		flex-shrink: 0;

		/* Fallback colors via CSS custom properties per type */
		background: #6b7280;
	}

	.type-label {
		flex: 1;
		text-transform: capitalize;
	}

	.type-count {
		font-size: var(--font-size-xs, 11px);
		color: var(--color-text-muted);
		min-width: 20px;
		text-align: right;
	}

	.left-quick-actions {
		display: flex;
		gap: var(--space-2, 8px);
		padding-top: var(--space-2, 8px);
		border-top: 1px solid var(--color-border);
	}

	.quick-link {
		font-size: var(--font-size-xs, 11px);
		padding: 2px 8px;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm, 3px);
		background: var(--color-bg-primary);
		color: var(--color-text-muted);
		cursor: pointer;
		transition: background-color var(--transition-fast, 150ms ease),
			border-color var(--transition-fast, 150ms ease);
	}

	.quick-link:hover {
		background: var(--color-bg-tertiary);
		border-color: var(--color-accent);
		color: var(--color-text-primary);
	}
</style>
